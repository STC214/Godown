package ui

import (
	"testing"
	"time"

	"ghost-downloader-go-win32/internal/core"
)

func TestTaskTableModelFilters(t *testing.T) {
	model := newTaskTableModel()
	model.SetTasks([]core.TaskSnapshot{
		{ID: "running", Title: "running.bin", Status: core.StatusRunning, CreatedAt: time.Unix(3, 0)},
		{ID: "completed", Title: "completed.bin", Status: core.StatusCompleted, CreatedAt: time.Unix(2, 0)},
		{ID: "failed", Title: "failed.bin", Status: core.StatusFailed, CreatedAt: time.Unix(1, 0)},
	})

	model.SetFilter(taskFilterAll)
	if model.RowCount() != 3 {
		t.Fatalf("all filter row count = %d", model.RowCount())
	}
	model.SetFilter(taskFilterActive)
	if model.RowCount() != 2 {
		t.Fatalf("active filter row count = %d", model.RowCount())
	}
	model.SetFilter(taskFilterCompleted)
	if model.RowCount() != 1 {
		t.Fatalf("completed filter row count = %d", model.RowCount())
	}
	task, ok := model.TaskAt(0)
	if !ok || task.ID != "completed" {
		t.Fatalf("expected completed task, got %#v ok=%v", task, ok)
	}
	model.SetFilter(taskFilterFailed)
	if model.RowCount() != 1 {
		t.Fatalf("failed filter row count = %d", model.RowCount())
	}
	task, ok = model.TaskAt(0)
	if !ok || task.ID != "failed" {
		t.Fatalf("expected failed task, got %#v ok=%v", task, ok)
	}
}

func TestTaskTableModelSearch(t *testing.T) {
	model := newTaskTableModel()
	model.SetTasks([]core.TaskSnapshot{
		{ID: "one", Title: "movie.mp4", URL: "https://example.com/movie.mp4", Path: `D:\Media`},
		{ID: "two", Title: "archive.zip", URL: "https://example.com/archive.zip", Path: `D:\Downloads`},
	})

	model.SetSearchText("media")
	if model.RowCount() != 1 {
		t.Fatalf("search row count = %d", model.RowCount())
	}
	task, ok := model.TaskAt(0)
	if !ok || task.ID != "one" {
		t.Fatalf("expected media task, got %#v ok=%v", task, ok)
	}
}
