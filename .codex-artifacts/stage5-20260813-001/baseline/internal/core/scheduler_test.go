package core

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type memoryTaskStore struct {
	mu    sync.Mutex
	tasks []Task
}

func (s *memoryTaskStore) Load() ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks := make([]Task, len(s.tasks))
	copy(tasks, s.tasks)
	return tasks, nil
}

func (s *memoryTaskStore) Save(tasks []Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = make([]Task, len(tasks))
	copy(s.tasks, tasks)
	return nil
}

type blockingWorker struct {
	started chan struct{}
	done    chan struct{}
	once    sync.Once
}

func (w *blockingWorker) Run(ctx context.Context, task Task, report func(ProgressUpdate)) error {
	w.once.Do(func() {
		close(w.started)
	})
	report(ProgressUpdate{Received: 1, Progress: 10})
	<-ctx.Done()
	close(w.done)
	return ctx.Err()
}

type countingWorker struct {
	started chan string
	done    chan string
}

func (w *countingWorker) Run(ctx context.Context, task Task, report func(ProgressUpdate)) error {
	w.started <- task.ID
	<-ctx.Done()
	w.done <- task.ID
	return ctx.Err()
}

type reportingWorker struct {
	started chan string
	update  ProgressUpdate
}

func (w *reportingWorker) Run(ctx context.Context, task Task, report func(ProgressUpdate)) error {
	w.started <- task.ID
	report(w.update)
	<-ctx.Done()
	return ctx.Err()
}

func TestSchedulerRemoveRunningTaskDoesNotRestoreStaleTask(t *testing.T) {
	registry := NewRegistry()
	worker := &blockingWorker{
		started: make(chan struct{}),
		done:    make(chan struct{}),
	}
	registry.Register("fake", worker)
	scheduler := NewScheduler(registry, &memoryTaskStore{}, 1)
	task := NewTask(
		"fake",
		"example.bin",
		"https://example.invalid/example.bin",
		t.TempDir(),
		100,
		NewStage("fake", "https://example.invalid/example.bin", 100, 1, 3, true, nil, ""),
	)

	scheduler.Add(task)
	waitForChannel(t, worker.started, "worker start")

	if err := scheduler.Remove(task.ID); err != nil {
		t.Fatalf("remove running task: %v", err)
	}
	waitForChannel(t, worker.done, "worker cancellation")

	if got := scheduler.Snapshot(); len(got) != 0 {
		t.Fatalf("removed task came back in snapshot: %#v", got)
	}
}

func TestSchedulerSetMaxRunningPausesOverflowTasks(t *testing.T) {
	registry := NewRegistry()
	worker := &countingWorker{
		started: make(chan string, 3),
		done:    make(chan string, 3),
	}
	registry.Register("fake", worker)
	scheduler := NewScheduler(registry, &memoryTaskStore{}, 3)
	defer scheduler.StopAll()

	for i := 0; i < 3; i++ {
		task := NewTask(
			"fake",
			"example.bin",
			"https://example.invalid/example.bin",
			t.TempDir(),
			100,
			NewStage("fake", "https://example.invalid/example.bin", 100, 1, 3, true, nil, ""),
		)
		scheduler.Add(task)
	}
	waitForStarts(t, worker.started, 3)

	scheduler.SetMaxRunning(1)
	waitForStarts(t, worker.done, 2)

	var running, paused int
	for _, task := range scheduler.Snapshot() {
		switch task.Status {
		case StatusRunning:
			running++
		case StatusPaused:
			paused++
		}
	}
	if running != 1 || paused != 2 {
		t.Fatalf("expected 1 running and 2 paused tasks, got running=%d paused=%d", running, paused)
	}
}

func TestSchedulerRedownloadCompletedTaskCleansFilesAndQueues(t *testing.T) {
	registry := NewRegistry()
	worker := &countingWorker{
		started: make(chan string, 1),
		done:    make(chan string, 1),
	}
	registry.Register("fake", worker)
	scheduler := NewScheduler(registry, &memoryTaskStore{}, 1)
	defer scheduler.StopAll()

	dir := t.TempDir()
	task := NewTask(
		"fake",
		"example.bin",
		"https://example.invalid/example.bin",
		dir,
		100,
		NewStage("fake", "https://example.invalid/example.bin", 100, 1, 3, true, nil, ""),
	)
	task.Status = StatusCompleted
	task.Stage.Status = StatusCompleted
	task.Received = 100
	task.Stage.Received = 100
	task.Progress = 100
	task.Stage.Progress = 100
	writeTestFile(t, task.OutputFile())
	writeTestFile(t, task.OutputFile()+".ghd")
	scheduler.Add(task)

	if err := scheduler.Redownload(task.ID); err != nil {
		t.Fatalf("redownload completed task: %v", err)
	}
	waitForStarts(t, worker.started, 1)

	if _, err := os.Stat(task.OutputFile()); !os.IsNotExist(err) {
		t.Fatalf("expected output file removed, stat err=%v", err)
	}
	if _, err := os.Stat(task.OutputFile() + ".ghd"); !os.IsNotExist(err) {
		t.Fatalf("expected progress record removed, stat err=%v", err)
	}
	snapshot := scheduler.Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("expected one task, got %d", len(snapshot))
	}
	if snapshot[0].Status != StatusRunning || snapshot[0].Received != 0 || snapshot[0].Progress != 0 {
		t.Fatalf("expected restarted clean task, got %#v", snapshot[0])
	}
}

