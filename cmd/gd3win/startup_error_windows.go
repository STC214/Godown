//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	user32DLL       = syscall.NewLazyDLL("user32.dll")
	messageBoxWProc = user32DLL.NewProc("MessageBoxW")
)

func showAlreadyRunning() {
	title, _ := syscall.UTF16PtrFromString("Ghost Downloader")
	message, _ := syscall.UTF16PtrFromString("Ghost Downloader 已在运行。")
	const mbIconInformation = 0x00000040
	messageBoxWProc.Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), mbIconInformation)
}

func showStartupError(err error) {
	if err == nil {
		return
	}
	title, _ := syscall.UTF16PtrFromString("Ghost Downloader 启动失败")
	message, _ := syscall.UTF16PtrFromString(fmt.Sprintf("程序启动时发生错误：\n\n%v", err))
	const mbIconError = 0x00000010
	messageBoxWProc.Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), mbIconError)
}
