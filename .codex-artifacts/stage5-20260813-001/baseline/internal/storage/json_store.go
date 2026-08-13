package storage

import (
	"encoding/json"
	"errors"
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
	return tasks, json.Unmarshal(data, &tasks)
}

func (s *JSONTaskStore) Save(tasks []core.Task) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
