package httpdownload

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ghost-downloader-go-win32/internal/core"
	"ghost-downloader-go-win32/internal/ratelimit"
)

const chunkSize = 64 * 1024

type Worker struct {
	Limiter *ratelimit.Limiter
}

type subworker struct {
	Start    int64
	Progress int64
	End      int64
}

func (w Worker) Run(ctx context.Context, task core.Task, report func(core.ProgressUpdate)) error {
	stage := task.Stage
	outputFile := task.OutputFile()
	if err := os.MkdirAll(filepath.Dir(outputFile), 0o755); err != nil {
		return err
	}

	parts, restored := restore(outputFile)
	if !restored {
		parts = generate(stage.FileSize, stage.BlockNum, stage.SupportsRange)
	}
	if len(parts) == 0 {
		return errors.New("no download parts generated")
	}

	openFlag := os.O_CREATE | os.O_RDWR
	if !stage.SupportsRange {
		openFlag |= os.O_TRUNC
	}
	file, err := os.OpenFile(outputFile, openFlag, 0o666)
	if err != nil {
		return err
	}
	defer file.Close()

	if !restored && stage.SupportsRange && stage.FileSize > 0 {
		if err := file.Truncate(stage.FileSize); err != nil {
			slog.Warn("preallocate file failed", "file", outputFile, "error", err)
		}
	}

	client, err := newClient(stage.ProxyURL, 0)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	errCh := make(chan error, len(parts))
	done := make(chan struct{})
	var received atomic.Int64
	var lastReceived int64
	var partsMu sync.Mutex

	for i := range parts {
		received.Add(parts[i].Progress - parts[i].Start)
	}
	report(progressUpdate(received.Load(), 0, task.Progress, stage.FileSize))

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		recordTicker := time.NewTicker(500 * time.Millisecond)
		defer recordTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				partsMu.Lock()
				writeRecord(outputFile, parts)
				partsMu.Unlock()
				return
			case <-done:
				return
			case <-recordTicker.C:
				if stage.SupportsRange {
					partsMu.Lock()
					writeRecord(outputFile, parts)
					partsMu.Unlock()
				}
			case <-ticker.C:
				current := received.Load()
				speed := current - lastReceived
				lastReceived = current
				report(progressUpdate(current, speed, task.Progress, stage.FileSize))
			}
		}
	}()

	for i := range parts {
		part := &parts[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.downloadPart(ctx, client, file, &stage, part, &received, &partsMu); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(done)
	close(errCh)

	if err := ctx.Err(); err != nil {
		return err
	}
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	report(core.ProgressUpdate{
		Received: received.Load(),
		Speed:    0,
		Progress: 100,
	})
	if stage.SupportsRange {
		_ = os.Remove(outputFile + ".ghd")
	}
	return nil
}

func progressUpdate(received int64, speed int64, fallbackProgress float64, fileSize int64) core.ProgressUpdate {
	progress := fallbackProgress
	if fileSize > 0 {
		progress = float64(received) / float64(fileSize) * 100
	}
	return core.ProgressUpdate{
		Received: received,
		Speed:    speed,
		Progress: progress,
	}
}

func (w Worker) downloadPart(ctx context.Context, client *http.Client, file *os.File, stage *core.Stage, part *subworker, received *atomic.Int64, partsMu *sync.Mutex) error {
	failures := 0
	for {
		partsMu.Lock()
		progress, end := part.Progress, part.End
		partsMu.Unlock()
		if !stage.SupportsRange {
			if err := resetNonRangePart(file, part, received, partsMu); err != nil {
				return err
			}
			progress = 0
			end = part.End
		}
		if end != UnknownSize && progress > end {
			return nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, stage.URL, nil)
		if err != nil {
			return err
		}
		for key, value := range stage.Headers {
			req.Header.Set(key, value)
		}
		req.Header.Set("Accept-Encoding", "identity")
		if stage.SupportsRange {
			if end == UnknownSize {
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-", progress))
			} else {
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", progress, end))
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if failures = updateRetryCount(failures, stage.MaxRetries, false); failures < 0 {
				return err
			}
			if err := sleepBeforeRetry(ctx, failures); err != nil {
				return err
			}
			continue
		}
		err = w.copyPart(ctx, resp, file, stage, part, received, partsMu)
		_ = resp.Body.Close()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		partsMu.Lock()
		nextProgress := part.Progress
		partsMu.Unlock()
		madeProgress := nextProgress > progress
		if failures = updateRetryCount(failures, stage.MaxRetries, madeProgress); failures < 0 {
			return err
		}
		if err := sleepBeforeRetry(ctx, failures); err != nil {
			return err
		}
	}
}

func updateRetryCount(failures int, maxRetries int, madeProgress bool) int {
	if maxRetries < 0 {
		maxRetries = 0
	}
	if madeProgress {
		failures = 0
	}
	failures++
	if failures > maxRetries {
		return -1
	}
	return failures
}

func resetNonRangePart(file *os.File, part *subworker, received *atomic.Int64, partsMu *sync.Mutex) error {
	partsMu.Lock()
	previous := part.Progress
	part.Progress = 0
	partsMu.Unlock()
	if previous > 0 {
		received.Add(-previous)
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	return nil
}

func sleepBeforeRetry(ctx context.Context, failures int) error {
	if failures <= 0 {
		failures = 1
	}
	delay := time.Duration(failures) * time.Second
	if delay > 10*time.Second {
		delay = 10 * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (w Worker) copyPart(ctx context.Context, resp *http.Response, file *os.File, stage *core.Stage, part *subworker, received *atomic.Int64, partsMu *sync.Mutex) error {
	if stage.SupportsRange && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("range request rejected: %s", resp.Status)
	}
	if stage.SupportsRange {
		partsMu.Lock()
		expectedStart := part.Progress
		partsMu.Unlock()
		start, end, err := parseContentRange(resp.Header.Get("Content-Range"))
		if err != nil {
			return err
		}
		if start != expectedStart {
			return fmt.Errorf("range response starts at %d, expected %d", start, expectedStart)
		}
		if end >= 0 && end < start {
			return fmt.Errorf("range response ends before it starts: %d-%d", start, end)
		}
	}
	if !stage.SupportsRange && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %s", resp.Status)
	}
	buf := make([]byte, chunkSize)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			partsMu.Lock()
			progress, end := part.Progress, part.End
			partsMu.Unlock()
			if stage.SupportsRange && end > 0 {
				remaining := end - progress + 1
				if int64(len(chunk)) > remaining {
					chunk = chunk[:remaining]
				}
			}
			if w.Limiter != nil {
				if err := w.Limiter.Wait(ctx, len(chunk)); err != nil {
					return err
				}
			}
			if _, writeErr := file.WriteAt(chunk, progress); writeErr != nil {
				return writeErr
			}
			partsMu.Lock()
			part.Progress += int64(len(chunk))
			progress = part.Progress
			end = part.End
			partsMu.Unlock()
			received.Add(int64(len(chunk)))
			if stage.SupportsRange && end > 0 && progress > end {
				return nil
			}
		}
		if errors.Is(err, io.EOF) {
			return validatePartComplete(stage, part, partsMu)
		}
		if err != nil {
			return err
		}
	}
}

func validatePartComplete(stage *core.Stage, part *subworker, partsMu *sync.Mutex) error {
	partsMu.Lock()
	progress, end := part.Progress, part.End
	partsMu.Unlock()
	if stage.SupportsRange {
		if end == UnknownSize {
			return nil
		}
		if progress <= end {
			return fmt.Errorf("incomplete range: received through %d, expected through %d", progress-1, end)
		}
		return nil
	}
	if stage.FileSize > 0 && progress < stage.FileSize {
		return fmt.Errorf("incomplete response: received %d bytes, expected %d", progress, stage.FileSize)
	}
	return nil
}

func parseContentRange(value string) (int64, int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, 0, errors.New("missing Content-Range")
	}
	unit, rest, ok := strings.Cut(value, " ")
	if !ok || strings.ToLower(strings.TrimSpace(unit)) != "bytes" {
		return 0, 0, fmt.Errorf("unsupported Content-Range: %q", value)
	}
	rangePart, _, ok := strings.Cut(strings.TrimSpace(rest), "/")
	if !ok {
		return 0, 0, fmt.Errorf("invalid Content-Range: %q", value)
	}
	startText, endText, ok := strings.Cut(rangePart, "-")
	if !ok {
		return 0, 0, fmt.Errorf("invalid Content-Range range: %q", value)
	}
	start, err := strconv.ParseInt(strings.TrimSpace(startText), 10, 64)
	if err != nil || start < 0 {
		return 0, 0, fmt.Errorf("invalid Content-Range start: %q", value)
	}
	end, err := strconv.ParseInt(strings.TrimSpace(endText), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid Content-Range end: %q", value)
	}
	return start, end, nil
}

