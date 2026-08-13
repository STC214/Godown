package storage

import (
	"path/filepath"
	"testing"
	"time"

	"ghost-downloader-go-win32/internal/core"
)

func TestSQLiteTaskStoreRoundTrip(t *testing.T) {
	store, err := NewSQLiteTaskStore(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := core.NewTask(
		"http",
		"file.bin",
		"https://example.com/file.bin",
		t.TempDir(),
		1024,
		core.NewStage("http", "https://example.com/file.bin", 1024, 4, 3, true, nil, ""),
	)
	task.CreatedAt = time.Unix(10, 0)
	task.Status = core.StatusPaused
	task.Progress = 25

	if err := store.Save([]core.Task{task}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected one task, got %d", len(loaded))
	}
	if loaded[0].ID != task.ID || loaded[0].Title != task.Title || loaded[0].Progress != task.Progress {
		t.Fatalf("loaded task mismatch: %#v", loaded[0])
	}
}
