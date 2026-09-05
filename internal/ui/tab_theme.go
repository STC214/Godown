package ui

import (
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

type tabThemePalette struct {
	header     win.COLORREF
	selected   win.COLORREF
	unselected win.COLORREF
	border     win.COLORREF
	text       win.COLORREF
}

func darkTabThemePalette() tabThemePalette {
	return tabThemePalette{
		header:     win.RGB(32, 33, 34),
		selected:   win.RGB(45, 46, 50),
		unselected: win.RGB(38, 39, 43),
		border:     win.RGB(70, 72, 78),
		text:       win.RGB(242, 243, 245),
	}
}

type nativeTabTheme struct {
	tab          *walk.TabWidget
	hwnd         win.HWND
	originalProc uintptr
	dark         bool
}

var (
	tabThemeRegistry sync.Map
	tabThemeWndProc  = syscall.NewCallback(themedTabWindowProc)
	user32ThemeDLL   = syscall.NewLazyDLL("user32.dll")
	gdi32ThemeDLL    = syscall.NewLazyDLL("gdi32.dll")
	fillRectProc     = user32ThemeDLL.NewProc("FillRect")
	createBrushProc  = gdi32ThemeDLL.NewProc("CreateSolidBrush")
)

func installNativeTabTheme(tab *walk.TabWidget) *nativeTabTheme {
	if tab == nil {
		return nil
	}
	hwnd := findChildWindowByClass(tab.Handle(), "SysTabControl32")
	if hwnd == 0 {
		return nil
	}
	style := &nativeTabTheme{tab: tab, hwnd: hwnd}
	style.originalProc = win.SetWindowLongPtr(hwnd, win.GWLP_WNDPROC, tabThemeWndProc)
	if style.originalProc == 0 {
		return nil
	}
	tabThemeRegistry.Store(hwnd, style)
	return style
}

func (s *nativeTabTheme) Apply(dark bool) {
	if s == nil || s.hwnd == 0 {
		return
	}
	s.dark = dark
	win.InvalidateRect(s.hwnd, nil, true)
}

func (s *nativeTabTheme) Dispose() {
	if s == nil || s.hwnd == 0 {
		return
	}
	tabThemeRegistry.Delete(s.hwnd)
	if s.originalProc != 0 {
		win.SetWindowLongPtr(s.hwnd, win.GWLP_WNDPROC, s.originalProc)
	}
	s.hwnd = 0
	s.originalProc = 0
}

func themedTabWindowProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	value, ok := tabThemeRegistry.Load(hwnd)
	if !ok {
		return win.DefWindowProc(hwnd, msg, wParam, lParam)
	}
	style := value.(*nativeTabTheme)
	result := win.CallWindowProc(style.originalProc, hwnd, msg, wParam, lParam)
	if msg == win.WM_PAINT && style.dark {
		style.paintDarkHeader()
	}
	return result
}

func (s *nativeTabTheme) paintDarkHeader() {
	if s == nil || s.hwnd == 0 || s.tab == nil {
		return
	}
	hdc := win.GetDC(s.hwnd)
	if hdc == 0 {
		return
	}
	defer win.ReleaseDC(s.hwnd, hdc)

	count := s.tab.Pages().Len()
	if count == 0 {
		return
	}
	palette := darkTabThemePalette()
	selected := int(win.SendMessage(s.hwnd, win.TCM_GETCURSEL, 0, 0))
	var client win.RECT
	win.GetClientRect(s.hwnd, &client)
	var last win.RECT
	if win.SendMessage(s.hwnd, win.TCM_GETITEMRECT, uintptr(count-1), uintptr(unsafe.Pointer(&last))) == 0 {
		return
	}
	header := win.RECT{Left: client.Left, Top: client.Top, Right: client.Right, Bottom: last.Bottom + 2}
	fillNativeRect(hdc, &header, palette.header)

	font := win.SendMessage(s.hwnd, win.WM_GETFONT, 0, 0)
	var oldFont win.HGDIOBJ
	if font != 0 {
		oldFont = win.SelectObject(hdc, win.HGDIOBJ(font))
		defer win.SelectObject(hdc, oldFont)
	}
	win.SetBkMode(hdc, win.TRANSPARENT)
	win.SetTextColor(hdc, palette.text)

	for index := 0; index < count; index++ {
		var rect win.RECT
		if win.SendMessage(s.hwnd, win.TCM_GETITEMRECT, uintptr(index), uintptr(unsafe.Pointer(&rect))) == 0 {
			continue
		}
		color := palette.unselected
		if index == selected {
			color = palette.selected
			rect.Top--
			rect.Bottom += 2
		}
		fillNativeRect(hdc, &rect, palette.border)
		inner := rect
		inner.Left++
		inner.Top++
		inner.Right--
		inner.Bottom--
		fillNativeRect(hdc, &inner, color)
		title := syscall.StringToUTF16(s.tab.Pages().At(index).Title())
		if len(title) > 1 {
			win.DrawTextEx(hdc, &title[0], int32(len(title)-1), &inner, win.DT_CENTER|win.DT_VCENTER|win.DT_SINGLELINE|win.DT_NOPREFIX, nil)
		}
	}
}

func fillNativeRect(hdc win.HDC, rect *win.RECT, color win.COLORREF) {
	brush, _, _ := createBrushProc.Call(uintptr(color))
	if brush == 0 {
		return
	}
	fillRectProc.Call(uintptr(hdc), uintptr(unsafe.Pointer(rect)), brush)
	win.DeleteObject(win.HGDIOBJ(brush))
}

func findChildWindowByClass(parent win.HWND, className string) win.HWND {
	for child := win.GetWindow(parent, win.GW_CHILD); child != 0; child = win.GetWindow(child, win.GW_HWNDNEXT) {
		var buffer [128]uint16
		length, err := win.GetClassName(child, &buffer[0], len(buffer))
		if err == nil && strings.EqualFold(syscall.UTF16ToString(buffer[:length]), className) {
			return child
		}
		if nested := findChildWindowByClass(child, className); nested != 0 {
			return nested
		}
	}
	return 0
}
