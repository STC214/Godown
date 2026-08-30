package win32

import "testing"

func TestDarkModeEnabledExplicitModes(t *testing.T) {
	if !DarkModeEnabled("dark") {
		t.Fatal("dark mode was not enabled explicitly")
	}
	if DarkModeEnabled("light") {
		t.Fatal("light mode unexpectedly enabled dark mode")
	}
}

func TestWindowClassNameRejectsNullHandle(t *testing.T) {
	if got := windowClassName(0); got != "" {
		t.Fatalf("windowClassName(0) = %q, want empty", got)
	}
}
