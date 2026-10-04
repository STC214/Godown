package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"ghost-downloader-go-win32/internal/core"
	"github.com/lxn/walk"
	"github.com/lxn/win"
)

var nativeTaskContext win.HANDLE

func TestMain(m *testing.M) {
	// Keep Walk's process-wide tooltip and its initialization thread alive.
	// Initializing it inside a short-lived test goroutine can destroy the
	// tooltip's OS thread and make subsequent native window creation fail.
	runtime.LockOSThread()
	manifest, err := filepath.Abs("../../cmd/gd3win/gd3win.manifest")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	nativeTaskContext = win.CreateActCtx(&win.ACTCTX{Source: syscall.StringToUTF16Ptr(manifest)})
	if nativeTaskContext == win.HANDLE(^uintptr(0)) {
		fmt.Fprintln(os.Stderr, "CreateActCtx failed")
		os.Exit(1)
	}
	cookie, ok := win.ActivateActCtx(nativeTaskContext)
	if !ok {
		fmt.Fprintln(os.Stderr, "ActivateActCtx failed")
		os.Exit(1)
	}
	keeper, err := walk.NewMainWindow()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	keeper.Dispose()
	kernel := syscall.NewLazyDLL("kernel32.dll")
	_, _, _ = kernel.NewProc("DeactivateActCtx").Call(0, cookie)
	_, _, _ = kernel.NewProc("ReleaseActCtx").Call(uintptr(nativeTaskContext))
	os.Exit(code)
}

// Exercise the actual Walk/ListView event chain without showing a window or
// moving the user's pointer. Unit tests of the model alone miss selection resets.
func newNativeTaskTable(t *testing.T) (*walk.TableView, *taskTableModel, *walk.TextEdit, *string) {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	kernel := syscall.NewLazyDLL("kernel32.dll")
	cookie, ok := win.ActivateActCtx(nativeTaskContext)
	if !ok {
		t.Fatal("ActivateActCtx failed")
	}
	t.Cleanup(func() { _, _, _ = kernel.NewProc("DeactivateActCtx").Call(0, cookie) })
	window, err := walk.NewMainWindow()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(window.Dispose)
	table, err := walk.NewTableView(window)
	if err != nil {
		t.Fatal(err)
	}
	if err := table.SetBoundsPixels(walk.Rectangle{Width: 640, Height: 320}); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"名称", "状态", "进度", "大小", "速度", "目录"} {
		column := walk.NewTableViewColumn()
		if err := column.SetTitle(title); err != nil {
			t.Fatal(err)
		}
		if err := table.Columns().Add(column); err != nil {
			t.Fatal(err)
		}
	}
	model := newTaskTableModel()
	if err := table.SetModel(model); err != nil {
		t.Fatal(err)
	}
	if err := model.Sort(taskSortCreatedAt, walk.SortDescending); err != nil {
		t.Fatal(err)
	}
	detail, err := walk.NewTextEdit(window)
	if err != nil {
		t.Fatal(err)
	}
	selected := new(string)
	table.SelectedIndexesChanged().Attach(func() { refreshTaskSelection(table, model, selected, detail) })
	return table, model, detail, selected
}

