package ffmpegdownload

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"ghost-downloader-go-win32/internal/core"
	m3u8download "ghost-downloader-go-win32/internal/download/m3u8"
)

type Worker struct{}

func (Worker) Run(ctx context.Context, task core.Task, report func(core.ProgressUpdate)) error {
	resources, err := ResourcesFromState(task)
	if err != nil {
		return err
	}
	ffmpeg := m3u8download.FindFFmpegExecutable(task.Stage.State["ffmpegInstallDir"])
	if ffmpeg == "" {
		return fmt.Errorf("ffmpeg runtime not found; configure the FFmpeg install directory in Settings")
	}
	if err := os.MkdirAll(task.Path, 0o755); err != nil {
		return err
	}
	tempDir := filepath.Join(task.Path, ".gd3_ffmpeg", task.ID)
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return err
	}

	inputs := make([]string, 0, len(resources))
	var received int64
	total := task.FileSize
	for _, resource := range resources {
		target := filepath.Join(tempDir, resource.Role+"."+resourceExtension(resource))
		if err := downloadResource(ctx, task.Stage.ProxyURL, resource, target, func(delta int64) {
			received += delta
			progress := 0.0
			if total > 0 {
				progress = minFloat(80, float64(received)/float64(total)*80)
			}
			report(core.ProgressUpdate{Received: received, FileSize: total, Progress: progress})
		}); err != nil {
			return err
		}
		inputs = append(inputs, target)
	}

	if err := runFFmpeg(ctx, ffmpeg, inputs[0], inputs[1], task.OutputFile(), report, received, total); err != nil {
		return err
	}
	stat, err := os.Stat(task.OutputFile())
	if err != nil {
		return err
	}
	_ = os.RemoveAll(tempDir)
	report(core.ProgressUpdate{Received: stat.Size(), FileSize: stat.Size(), Progress: 100})
	return nil
}

func downloadResource(ctx context.Context, proxyURL string, resource MergeResource, target string, report func(delta int64)) error {
	client, err := newClient(proxyURL)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resource.URL, nil)
	if err != nil {
		return err
	}
	for name, value := range resource.Headers {
		if strings.TrimSpace(value) != "" {
			req.Header.Set(name, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("resource request failed: %s", resp.Status)
	}
	file, err := os.Create(target)
	if err != nil {
		return err
	}
	defer file.Close()
	buf := make([]byte, 128*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := file.Write(buf[:n]); err != nil {
				return err
			}
			report(int64(n))
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func runFFmpeg(ctx context.Context, ffmpeg, video, audio, output string, report func(core.ProgressUpdate), received, total int64) error {
	args := []string{
		"-y", "-v", "error", "-nostats", "-progress", "pipe:1",
		"-i", video,
		"-i", audio,
		"-c", "copy",
		output,
	}
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	errCh := make(chan string, 1)
	go drainFFmpegProgress(stdout, report, received, total)
	go func() {
		body, _ := io.ReadAll(stderr)
		errCh <- strings.TrimSpace(string(body))
	}()
	waitErr := cmd.Wait()
	stderrText := <-errCh
	if ctx.Err() != nil {
		killProcessTree(cmd)
		return ctx.Err()
	}
	if waitErr != nil {
		if stderrText != "" {
			return fmt.Errorf("ffmpeg failed: %s", stderrText)
		}
		return waitErr
	}
	return nil
}

func drainFFmpegProgress(reader io.Reader, report func(core.ProgressUpdate), received, total int64) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "progress=end" {
			report(core.ProgressUpdate{Received: received, FileSize: total, Progress: 99.5})
			continue
		}
		if strings.HasPrefix(line, "out_time_ms=") || strings.HasPrefix(line, "out_time_us=") {
			value := strings.TrimPrefix(strings.TrimPrefix(line, "out_time_ms="), "out_time_us=")
			if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > 0 {
				_ = parsed
				report(core.ProgressUpdate{Received: received, FileSize: total, Progress: 90})
			}
		}
	}
}

func newClient(proxyURL string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(proxyURL) != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	return &http.Client{Transport: transport}, nil
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill.exe", "/T", "/F", "/PID", fmt.Sprint(cmd.Process.Pid)).Run()
		return
	}
	_ = cmd.Process.Kill()
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
