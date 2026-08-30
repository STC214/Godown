package win32

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// HideCommandWindow prevents console-based helper executables from flashing a
// terminal window when they are started by the desktop GUI.
func HideCommandWindow(command *exec.Cmd) {
	if command == nil {
		return
	}
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
}
