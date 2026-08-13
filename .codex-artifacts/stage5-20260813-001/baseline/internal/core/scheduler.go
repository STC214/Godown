package core

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"
)

type Scheduler struct {
	mu       sync.Mutex
	tasks    map[string]*Task
	order    []string
	running  map[string]*runningTask
	registry *registry
	store    TaskStore
	events   chan Event

	maxRunning int
	saveTimer  *time.Timer
}

type runningTask struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func NewScheduler(registry *registry, store TaskStore, maxRunning int) *Scheduler {
	if maxRunning <= 0 {
		maxRunning = 3
	}
	return &Scheduler{
		tasks:      make(map[string]*Task),
		running:    make(map[string]*runningTask),
		registry:   registry,
		store:      store,
		events:     make(chan Event, 16),
		maxRunning: maxRunning,
	}
}

func (s *Scheduler) Load() error {
	if s.store == nil {
		return nil
	}
	tasks, err := s.store.Load()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range tasks {
		task := tasks[i]
		if task.Status == StatusRunning {
			task.Status = StatusPaused
		}
		s.tasks[task.ID] = &task
		s.order = append(s.order, task.ID)
	}
	s.emitLocked()
	return nil
}

func (s *Scheduler) Events() <-chan Event {
	return s.events
}

func (s *Scheduler) SetMaxRunning(maxRunning int) {
	if maxRunning <= 0 {
		maxRunning = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxRunning = maxRunning
	s.enforceMaxRunningLocked()
	s.scheduleWaitingLocked()
	s.scheduleSaveLocked()
	s.emitLocked()
}

func (s *Scheduler) Add(task Task) {
	s.mu.Lock()
	s.tasks[task.ID] = &task
	s.order = append(s.order, task.ID)
	s.scheduleLocked(task.ID)
	s.scheduleSaveLocked()
	s.emitLocked()
	s.mu.Unlock()
}

func (s *Scheduler) TogglePause(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[taskID]
	if task == nil {
		return errors.New("task not found")
	}
	if task.Status == StatusRunning {
		if running := s.running[taskID]; running != nil {
			task.Status = StatusPaused
			task.Stage.Status = StatusPaused
			running.cancel()
			s.scheduleSaveLocked()
			s.emitLocked()
			return nil
		}
	}
	if task.Status == StatusCompleted {
		return errors.New("task completed")
	}
	task.Status = StatusWaiting
	task.Stage.Status = StatusWaiting
	s.scheduleLocked(taskID)
	s.scheduleSaveLocked()
	s.emitLocked()
	return nil
}

func (s *Scheduler) StartAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.order {
		task := s.tasks[id]
		if task == nil {
			continue
		}
		if task.Status == StatusPaused || task.Status == StatusWaiting {
			task.Status = StatusWaiting
			task.Stage.Status = StatusWaiting
		}
	}
	s.scheduleWaitingLocked()
	s.scheduleSaveLocked()
	s.emitLocked()
}

func (s *Scheduler) PauseAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, running := range s.running {
		if task := s.tasks[id]; task != nil {
			task.Status = StatusPaused
			task.Stage.Status = StatusPaused
			task.Speed = 0
			task.Stage.Speed = 0
		}
		running.cancel()
	}
	for _, task := range s.tasks {
		if task.Status == StatusWaiting {
			task.Status = StatusPaused
			task.Stage.Status = StatusPaused
			task.Speed = 0
			task.Stage.Speed = 0
		}
	}
	s.scheduleSaveLocked()
	s.emitLocked()
}

