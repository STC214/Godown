package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

func Initialize(logFile string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}

	writer := io.MultiWriter(os.Stderr, file)
	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	slog.SetDefault(slog.New(handler))

	return func() {
		_ = file.Close()
	}, nil
}
