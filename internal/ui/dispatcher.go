package ui

import (
	"sync"

	"github.com/lxn/walk"
)

type ApplicationDispatcher struct {
	mu         sync.RWMutex
	mainWindow *walk.MainWindow
}

type serialExecutor struct {
	mu     sync.Mutex
	ready  *sync.Cond
	queue  []func()
	closed bool
	done   chan struct{}
}

func newSerialExecutor() *serialExecutor {
	executor := &serialExecutor{done: make(chan struct{})}
	executor.ready = sync.NewCond(&executor.mu)
	go executor.run()
	return executor
}

func (e *serialExecutor) Submit(action func()) bool {
	if action == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return false
	}
	e.queue = append(e.queue, action)
	e.ready.Signal()
	return true
}

func (e *serialExecutor) CloseAndWait() {
	e.mu.Lock()
	if !e.closed {
		e.closed = true
		e.ready.Broadcast()
	}
	e.mu.Unlock()
	<-e.done
}

func (e *serialExecutor) run() {
	defer close(e.done)
	for {
		e.mu.Lock()
		for len(e.queue) == 0 && !e.closed {
			e.ready.Wait()
		}
		if len(e.queue) == 0 {
			e.mu.Unlock()
			return
		}
		action := e.queue[0]
		e.queue[0] = nil
		e.queue = e.queue[1:]
		e.mu.Unlock()
		action()
	}
}

func NewApplicationDispatcher() *ApplicationDispatcher {
	return &ApplicationDispatcher{}
}

func (d *ApplicationDispatcher) SetMainWindow(mainWindow *walk.MainWindow) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mainWindow = mainWindow
}

func (d *ApplicationDispatcher) Post(fn func()) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.mainWindow == nil {
		return
	}
	d.mainWindow.Synchronize(fn)
}

func (d *ApplicationDispatcher) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mainWindow = nil
}
