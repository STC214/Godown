package ui

import (
	"sync"

	"github.com/lxn/walk"
)

type ApplicationDispatcher struct {
	mu         sync.RWMutex
	mainWindow *walk.MainWindow
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
