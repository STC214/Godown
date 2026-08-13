package httpdownload

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ghost-downloader-go-win32/internal/core"
)

func TestUpdateRetryCountStopsAfterLimit(t *testing.T) {
	failures := 0
	failures = updateRetryCount(failures, 2, false)
	if failures != 1 {
		t.Fatalf("expected first retry count 1, got %d", failures)
	}
	failures = updateRetryCount(failures, 2, false)
	if failures != 2 {
		t.Fatalf("expected second retry count 2, got %d", failures)
	}
	failures = updateRetryCount(failures, 2, false)
	if failures != -1 {
		t.Fatalf("expected retry exhaustion, got %d", failures)
	}
}

func TestProgressUpdateUsesFileSizeWhenKnown(t *testing.T) {
	update := progressUpdate(25, 5, 70, 100)
	if update.Received != 25 || update.Speed != 5 || update.Progress != 25 {
		t.Fatalf("unexpected progress update: %#v", update)
	}
}

func TestProgressUpdateKeepsFallbackWhenSizeUnknown(t *testing.T) {
	update := progressUpdate(25, 5, 70, UnknownSize)
	if update.Progress != 70 {
		t.Fatalf("expected fallback progress, got %#v", update)
	}
}

func TestUpdateRetryCountResetsAfterProgress(t *testing.T) {
	failures := updateRetryCount(2, 3, true)
	if failures != 1 {
		t.Fatalf("expected progress to reset failure streak, got %d", failures)
	}
}

func TestSleepBeforeRetryReturnsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := sleepBeforeRetry(ctx, 10); err == nil {
		t.Fatal("expected cancellation error")
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("retry sleep did not return promptly: %s", elapsed)
	}
}

func TestResetNonRangePartRollsBackProgressAndTruncatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "download.bin")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("partial payload"); err != nil {
		t.Fatal(err)
	}

	part := &subworker{Start: 0, Progress: 7, End: NotSupportedSize}
	var received atomic.Int64
	received.Store(7)
	var mu sync.Mutex

	if err := resetNonRangePart(file, part, &received, &mu); err != nil {
		t.Fatal(err)
	}
	if part.Progress != 0 {
		t.Fatalf("expected part progress reset, got %d", part.Progress)
	}
	if got := received.Load(); got != 0 {
		t.Fatalf("expected received rollback to 0, got %d", got)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("expected file truncated, got size %d", info.Size())
	}
}

func TestParseContentRange(t *testing.T) {
	start, end, err := parseContentRange("bytes 10-19/100")
	if err != nil {
		t.Fatal(err)
	}
	if start != 10 || end != 19 {
		t.Fatalf("unexpected range: %d-%d", start, end)
	}
}

func TestParseContentRangeRejectsInvalidValues(t *testing.T) {
	cases := []string{
		"",
		"items 0-1/10",
		"bytes */10",
		"bytes 9-",
		"bytes x-y/10",
	}
	for _, value := range cases {
		if _, _, err := parseContentRange(value); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}

func TestCopyPartRejectsMismatchedContentRangeStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "download.bin")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	resp := &http.Response{
		StatusCode: http.StatusPartialContent,
		Status:     "206 Partial Content",
		Header:     http.Header{"Content-Range": []string{"bytes 0-9/100"}},
		Body:       io.NopCloser(strings.NewReader("0123456789")),
	}
	stage := core.Stage{SupportsRange: true}
	part := &subworker{Start: 10, Progress: 10, End: 19}
	var received atomic.Int64
	var mu sync.Mutex

	err = Worker{}.copyPart(context.Background(), resp, file, &stage, part, &received, &mu)
	if err == nil {
		t.Fatal("expected mismatched Content-Range error")
	}
	if got := received.Load(); got != 0 {
		t.Fatalf("expected no received bytes after rejected range, got %d", got)
	}
}

func TestCopyPartRejectsShortRangeResponse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "download.bin")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	resp := &http.Response{
		StatusCode: http.StatusPartialContent,
		Status:     "206 Partial Content",
		Header:     http.Header{"Content-Range": []string{"bytes 10-19/100"}},
		Body:       io.NopCloser(strings.NewReader("short")),
	}
	stage := core.Stage{SupportsRange: true}
	part := &subworker{Start: 10, Progress: 10, End: 19}
	var received atomic.Int64
	var mu sync.Mutex

	err = Worker{}.copyPart(context.Background(), resp, file, &stage, part, &received, &mu)
	if err == nil {
		t.Fatal("expected incomplete range error")
	}
	if part.Progress != 15 {
		t.Fatalf("expected partial progress retained for retry, got %d", part.Progress)
	}
}

func TestCopyPartAcceptsCompleteRangeResponse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "download.bin")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	resp := &http.Response{
		StatusCode: http.StatusPartialContent,
		Status:     "206 Partial Content",
		Header:     http.Header{"Content-Range": []string{"bytes 10-14/100"}},
		Body:       io.NopCloser(strings.NewReader("hello")),
	}
	stage := core.Stage{SupportsRange: true}
	part := &subworker{Start: 10, Progress: 10, End: 14}
	var received atomic.Int64
	var mu sync.Mutex

	if err := (Worker{}).copyPart(context.Background(), resp, file, &stage, part, &received, &mu); err != nil {
		t.Fatal(err)
	}
	if part.Progress != 15 || received.Load() != 5 {
		t.Fatalf("expected completed part progress=15 received=5, got progress=%d received=%d", part.Progress, received.Load())
	}
}

func TestCopyPartRejectsShortNonRangeResponseWithKnownSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "download.bin")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader("short")),
	}
	stage := core.Stage{SupportsRange: false, FileSize: 10}
	part := &subworker{Start: 0, Progress: 0, End: NotSupportedSize}
	var received atomic.Int64
	var mu sync.Mutex

	if err := (Worker{}).copyPart(context.Background(), resp, file, &stage, part, &received, &mu); err == nil {
		t.Fatal("expected incomplete non-range response error")
	}
}