func generate(fileSize int64, blockNum int, supportsRange bool) []subworker {
	if blockNum <= 0 {
		blockNum = 8
	}
	if !supportsRange {
		return []subworker{{Start: 0, Progress: 0, End: NotSupportedSize}}
	}
	if fileSize <= 0 {
		return []subworker{{Start: 0, Progress: 0, End: UnknownSize}}
	}
	if int64(blockNum) > fileSize {
		blockNum = int(fileSize)
	}
	step := fileSize / int64(blockNum)
	parts := make([]subworker, 0, blockNum)
	var start int64
	for i := 0; i < blockNum-1; i++ {
		end := start + step - 1
		parts = append(parts, subworker{Start: start, Progress: start, End: end})
		start = end + 1
	}
	parts = append(parts, subworker{Start: start, Progress: start, End: fileSize - 1})
	return parts
}

func restore(outputFile string) ([]subworker, bool) {
	data, err := os.ReadFile(outputFile + ".ghd")
	if err != nil || len(data)%24 != 0 {
		return nil, false
	}
	parts := make([]subworker, 0, len(data)/24)
	for offset := 0; offset < len(data); offset += 24 {
		parts = append(parts, subworker{
			Start:    int64(binary.LittleEndian.Uint64(data[offset:])),
			Progress: int64(binary.LittleEndian.Uint64(data[offset+8:])),
			End:      int64(binary.LittleEndian.Uint64(data[offset+16:])),
		})
	}
	return parts, len(parts) > 0
}

func writeRecord(outputFile string, parts []subworker) {
	data := make([]byte, len(parts)*24)
	for i, part := range parts {
		offset := i * 24
		binary.LittleEndian.PutUint64(data[offset:], uint64(part.Start))
		binary.LittleEndian.PutUint64(data[offset+8:], uint64(part.Progress))
		binary.LittleEndian.PutUint64(data[offset+16:], uint64(part.End))
	}
	_ = os.WriteFile(outputFile+".ghd", data, 0o644)
}
