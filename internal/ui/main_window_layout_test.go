package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"ghost-downloader-go-win32/internal/core"
	"github.com/lxn/walk"
	"github.com/lxn/win"
)

func TestMainWindowVisualLayout(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	cookie, ok := win.ActivateActCtx(nativeTaskContext)
	if !ok {
		t.Fatal("ActivateActCtx")
	}
	defer syscall.NewLazyDLL("kernel32.dll").NewProc("DeactivateActCtx").Call(0, cookie)
	var window *walk.MainWindow
	var status, summary *walk.Label
	var url, search *walk.LineEdit
	var filter *walk.ComboBox
	var table *walk.TableView
	var detail *walk.TextEdit
	var headers [6]*walk.PushButton
	model := newTaskTableModel()
	clicks := 0
	selectedTaskID := ""
	layout := mainWindowLayout("visual preview", `D:\Downloads`, mainWindowControls{
		window: &window, status: &status, summary: &summary, url: &url, search: &search,
		filter: &filter, table: &table, detail: &detail, headers: &headers, model: model,
	}, mainWindowActions{
		add:       func() { clicks++ },
		selection: func() { refreshTaskSelection(table, model, &selectedTaskID, detail) },
	})
	layout.Visible = false
	if err := layout.Create(); err != nil {
		t.Fatal(err)
	}
	defer window.Dispose()
	trackStatusTooltip(status)
	if status.ToolTipText() != status.Text() {
		t.Fatal("initial status tooltip missing")
	}
	// Native layout ignores invisible ancestors. Keep this fixture off-screen
	// and show it without activation; capture uses WM_PRINT, not the desktop.
	win.SetWindowPos(window.Handle(), 0, -20000, -20000, 0, 0, win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE)
	win.ShowWindow(window.Handle(), win.SW_SHOWNOACTIVATE)
	style, err := newWindowThemeStyle(window, "dark", url, search, detail)
	if err != nil {
		t.Fatal(err)
	}
	defer style.Dispose()
	tasks := []core.TaskSnapshot{
		{ID: "1", Title: "Ubuntu Desktop 24.04.iso", Status: core.StatusRunning, Progress: 68.4, Received: 3420000000, FileSize: 5000000000, Speed: 12800000},
		{ID: "2", Title: "Design resources.zip", Status: core.StatusCompleted, Progress: 100, Received: 256000000, FileSize: 256000000},
		{ID: "3", Title: "Project archive.7z", Status: core.StatusPaused, Progress: 32.6, Received: 156000000, FileSize: 480000000},
		{ID: "4", Title: "Reference video.mp4", Status: core.StatusWaiting, FileSize: 1200000000},
		{ID: "5", Title: "Documentation bundle.zip", Status: core.StatusFailed, FileSize: 42000000},
	}
	for i := range tasks {
		tasks[i].Path = `D:\Downloads`
		tasks[i].CreatedAt = time.Unix(int64(10-i), 0)
	}
	model.SetTasks(tasks)
	if err := model.Sort(taskSortCreatedAt, walk.SortDescending); err != nil {
		t.Fatal(err)
	}
	table.SetCurrentIndex(-1)
	summary.SetText("全部 5  ·  活动 4  ·  完成 1  ·  失败 1")
	// Exercise the original native BUTTON behavior underneath the paint subclass.
	var primary *nativeButtonStyle
	for _, button := range style.buttons {
		if button.button.Name() == "primaryButton" {
			primary = button
		}
	}
	if primary == nil {
		t.Fatal("primary button missing")
	}
	primary.button.SetEnabled(false)
	if win.IsWindowEnabled(primary.hwnd) {
		t.Fatal("disabled state lost")
	}
	primary.button.SetEnabled(true)
	for _, mode := range []string{"dark", "light"} {
		style.Apply(mode)
		assertNativeTablePalette(t, table, paletteForDarkMode(mode == "dark"))
		model.SetDarkMode(mode == "dark")
		if err := table.SetCurrentIndex(0); err != nil {
			t.Fatal(err)
		}
		if selectedTaskID != tasks[0].ID || !strings.HasPrefix(detail.Text(), tasks[0].Title+"\r\n") {
			t.Errorf("single selection did not update toolbar target/detail: id=%q detail=%q", selectedTaskID, detail.Text())
		}
		selected := walk.CellStyle{
			BackgroundColor: walk.Color(win.GetSysColor(win.COLOR_HIGHLIGHT)),
			TextColor:       walk.Color(win.GetSysColor(win.COLOR_HIGHLIGHTTEXT)),
		}
		table.CellStyler().StyleCell(&selected)
		p := paletteForDarkMode(mode == "dark")
		if selected.BackgroundColor != p.selectedBackground || selected.TextColor != p.selectedText {
			t.Errorf("selected cell contrast palette: got bg=%x text=%x want bg=%x text=%x", selected.BackgroundColor, selected.TextColor, p.selectedBackground, p.selectedText)
		}
		table.SetCurrentIndex(-1)
		if selectedTaskID != "" || detail.Text() != "未选择任务。" {
			t.Errorf("single deselection left stale target/detail: id=%q detail=%q", selectedTaskID, detail.Text())
		}
		for _, width := range []int{1160, 860} {
			height := 780
			if width == 860 {
				height = 650
				status.SetText("下载失败 · " + strings.Repeat("long-folder-name/", 25) + "file & details.zip")
			} else {
				status.SetText("就绪 · 下载目录：D:\\Downloads")
			}
			if status.ToolTipText() != status.Text() || status.EllipsisMode() != walk.EllipsisEnd {
				t.Fatal("truncated status lost its full-text tooltip")
			}
			if err := window.SetSize(walk.Size{Width: width, Height: height}); err != nil {
				t.Fatal(err)
			}
			window.SetSuspended(false)
			// Run the normal Walk message loop, without showing the fixture,
			// so its asynchronous layout results are applied on the UI thread.
			threadID := win.GetCurrentThreadId()
			timer := time.AfterFunc(200*time.Millisecond, func() {
				syscall.NewLazyDLL("user32.dll").NewProc("PostThreadMessageW").Call(uintptr(threadID), win.WM_QUIT, 0, 0)
			})
			window.Run()
			timer.Stop()
			if window.Size().Width != width {
				t.Errorf("status text enlarged window: width=%d want=%d", window.Size().Width, width)
			}
			if table.Bounds().Height < 150 || url.Bounds().Width < 160 {
				t.Fatalf("clipped layout: table=%v input=%v", table.Bounds(), url.Bounds())
			}
			assertVisualChildrenFit(t, window)
			table.EnsureItemVisible(0)
			if clicks == 0 {
				win.SendMessage(primary.hwnd, win.BM_CLICK, 0, 0)
				if clicks != 1 {
					t.Fatalf("native click count=%d", clicks)
				}
			}
			table.SetCurrentIndex(0)
			for row := 0; row < len(tasks); row++ {
				if err := table.SetCurrentIndex(row); err != nil {
					t.Fatal(err)
				}
				assertSelectedTaskPixels(t, table, p, row)
			}
			if err := table.SetCurrentIndex(0); err != nil {
				t.Fatal(err)
			}
			if mode == "dark" && width == 1160 {
				assertSelectedCaptureResources(t, table)
			}
			if dir := os.Getenv("GDOWNLOADER_PREVIEW_DIR"); dir != "" && width == 1160 {
				img, err := captureNativeWindow(window)
				if err != nil {
					t.Fatal(err)
				}
				file, err := os.Create(filepath.Join(dir, "visual-refresh-"+mode+".png"))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(file, img)
				closeErr := file.Close()
				if err != nil || closeErr != nil {
					t.Fatalf("save preview: %v / %v", err, closeErr)
				}
			}
		}
	}
	// Cover both lifetime orders: native destruction before style disposal,
	// and style disposal while the native button is still alive.
	destroyed := style.buttons[0]
	destroyedHWND := destroyed.hwnd
	destroyed.button.Dispose()
	if _, retained := buttonStyles.Load(destroyedHWND); retained || destroyed.hwnd != 0 {
		t.Error("destroyed button retained its subclass/Go registry reference")
	}
	primaryHWND, originalProc := primary.hwnd, primary.originalProc
	style.Dispose()
	style.Dispose()
	if got := win.GetWindowLongPtr(primaryHWND, win.GWLP_WNDPROC); got != originalProc {
		t.Errorf("button procedure not restored: got=%x want=%x", got, originalProc)
	}
	if _, retained := buttonStyles.Load(primaryHWND); retained {
		t.Error("disposed style retained its native registry entry")
	}
}

