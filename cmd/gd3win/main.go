package main

import (
	"fmt"
	"log/slog"
	"os"

	"ghost-downloader-go-win32/internal/app"
	"ghost-downloader-go-win32/internal/buildinfo"
	"ghost-downloader-go-win32/internal/config"
	"ghost-downloader-go-win32/internal/logging"
)

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Println(buildinfo.Version)
		return
	}
	os.Exit(run())
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
	if err := app.Run(); err != nil {
		slog.Error("application exited with error", "error", err)
		return 1
	}
	return 0
}
