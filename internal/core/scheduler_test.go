package core

import (
	"context"
	"errors"
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

type failingLoadStore struct {
	saves int
}

func (s *failingLoadStore) Load() ([]Task, error) {
	return nil, errors.New("fixture: task credentials could not be decrypted")
}

func (s *failingLoadStore) Save([]Task) error {
	s.saves++
	return nil
}

func TestFailedLoadNeverOverwritesStore(t *testing.T) {
	store := &failingLoadStore{}
	scheduler := NewScheduler(NewRegistry(), store, 1)
	if err := scheduler.Load(); err == nil {
		t.Fatal("expected load error")
	}
	scheduler.StopAll()
	if store.saves != 0 {
		t.Fatalf("failed load overwrote existing store: saves=%d", store.saves)
	}
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

type editableWorker struct {
	started chan Task
	done    chan string
}

type blockingResetterWorker struct {
	resetEntered chan struct{}
	releaseReset chan struct{}
	started      chan struct{}
}

func (w *blockingResetterWorker) Run(ctx context.Context, task Task, report func(ProgressUpdate)) error {
	w.started <- struct{}{}
	<-ctx.Done()
	return ctx.Err()
}

func (w *blockingResetterWorker) ResetTask(task Task) (Task, error) {
	close(w.resetEntered)
	<-w.releaseReset
	task.Stage.State["reset"] = "saved"
	return task, nil
}

func (w *editableWorker) Run(ctx context.Context, task Task, report func(ProgressUpdate)) error {
	w.started <- task
	<-ctx.Done()
	w.done <- task.ID
	return ctx.Err()
}

func (w *countingWorker) Run(ctx context.Context, task Task, report func(ProgressUpdate)) error {
	w.started <- task.ID
	<-ctx.Done()
	w.done <- task.ID
	return ctx.Err()
}

func TestSchedulerEditTaskRestartsActiveWorkerWithLatestState(t *testing.T) {
	registry := NewRegistry()
	worker := &editableWorker{started: make(chan Task, 2), done: make(chan string, 2)}
	registry.Register("fake", worker)
	scheduler := NewScheduler(registry, &memoryTaskStore{}, 1)
	defer scheduler.StopAll()
	task := NewTask("fake", "example.bin", "https://example.test/example.bin", t.TempDir(), 10, NewStage("fake", "https://example.test/example.bin", 10, 1, 0, true, nil, ""))
	task.Stage.State = map[string]string{"selection": "old"}
	scheduler.Add(task)
	select {
	case <-worker.started:
	case <-time.After(2 * time.Second):
		t.Fatal("initial worker did not start")
	}
	if err := scheduler.EditTask(task.ID, func(current Task) (Task, error) {
		current.Stage.State["selection"] = "new"
		current.FileSize = 20
		current.Stage.FileSize = 20
		return current, nil
	}); err != nil {
		t.Fatal(err)
	}
	var restarted Task
	select {
	case restarted = <-worker.started:
	case <-time.After(2 * time.Second):
		t.Fatal("edited worker did not restart")
	}
	if restarted.Stage.State["selection"] != "new" || restarted.FileSize != 20 {
		t.Fatalf("worker received stale task: %#v", restarted)
	}
}

func TestSchedulerEditTaskSerializesConcurrentEditors(t *testing.T) {
	task := NewTask("fake", "example.bin", "https://example.test/example.bin", t.TempDir(), 10, NewStage("fake", "https://example.test/example.bin", 10, 1, 0, true, nil, ""))
	task.Status = StatusPaused
	task.Stage.Status = StatusPaused
	task.Stage.State = map[string]string{}
	scheduler := NewScheduler(NewRegistry(), &memoryTaskStore{tasks: []Task{task}}, 1)
	if err := scheduler.Load(); err != nil {
		t.Fatal(err)
	}

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- scheduler.EditTask(task.ID, func(current Task) (Task, error) {
			close(firstEntered)
			<-releaseFirst
			current.Stage.State["first"] = "saved"
			return current, nil
		})
	}()
	<-firstEntered

	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- scheduler.EditTask(task.ID, func(current Task) (Task, error) {
			close(secondEntered)
			current.Stage.State["second"] = current.Stage.State["first"]
			return current, nil
		})
	}()
	select {
	case <-secondEntered:
		t.Fatal("second editor entered before first editor completed")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	updated, ok := scheduler.Task(task.ID)
	if !ok || updated.Stage.State["first"] != "saved" || updated.Stage.State["second"] != "saved" {
		t.Fatalf("concurrent edits were not composed: %#v", updated.Stage.State)
	}
}

func TestSchedulerEditTaskSerializesWithRedownload(t *testing.T) {
	worker := &blockingResetterWorker{
		resetEntered: make(chan struct{}),
		releaseReset: make(chan struct{}),
		started:      make(chan struct{}, 2),
	}
	registry := NewRegistry()
	registry.Register("resettable", worker)
	task := NewTask("resettable", "example.bin", "fixture://example.bin", t.TempDir(), 10, NewStage("resettable", "fixture://example.bin", 10, 1, 0, true, nil, ""))
	task.Status = StatusPaused
	task.Stage.Status = StatusPaused
	task.Stage.State = map[string]string{}
	scheduler := NewScheduler(registry, &memoryTaskStore{tasks: []Task{task}}, 1)
	if err := scheduler.Load(); err != nil {
		t.Fatal(err)
	}
	defer scheduler.StopAll()

	redownloadDone := make(chan error, 1)
	go func() { redownloadDone <- scheduler.Redownload(task.ID) }()
	<-worker.resetEntered

	editEntered := make(chan struct{})
	editDone := make(chan error, 1)
	go func() {
		editDone <- scheduler.EditTask(task.ID, func(current Task) (Task, error) {
			close(editEntered)
			if current.Stage.State["reset"] != "saved" {
				return Task{}, errors.New("edit observed stale pre-reset state")
			}
			current.Stage.State["edited"] = "saved"
			return current, nil
		})
	}()
	select {
	case <-editEntered:
		t.Fatal("task edit entered while redownload reset was active")
	case <-time.After(50 * time.Millisecond):
	}
	close(worker.releaseReset)
	if err := <-redownloadDone; err != nil {
		t.Fatal(err)
	}
	if err := <-editDone; err != nil {
		t.Fatal(err)
	}
	updated, ok := scheduler.Task(task.ID)
	if !ok || updated.Stage.State["reset"] != "saved" || updated.Stage.State["edited"] != "saved" {
		t.Fatalf("redownload and edit were not composed: %#v", updated.Stage.State)
	}
}

func TestSchedulerPauseAllRunsAfterActiveRedownload(t *testing.T) {
	worker := &blockingResetterWorker{
		resetEntered: make(chan struct{}),
		releaseReset: make(chan struct{}),
		started:      make(chan struct{}, 2),
	}
	registry := NewRegistry()
	registry.Register("resettable", worker)
	task := NewTask("resettable", "example.bin", "fixture://example.bin", t.TempDir(), 10, NewStage("resettable", "fixture://example.bin", 10, 1, 0, true, nil, ""))
	task.Status = StatusPaused
	task.Stage.Status = StatusPaused
	task.Stage.State = map[string]string{}
	scheduler := NewScheduler(registry, &memoryTaskStore{tasks: []Task{task}}, 1)
	if err := scheduler.Load(); err != nil {
		t.Fatal(err)
	}
	defer scheduler.StopAll()

	redownloadDone := make(chan error, 1)
	go func() { redownloadDone <- scheduler.Redownload(task.ID) }()
	<-worker.resetEntered
	pauseDone := make(chan struct{})
	go func() {
		scheduler.PauseAll()
		close(pauseDone)
	}()
	select {
	case <-pauseDone:
		t.Fatal("PauseAll bypassed an active redownload")
	case <-time.After(50 * time.Millisecond):
	}
	close(worker.releaseReset)
	if err := <-redownloadDone; err != nil {
		t.Fatal(err)
	}
	<-pauseDone
	waitUntil(t, func() bool {
		current, ok := scheduler.Task(task.ID)
		return ok && current.Status == StatusPaused
	}, "pause after redownload")
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

type stateAndSlotWorker struct {
	firstTaskID string
	started     chan string
	copyMutated chan struct{}
	releaseSlot chan struct{}
}

func (w *stateAndSlotWorker) Run(ctx context.Context, task Task, report func(ProgressUpdate)) error {
	w.started <- task.ID
	if task.ID == w.firstTaskID {
		task.Stage.State["direct-worker-mutation"] = "private"
		close(w.copyMutated)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.releaseSlot:
		}
		usesSlot := false
		stateDelta := map[string]string{"checkpoint": "saved"}
		report(ProgressUpdate{StageState: stateDelta, UsesSlot: &usesSlot})
		stateDelta["checkpoint"] = "mutated-after-report"
	}
	<-ctx.Done()
	return ctx.Err()
}

type delayedStopWorker struct {
	started  chan struct{}
	canceled chan struct{}
	release  chan struct{}
}

type resettingWorker struct {
	started chan Task
	done    chan struct{}
}

func (w *resettingWorker) Run(ctx context.Context, task Task, report func(ProgressUpdate)) error {
	w.started <- task
	<-ctx.Done()
	close(w.done)
	return ctx.Err()
}

func (w *resettingWorker) ResetTask(task Task) (Task, error) {
	state := cloneStringMap(task.Stage.State)
	state["runtime"] = "reset"
	task.Stage.State = state
	task.UsesSlot = true
	return task, nil
}

func (w *delayedStopWorker) Run(ctx context.Context, task Task, report func(ProgressUpdate)) error {
	close(w.started)
	<-ctx.Done()
	close(w.canceled)
	<-w.release
	report(ProgressUpdate{StageState: map[string]string{"afterCancel": "saved"}})
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

func TestSchedulerRedownloadUsesPackResetterBeforeRestart(t *testing.T) {
	registry := NewRegistry()
	worker := &resettingWorker{started: make(chan Task, 1), done: make(chan struct{})}
	registry.Register("resettable", worker)
	scheduler := NewScheduler(registry, nil, 1)
	defer scheduler.StopAll()

	task := NewTask("resettable", "fixture.bin", "fixture://download", t.TempDir(), 10, NewStage("resettable", "fixture://download", 10, 1, 0, false, nil, ""))
	task.Status = StatusCompleted
	task.Stage.Status = StatusCompleted
	task.UsesSlot = false
	task.Stage.State = map[string]string{"immutable": "kept", "runtime": "stale"}
	scheduler.Add(task)
	if err := scheduler.Redownload(task.ID); err != nil {
		t.Fatal(err)
	}
	started := <-worker.started
	if started.Stage.State["immutable"] != "kept" || started.Stage.State["runtime"] != "reset" || !started.UsesSlot {
		t.Fatalf("worker received unreset task: %#v", started)
	}
	if task.Stage.State["runtime"] != "stale" {
		t.Fatalf("scheduler/resetter mutated caller-owned task: %#v", task.Stage.State)
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

func TestSchedulerProgressUpdateOwnsStageStateAndReleasesSlot(t *testing.T) {
	registry := NewRegistry()
	first := NewTask(
		"fake",
		"first.bin",
		"https://example.invalid/first.bin",
		t.TempDir(),
		100,
		NewStage("fake", "https://example.invalid/first.bin", 100, 1, 3, true, nil, ""),
	)
	first.Stage.State = map[string]string{"base": "kept"}
	worker := &stateAndSlotWorker{
		firstTaskID: first.ID,
		started:     make(chan string, 3),
		copyMutated: make(chan struct{}),
		releaseSlot: make(chan struct{}),
	}
	registry.Register("fake", worker)
	scheduler := NewScheduler(registry, &memoryTaskStore{}, 1)
	defer scheduler.StopAll()

	scheduler.Add(first)
	waitForStarts(t, worker.started, 1)
	waitForChannel(t, worker.copyMutated, "worker task-copy mutation")
	first.Stage.State["caller-mutation"] = "private"

	scheduler.mu.Lock()
	storedState := cloneStringMap(scheduler.tasks[first.ID].Stage.State)
	scheduler.mu.Unlock()
	if storedState["base"] != "kept" || storedState["direct-worker-mutation"] != "" || storedState["caller-mutation"] != "" {
		t.Fatalf("scheduler state shared with caller or worker: %#v", storedState)
	}

	second := NewTask(
		"fake",
		"second.bin",
		"https://example.invalid/second.bin",
		t.TempDir(),
		100,
		NewStage("fake", "https://example.invalid/second.bin", 100, 1, 3, true, nil, ""),
	)
	scheduler.Add(second)
	if status := taskStatusByID(t, scheduler, second.ID); status != StatusWaiting {
		t.Fatalf("slot-using task should wait, got %s", status)
	}

	nonSlot := NewTask(
		"fake",
		"seeding.bin",
		"https://example.invalid/seeding.bin",
		t.TempDir(),
		100,
		NewStage("fake", "https://example.invalid/seeding.bin", 100, 1, 3, true, nil, ""),
	)
	nonSlot.UsesSlot = false
	scheduler.Add(nonSlot)
	if startedID := waitForStart(t, worker.started, "non-slot worker start"); startedID != nonSlot.ID {
		t.Fatalf("expected non-slot task %s to start, got %s", nonSlot.ID, startedID)
	}

	close(worker.releaseSlot)
	if startedID := waitForStart(t, worker.started, "waiting slot task start"); startedID != second.ID {
		t.Fatalf("expected waiting task %s to start after slot release, got %s", second.ID, startedID)
	}
	waitUntil(t, func() bool {
		scheduler.mu.Lock()
		defer scheduler.mu.Unlock()
		task := scheduler.tasks[first.ID]
		return task != nil && !task.UsesSlot && task.Stage.State["checkpoint"] == "saved"
	}, "stage state and slot update")

	scheduler.mu.Lock()
	storedState = cloneStringMap(scheduler.tasks[first.ID].Stage.State)
	runningSlots := scheduler.runningSlotCountLocked()
	scheduler.mu.Unlock()
	if storedState["base"] != "kept" || storedState["checkpoint"] != "saved" {
		t.Fatalf("stage state delta was not merged: %#v", storedState)
	}
	if storedState["checkpoint"] == "mutated-after-report" {
		t.Fatalf("scheduler retained worker-owned state map: %#v", storedState)
	}
	if runningSlots != 1 {
		t.Fatalf("expected one slot-using running task, got %d", runningSlots)
	}
}

func TestSchedulerTogglePausePausesWaitingTask(t *testing.T) {
	registry := NewRegistry()
	worker := &countingWorker{
		started: make(chan string, 2),
		done:    make(chan string, 2),
	}
	registry.Register("fake", worker)
	scheduler := NewScheduler(registry, nil, 1)
	defer scheduler.StopAll()

	first := NewTask("fake", "first.bin", "https://example.invalid/first.bin", t.TempDir(), 100, NewStage("fake", "https://example.invalid/first.bin", 100, 1, 3, true, nil, ""))
	second := NewTask("fake", "second.bin", "https://example.invalid/second.bin", t.TempDir(), 100, NewStage("fake", "https://example.invalid/second.bin", 100, 1, 3, true, nil, ""))
	scheduler.Add(first)
	waitForStarts(t, worker.started, 1)
	scheduler.Add(second)

	if err := scheduler.TogglePause(second.ID); err != nil {
		t.Fatalf("pause waiting task: %v", err)
	}
	if status := taskStatusByID(t, scheduler, second.ID); status != StatusPaused {
		t.Fatalf("waiting task should become paused, got %s", status)
	}
}

func TestSchedulerSeedingTaskCanPauseAndLoadsAsPaused(t *testing.T) {
	registry := NewRegistry()
	usesSlot := false
	worker := &reportingWorker{
		started: make(chan string, 1),
		update:  ProgressUpdate{Received: 10, FileSize: 10, Progress: 100, Status: StatusSeeding, UsesSlot: &usesSlot},
	}
	registry.Register("seed", worker)
	scheduler := NewScheduler(registry, nil, 1)
	task := NewTask("seed", "seed.bin", "fixture://seed", t.TempDir(), 10, NewStage("seed", "fixture://seed", 10, 1, 0, false, nil, ""))
	scheduler.Add(task)
	waitForStarts(t, worker.started, 1)
	waitUntil(t, func() bool { return taskStatusByID(t, scheduler, task.ID) == StatusSeeding }, "seeding status")
	if err := scheduler.TogglePause(task.ID); err != nil {
		t.Fatal(err)
	}
	if status := taskStatusByID(t, scheduler, task.ID); status != StatusPaused {
		t.Fatalf("seeding task did not pause: %s", status)
	}
	scheduler.StopAll()

	persisted := cloneTask(task)
	persisted.Status = StatusSeeding
	persisted.Stage.Status = StatusSeeding
	loaded := NewScheduler(NewRegistry(), &memoryTaskStore{tasks: []Task{persisted}}, 1)
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	if status := taskStatusByID(t, loaded, task.ID); status != StatusPaused {
		t.Fatalf("persisted seeding task did not load paused: %s", status)
	}
	loaded.StopAll()
}

func TestSchedulerStopAllWaitsForWorkersAndSavesFinalState(t *testing.T) {
	registry := NewRegistry()
	worker := &delayedStopWorker{
		started:  make(chan struct{}),
		canceled: make(chan struct{}),
		release:  make(chan struct{}),
	}
	registry.Register("fake", worker)
	store := &memoryTaskStore{}
	scheduler := NewScheduler(registry, store, 1)
	task := NewTask("fake", "example.bin", "https://example.invalid/example.bin", t.TempDir(), 100, NewStage("fake", "https://example.invalid/example.bin", 100, 1, 3, true, nil, ""))
	scheduler.Add(task)
	waitForChannel(t, worker.started, "worker start")

	stopped := make(chan struct{})
	go func() {
		scheduler.StopAll()
		close(stopped)
	}()
	waitForChannel(t, worker.canceled, "worker cancellation")
	select {
	case <-stopped:
		close(worker.release)
		t.Fatal("StopAll returned before the worker exited")
	default:
	}
	close(worker.release)
	waitForChannel(t, stopped, "scheduler stop")

	stored, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 {
		t.Fatalf("expected one saved task, got %d", len(stored))
	}
	if stored[0].Status != StatusPaused || stored[0].Stage.Status != StatusPaused {
		t.Fatalf("expected paused task in final save, got task=%s stage=%s", stored[0].Status, stored[0].Stage.Status)
	}
	if stored[0].Stage.State["afterCancel"] != "saved" {
		t.Fatalf("final worker state missing from save: %#v", stored[0].Stage.State)
	}
}

func TestTaskCleanupFilesRemovesOnlyBitTorrentOwnedPaths(t *testing.T) {
	base := t.TempDir()
	task := NewTask("bt", "bundle", "fixture.torrent", base, 10, NewStage("bt", "fixture.torrent", 10, 1, 0, false, nil, ""))
	output := filepath.Join(base, task.Title)
	checkpoint := filepath.Join(base, ".gd3_bt", task.ID)
	if err := os.MkdirAll(filepath.Join(output, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "nested", "file.bin"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(checkpoint, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkpoint, "resume.db"), []byte("state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output+".torrent", []byte("meta"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output+".part", []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(base, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := task.CleanupFiles(); err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{output, output + ".part", checkpoint, output + ".torrent"} {
		if _, err := os.Stat(removed); !os.IsNotExist(err) {
			t.Fatalf("owned path still exists: %s (err=%v)", removed, err)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated file was removed: %v", err)
	}

	unsafe := task
	unsafe.Title = ".."
	if err := unsafe.CleanupFiles(); err == nil {
		t.Fatal("unsafe cleanup target was accepted")
	}
}

func TestTaskCleanupFilesRemovesOnlyFTPDirectoryTaskRoot(t *testing.T) {
	base := t.TempDir()
	task := NewTask("ftp", "remote-tree", "ftp://fixture/tree/", base, 4, NewStage("ftp", "ftp://fixture/tree/", 4, 1, 0, true, nil, ""))
	task.Stage.State = map[string]string{"directory": "true"}
	output := task.OutputFile()
	if err := os.MkdirAll(filepath.Join(output, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, FTPDirectoryOwnerMarker), []byte(task.ID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "nested", "file.bin.part"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	completionRoot := filepath.Join(base, ".gd3_ftp", task.ID)
	if err := os.MkdirAll(completionRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(completionRoot, "file.complete"), []byte("digest"), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(base, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := task.CleanupFiles(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("FTP directory root still exists: %v", err)
	}
	if _, err := os.Stat(completionRoot); !os.IsNotExist(err) {
		t.Fatalf("FTP completion state still exists: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated file was removed: %v", err)
	}
	unsafe := task
	unsafe.Title = ".."
	if err := unsafe.CleanupFiles(); err == nil {
		t.Fatal("unsafe FTP directory cleanup target was accepted")
	}
}

func TestFTPDirectoryCleanupPreservesUnownedOutput(t *testing.T) {
	base := t.TempDir()
	task := NewTask("ftp", "existing", "ftp://fixture/existing/", base, 0, NewStage("ftp", "ftp://fixture/existing/", 0, 1, 0, true, nil, ""))
	task.Stage.State = map[string]string{"directory": "true"}
	output := task.OutputFile()
	if err := os.Mkdir(output, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(output, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := task.CleanupFiles(); err == nil {
		t.Fatal("unowned directory was removed")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unowned file was lost: %v", err)
	}
}

func TestSchedulerLoadsLegacyFTPDirectoryWithoutTouchingOldFiles(t *testing.T) {
	base := t.TempDir()
	task := NewTask("ftp", "tree", "ftp://fixture/tree/", base, 10, NewStage("ftp", "ftp://fixture/tree/", 10, 1, 0, true, nil, ""))
	task.Stage.State = map[string]string{"directory": "true"}
	task.Status, task.Stage.Status = StatusCompleted, StatusCompleted
	task.Received, task.Stage.Received = 10, 10
	task.Progress, task.Stage.Progress = 100, 100
	if err := os.Mkdir(task.OutputFile(), 0o755); err != nil {
		t.Fatal(err)
	}
	oldFile := filepath.Join(task.OutputFile(), "keep.bin")
	if err := os.WriteFile(oldFile, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &memoryTaskStore{tasks: []Task{task}}
	scheduler := NewScheduler(NewRegistry(), store, 1)
	if err := scheduler.Load(); err != nil {
		t.Fatal(err)
	}
	migrated, ok := scheduler.Task(task.ID)
	if !ok || migrated.OutputFile() == task.OutputFile() || migrated.Status != StatusPaused || migrated.Received != 0 {
		t.Fatalf("legacy task was not safely relocated: %#v", migrated)
	}
	if _, err := os.Stat(oldFile); err != nil {
		t.Fatalf("legacy output was changed: %v", err)
	}
	scheduler.StopAll()
	saved, err := store.Load()
	if err != nil || len(saved) != 1 || saved[0].OutputFile() != migrated.OutputFile() {
		t.Fatalf("relocated output was not persisted: %#v, %v", saved, err)
	}
	reloaded := NewScheduler(NewRegistry(), store, 1)
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	stable, ok := reloaded.Task(task.ID)
	if !ok || stable.OutputFile() != migrated.OutputFile() {
		t.Fatalf("relocated output changed after reload: %#v", stable)
	}
	reloaded.StopAll()
}

func TestDirectoryMigrationFailurePreservesWholeTaskStore(t *testing.T) {
	base := t.TempDir()
	first := NewTask("missing", "first.bin", "fixture://first", base, 1, NewStage("missing", "fixture://first", 1, 1, 0, false, nil, ""))
	second := NewTask("ftp", "tree", "ftp://fixture/tree/", base, 1, NewStage("ftp", "ftp://fixture/tree/", 1, 1, 0, true, nil, ""))
	second.Stage.State = map[string]string{"directory": "true"}
	for _, name := range []string{second.Title, "ftp-" + second.ID} {
		if err := os.Mkdir(filepath.Join(base, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	store := &memoryTaskStore{tasks: []Task{first, second}}
	scheduler := NewScheduler(NewRegistry(), store, 1)
	if err := scheduler.Load(); err == nil {
		t.Fatal("occupied migration output was accepted")
	}
	scheduler.StopAll()
	saved, err := store.Load()
	if err != nil || len(saved) != 2 {
		t.Fatalf("failed migration overwrote task store: %#v, %v", saved, err)
	}
}

func TestSchedulerEditTaskCallbackCanReadScheduler(t *testing.T) {
	scheduler := NewScheduler(NewRegistry(), nil, 1)
	defer scheduler.StopAll()
	task := NewTask("missing", "file.bin", "fixture://file", t.TempDir(), 1, NewStage("missing", "fixture://file", 1, 1, 0, false, nil, ""))
	task.Status, task.Stage.Status = StatusPaused, StatusPaused
	scheduler.Add(task)
	done := make(chan error, 1)
	go func() {
		done <- scheduler.EditTask(task.ID, func(current Task) (Task, error) {
			if _, ok := scheduler.Task(task.ID); !ok || len(scheduler.Snapshot()) != 1 {
				return Task{}, errors.New("task disappeared during edit")
			}
			return current, nil
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("edit callback blocked on scheduler lock")
	}
}

func TestSchedulerEditTaskCallbackCanRunSchedulerAction(t *testing.T) {
	scheduler := NewScheduler(NewRegistry(), nil, 1)
	defer scheduler.StopAll()
	task := NewTask("missing", "file.bin", "fixture://file", t.TempDir(), 1, NewStage("missing", "fixture://file", 1, 1, 0, false, nil, ""))
	task.Status, task.Stage.Status = StatusPaused, StatusPaused
	scheduler.Add(task)
	done := make(chan error, 1)
	go func() {
		done <- scheduler.EditTask(task.ID, func(current Task) (Task, error) {
			if err := scheduler.TogglePause(task.ID); err != nil {
				return Task{}, err
			}
			return current, nil
		})
	}()
	select {
	case err := <-done:
		if err == nil || err.Error() != "task changed during edit" {
			t.Fatalf("interleaved action was overwritten: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("edit callback blocked on scheduler action")
	}
}

func TestSchedulerEventBackpressureKeepsNewestStateAndCloses(t *testing.T) {
	scheduler := NewScheduler(NewRegistry(), nil, 1)
	for i := 0; i < cap(scheduler.events); i++ {
		scheduler.events <- Event{Kind: EventTasksChanged}
	}
	task := NewTask("missing", "latest.bin", "fixture://latest", t.TempDir(), 1, NewStage("missing", "fixture://latest", 1, 1, 0, false, nil, ""))
	scheduler.mu.Lock()
	scheduler.tasks[task.ID] = &task
	scheduler.order = append(scheduler.order, task.ID)
	scheduler.emitLocked()
	scheduler.mu.Unlock()

	var last Event
	for len(scheduler.events) > 0 {
		last = <-scheduler.events
	}
	if len(last.Tasks) != 1 || last.Tasks[0].ID != task.ID {
		t.Fatalf("newest event was dropped under backpressure: %#v", last)
	}
	scheduler.StopAll()
	select {
	case _, open := <-scheduler.Events():
		if open {
			t.Fatal("scheduler event channel remained open after StopAll")
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler event channel did not close")
	}
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

func waitForStart(t *testing.T, ch <-chan string, name string) string {
	t.Helper()
	select {
	case taskID := <-ch:
		return taskID
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
		return ""
	}
}

func taskStatusByID(t *testing.T, scheduler *Scheduler, taskID string) TaskStatus {
	t.Helper()
	for _, task := range scheduler.Snapshot() {
		if task.ID == taskID {
			return task.Status
		}
	}
	t.Fatalf("task %s not found", taskID)
	return ""
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
