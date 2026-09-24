package storage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ghost-downloader-go-win32/internal/core"
)

func TestJSONTaskStoreProtectsAndMigratesFTPPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	store := NewJSONTaskStore(path)
	task := ftpTaskWithPassword("plain-secret")
	if err := store.Save([]core.Task{task}); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("plain-secret")) || !bytes.Contains(stored, []byte(protectedSecretPrefix)) {
		t.Fatalf("FTP password was not protected at rest: %s", stored)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Stage.State["password"] != "plain-secret" {
		t.Fatalf("FTP password round trip failed: %#v", loaded)
	}

	legacy := []byte(`[{"id":"legacy","packId":"ftp","stage":{"state":{"password":"legacy-secret"}}}]`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded[0].Stage.State["password"] != "legacy-secret" {
		t.Fatalf("legacy password unavailable: %#v", loaded[0].Stage.State)
	}
	migrated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(migrated, []byte("legacy-secret")) || !bytes.Contains(migrated, []byte(protectedSecretPrefix)) {
		t.Fatalf("legacy FTP password was not migrated: %s", migrated)
	}
}

func TestTaskStoreProtectsPasswordThatLooksLikeEnvelope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	store := NewJSONTaskStore(path)
	password := protectedSecretPrefix + "literal-password"
	if err := store.Save([]core.Task{ftpTaskWithPassword(password)}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Stage.State["password"] != password {
		t.Fatalf("envelope-like password round trip failed: %#v", loaded)
	}
}

func TestSQLiteTaskStoreProtectsFTPPassword(t *testing.T) {
	store, err := NewSQLiteTaskStore(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Save([]core.Task{ftpTaskWithPassword("database-secret")}); err != nil {
		t.Fatal(err)
	}
	var payload []byte
	if err := store.db.QueryRow(`SELECT payload_json FROM tasks`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, []byte("database-secret")) || !bytes.Contains(payload, []byte(protectedSecretPrefix)) {
		t.Fatalf("SQLite FTP password was not protected: %s", payload)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Stage.State["password"] != "database-secret" {
		t.Fatalf("SQLite FTP password round trip failed: %#v", loaded)
	}
}

func TestSQLiteTaskStoreMigratesLegacyFTPPassword(t *testing.T) {
	store, err := NewSQLiteTaskStore(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	task := ftpTaskWithPassword("legacy-database-secret")
	payload, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO tasks (id, pack_id, title, status, created_at, payload_json, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, task.ID, task.PackID, task.Title, string(task.Status), task.CreatedAt.UnixMilli(), payload, 0); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Stage.State["password"] != "legacy-database-secret" {
		t.Fatalf("legacy SQLite password unavailable: %#v", loaded)
	}
	if err := store.db.QueryRow(`SELECT payload_json FROM tasks`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, []byte("legacy-database-secret")) || !bytes.Contains(payload, []byte(protectedSecretPrefix)) {
		t.Fatalf("legacy SQLite password was not migrated: %s", payload)
	}
}

func ftpTaskWithPassword(password string) core.Task {
	task := core.NewTask("ftp", "file.bin", "ftp://fixture/file.bin", ".", 1, core.NewStage("ftp", "ftp://fixture/file.bin", 1, 1, 0, true, nil, ""))
	task.Stage.State = map[string]string{"password": password}
	return task
}
