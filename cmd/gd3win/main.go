package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"ghost-downloader-go-win32/internal/app"
	"ghost-downloader-go-win32/internal/buildinfo"
	"ghost-downloader-go-win32/internal/config"
	"ghost-downloader-go-win32/internal/logging"
	"ghost-downloader-go-win32/internal/win32"
)

var (
	runApplication         = app.Run
	showAlreadyRunningFunc = showAlreadyRunning
	showStartupErrorFunc   = showStartupError
)

func main() {
	if isVersionRequest(os.Args[1:]) {
		fmt.Println(buildinfo.Version)
		return
	}
	os.Exit(run())
}

func isVersionRequest(arguments []string) bool {
	return len(arguments) == 1 && (arguments[0] == "--version" || arguments[0] == "-version")
}

func run() (exitCode int) {
	paths, pathsErr := config.ResolvePaths()
	defer func() {
		if recovered := recover(); recovered != nil {
			exitCode = 2
			if pathsErr == nil {
				if path, err := logging.WriteCrashReport(paths.DataDir, recovered); err == nil {
					fmt.Fprintln(os.Stderr, "Crash report:", path)
				} else {
					fmt.Fprintln(os.Stderr, "Write crash report:", err)
				}
			}
		}
	}()
	if err := runApplication(); err != nil {
		if errors.Is(err, win32.ErrAlreadyRunning) {
			showAlreadyRunningFunc()
			return 0
		}
		slog.Error("application exited with error", "error", err)
		showStartupErrorFunc(err)
		return 1
	}
	return 0
}