func TestSchedulerRedownloadRunningTaskWaitsForWorkerExit(t *testing.T) {
	registry := NewRegistry()
	worker := &countingWorker{
		started: make(chan string, 2),
		done:    make(chan string, 2),
	}
	registry.Register("fake", worker)
	scheduler := NewScheduler(registry, &memoryTaskStore{}, 1)
	defer scheduler.StopAll()

	dir := t.TempDir()
	task := NewTask(
		"fake",
		"example.bin",
		"https://example.invalid/example.bin",
		dir,
		100,
		NewStage("fake", "https://example.invalid/example.bin", 100, 1, 3, true, nil, ""),
	)
	writeTestFile(t, filepath.Join(dir, "example.bin"))
	scheduler.Add(task)
	waitForStarts(t, worker.started, 1)

	if err := scheduler.Redownload(task.ID); err != nil {
		t.Fatalf("redownload running task: %v", err)
	}
	waitForStarts(t, worker.done, 1)
	waitForStarts(t, worker.started, 1)

	if _, err := os.Stat(task.OutputFile()); !os.IsNotExist(err) {
		t.Fatalf("expected output file removed after old worker exit, stat err=%v", err)
	}
}

func TestSchedulerStartAllAfterPauseAllWaitsForOldWorkerExit(t *testing.T) {
	registry := NewRegistry()
	worker := &countingWorker{
		started: make(chan string, 2),
		done:    make(chan string, 2),
	}
	registry.Register("fake", worker)
	scheduler := NewScheduler(registry, &memoryTaskStore{}, 1)
	defer scheduler.StopAll()

	task := NewTask(
		"fake",
		"example.bin",
		"https://example.invalid/example.bin",
		t.TempDir(),
		100,
		NewStage("fake", "https://example.invalid/example.bin", 100, 1, 3, true, nil, ""),
	)
	scheduler.Add(task)
	waitForStarts(t, worker.started, 1)

	scheduler.PauseAll()
	scheduler.StartAll()
	waitForStarts(t, worker.done, 1)
	waitForStarts(t, worker.started, 1)

	var running, waiting, paused int
	for _, task := range scheduler.Snapshot() {
		switch task.Status {
		case StatusRunning:
			running++
		case StatusWaiting:
			waiting++
		case StatusPaused:
			paused++
		}
	}
	if running != 1 || waiting != 0 || paused != 0 {
		t.Fatalf("expected task restarted once, got running=%d waiting=%d paused=%d", running, waiting, paused)
	}
}

func TestSchedulerProgressUpdateCanUpdateFileSize(t *testing.T) {
	registry := NewRegistry()
	worker := &reportingWorker{started: make(chan string, 1), update: ProgressUpdate{Received: 123, FileSize: 456, Progress: 50}}
	registry.Register("fake", worker)
	scheduler := NewScheduler(registry, nil, 1)

	task := NewTask(
		"fake",
		"example.bin",
		"https://example.invalid/example.bin",
		t.TempDir(),
		1,
		NewStage("fake", "https://example.invalid/example.bin", 1, 1, 3, true, nil, ""),
	)
	scheduler.Add(task)
	waitForStarts(t, worker.started, 1)
	waitUntil(t, func() bool {
		snapshot := scheduler.Snapshot()
		return len(snapshot) == 1 && snapshot[0].FileSize == 456 && snapshot[0].Received == 123
	}, "file size update")
	scheduler.StopAll()
}

func waitForChannel(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatalf("write test file %s: %v", path, err)
	}
}

func waitForStarts(t *testing.T, ch <-chan string, count int) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for i := 0; i < count; i++ {
		select {
		case <-ch:
		case <-timeout:
			t.Fatalf("timed out waiting for %d worker events, got %d", count, i)
		}
	}
}

func waitUntil(t *testing.T, condition func() bool, name string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", name)
}