func (s *Scheduler) Remove(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[taskID]
	if task == nil {
		return errors.New("task not found")
	}
	if running := s.running[taskID]; running != nil {
		running.cancel()
		delete(s.running, taskID)
	}
	delete(s.tasks, taskID)
	for i, id := range s.order {
		if id == taskID {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	s.scheduleWaitingLocked()
	s.scheduleSaveLocked()
	s.emitLocked()
	return nil
}

func (s *Scheduler) Redownload(taskID string) error {
	s.mu.Lock()
	task := s.tasks[taskID]
	if task == nil {
		s.mu.Unlock()
		return errors.New("task not found")
	}
	taskCopy := *task
	var done <-chan struct{}
	if running := s.running[taskID]; running != nil {
		task.Status = StatusPaused
		task.Stage.Status = StatusPaused
		running.cancel()
		done = running.done
	}
	s.mu.Unlock()

	if done != nil {
		<-done
	}

	if err := taskCopy.CleanupFiles(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	task = s.tasks[taskID]
	if task == nil {
		return errors.New("task not found")
	}
	task.Status = StatusWaiting
	task.Received = 0
	task.Speed = 0
	task.Progress = 0
	task.Error = ""
	task.Stage.Status = StatusWaiting
	task.Stage.Received = 0
	task.Stage.Speed = 0
	task.Stage.Progress = 0
	task.Stage.Error = ""
	s.scheduleWaitingLocked()
	s.scheduleSaveLocked()
	s.emitLocked()
	return nil
}

func (s *Scheduler) Snapshot() []TaskSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *Scheduler) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, running := range s.running {
		running.cancel()
	}
	for _, task := range s.tasks {
		if task.Status == StatusRunning || task.Status == StatusWaiting {
			task.Status = StatusPaused
			task.Stage.Status = StatusPaused
			task.Speed = 0
			task.Stage.Speed = 0
		}
	}
	s.running = make(map[string]*runningTask)
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	s.saveLocked()
}

func (s *Scheduler) scheduleLocked(taskID string) {
	if len(s.running) >= s.maxRunning {
		return
	}
	task := s.tasks[taskID]
	if task == nil || task.Status == StatusRunning || task.Status == StatusCompleted {
		return
	}
	worker, ok := s.registry.Worker(task.PackID)
	if !ok {
		task.Status = StatusFailed
		task.Error = "worker not registered"
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.running[task.ID] = &runningTask{cancel: cancel, done: done}
	task.Status = StatusRunning
	task.Stage.Status = StatusRunning
	go s.runTask(ctx, task.ID, worker, done)
}

func (s *Scheduler) enforceMaxRunningLocked() {
	if len(s.running) <= s.maxRunning {
		return
	}
	kept := 0
	for _, id := range s.order {
		running := s.running[id]
		if running == nil {
			continue
		}
		if kept < s.maxRunning {
			kept++
			continue
		}
		if task := s.tasks[id]; task != nil {
			task.Status = StatusPaused
			task.Stage.Status = StatusPaused
			task.Speed = 0
			task.Stage.Speed = 0
		}
		running.cancel()
	}
}

func (s *Scheduler) scheduleWaitingLocked() {
	for _, id := range s.order {
		if len(s.running) >= s.maxRunning {
			break
		}
		task := s.tasks[id]
		if task != nil && task.Status == StatusWaiting {
			s.scheduleLocked(id)
		}
	}
}

func (s *Scheduler) runTask(ctx context.Context, taskID string, worker Worker, done chan<- struct{}) {
	defer close(done)

	report := func(update ProgressUpdate) {
		s.mu.Lock()
		defer s.mu.Unlock()
		task := s.tasks[taskID]
		if task == nil {
			return
		}
		task.Received = update.Received
		if update.FileSize > 0 {
			task.FileSize = update.FileSize
			task.Stage.FileSize = update.FileSize
		}
		task.Speed = update.Speed
		task.Progress = update.Progress
		task.Stage.Received = update.Received
		task.Stage.Speed = update.Speed
		task.Stage.Progress = update.Progress
		s.scheduleSaveLocked()
		s.emitLocked()
	}

	s.mu.Lock()
	task := s.tasks[taskID]
	var taskCopy Task
	if task != nil {
		taskCopy = *task
	}
	s.mu.Unlock()
	if task == nil {
		return
	}

	err := worker.Run(ctx, taskCopy, report)

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.running, taskID)
	task = s.tasks[taskID]
	if task == nil {
		return
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			if task.Status == StatusRunning {
				task.Status = StatusPaused
				task.Stage.Status = StatusPaused
			}
		} else {
			task.Status = StatusFailed
			task.Stage.Status = StatusFailed
			task.Error = err.Error()
			task.Stage.Error = err.Error()
			slog.Error("task failed", "taskID", taskID, "error", err)
		}
	} else {
		task.Status = StatusCompleted
		task.Stage.Status = StatusCompleted
		task.Progress = 100
		task.Stage.Progress = 100
		task.Speed = 0
		task.Stage.Speed = 0
	}
	s.scheduleWaitingLocked()
	s.scheduleSaveLocked()
	s.emitLocked()
}

func (s *Scheduler) snapshotLocked() []TaskSnapshot {
	result := make([]TaskSnapshot, 0, len(s.tasks))
	for _, id := range s.order {
		if task := s.tasks[id]; task != nil {
			result = append(result, snapshotOf(*task))
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

func (s *Scheduler) emitLocked() {
	event := Event{Kind: EventTasksChanged, Tasks: s.snapshotLocked()}
	select {
	case s.events <- event:
	default:
	}
}

func (s *Scheduler) scheduleSaveLocked() {
	if s.store == nil {
		return
	}
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	s.saveTimer = time.AfterFunc(500*time.Millisecond, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.saveLocked()
	})
}

func (s *Scheduler) saveLocked() {
	if s.store == nil {
		return
	}
	tasks := make([]Task, 0, len(s.tasks))
	for _, id := range s.order {
		if task := s.tasks[id]; task != nil {
			tasks = append(tasks, *task)
		}
	}
	if err := s.store.Save(tasks); err != nil {
		slog.Error("save tasks failed", "error", err)
	}
}