func assertSelectedCaptureResources(t *testing.T, table *walk.TableView) {
	t.Helper()
	getResources := syscall.NewLazyDLL("user32.dll").NewProc("GetGuiResources")
	before, _, _ := getResources.Call(^uintptr(0), 0)
	if before == 0 {
		t.Fatal("GetGuiResources baseline failed")
	}
	for i := 0; i < 20; i++ {
		_, err := captureNativeWindow(table)
		if err != nil {
			t.Fatal(err)
		}
	}
	after, _, _ := getResources.Call(^uintptr(0), 0)
	if after == 0 {
		t.Fatal("GetGuiResources verification failed")
	}
	t.Logf("20 native captures: GDI objects before=%d after=%d", before, after)
	if after > before {
		t.Errorf("repeated selected-row captures leaked GDI objects: before=%d after=%d", before, after)
	}
}

func assertSelectedTaskPixels(t *testing.T, table *walk.TableView, p visualPalette, row int) {
	t.Helper()
	lv := selectedTaskListView(table)
	if lv == 0 {
		t.Fatal("native task list missing")
	}
	// Focus notifications exercise the native control's two highlight states
	// without activating the off-screen fixture or moving the user's focus.
	for _, notification := range []uint32{win.WM_SETFOCUS, win.WM_KILLFOCUS} {
		win.SendMessage(lv, notification, 0, 0)
		img, err := captureNativeWindow(table)
		if err != nil {
			t.Fatal(err)
		}
		var windowRect win.RECT
		win.GetWindowRect(table.Handle(), &windowRect)
		origin := win.POINT{}
		win.ClientToScreen(lv, &origin)
		for col := 0; col < table.Columns().Len(); col++ {
			rect := win.RECT{Top: int32(col), Left: win.LVIR_BOUNDS}
			result, _, _ := syscall.NewLazyDLL("user32.dll").NewProc("SendMessageW").Call(uintptr(lv), win.LVM_GETSUBITEMRECT, uintptr(row), uintptr(unsafe.Pointer(&rect)))
			if result == 0 || rect.Bottom <= rect.Top {
				t.Fatal("native selected cell bounds missing")
			}
			rect.Right = rect.Left + int32(win.SendMessage(lv, win.LVM_GETCOLUMNWIDTH, uintptr(col), 0))
			x := int(origin.X - windowRect.Left + rect.Left + 2)
			y := int(origin.Y - windowRect.Top + (rect.Top+rect.Bottom)/2)
			pixel := img.RGBAAt(x, y)
			actual := walk.RGB(pixel.R, pixel.G, pixel.B)
			if actual != p.selectedBackground {
				t.Errorf("focus notification=%x row=%d col=%d selected background=%x want=%x rect=%v current=%d", notification, row, col, actual, p.selectedBackground, rect, table.CurrentIndex())
			}
			textPixels := 0
			for yy := int(origin.Y - windowRect.Top + rect.Top); yy < int(origin.Y-windowRect.Top+rect.Bottom); yy++ {
				for xx := int(origin.X - windowRect.Left + rect.Left); xx < int(origin.X-windowRect.Left+rect.Right); xx++ {
					pixel := img.RGBAAt(xx, yy)
					if walk.RGB(pixel.R, pixel.G, pixel.B) == p.selectedText {
						textPixels++
					}
				}
			}
			if textPixels == 0 {
				t.Errorf("focus notification=%x row=%d col=%d high-contrast text not rendered rect=%v current=%d", notification, row, col, rect, table.CurrentIndex())
			}
		}
	}
}

