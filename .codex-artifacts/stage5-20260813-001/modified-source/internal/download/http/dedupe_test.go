package httpdownload

import (
	"os"
	"path/filepath"
	"testing"

	"ghost-downloader-go-win32/internal/core"
)

func TestDeduplicateTitle(t *testing.T) {
	existing := []core.TaskSnapshot{
		{Path: `C:\Downloads`, Title: "video.mp4"},
		{Path: `C:\Downloads`, Title: "video (1).mp4"},
	}

	got := DeduplicateTitle("video.mp4", `C:\Downloads`, existing)
	if got != "video (2).mp4" {
		t.Fatalf("expected video (2).mp4, got %q", got)
	}
}

func TestDeduplicateTitleAvoidsExistingDiskEntries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "video.mp4"), []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "video (1).mp4.ghd"), []byte("checkpoint"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := DeduplicateTitle("video.mp4", dir, nil); got != "video (2).mp4" {
		t.Fatalf("disk entries were not deduplicated: %q", got)
	}
}

func TestDeduplicateTitleIgnoresOtherDirectories(t *testing.T) {
	existing := []core.TaskSnapshot{
		{Path: `D:\Other`, Title: "video.mp4"},
	}

	got := DeduplicateTitle("video.mp4", `C:\Downloads`, existing)
	if got != "video.mp4" {
		t.Fatalf("expected original filename, got %q", got)
	}
}
