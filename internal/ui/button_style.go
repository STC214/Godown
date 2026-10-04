package ui

import (
	"sync"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// Subclass only painting; the BUTTON control still owns input, focus,
// accessibility, keyboard activation and Walk's Clicked event.
type nativeButtonStyle struct {
	button       *walk.PushButton
	hwnd         win.HWND
	originalProc uintptr
	palette      visualPalette
	parentColor  walk.Color
	hovered      bool
}

var buttonStyles sync.Map
var styledButtonProc = syscall.NewCallback(styledButtonWindowProc)

func installButtonStyle(button *walk.PushButton) *nativeButtonStyle {
	style := &nativeButtonStyle{button: button, hwnd: button.Handle()}
	style.originalProc = win.SetWindowLongPtr(style.hwnd, win.GWLP_WNDPROC, styledButtonProc)
	if style.originalProc == 0 {
		return nil
	}
	buttonStyles.Store(style.hwnd, style)
	return style
}

func (s *nativeButtonStyle) Apply(palette visualPalette, parentColor walk.Color) {
	s.palette, s.parentColor = palette, parentColor
	if s.hwnd != 0 {
		win.InvalidateRect(s.hwnd, nil, false)
	}
}

func (s *nativeButtonStyle) Dispose() {
	if s == nil || s.hwnd == 0 {
		return
	}
	win.SetWindowLongPtr(s.hwnd, win.GWLP_WNDPROC, s.originalProc)
	buttonStyles.Delete(s.hwnd)
	s.hwnd = 0
}

func styledButtonWindowProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	value, ok := buttonStyles.Load(hwnd)
	if !ok {
		return win.DefWindowProc(hwnd, msg, wParam, lParam)
	}
	s := value.(*nativeButtonStyle)
	switch msg {
	case win.WM_PAINT:
		var paint win.PAINTSTRUCT
		hdc := win.BeginPaint(hwnd, &paint)
		if hdc != 0 {
			s.paint(hdc)
			win.EndPaint(hwnd, &paint)
		}
		return 0
	case win.WM_PRINTCLIENT:
		s.paint(win.HDC(wParam))
		return 0
	case win.WM_ERASEBKGND:
		return 1
	case win.WM_MOUSEMOVE:
		if !s.hovered {
			s.hovered = true
			tracking := win.TRACKMOUSEEVENT{CbSize: uint32(unsafe.Sizeof(win.TRACKMOUSEEVENT{})), DwFlags: win.TME_LEAVE, HwndTrack: hwnd}
			win.TrackMouseEvent(&tracking)
			win.InvalidateRect(hwnd, nil, false)
		}
	case win.WM_MOUSELEAVE:
		s.hovered = false
		win.InvalidateRect(hwnd, nil, false)
	case win.WM_NCDESTROY:
		result := win.CallWindowProc(s.originalProc, hwnd, msg, wParam, lParam)
		buttonStyles.Delete(hwnd)
		s.hwnd = 0
		return result
	}
	result := win.CallWindowProc(s.originalProc, hwnd, msg, wParam, lParam)
	switch msg {
	case win.WM_ENABLE, win.WM_SETFOCUS, win.WM_KILLFOCUS, win.BM_SETSTATE, win.WM_UPDATEUISTATE:
		win.InvalidateRect(hwnd, nil, false)
	}
	return result
}

func (s *nativeButtonStyle) paint(hdc win.HDC) {
	if hdc == 0 {
		return
	}
	saved := win.SaveDC(hdc)
	defer win.RestoreDC(hdc, saved)
	var bounds win.RECT
	if !win.GetClientRect(s.hwnd, &bounds) {
		return
	}
	p := s.palette
	fillNativeRect(hdc, &bounds, win.COLORREF(s.parentColor))
	background, foreground, border := p.raised, p.text, p.border
	state := win.SendMessage(s.hwnd, win.BM_GETSTATE, 0, 0)
	enabled := win.IsWindowEnabled(s.hwnd)
	if s.button.Name() == "primaryButton" {
		background, foreground, border = p.accent, p.onAccent, p.accent
		if s.hovered || state&win.BST_PUSHED != 0 {
			background, border = p.accentHover, p.accentHover
		}
	} else if s.button.Name() == "sortButton" {
		foreground, border = p.muted, p.raised
		if s.hovered {
			foreground = p.text
		}
	} else if s.hovered || state&win.BST_PUSHED != 0 {
		background = p.border
	}
	if !enabled {
		background, foreground, border = p.raised, p.muted, p.border
	}
	radius := int32(walk.IntFrom96DPI(8, s.button.DPI()))
	fillRoundedNativeRect(hdc, bounds, radius, border)
	inset := bounds
	inset.Left++
	inset.Top++
	inset.Right--
	inset.Bottom--
	fillRoundedNativeRect(hdc, inset, radius, background)
	if font := win.SendMessage(s.hwnd, win.WM_GETFONT, 0, 0); font != 0 {
		win.SelectObject(hdc, win.HGDIOBJ(font))
	}
	win.SetBkMode(hdc, win.TRANSPARENT)
	win.SetTextColor(hdc, win.COLORREF(foreground))
	text := syscall.StringToUTF16(s.button.Text())
	win.DrawTextEx(hdc, &text[0], int32(len(text)-1), &bounds, win.DT_CENTER|win.DT_VCENTER|win.DT_SINGLELINE|win.DT_END_ELLIPSIS, nil)
	if enabled && state&win.BST_FOCUS != 0 && win.SendMessage(s.hwnd, win.WM_QUERYUISTATE, 0, 0)&win.UISF_HIDEFOCUS == 0 {
		focus := bounds
		padding := int32(walk.IntFrom96DPI(4, s.button.DPI()))
		focus.Left += padding
		focus.Top += padding
		focus.Right -= padding
		focus.Bottom -= padding
		win.DrawFocusRect(hdc, &focus)
	}
}

func fillRoundedNativeRect(hdc win.HDC, bounds win.RECT, radius int32, color walk.Color) {
	brush, _, _ := createBrushProc.Call(uintptr(color))
	if brush == 0 {
		return
	}
	oldBrush := win.SelectObject(hdc, win.HGDIOBJ(brush))
	oldPen := win.SelectObject(hdc, win.GetStockObject(win.NULL_PEN))
	win.RoundRect(hdc, bounds.Left, bounds.Top, bounds.Right, bounds.Bottom, radius, radius)
	win.SelectObject(hdc, oldBrush)
	win.SelectObject(hdc, oldPen)
	win.DeleteObject(win.HGDIOBJ(brush))
}
