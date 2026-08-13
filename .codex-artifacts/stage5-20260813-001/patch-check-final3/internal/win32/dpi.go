package win32

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

const dpiAwarenessContextPerMonitorAwareV2 = ^uintptr(3)

func EnablePerMonitorDPIAwareness() error {
	user32 := windows.NewLazySystemDLL("user32.dll")
	setProcessDPIAwarenessContext := user32.NewProc("SetProcessDpiAwarenessContext")
	if err := user32.Load(); err != nil {
		return err
	}

	if err := setProcessDPIAwarenessContext.Find(); err == nil {
		ret, _, callErr := setProcessDPIAwarenessContext.Call(dpiAwarenessContextPerMonitorAwareV2)
		if ret != 0 {
			return nil
		}
		if callErr != syscall.Errno(0) {
			return callErr
		}
		return errors.New("SetProcessDpiAwarenessContext returned false")
	}

	setProcessDPIAware := user32.NewProc("SetProcessDPIAware")
	if err := setProcessDPIAware.Find(); err != nil {
		return err
	}
	ret, _, callErr := setProcessDPIAware.Call()
	if ret != 0 {
		return nil
	}
	if callErr != syscall.Errno(0) {
		return callErr
	}
	return errors.New("SetProcessDPIAware returned false")
}
