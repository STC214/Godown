package main

import (
	"errors"
	"testing"

	"ghost-downloader-go-win32/internal/win32"
)

func TestIsVersionRequest(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "long", args: []string{"--version"}, want: true},
		{name: "short", args: []string{"-version"}, want: true},
		{name: "none"},
		{name: "extra", args: []string{"--version", "extra"}},
		{name: "other", args: []string{"--help"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isVersionRequest(test.args); got != test.want {
				t.Fatalf("isVersionRequest(%q) = %v, want %v", test.args, got, test.want)
			}
		})
	}
}

func TestRunExitBehavior(t *testing.T) {
	tests := []struct {
		name        string
		runError    error
		wantCode    int
		wantAlready int
		wantError   int
	}{
		{name: "success", wantCode: 0},
		{name: "already running", runError: win32.ErrAlreadyRunning, wantCode: 0, wantAlready: 1},
		{name: "startup error", runError: errors.New("startup failed"), wantCode: 1, wantError: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			alreadyCalls, errorCalls := installRunFakes(t, func() error { return test.runError })
			if got := run(); got != test.wantCode {
				t.Fatalf("run() = %d, want %d", got, test.wantCode)
			}
			if *alreadyCalls != test.wantAlready || *errorCalls != test.wantError {
				t.Fatalf("dialogs = already:%d error:%d, want already:%d error:%d", *alreadyCalls, *errorCalls, test.wantAlready, test.wantError)
			}
		})
	}
}

func TestRunRecoversPanic(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	installRunFakes(t, func() error { panic("boom") })
	if got := run(); got != 2 {
		t.Fatalf("run() after panic = %d, want 2", got)
	}
}

func installRunFakes(t *testing.T, application func() error) (*int, *int) {
	t.Helper()
	originalRun := runApplication
	originalAlready := showAlreadyRunningFunc
	originalError := showStartupErrorFunc
	alreadyCalls, errorCalls := 0, 0
	runApplication = application
	showAlreadyRunningFunc = func() { alreadyCalls++ }
	showStartupErrorFunc = func(error) { errorCalls++ }
	t.Cleanup(func() {
		runApplication = originalRun
		showAlreadyRunningFunc = originalAlready
		showStartupErrorFunc = originalError
	})
	return &alreadyCalls, &errorCalls
}
