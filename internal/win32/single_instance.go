package win32

import (
	"errors"

	"golang.org/x/sys/windows"
)

const singleInstanceMutexName = `Local\GhostDownloaderGo.SingleInstance`

var ErrAlreadyRunning = errors.New("Ghost Downloader Go is already running")

type SingleInstance struct {
	handle windows.Handle
}

func AcquireSingleInstance() (*SingleInstance, error) {
	name, err := windows.UTF16PtrFromString(singleInstanceMutexName)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		return nil, err
	}
	if windows.GetLastError() == windows.ERROR_ALREADY_EXISTS {
		_ = windows.CloseHandle(handle)
		return nil, ErrAlreadyRunning
	}
	return &SingleInstance{handle: handle}, nil
}

func (s *SingleInstance) Release() {
	if s == nil || s.handle == 0 {
		return
	}
	_ = windows.CloseHandle(s.handle)
	s.handle = 0
}
