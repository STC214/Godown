package win32

import (
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestHideCommandWindow(t *testing.T) {
	command := exec.Command("helper.exe")
	HideCommandWindow(command)
	if command.SysProcAttr == nil {
		t.Fatal("SysProcAttr was not configured")
	}
	if !command.SysProcAttr.HideWindow {
		t.Fatal("HideWindow=false")
	}
	if command.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatalf("CreationFlags=%#x", command.SysProcAttr.CreationFlags)
	}
}
