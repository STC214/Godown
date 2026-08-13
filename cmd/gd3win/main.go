package main

import (
	"log/slog"
	"os"

	"ghost-downloader-go-win32/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		slog.Error("application exited with error", "error", err)
		os.Exit(1)
	}
}