func assertNativeTablePalette(t *testing.T, table *walk.TableView, p visualPalette) {
	t.Helper()
	count := 0
	for child := win.GetWindow(table.Handle(), win.GW_CHILD); child != 0; child = win.GetWindow(child, win.GW_HWNDNEXT) {
		var name [128]uint16
		length, _ := win.GetClassName(child, &name[0], len(name))
		if syscall.UTF16ToString(name[:length]) != "SysListView32" {
			continue
		}
		count++
		for _, pair := range []struct {
			message uint32
			color   walk.Color
		}{{0x1000 /* LVM_GETBKCOLOR */, p.surface}, {win.LVM_GETTEXTBKCOLOR, p.surface}, {win.LVM_GETTEXTCOLOR, p.text}} {
			if actual := win.SendMessage(child, pair.message, 0, 0); actual != uintptr(pair.color) {
				t.Errorf("native list-view palette: msg=%x got=%x want=%x", pair.message, actual, pair.color)
			}
		}
	}
	if count != 2 {
		t.Errorf("expected both normal/frozen native list views, got=%d", count)
	}
}

func assertVisualChildrenFit(t *testing.T, container walk.Container) {
	t.Helper()
	client := container.ClientBounds()
	for i := 0; i < container.Children().Len(); i++ {
		child := container.Children().At(i)
		bounds := child.Bounds()
		if bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.Width > client.Width+1 || bounds.Y+bounds.Height > client.Height+1 {
			t.Fatalf("child %q clipped: bounds=%v parent=%v", child.Name(), bounds, client)
		}
		if nested, ok := child.(walk.Container); ok {
			assertVisualChildrenFit(t, nested)
		}
	}
}
