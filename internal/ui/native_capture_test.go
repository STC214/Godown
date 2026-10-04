package ui

import (
	"fmt"
	"image"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// Capture native WM_PRINT rendering without Bitmap.ToImage's unreleased
// desktop DC. Every native handle acquired here has a matching release.
func captureNativeWindow(window walk.Window) (*image.RGBA, error) {
	var rect win.RECT
	if !win.GetWindowRect(window.Handle(), &rect) {
		return nil, fmt.Errorf("GetWindowRect failed")
	}
	width, height := rect.Right-rect.Left, rect.Bottom-rect.Top
	if width <= 0 || height <= 0 || int64(width)*int64(height) > 1<<26 {
		return nil, fmt.Errorf("invalid capture size: %dx%d", width, height)
	}
	dc := win.GetDC(window.Handle())
	if dc == 0 {
		return nil, fmt.Errorf("GetDC failed")
	}
	defer win.ReleaseDC(window.Handle(), dc)
	mem := win.CreateCompatibleDC(dc)
	if mem == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer win.DeleteDC(mem)
	bitmap := win.CreateCompatibleBitmap(dc, width, height)
	if bitmap == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer win.DeleteObject(win.HGDIOBJ(bitmap))
	previous := win.SelectObject(mem, win.HGDIOBJ(bitmap))
	if previous == 0 || previous == win.HGDIOBJ(^uintptr(0)) {
		return nil, fmt.Errorf("SelectObject failed")
	}
	defer win.SelectObject(mem, previous)
	flags := win.PRF_CHILDREN | win.PRF_CLIENT | win.PRF_ERASEBKGND | win.PRF_NONCLIENT | win.PRF_OWNED
	window.SendMessage(win.WM_PRINT, uintptr(mem), uintptr(flags))
	win.SelectObject(mem, previous) // GetDIBits requires a deselected bitmap.
	img := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	info := win.BITMAPINFO{}
	info.BmiHeader.BiSize = uint32(unsafe.Sizeof(info.BmiHeader))
	info.BmiHeader.BiWidth, info.BmiHeader.BiHeight = width, -height
	info.BmiHeader.BiPlanes, info.BmiHeader.BiBitCount = 1, 32
	info.BmiHeader.BiCompression = win.BI_RGB
	if rows := win.GetDIBits(dc, bitmap, 0, uint32(height), &img.Pix[0], &info, win.DIB_RGB_COLORS); rows != height {
		return nil, fmt.Errorf("GetDIBits returned %d rows, want %d", rows, height)
	}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+2], img.Pix[i+3] = img.Pix[i+2], img.Pix[i], 255
	}
	return img, nil
}
