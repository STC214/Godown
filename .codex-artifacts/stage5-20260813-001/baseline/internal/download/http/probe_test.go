package httpdownload

import (
	"net/http"
	"testing"
)

func TestFilenameFromContentDispositionRFC5987(t *testing.T) {
	headers := http.Header{}
	headers.Set("Content-Disposition", "attachment; filename*=UTF-8''%E4%B8%8B%E8%BD%BD%E6%96%87%E4%BB%B6.zip")

	got := filenameFrom("https://example.com/fallback.bin", headers)
	if got != "下载文件.zip" {
		t.Fatalf("expected decoded filename*, got %q", got)
	}
}

func TestFilenameFromContentDispositionFallbackFilename(t *testing.T) {
	headers := http.Header{}
	headers.Set("Content-Disposition", `attachment; filename="report?.pdf"`)

	got := filenameFrom("https://example.com/fallback.bin", headers)
	if got != "report_.pdf" {
		t.Fatalf("expected safe filename, got %q", got)
	}
}

func TestFilenameFromContentLocation(t *testing.T) {
	headers := http.Header{}
	headers.Set("Content-Location", "https://cdn.example.com/files/archive%201")
	headers.Set("Content-Type", "application/zip")

	got := filenameFrom("https://example.com/download", headers)
	if got != "archive 1.zip" {
		t.Fatalf("expected content-location filename with extension, got %q", got)
	}
}

func TestFilenameFromResponseContentDispositionQuery(t *testing.T) {
	headers := http.Header{}
	rawURL := "https://storage.example.com/object?response-content-disposition=attachment%3B%20filename%2A%3DUTF-8%27%27hello%2520world.txt"

	got := filenameFrom(rawURL, headers)
	if got != "hello world.txt" {
		t.Fatalf("expected query content-disposition filename, got %q", got)
	}
}

func TestFilenameFromPathAddsContentTypeExtension(t *testing.T) {
	headers := http.Header{}
	headers.Set("Content-Type", "text/plain; charset=utf-8")

	got := filenameFrom("https://example.com/export", headers)
	if got != "export.txt" {
		t.Fatalf("expected path filename with content-type extension, got %q", got)
	}
}
