package m3u8download

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindRuntimeExecutable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "N_m3u8DL-RE")
	if err := os.WriteFile(exe, []byte("placeholder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := FindRuntimeExecutable(dir); got != exe {
		t.Fatalf("FindRuntimeExecutable=%q want %q", got, exe)
	}
	if got := FindRuntimeExecutable(exe); got != exe {
		t.Fatalf("direct executable=%q want %q", got, exe)
	}
}

func TestFindFFmpegExecutable(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(bin, "ffmpeg.exe")
	if err := os.WriteFile(exe, []byte("placeholder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := FindFFmpegExecutable(dir); got != exe {
		t.Fatalf("FindFFmpegExecutable=%q want %q", got, exe)
	}
	if got := FindFFmpegExecutable(exe); got != exe {
		t.Fatalf("direct ffmpeg executable=%q want %q", got, exe)
	}
}

func TestFindRuntimeExecutableMissing(t *testing.T) {
	if got := FindRuntimeExecutable(filepath.Join(t.TempDir(), "missing")); got != "" {
		t.Fatalf("expected missing runtime, got %q", got)
	}
}
