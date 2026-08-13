package httpdownload

import (
	"fmt"
	"path/filepath"
	"strings"

	"ghost-downloader-go-win32/internal/core"
)

func DeduplicateTitle(title, downloadDir string, existing []core.TaskSnapshot) string {
	used := make(map[string]struct{}, len(existing))
	for _, task := range existing {
		used[strings.ToLower(filepath.Clean(filepath.Join(task.Path, task.Title)))] = struct{}{}
	}

	candidate := title
	for index := 1; ; index++ {
		key := strings.ToLower(filepath.Clean(filepath.Join(downloadDir, candidate)))
		if _, ok := used[key]; !ok {
			return candidate
		}
		candidate = withCopySuffix(title, index)
	}
}

func withCopySuffix(title string, index int) string {
	ext := filepath.Ext(title)
	base := strings.TrimSuffix(title, ext)
	if base == "" {
		base = "download"
	}
	if index == 1 {
		return fmt.Sprintf("%s (%d)%s", base, 1, ext)
	}
	return fmt.Sprintf("%s (%d)%s", base, index, ext)
}
