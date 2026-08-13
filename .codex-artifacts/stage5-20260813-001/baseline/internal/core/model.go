package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type TaskStatus string

const (
	StatusWaiting   TaskStatus = "waiting"
	StatusRunning   TaskStatus = "running"
	StatusPaused    TaskStatus = "paused"
	StatusCompleted TaskStatus = "completed"
	StatusFailed    TaskStatus = "failed"
	StatusCanceled  TaskStatus = "canceled"
)

type Task struct {
	ID        string     `json:"id"`
	PackID    string     `json:"packId"`
	Title     string     `json:"title"`
	URL       string     `json:"url"`
	Status    TaskStatus `json:"status"`
	Path      string     `json:"path"`
	FileSize  int64      `json:"fileSize"`
	Received  int64      `json:"received"`
	Speed     int64      `json:"speed"`
	Progress  float64    `json:"progress"`
	CreatedAt time.Time  `json:"createdAt"`
	Error     string     `json:"error"`
	UsesSlot  bool       `json:"usesSlot"`
	Stage     Stage      `json:"stage"`
}

func NewTask(packID, title, rawURL, path string, fileSize int64, stage Stage) Task {
	return Task{
		ID:        newID("tsk"),
		PackID:    packID,
		Title:     title,
		URL:       rawURL,
		Status:    StatusWaiting,
		Path:      path,
		FileSize:  fileSize,
		CreatedAt: time.Now(),
		UsesSlot:  true,
		Stage:     stage,
	}
}

func (t Task) OutputFile() string {
	return filepath.Join(t.Path, t.Title)
}

func (t Task) CleanupFiles() error {
	outputFile := t.OutputFile()
	if err := removeIfExists(outputFile); err != nil {
		return err
	}
	return removeIfExists(outputFile + ".ghd")
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}

type Stage struct {
	ID            string            `json:"id"`
	Index         int               `json:"index"`
	Kind          string            `json:"kind"`
	Status        TaskStatus        `json:"status"`
	Progress      float64           `json:"progress"`
	Received      int64             `json:"received"`
	Speed         int64             `json:"speed"`
	Error         string            `json:"error"`
	URL           string            `json:"url"`
	FileSize      int64             `json:"fileSize"`
	Headers       map[string]string `json:"headers,omitempty"`
	ProxyURL      string            `json:"proxyUrl,omitempty"`
	BlockNum      int               `json:"blockNum"`
	MaxRetries    int               `json:"maxRetries"`
	SupportsRange bool              `json:"supportsRange"`
	State         map[string]string `json:"state,omitempty"`
}

func NewStage(kind, rawURL string, fileSize int64, blockNum int, maxRetries int, supportsRange bool, headers map[string]string, proxyURL string) Stage {
	if maxRetries < 0 {
		maxRetries = 0
	}
	return Stage{
		ID:            newID("stg"),
		Index:         1,
		Kind:          kind,
		Status:        StatusWaiting,
		URL:           rawURL,
		FileSize:      fileSize,
		BlockNum:      blockNum,
		MaxRetries:    maxRetries,
		SupportsRange: supportsRange,
		Headers:       headers,
		ProxyURL:      proxyURL,
	}
}

type TaskSnapshot struct {
	ID        string
	PackID    string
	Title     string
	URL       string
	Status    TaskStatus
	Path      string
	FileSize  int64
	Received  int64
	Speed     int64
	Progress  float64
	Error     string
	CreatedAt time.Time
}

type Worker interface {
	Run(ctx context.Context, task Task, report func(ProgressUpdate)) error
}

type ProgressUpdate struct {
	Received int64
	FileSize int64
	Speed    int64
	Progress float64
}

type TaskStore interface {
	Load() ([]Task, error)
	Save([]Task) error
}

type EventKind string

const (
	EventTasksChanged EventKind = "tasks_changed"
)

type Event struct {
	Kind  EventKind
	Tasks []TaskSnapshot
}

func snapshotOf(task Task) TaskSnapshot {
	return TaskSnapshot{
		ID:        task.ID,
		PackID:    task.PackID,
		Title:     task.Title,
		URL:       task.URL,
		Status:    task.Status,
		Path:      task.Path,
		FileSize:  task.FileSize,
		Received:  task.Received,
		Speed:     task.Speed,
		Progress:  task.Progress,
		Error:     task.Error,
		CreatedAt: task.CreatedAt,
	}
}

func newID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + "_" + hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

type registry struct {
	mu      sync.RWMutex
	workers map[string]Worker
}

func NewRegistry() *registry {
	return &registry{workers: make(map[string]Worker)}
}

func (r *registry) Register(packID string, worker Worker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workers[packID] = worker
}

func (r *registry) Worker(packID string) (Worker, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	worker, ok := r.workers[packID]
	return worker, ok
}
