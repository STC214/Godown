package logging

import (
	"os"
	"strings"
	"testing"
)

func TestWriteCrashReport(t *testing.T) {
	path, err := WriteCrashReport(t.TempDir(), "synthetic panic")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "synthetic panic") || !strings.Contains(text, "TestWriteCrashReport") {
		t.Fatalf("crash report lacks panic or stack: %q", text)
	}
}
