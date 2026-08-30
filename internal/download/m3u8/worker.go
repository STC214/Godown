package m3u8download

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"ghost-downloader-go-win32/internal/core"
	appwin32 "ghost-downloader-go-win32/internal/win32"
)

type Worker struct{}

func (Worker) Run(ctx context.Context, task core.Task, report func(core.ProgressUpdate)) error {
	execPath := FindRuntimeExecutable(installDirFromTask(task))
	if execPath == "" {
		return errors.New("N_m3u8DL-RE runtime not found; configure the M3U8 install directory in Settings")
	}
	options := OptionsFromTask(task)
	options.FFmpegPath = FindFFmpegExecutable(ffmpegInstallDirFromTask(task))
	if err := prepareOutput(task, options); err != nil {
		return err
	}

	cmd := exec.Command(execPath, options.BuildArgs()...)
	appwin32.HideCommandWindow(cmd)
	cmd.Dir = filepath.Dir(execPath)
	if boolState(task.Stage.State, "keepImageSegments", false) {
		cmd.Env = append(os.Environ(), "RE_KEEP_IMAGE_SEGMENTS=1")
	}

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

	var mu sync.Mutex
	lastMessage := ""
	updateLast := func(message string) {
		message = strings.TrimSpace(message)
		if message == "" {
			return
		}
		if len(message) > 1000 {
			message = message[:1000]
		}
		mu.Lock()
		lastMessage = message
		mu.Unlock()
	}

	var scanWG sync.WaitGroup
	scanWG.Add(2)
	go func() {
		defer scanWG.Done()
		scanOutput(stdout, report, updateLast)
	}()
	go func() {
		defer scanWG.Done()
		scanOutput(stderr, report, updateLast)
	}()

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		killProcessTree(cmd)
		<-waitCh
		scanWG.Wait()
		if options.IsLive {
			_, _ = finalizeOutput(task, options)
		}
		return ctx.Err()
	case err := <-waitCh:
		scanWG.Wait()
		if err != nil {
			mu.Lock()
			message := lastMessage
			mu.Unlock()
			if message == "" {
				message = err.Error()
			}
			return fmt.Errorf("N_m3u8DL-RE failed: %s", message)
		}
		fileSize, err := finalizeOutput(task, options)
		if err != nil {
			return err
		}
		report(core.ProgressUpdate{Received: fileSize, FileSize: fileSize, Speed: 0, Progress: 100})
		return nil
	}
}

func prepareOutput(task core.Task, options Options) error {
	if err := os.MkdirAll(options.SaveDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(options.SaveDir, ".gd3_m3u8", task.ID), 0o755); err != nil {
		return err
	}
	placeholder, err := os.OpenFile(task.OutputFile()+".ghd", os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		return err
	}
	return placeholder.Close()
}

func scanOutput(reader io.Reader, report func(core.ProgressUpdate), updateLast func(string)) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		updateLast(line)
		progress := ParseProgressLine(line)
		if progress.Matched {
			report(core.ProgressUpdate{
				Received: progress.Received,
				Speed:    progress.Speed,
				Progress: progress.Progress,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		updateLast(err.Error())
	}
}

func finalizeOutput(task core.Task, options Options) (int64, error) {
	_ = os.Remove(task.OutputFile() + ".ghd")
	if stat, err := os.Stat(task.OutputFile()); err == nil && !stat.IsDir() && stat.Size() > 0 {
		return stat.Size(), nil
	}

	found, err := findOutputCandidate(options.SaveDir, options.Title, options.OutputFormat, options.IsLive)
	if err != nil {
		return 0, err
	}
	if found == "" {
		return 0, fmt.Errorf("N_m3u8DL-RE completed but output file was not found")
	}
	if filepath.Clean(found) == filepath.Clean(task.OutputFile()) {
		stat, err := os.Stat(found)
		if err != nil {
			return 0, err
		}
		return stat.Size(), nil
	}
	if err := os.Remove(task.OutputFile()); err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	if err := os.Rename(found, task.OutputFile()); err != nil {
		return 0, err
	}
	stat, err := os.Stat(task.OutputFile())
	if err != nil {
		return 0, err
	}
	return stat.Size(), nil
}

func findOutputCandidate(saveDir, title, outputFormat string, isLive bool) (string, error) {
	entries, err := os.ReadDir(saveDir)
	if err != nil {
		return "", err
	}
	prefix := strings.ToLower(saveName(title))
	expectedExt := "." + outputFormat
	if isLive {
		expectedExt = ".ts"
	}
	var fallback string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lowered := strings.ToLower(name)
		if !strings.HasPrefix(lowered, prefix) {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ignoredOutputExt(ext) {
			continue
		}
		path := filepath.Join(saveDir, name)
		if ext == expectedExt {
			return path, nil
		}
		if fallback == "" {
			fallback = path
		}
	}
	return fallback, nil
}

func ignoredOutputExt(ext string) bool {
	switch ext {
	case ".json", ".txt", ".log", ".tmp", ".ghd":
		return true
	default:
		return false
	}
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		kill := exec.Command("taskkill.exe", "/T", "/F", "/PID", fmt.Sprint(cmd.Process.Pid))
		appwin32.HideCommandWindow(kill)
		_ = kill.Run()
		return
	}
	_ = cmd.Process.Kill()
}