func TestNativeTaskTableKeepsSelectionOnRefreshAndSort(t *testing.T) {
	table, model, detail, selected := newNativeTaskTable(t)
	history := core.TaskSnapshot{ID: "history", Title: "history.bin", Status: core.StatusCompleted, CreatedAt: time.Unix(1, 0)}
	latest := core.TaskSnapshot{ID: "latest", Title: "latest.bin", Status: core.StatusRunning, CreatedAt: time.Unix(2, 0)}
	model.SetTasks([]core.TaskSnapshot{history})
	if err := table.SetCurrentIndex(0); err != nil {
		t.Fatal(err)
	}
	refreshTaskSelection(table, model, selected, detail)
	for _, step := range []struct {
		name string
		run  func()
	}{
		{"insert", func() { model.SetTasks([]core.TaskSnapshot{history, latest}) }},
		{"progress", func() {
			history.Progress = 75
			model.SetTasks([]core.TaskSnapshot{history, latest})
		}},
		{"sort", func() {
			if err := model.Sort(0, walk.SortDescending); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		step.run()
		refreshTaskSelection(table, model, selected, detail)
		task, ok := model.TaskAt(table.CurrentIndex())
		if !ok || task.ID != history.ID || *selected != history.ID {
			t.Errorf("%s: current=%d task=%q selected=%q, want history", step.name, table.CurrentIndex(), task.ID, *selected)
		}
		if !strings.HasPrefix(detail.Text(), history.Title+"\r\n") {
			t.Errorf("%s: detail = %q, want history", step.name, detail.Text())
		}
	}
}

func TestNativeTaskTableClearsHiddenAndDeselectedDetails(t *testing.T) {
	table, model, detail, selected := newNativeTaskTable(t)
	model.SetTasks([]core.TaskSnapshot{
		{ID: "old", Title: "old.bin", Status: core.StatusCompleted, CreatedAt: time.Unix(1, 0)},
		{ID: "new", Title: "new.bin", Status: core.StatusRunning, CreatedAt: time.Unix(2, 0)},
	})
	if err := table.SetCurrentIndex(1); err != nil {
		t.Fatal(err)
	}
	refreshTaskSelection(table, model, selected, detail)
	model.SetSearchText("no matching task")
	refreshTaskSelection(table, model, selected, detail)
	if *selected != "" || detail.Text() != "未选择任务。" {
		t.Errorf("filtered-out task still actionable: selected=%q detail=%q", *selected, detail.Text())
	}
	model.SetSearchText("")
	if err := table.SetCurrentIndex(0); err != nil {
		t.Fatal(err)
	}
	refreshTaskSelection(table, model, selected, detail)
	if err := table.SetCurrentIndex(-1); err != nil {
		t.Fatal(err)
	}
	refreshTaskSelection(table, model, selected, detail)
	if *selected != "" || detail.Text() != "未选择任务。" {
		t.Errorf("deselected task still actionable: selected=%q detail=%q", *selected, detail.Text())
	}
}

func TestNativeTaskTableRepaintsOnUpdatesAndTheme(t *testing.T) {
	table, model, _, _ := newNativeTaskTable(t)
	hwnd := findChildWindowByClass(table.Handle(), "SysListView32")
	if hwnd == 0 {
		t.Fatal("native ListView not found")
	}
	redraws := 0
	var original uintptr
	callback := syscall.NewCallback(func(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
		if msg == win.LVM_REDRAWITEMS {
			redraws++
		}
		return win.CallWindowProc(original, hwnd, msg, wParam, lParam)
	})
	original = win.SetWindowLongPtr(hwnd, win.GWLP_WNDPROC, callback)
	if original == 0 {
		t.Fatal("subclass ListView failed")
	}
	t.Cleanup(func() { win.SetWindowLongPtr(hwnd, win.GWLP_WNDPROC, original) })
	for _, step := range []struct {
		name string
		run  func()
	}{
		{"tasks", func() { model.SetTasks([]core.TaskSnapshot{{ID: "one", Title: "one"}}) }},
		{"dark theme", func() { model.SetDarkMode(true) }},
		{"light theme", func() { model.SetDarkMode(false) }},
	} {
		redraws = 0
		step.run()
		if redraws == 0 {
			t.Errorf("%s: native ListView received no redraw notification", step.name)
		}
	}
}

func TestNativeTaskTableRapidUpdatesFiltersAndRemoval(t *testing.T) {
	table, model, detail, selected := newNativeTaskTable(t)
	tasks := make([]core.TaskSnapshot, 128)
	for i := range tasks {
		tasks[i] = core.TaskSnapshot{
			ID: fmt.Sprintf("task-%03d", i), Title: fmt.Sprintf("file-%03d.bin", i),
			Status: core.StatusRunning, FileSize: int64(128 - i), Path: fmt.Sprintf(`D:\%03d`, i),
			CreatedAt: time.Unix(int64(i), 0),
		}
	}
	model.SetTasks(tasks)
	if err := table.SetCurrentIndex(64); err != nil {
		t.Fatal(err)
	}
	refreshTaskSelection(table, model, selected, detail)
	chosen := *selected
	for iteration := 0; iteration < 100; iteration++ {
		for i := range tasks {
			tasks[i].Progress = float64(iteration)
			tasks[i].Received = int64(iteration)
			tasks[i].Speed = int64(i + iteration)
		}
		model.SetTasks(tasks)
		if err := model.Sort(iteration%6, walk.SortOrder(iteration%2)); err != nil {
			t.Fatal(err)
		}
		refreshTaskSelection(table, model, selected, detail)
		current, ok := model.TaskAt(table.CurrentIndex())
		if !ok || current.ID != chosen || *selected != chosen {
			t.Fatalf("iteration %d: current=%q selected=%q, want %q", iteration, current.ID, *selected, chosen)
		}
		if !strings.Contains(detail.Text(), fmt.Sprintf("进度：%.1f%%", float64(iteration))) {
			t.Fatalf("iteration %d: stale detail %q", iteration, detail.Text())
		}
	}
	// Completion removes the selected task from the active view. Walk may
	// choose the first remaining visible task; actions/details must follow it.
	model.SetFilter(taskFilterActive)
	for i := range tasks {
		if tasks[i].ID == chosen {
			tasks[i].Status = core.StatusCompleted
		}
	}
	model.SetTasks(tasks)
	refreshTaskSelection(table, model, selected, detail)
	current, ok := model.TaskAt(table.CurrentIndex())
	if !ok || current.ID == chosen || *selected != current.ID || !strings.HasPrefix(detail.Text(), current.Title+"\r\n") {
		t.Fatalf("completion/filter: current=%q selected=%q detail=%q", current.ID, *selected, detail.Text())
	}
	model.SetTasks(nil)
	refreshTaskSelection(table, model, selected, detail)
	if table.CurrentIndex() != -1 || *selected != "" || detail.Text() != "未选择任务。" {
		t.Fatalf("remove all: current=%d selected=%q detail=%q", table.CurrentIndex(), *selected, detail.Text())
	}
	if model.ID(-1) != nil || model.ID(model.RowCount()) != nil {
		t.Fatal("invalid row ID must be nil")
	}
}
