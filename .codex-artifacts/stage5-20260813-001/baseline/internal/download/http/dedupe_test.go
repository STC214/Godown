package httpdownload

import (
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

func TestDeduplicateTitleIgnoresOtherDirectories(t *testing.T) {
	existing := []core.TaskSnapshot{
		{Path: `D:\Other`, Title: "video.mp4"},
	}

	got := DeduplicateTitle("video.mp4", `C:\Downloads`, existing)
	if got != "video.mp4" {
		t.Fatalf("expected original filename, got %q", got)
	}
}
