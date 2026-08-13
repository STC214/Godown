package m3u8download

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ghost-downloader-go-win32/internal/core"
)

func TestWorkerMissingRuntime(t *testing.T) {
	task := testM3U8Task()
	task.Stage.State["installDir"] = filepath.Join(t.TempDir(), "missing")
	err := Worker{}.Run(context.Background(), task, func(core.ProgressUpdate) {})
	if err == nil || !strings.Contains(err.Error(), "runtime not found") {
		t.Fatalf("expected missing runtime error, got %v", err)
	}
}

func TestScanOutputReportsProgress(t *testing.T) {
	var updates []core.ProgressUpdate
	scanOutput(
		strings.NewReader("12/24 50.00% 10.00MB/20.00MB 2.50MBps eta\n"),
		func(update core.ProgressUpdate) { updates = append(updates, update) },
		func(string) {},
	)
	if len(updates) != 1 {
		t.Fatalf("updates=%d", len(updates))
	}
	if updates[0].Progress != 50 || updates[0].Received != 10*1024*1024 || updates[0].Speed != int64(2.5*1024*1024) {
		t.Fatalf("unexpected update: %#v", updates[0])
	}
}

func TestFinalizeOutputRenamesCandidate(t *testing.T) {
	task := testM3U8Task()
	task.Path = t.TempDir()
	task.Title = "movie.mp4"
	options := OptionsFromTask(task)
	options.SaveDir = task.Path
	options.Title = task.Title
	options.OutputFormat = "mp4"
	candidate := filepath.Join(task.Path, "movie.copy.mp4")
	if err := os.WriteFile(candidate, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(task.OutputFile()+".ghd", []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	fileSize, err := finalizeOutput(task, options)
	if err != nil {
		t.Fatal(err)
	}
	if fileSize != 2 {
		t.Fatalf("file size=%d", fileSize)
	}
	if _, err := os.Stat(task.OutputFile()); err != nil {
		t.Fatalf("expected renamed output: %v", err)
	}
	if _, err := os.Stat(task.OutputFile() + ".ghd"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("placeholder should be removed, got %v", err)
	}
}

func TestWorkerRunWithFakeRuntime(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("fake runtime script uses Windows batch syntax")
	}
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeRuntime := filepath.Join(runtimeDir, "N_m3u8DL-RE.bat")
	task := testM3U8Task()
	task.Path = filepath.Join(dir, "downloads")
	task.Title = "movie.mp4"
	if err := os.MkdirAll(task.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	outputFile := filepath.Join(task.Path, "movie.mp4")
	task.Stage.State["installDir"] = fakeRuntime
	task.Stage.State["isLive"] = "false"
	task.Stage.State["outputFormat"] = "mp4"
	script := "@echo off\r\n" +
		"echo 12/24 50.00%% 10.00MB/20.00MB 2.50MBps eta\r\n" +
		"echo ok>\"" + outputFile + "\"\r\n"
	if err := os.WriteFile(fakeRuntime, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	var updates []core.ProgressUpdate
	err := Worker{}.Run(context.Background(), task, func(update core.ProgressUpdate) {
		updates = append(updates, update)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(task.OutputFile()); err != nil {
		t.Fatalf("expected output file: %v", err)
	}
	if len(updates) == 0 || updates[len(updates)-1].Progress != 100 {
		t.Fatalf("expected completion update, got %#v", updates)
	}
}

func TestKillProcessTreeNilSafe(t *testing.T) {
	killProcessTree(nil)
	killProcessTree(&exec.Cmd{})
}

func testM3U8Task() core.Task {
	stage := core.NewStage("m3u8", "https://example.test/master.m3u8", 1, 8, 3, false, map[string]string{"Referer": "https://example.test/"}, "")
	stage.State = map[string]string{
		"installDir":             `C:\Tools\N_m3u8DL-RE`,
		"ffmpegInstallDir":       `C:\Tools\FFmpeg`,
		"isLive":                 "true",
		"outputFormat":           "mkv",
		"requestTimeoutSec":      "42",
		"concurrentDownload":     "true",
		"checkSegmentsCount":     "true",
		"deleteAfterDone":        "true",
		"selectAllAudioSubtitle": "true",
		"mp4RealTimeDecryption":  "true",
		"subtitleFormat":         "VTT",
	}
	return core.NewTask("m3u8", "movie.mkv", "https://example.test/master.m3u8", `D:\Downloads`, 1, stage)
}
