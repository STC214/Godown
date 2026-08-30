package win32

import (
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const dwmwaUseImmersiveDarkMode = 20

var dwmSetWindowAttribute = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")

func DarkModeEnabled(mode string) bool {
	switch mode {
	case "dark":
		return true
	case "light":
		return false
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	value, _, err := key.GetIntegerValue("AppsUseLightTheme")
	return err == nil && value == 0
}

// ApplyTheme enables the Windows dark title bar and Explorer dark rendering on
// existing child controls. It is best effort because older Windows builds do
// not expose the immersive dark-mode attribute.
func ApplyTheme(hwnd win.HWND, mode string) {
	dark := DarkModeEnabled(mode)
	value := int32(0)
	theme := syscall.StringToUTF16Ptr("Explorer")
	if dark {
		value = 1
		theme = syscall.StringToUTF16Ptr("DarkMode_Explorer")
	}
	_, _, _ = dwmSetWindowAttribute.Call(uintptr(hwnd), dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&value)), unsafe.Sizeof(value))
	_ = win.SetWindowTheme(hwnd, theme, nil)
	callback := syscall.NewCallback(func(child uintptr, _ uintptr) uintptr {
		_ = win.SetWindowTheme(win.HWND(child), theme, nil)
		return 1
	})
	win.EnumChildWindows(hwnd, callback, 0)
	win.RedrawWindow(hwnd, nil, 0, win.RDW_INVALIDATE|win.RDW_ALLCHILDREN|win.RDW_FRAME)
}
