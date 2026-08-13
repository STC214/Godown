package ffmpegdownload

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"ghost-downloader-go-win32/internal/config"
	"ghost-downloader-go-win32/internal/core"
)

func TestWorkerRunWithFakeFFmpeg(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("fake ffmpeg script uses Windows batch syntax")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("media"))
	}))
	defer server.Close()

	root := t.TempDir()
	ffmpegPath := filepath.Join(root, "ffmpeg.bat")
	task, err := NewMergeTask("merged", filepath.Join(root, "downloads"), []MergeResource{
		{URL: server.URL + "/video.mp4", Filename: "video.mp4", Size: 5},
		{URL: server.URL + "/audio.m4a", Filename: "audio.m4a", Size: 5},
	}, config.Settings{FFmpegInstallDir: ffmpegPath, RetryCount: 3})
	if err != nil {
		t.Fatal(err)
	}
	script := "@echo off\r\n" +
		"echo progress=end\r\n" +
		"set \"out=\"\r\n" +
		"for %%A in (%*) do set \"out=%%~A\"\r\n" +
		"echo merged>\"%out%\"\r\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	var updates []core.ProgressUpdate
	if err := (Worker{}).Run(context.Background(), task, func(update core.ProgressUpdate) {
		updates = append(updates, update)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(task.OutputFile()); err != nil {
		t.Fatalf("output missing: %v", err)
	}
	if len(updates) == 0 || updates[len(updates)-1].Progress != 100 {
		t.Fatalf("updates=%#v", updates)
	}
}

func TestWorkerMissingRuntime(t *testing.T) {
	task, err := NewMergeTask("merged", t.TempDir(), []MergeResource{
		{URL: "https://example.test/video.mp4"},
		{URL: "https://example.test/audio.m4a"},
	}, config.Settings{FFmpegInstallDir: filepath.Join(t.TempDir(), "missing")})
	if err != nil {
		t.Fatal(err)
	}
	err = (Worker{}).Run(context.Background(), task, func(core.ProgressUpdate) {})
	if err == nil || fmt.Sprint(err) == "" {
		t.Fatalf("expected missing runtime error")
	}
}
