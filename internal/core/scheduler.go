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

	maxRunning   int
	saveTimer    *time.Timer
	stopping     bool
	eventsClosed bool
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
		task := cloneTask(tasks[i])
		if task.Status == StatusRunning || task.Status == StatusSeeding {
			task.Status = StatusPaused
			task.Stage.Status = StatusPaused
			task.Speed = 0
			task.Stage.Speed = 0
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
	task = cloneTask(task)
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
	if task.Status == StatusRunning || task.Status == StatusSeeding {
		if running := s.running[taskID]; running != nil {
			task.Status = StatusPaused
			task.Stage.Status = StatusPaused
			task.Speed = 0
			task.Stage.Speed = 0
			running.cancel()
			s.scheduleSaveLocked()
			s.emitLocked()
			return nil
		}
	}
	if task.Status == StatusWaiting {
		task.Status = StatusPaused
		task.Stage.Status = StatusPaused
		task.Speed = 0
		task.Stage.Speed = 0
		s.scheduleSaveLocked()
		s.emitLocked()
		return nil
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

	s.mu.Lock()
	task = s.tasks[taskID]
	if task == nil {
		s.mu.Unlock()
		return errors.New("task not found")
	}
	taskCopy := cloneTask(*task)
	worker, _ := s.registry.Worker(task.PackID)
	s.mu.Unlock()

	if err := taskCopy.CleanupFiles(); err != nil {
		return err
	}
	if resetter, ok := worker.(TaskResetter); ok {
		reset, err := resetter.ResetTask(taskCopy)
		if err != nil {
			return err
		}
		taskCopy = cloneTask(reset)
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
	task.Detail = ""
	task.Error = ""
	task.Stage.Status = StatusWaiting
	task.Stage.Received = 0
	task.Stage.Speed = 0
	task.Stage.Progress = 0
	task.Stage.Error = ""
	task.Stage.State = cloneStringMap(taskCopy.Stage.State)
	task.FileSize = taskCopy.FileSize
	task.Stage.FileSize = taskCopy.Stage.FileSize
	task.UsesSlot = taskCopy.UsesSlot
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

// Task returns a detached copy suitable for task-specific edit dialogs.
func (s *Scheduler) Task(taskID string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[taskID]
	if task == nil {
		return Task{}, false
	}
	return cloneTask(*task), true
}

// EditTask stops an active worker, applies an edit to its latest checkpoint,
// persists the result, and resumes tasks that were not explicitly paused.
func (s *Scheduler) EditTask(taskID string, edit func(Task) (Task, error)) error {
	if edit == nil {
		return errors.New("task editor is nil")
	}
	s.mu.Lock()
	task := s.tasks[taskID]
	if task == nil {
		s.mu.Unlock()
		return errors.New("task not found")
	}
	resume := task.Status != StatusPaused && task.Status != StatusCanceled
	var done <-chan struct{}
	if running := s.running[taskID]; running != nil {
		task.Status = StatusPaused
		task.Stage.Status = StatusPaused
		task.Speed = 0
		task.Stage.Speed = 0
		running.cancel()
		done = running.done
		s.emitLocked()
	}
	s.mu.Unlock()

	if done != nil {
		<-done
	}

	s.mu.Lock()
	task = s.tasks[taskID]
	if task == nil {
		s.mu.Unlock()
		return errors.New("task not found")
	}
	original := cloneTask(*task)
	s.mu.Unlock()

	edited, err := edit(original)
	if err != nil {
		s.mu.Lock()
		if current := s.tasks[taskID]; current != nil && resume {
			current.Status = StatusWaiting
			current.Stage.Status = StatusWaiting
			s.scheduleLocked(taskID)
			s.scheduleSaveLocked()
			s.emitLocked()
		}
		s.mu.Unlock()
		return err
	}
	edited.ID = original.ID
	edited.CreatedAt = original.CreatedAt
	edited.PackID = original.PackID
	edited.Speed = 0
	edited.Stage.Speed = 0
	edited.Error = ""
	edited.Stage.Error = ""
	if resume {
		edited.Status = StatusWaiting
		edited.Stage.Status = StatusWaiting
	} else {
		edited.Status = StatusPaused
		edited.Stage.Status = StatusPaused
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tasks[taskID] == nil {
		return errors.New("task not found")
	}
	cloned := cloneTask(edited)
	s.tasks[taskID] = &cloned
	if resume {
		s.scheduleLocked(taskID)
	}
	s.scheduleSaveLocked()
	s.emitLocked()
	return nil
}

func (s *Scheduler) StopAll() {
	s.mu.Lock()
	s.stopping = true
	runningTasks := make([]*runningTask, 0, len(s.running))
	for _, running := range s.running {
		runningTasks = append(runningTasks, running)
		running.cancel()
	}
	for _, task := range s.tasks {
		if task.Status == StatusRunning || task.Status == StatusSeeding || task.Status == StatusWaiting {
			task.Status = StatusPaused
			task.Stage.Status = StatusPaused
			task.Speed = 0
			task.Stage.Speed = 0
		}
	}
	if s.saveTimer != nil {
		s.saveTimer.Stop()
		s.saveTimer = nil
	}
	s.mu.Unlock()

	for _, running := range runningTasks {
		<-running.done
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveTimer != nil {
		s.saveTimer.Stop()
		s.saveTimer = nil
	}
	s.saveLocked()
	if !s.eventsClosed {
		close(s.events)
		s.eventsClosed = true
	}
}

func (s *Scheduler) scheduleLocked(taskID string) {
	if s.stopping {
		return
	}
	task := s.tasks[taskID]
	if task == nil || s.running[taskID] != nil || task.Status == StatusRunning || task.Status == StatusSeeding || task.Status == StatusCompleted {
		return
	}
	if task.UsesSlot && s.runningSlotCountLocked() >= s.maxRunning {
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
	if s.runningSlotCountLocked() <= s.maxRunning {
		return
	}
	kept := 0
	for _, id := range s.order {
		running := s.running[id]
		if running == nil {
			continue
		}
		task := s.tasks[id]
		if task == nil || !task.UsesSlot {
			continue
		}
		if kept < s.maxRunning {
			kept++
			continue
		}
		task.Status = StatusPaused
		task.Stage.Status = StatusPaused
		task.Speed = 0
		task.Stage.Speed = 0
		running.cancel()
	}
}

func (s *Scheduler) scheduleWaitingLocked() {
	for _, id := range s.order {
		task := s.tasks[id]
		if task != nil && task.Status == StatusWaiting {
			s.scheduleLocked(id)
		}
	}
}

func (s *Scheduler) runningSlotCountLocked() int {
	count := 0
	for id := range s.running {
		if task := s.tasks[id]; task != nil && task.UsesSlot {
			count++
		}
	}
	return count
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
		if update.Detail != "" {
			task.Detail = update.Detail
		}
		if update.Status != "" && (task.Status == StatusRunning || task.Status == StatusSeeding) {
			task.Status = update.Status
			task.Stage.Status = update.Status
		}
		task.Stage.Received = update.Received
		task.Stage.Speed = update.Speed
		task.Stage.Progress = update.Progress
		if len(update.StageState) > 0 {
			if task.Stage.State == nil {
				task.Stage.State = make(map[string]string, len(update.StageState))
			}
			for key, value := range update.StageState {
				task.Stage.State[key] = value
			}
		}
		if update.UsesSlot != nil {
			previous := task.UsesSlot
			task.UsesSlot = *update.UsesSlot
			if previous && !task.UsesSlot {
				s.scheduleWaitingLocked()
			} else if !previous && task.UsesSlot {
				s.enforceMaxRunningLocked()
			}
		}
		s.scheduleSaveLocked()
		s.emitLocked()
	}

	s.mu.Lock()
	task := s.tasks[taskID]
	var taskCopy Task
	if task != nil {
		taskCopy = cloneTask(*task)
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
			if task.Status == StatusRunning || task.Status == StatusSeeding {
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
	if s.eventsClosed {
		return
	}
	event := Event{Kind: EventTasksChanged, Tasks: s.snapshotLocked()}
	select {
	case s.events <- event:
	default:
		// Preserve the newest state transition. A stale progress event is less
		// useful than a terminal/paused event when the UI momentarily falls
		// behind.
		select {
		case <-s.events:
		default:
		}
		select {
		case s.events <- event:
		default:
		}
	}
}

func (s *Scheduler) scheduleSaveLocked() {
	if s.store == nil || s.stopping {
		return
	}
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	s.saveTimer = time.AfterFunc(500*time.Millisecond, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.stopping {
			return
		}
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
			tasks = append(tasks, cloneTask(*task))
		}
	}
	if err := s.store.Save(tasks); err != nil {
		slog.Error("save tasks failed", "error", err)
	}
}
