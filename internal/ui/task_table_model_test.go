package ui

import (
	"fmt"
	"testing"
	"time"

	"ghost-downloader-go-win32/internal/core"
	"github.com/lxn/walk"
)

func TestTaskTableModelHandlesTenThousandRows(t *testing.T) {
	model := newTaskTableModel()
	tasks := make([]core.TaskSnapshot, 10000)
	for i := range tasks {
		tasks[i] = core.TaskSnapshot{ID: fmt.Sprintf("task-%05d", i), Title: fmt.Sprintf("Download %05d", i), Status: core.StatusCompleted, CreatedAt: time.Unix(int64(i), 0)}
	}
	model.SetTasks(tasks)
	if model.RowCount() != len(tasks) {
		t.Fatalf("row count=%d want=%d", model.RowCount(), len(tasks))
	}
	model.SetSearchText("09999")
	if model.RowCount() != 1 {
		t.Fatalf("filtered row count=%d want=1", model.RowCount())
	}
}

func TestTaskTableModelRebuildNotifiesVisibleRows(t *testing.T) {
	model := newTaskTableModel()
	history := core.TaskSnapshot{ID: "history", Title: "history.bin", Status: core.StatusCompleted, CreatedAt: time.Unix(1, 0)}
	latest := core.TaskSnapshot{ID: "latest", Title: "latest.bin", Status: core.StatusWaiting, Path: `D:\New`, CreatedAt: time.Unix(2, 0)}
	model.SetTasks([]core.TaskSnapshot{history})
	reset, changed := false, false
	model.RowsReset().Attach(func() { reset = true })
	model.RowsChanged().Attach(func(from, to int) {
		if !reset {
			t.Fatal("rows changed before row count was reset")
		}
		if from != 0 || to != model.RowCount()-1 {
			t.Fatalf("changed range = [%d,%d], want [0,%d]", from, to, model.RowCount()-1)
		}
		changed = true
	})

	steps := []struct {
		name string
		run  func()
		rows int
	}{
		{"insert ahead of history", func() { model.SetTasks([]core.TaskSnapshot{history, latest}) }, 2},
		{"same count progress update", func() {
			latest.Status, latest.Progress, latest.Received, latest.Speed = core.StatusRunning, 25, 256, 128
			model.SetTasks([]core.TaskSnapshot{history, latest})
		}, 2},
		{"search", func() { model.SetSearchText("latest") }, 1},
		{"clear search", func() { model.SetSearchText("") }, 2},
		{"filter", func() { model.SetFilter(taskFilterActive) }, 1},
		{"empty", func() { model.SetTasks(nil) }, 0},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			reset, changed = false, false
			step.run()
			if !reset || changed != (step.rows > 0) {
				t.Fatalf("notifications: reset=%v changed=%v, want reset=true changed=%v", reset, changed, step.rows > 0)
			}
			if model.RowCount() != step.rows {
				t.Fatalf("row count = %d, want %d", model.RowCount(), step.rows)
			}
			if step.rows > 0 {
				if task, _ := model.TaskAt(0); task.ID != latest.ID {
					t.Fatalf("first row = %q, want latest", task.ID)
				}
				if model.Value(0, 0) != latest.Title || model.Value(0, 1) != displayStatus(latest.Status) || model.Value(0, 2) != fmt.Sprintf("%.1f%%", latest.Progress) || model.Value(0, 5) != latest.Path {
					t.Fatal("latest row still displays historical values")
				}
			}
		})
	}
}

func BenchmarkTaskTableModelTenThousandRows(b *testing.B) {
	tasks := make([]core.TaskSnapshot, 10000)
	for i := range tasks {
		tasks[i] = core.TaskSnapshot{ID: fmt.Sprintf("task-%05d", i), Title: fmt.Sprintf("Download %05d", i), Status: core.StatusCompleted, CreatedAt: time.Unix(int64(i), 0)}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model := newTaskTableModel()
		model.SetTasks(tasks)
	}
}

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

func TestTaskTableModelToggleSort(t *testing.T) {
	model := newTaskTableModel()
	model.SetTasks([]core.TaskSnapshot{
		{ID: "b", Title: "bravo", CreatedAt: time.Unix(2, 0)},
		{ID: "a", Title: "alpha", CreatedAt: time.Unix(1, 0)},
	})

	if err := model.ToggleSort(0); err != nil {
		t.Fatal(err)
	}
	if task, _ := model.TaskAt(0); task.ID != "a" {
		t.Fatalf("ascending first task = %q, want a", task.ID)
	}
	if column, order := model.SortState(); column != 0 || order != walk.SortAscending {
		t.Fatalf("sort state = (%d, %v), want (0, ascending)", column, order)
	}

	if err := model.ToggleSort(0); err != nil {
		t.Fatal(err)
	}
	if task, _ := model.TaskAt(0); task.ID != "b" {
		t.Fatalf("descending first task = %q, want b", task.ID)
	}
}

