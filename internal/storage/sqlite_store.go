package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ghost-downloader-go-win32/internal/core"

	_ "modernc.org/sqlite"
)

type SQLiteTaskStore struct {
	path string
	db   *sql.DB
}

func NewSQLiteTaskStore(path string) (*SQLiteTaskStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	store := &SQLiteTaskStore{path: path, db: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteTaskStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteTaskStore) Load() ([]core.Task, error) {
	rows, err := s.db.Query(`SELECT payload_json FROM tasks ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []core.Task
	needsMigration := false
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var task core.Task
		if err := json.Unmarshal(payload, &task); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("decode task payload: %w", err)
		}
		task, migrate, err := unprotectTaskSecrets(task)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		needsMigration = needsMigration || migrate
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if needsMigration {
		if err := s.Save(tasks); err != nil {
			return nil, fmt.Errorf("migrate task secrets: %w", err)
		}
	}
	return tasks, nil
}

func (s *SQLiteTaskStore) Save(tasks []core.Task) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM tasks`); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`
		INSERT INTO tasks (id, pack_id, title, status, created_at, payload_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UnixMilli()
	for _, task := range tasks {
		storedTask, err := protectTaskSecrets(task)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(storedTask)
		if err != nil {
			return err
		}
		if _, err := stmt.Exec(
			task.ID,
			task.PackID,
			task.Title,
			string(task.Status),
			task.CreatedAt.UnixMilli(),
			payload,
			now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteTaskStore) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			pack_id TEXT NOT NULL,
			title TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			payload_json BLOB NOT NULL,
			updated_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at);
		CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
	`)
	return err
}
