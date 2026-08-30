package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

func WriteCrashReport(dataDir string, recovered any) (string, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dataDir, "crash-"+time.Now().Format("20060102-150405")+".log")
	content := fmt.Sprintf("Ghost Downloader crash\ntime: %s\npanic: %v\n\n%s", time.Now().Format(time.RFC3339), recovered, debug.Stack())
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