func TestTaskTableModelSortStateMatchesWalk(t *testing.T) {
	model := newTaskTableModel()
	column, order := model.SortState()
	if column != model.SortedColumn() || order != model.SortOrder() {
		t.Fatalf("model sort=(%d,%v), Walk sort=(%d,%v)", column, order, model.SortedColumn(), model.SortOrder())
	}
}

func TestTaskTableModelSizeSortAndNewestDefault(t *testing.T) {
	model := newTaskTableModel()
	model.SetTasks([]core.TaskSnapshot{
		{ID: "large", FileSize: 1000, Received: 1, CreatedAt: time.Unix(1, 0)},
		{ID: "small", FileSize: 10, Received: 10, CreatedAt: time.Unix(3, 0)},
		{ID: "unknown", Received: 100, CreatedAt: time.Unix(2, 0)},
	})
	if task, _ := model.TaskAt(0); task.ID != "small" {
		t.Fatalf("default first=%q, want newest task small", task.ID)
	}
	for _, test := range []struct {
		order walk.SortOrder
		want  []string
	}{
		{walk.SortAscending, []string{"small", "unknown", "large"}},
		{walk.SortDescending, []string{"large", "unknown", "small"}},
	} {
		if err := model.Sort(3, test.order); err != nil {
			t.Fatal(err)
		}
		for row, want := range test.want {
			if task, _ := model.TaskAt(row); task.ID != want {
				t.Errorf("size sort %v row %d=%q, want %q", test.order, row, task.ID, want)
			}
		}
	}
}

func TestTaskTableModelCountsLookupAndValues(t *testing.T) {
	model := newTaskTableModel()
	model.SetTasks([]core.TaskSnapshot{
		{ID: "running", Title: "running.bin", Status: core.StatusRunning, Progress: 12.5, FileSize: 100, Received: 25, Speed: 10, Path: `D:\One`, CreatedAt: time.Unix(3, 0)},
		{ID: "completed", Title: "completed.bin", Status: core.StatusCompleted, Progress: 100, FileSize: 200, Received: 200, Path: `D:\Two`, CreatedAt: time.Unix(2, 0)},
		{ID: "failed", Title: "failed.bin", Status: core.StatusFailed, Received: 5, Path: `D:\Three`, CreatedAt: time.Unix(1, 0)},
	})

	all, active, completed, failed := model.Counts()
	if all != 3 || active != 2 || completed != 1 || failed != 1 {
		t.Fatalf("counts = %d/%d/%d/%d, want 3/2/1/1", all, active, completed, failed)
	}
	if task, ok := model.TaskByID("completed"); !ok || task.Title != "completed.bin" {
		t.Fatalf("TaskByID(completed) = %#v, %v", task, ok)
	}
	if _, ok := model.TaskByID("missing"); ok {
		t.Fatal("TaskByID found a missing task")
	}

	for column := 0; column < 6; column++ {
		if err := model.Sort(column, walk.SortAscending); err != nil {
			t.Fatalf("Sort(%d): %v", column, err)
		}
		if gotColumn, order := model.SortState(); gotColumn != column || order != walk.SortAscending {
			t.Fatalf("sort state = (%d,%v), want (%d,ascending)", gotColumn, order, column)
		}
	}
	if got := model.Value(-1, 0); got != "" {
		t.Fatalf("Value(-1,0) = %#v, want empty", got)
	}
	if got := model.Value(0, 99); got != "" {
		t.Fatalf("Value(0,99) = %#v, want empty", got)
	}
}

func TestDisplayStatusUsesSimplifiedChinese(t *testing.T) {
	tests := []struct {
		status core.TaskStatus
		want   string
	}{
		{core.StatusWaiting, "等待中"},
		{core.StatusRunning, "下载中"},
		{core.StatusSeeding, "做种中"},
		{core.StatusPaused, "已暂停"},
		{core.StatusCompleted, "已完成"},
		{core.StatusFailed, "失败"},
		{core.StatusCanceled, "已取消"},
	}
	for _, test := range tests {
		if got := displayStatus(test.status); got != test.want {
			t.Errorf("displayStatus(%q) = %q, want %q", test.status, got, test.want)
		}
	}
}
