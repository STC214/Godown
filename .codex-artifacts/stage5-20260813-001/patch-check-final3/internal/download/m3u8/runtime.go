package m3u8download

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type RuntimeInfo struct {
	ExecutablePath string
	InstallDir     string
	Version        string
	Available      bool
}

func ProbeRuntime(ctx context.Context, installDir string) RuntimeInfo {
	executable := FindRuntimeExecutable(installDir)
	return probeExecutable(ctx, installDir, executable, "--version")
}

func ProbeFFmpegRuntime(ctx context.Context, installDir string) RuntimeInfo {
	executable := FindFFmpegExecutable(installDir)
	return probeExecutable(ctx, installDir, executable, "-version")
}

func FindRuntimeExecutable(installDir string) string {
	return findExecutable(installDir, []string{"N_m3u8DL-RE.exe", "N_m3u8DL-RE"})
}

func FindFFmpegExecutable(installDir string) string {
	return findExecutable(installDir, []string{
		filepath.Join("bin", "ffmpeg.exe"),
		filepath.Join("bin", "ffmpeg"),
		"ffmpeg.exe",
		"ffmpeg",
	})
}

func FindFFprobeExecutable(installDir string) string {
	return findExecutable(installDir, []string{
		filepath.Join("bin", "ffprobe.exe"),
		filepath.Join("bin", "ffprobe"),
		"ffprobe.exe",
		"ffprobe",
	})
}

func findExecutable(installDir string, names []string) string {
	installDir = strings.TrimSpace(installDir)
	if installDir == "" {
		return ""
	}
	candidates := []string{installDir}
	if stat, err := os.Stat(installDir); err == nil && stat.IsDir() {
		candidates = candidates[:0]
		for _, name := range names {
			candidates = append(candidates, filepath.Join(installDir, name))
		}
	}
	for _, candidate := range candidates {
		if isExecutableFile(candidate) {
			return candidate
		}
	}
	return ""
}

func probeExecutable(ctx context.Context, installDir, executable string, versionArg string) RuntimeInfo {
	if executable == "" {
		return RuntimeInfo{InstallDir: installDir}
	}
	info := RuntimeInfo{
		ExecutablePath: executable,
		InstallDir:     filepath.Dir(executable),
		Available:      true,
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, executable, versionArg).CombinedOutput()
	if err != nil && !errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
		return info
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if text := strings.TrimSpace(line); text != "" {
			info.Version = text
			break
		}
	}
	return info
}

func isExecutableFile(path string) bool {
	stat, err := os.Stat(path)
	if err != nil || stat.IsDir() {
		return false
	}
	return true
}
