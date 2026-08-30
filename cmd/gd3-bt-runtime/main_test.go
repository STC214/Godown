package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"ghost-downloader-go-win32/internal/btruntime"
	"ghost-downloader-go-win32/internal/core"
)

func TestRunRejectsInvalidArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing", want: "usage"},
		{name: "extra", args: []string{"run", "extra"}, want: "usage"},
		{name: "unknown", args: []string{"unknown"}, want: `unknown action "unknown"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := run(test.args, strings.NewReader(""), new(bytes.Buffer))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("run(%q) error = %v, want substring %q", test.args, err, test.want)
			}
		})
	}
}

func TestRunRejectsMalformedJSON(t *testing.T) {
	for _, action := range []string{"resolve", "reset", "run"} {
		t.Run(action, func(t *testing.T) {
			err := run([]string{action}, strings.NewReader("{"), new(bytes.Buffer))
			if err == nil {
				t.Fatalf("run(%q) accepted malformed JSON", action)
			}
		})
	}
}

func TestEncodeResult(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var output bytes.Buffer
		wantTask := core.Task{ID: "task-1", Title: "example.bin"}
		if err := encodeResult(&output, wantTask, nil); err != nil {
			t.Fatal(err)
		}
		var message btruntime.RuntimeMessage
		if err := json.NewDecoder(&output).Decode(&message); err != nil {
			t.Fatal(err)
		}
		if !message.Done || message.Task == nil || message.Task.ID != wantTask.ID || message.Error != "" {
			t.Fatalf("unexpected message: %#v", message)
		}
	})

	t.Run("error", func(t *testing.T) {
		var output bytes.Buffer
		if err := encodeResult(&output, core.Task{}, errors.New("reset failed")); err != nil {
			t.Fatal(err)
		}
		var message btruntime.RuntimeMessage
		if err := json.NewDecoder(&output).Decode(&message); err != nil {
			t.Fatal(err)
		}
		if !message.Done || message.Task != nil || message.Error != "reset failed" {
			t.Fatalf("unexpected message: %#v", message)
		}
	})
}

func TestEncodeResultPropagatesWriterError(t *testing.T) {
	err := encodeResult(failingWriter{}, core.Task{}, nil)
	if err == nil || err.Error() != "write failed" {
		t.Fatalf("encodeResult error = %v", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
