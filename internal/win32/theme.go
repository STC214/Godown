package win32

import (
	"strings"
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
	windowTheme := syscall.StringToUTF16Ptr("Explorer")
	if dark {
		value = 1
		windowTheme = syscall.StringToUTF16Ptr("DarkMode_Explorer")
	}
	_, _, _ = dwmSetWindowAttribute.Call(uintptr(hwnd), dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&value)), unsafe.Sizeof(value))
	_ = win.SetWindowTheme(hwnd, windowTheme, nil)
	forEachChildWindow(hwnd, func(childHWND win.HWND) {
		theme := windowTheme
		if dark {
			switch strings.ToLower(windowClassName(childHWND)) {
			case "combobox":
				theme = syscall.StringToUTF16Ptr("DarkMode_CFD")
			case "syslistview32", "sysheader32":
				theme = syscall.StringToUTF16Ptr("DarkMode_ItemsView")
			}
		}
		_ = win.SetWindowTheme(childHWND, theme, nil)
	})
	ApplyControlPalette(hwnd, dark)
	win.RedrawWindow(hwnd, nil, 0, win.RDW_INVALIDATE|win.RDW_ALLCHILDREN|win.RDW_FRAME)
}

func ApplyControlPalette(hwnd win.HWND, dark bool) {
	background := win.RGB(255, 255, 255)
	text := win.RGB(17, 17, 17)
	if dark {
		background = win.RGB(30, 31, 34)
		text = win.RGB(242, 243, 245)
	}
	apply := func(child win.HWND) {
		if strings.EqualFold(windowClassName(child), "SysListView32") {
			win.SendMessage(child, win.LVM_SETBKCOLOR, 0, uintptr(background))
			win.SendMessage(child, win.LVM_SETTEXTBKCOLOR, 0, uintptr(background))
			win.SendMessage(child, win.LVM_SETTEXTCOLOR, 0, uintptr(text))
		}
	}
	apply(hwnd)
	forEachChildWindow(hwnd, apply)
}

func forEachChildWindow(parent win.HWND, visit func(win.HWND)) {
	for child := win.GetWindow(parent, win.GW_CHILD); child != 0; child = win.GetWindow(child, win.GW_HWNDNEXT) {
		visit(child)
		forEachChildWindow(child, visit)
	}
}

func windowClassName(hwnd win.HWND) string {
	var buffer [128]uint16
	length, err := win.GetClassName(hwnd, &buffer[0], len(buffer))
	if err != nil || length == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer[:length])
}
