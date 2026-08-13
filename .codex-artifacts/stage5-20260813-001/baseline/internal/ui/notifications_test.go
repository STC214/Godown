package ui

import (
	"testing"

	"ghost-downloader-go-win32/internal/core"
)

func TestTaskNotificationTrackerOnlyReportsTerminalTransitions(t *testing.T) {
	tracker := newTaskNotificationTracker([]core.TaskSnapshot{
		{ID: "old-complete", Title: "old.bin", Status: core.StatusCompleted},
		{ID: "running", Title: "run.bin", Status: core.StatusRunning},
		{ID: "waiting", Title: "wait.bin", Status: core.StatusWaiting},
	})

	if got := tracker.Update([]core.TaskSnapshot{
		{ID: "old-complete", Title: "old.bin", Status: core.StatusCompleted},
		{ID: "running", Title: "run.bin", Status: core.StatusRunning},
		{ID: "waiting", Title: "wait.bin", Status: core.StatusWaiting},
		{ID: "new-complete", Title: "new.bin", Status: core.StatusCompleted},
	}); len(got) != 0 {
		t.Fatalf("startup/new terminal tasks should not notify, got %#v", got)
	}

	got := tracker.Update([]core.TaskSnapshot{
		{ID: "old-complete", Title: "old.bin", Status: core.StatusCompleted},
		{ID: "running", Title: "run.bin", Status: core.StatusCompleted},
		{ID: "waiting", Title: "wait.bin", Status: core.StatusFailed, Error: "network error"},
		{ID: "new-complete", Title: "new.bin", Status: core.StatusCompleted},
	})
	if len(got) != 2 {
		t.Fatalf("expected two notifications, got %#v", got)
	}
	if got[0].Kind != taskNotificationInfo || got[0].Title != "Download completed" || got[0].Message != "run.bin" {
		t.Fatalf("unexpected completed notification: %#v", got[0])
	}
	if got[1].Kind != taskNotificationError || got[1].Title != "Download failed" || got[1].Message != "wait.bin\r\nnetwork error" {
		t.Fatalf("unexpected failed notification: %#v", got[1])
	}

	if got := tracker.Update([]core.TaskSnapshot{
		{ID: "running", Title: "run.bin", Status: core.StatusCompleted},
		{ID: "waiting", Title: "wait.bin", Status: core.StatusFailed, Error: "network error"},
	}); len(got) != 0 {
		t.Fatalf("unchanged terminal states should not repeat, got %#v", got)
	}
}
