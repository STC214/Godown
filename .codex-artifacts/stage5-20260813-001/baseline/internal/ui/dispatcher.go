package ui

import "github.com/lxn/walk"

type ApplicationDispatcher struct {
	mainWindow *walk.MainWindow
}

func NewApplicationDispatcher() *ApplicationDispatcher {
	return &ApplicationDispatcher{}
}

func (d *ApplicationDispatcher) SetMainWindow(mainWindow *walk.MainWindow) {
	d.mainWindow = mainWindow
}

func (d *ApplicationDispatcher) Post(fn func()) {
	if d.mainWindow == nil {
		return
	}
	d.mainWindow.Synchronize(fn)
}

func (d *ApplicationDispatcher) Clear() {
	d.mainWindow = nil
}
