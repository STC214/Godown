package storage

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"ghost-downloader-go-win32/internal/core"
)

const protectedSecretPrefix = "dpapi:v1:"
const protectedSecretStateKey = "passwordProtection"

func protectTaskSecrets(task core.Task) (core.Task, error) {
	if task.PackID != "ftp" || task.Stage.State == nil {
		return task, nil
	}
	password := task.Stage.State["password"]
	if password == "" {
		return task, nil
	}
	protected, err := protectSecret(password)
	if err != nil {
		return core.Task{}, fmt.Errorf("protect FTP password: %w", err)
	}
	task.Stage.State = cloneState(task.Stage.State)
	task.Stage.State["password"] = protected
	task.Stage.State[protectedSecretStateKey] = "dpapi:v1"
	return task, nil
}

func unprotectTaskSecrets(task core.Task) (core.Task, bool, error) {
	if task.PackID != "ftp" || task.Stage.State == nil {
		return task, false, nil
	}
	password := task.Stage.State["password"]
	if password == "" {
		return task, false, nil
	}
	if task.Stage.State[protectedSecretStateKey] != "dpapi:v1" {
		return task, true, nil
	}
	if !strings.HasPrefix(password, protectedSecretPrefix) {
		return core.Task{}, false, fmt.Errorf("protected FTP password has an invalid envelope")
	}
	plain, err := unprotectSecret(strings.TrimPrefix(password, protectedSecretPrefix))
	if err != nil {
		return core.Task{}, false, fmt.Errorf("unprotect FTP password: %w", err)
	}
	task.Stage.State = cloneState(task.Stage.State)
	task.Stage.State["password"] = plain
	delete(task.Stage.State, protectedSecretStateKey)
	return task, false, nil
}

func protectSecret(value string) (string, error) {
	inputBytes := []byte(value)
	input := dataBlob(inputBytes)
	var output windows.DataBlob
	if err := windows.CryptProtectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(output.Data))))
	protected := unsafe.Slice(output.Data, int(output.Size))
	return protectedSecretPrefix + base64.StdEncoding.EncodeToString(protected), nil
}

func unprotectSecret(encoded string) (string, error) {
	inputBytes, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	input := dataBlob(inputBytes)
	var output windows.DataBlob
	if err := windows.CryptUnprotectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(output.Data))))
	plain := unsafe.Slice(output.Data, int(output.Size))
	return string(plain), nil
}

func dataBlob(data []byte) windows.DataBlob {
	result := windows.DataBlob{Size: uint32(len(data))}
	if len(data) > 0 {
		result.Data = &data[0]
	}
	return result
}

func cloneState(state map[string]string) map[string]string {
	cloned := make(map[string]string, len(state))
	for key, value := range state {
		cloned[key] = value
	}
	return cloned
}
