package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"ghost-downloader-go-win32/internal/core"
)

type JSONTaskStore struct {
	path string
}

func NewJSONTaskStore(path string) *JSONTaskStore {
	return &JSONTaskStore{path: path}
}

func (s *JSONTaskStore) Load() ([]core.Task, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tasks []core.Task
	if len(data) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, err
	}
	needsMigration := false
	for index := range tasks {
		unprotected, migrate, err := unprotectTaskSecrets(tasks[index])
		if err != nil {
			return nil, err
		}
		tasks[index] = unprotected
		needsMigration = needsMigration || migrate
	}
	if needsMigration {
		if err := s.Save(tasks); err != nil {
			return nil, fmt.Errorf("migrate task secrets: %w", err)
		}
	}
	return tasks, nil
}

func (s *JSONTaskStore) Save(tasks []core.Task) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	storedTasks := make([]core.Task, len(tasks))
	for index, task := range tasks {
		protected, err := protectTaskSecrets(task)
		if err != nil {
			return err
		}
		storedTasks[index] = protected
	}
	data, err := json.MarshalIndent(storedTasks, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
