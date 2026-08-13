package storage

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"

	"ghost-downloader-go-win32/internal/config"

	_ "modernc.org/sqlite"
)

type SQLiteConfigStore struct {
	db *sql.DB
}

func NewSQLiteConfigStore(path string) (*SQLiteConfigStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	store := &SQLiteConfigStore{db: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteConfigStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteConfigStore) LoadSettings(defaults config.Settings) (config.Settings, error) {
	var payload []byte
	err := s.db.QueryRow(`SELECT value_json FROM config WHERE key = ?`, "settings").Scan(&payload)
	if err == sql.ErrNoRows {
		return defaults, nil
	}
	if err != nil {
		return defaults, err
	}
	settings := defaults
	if err := json.Unmarshal(payload, &settings); err != nil {
		return defaults, err
	}
	return settings, nil
}

func (s *SQLiteConfigStore) SaveSettings(settings config.Settings) error {
	payload, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO config (key, value_json, updated_at)
		VALUES (?, ?, strftime('%s', 'now') * 1000)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at
	`, "settings", payload)
	return err
}

func (s *SQLiteConfigStore) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS config (
			key TEXT PRIMARY KEY,
			value_json BLOB NOT NULL,
			updated_at INTEGER NOT NULL
		);
	`)
	return err
}
