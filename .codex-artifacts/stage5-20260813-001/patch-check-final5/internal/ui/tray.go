package ui

import (
	"fmt"
	"log/slog"
	"os/exec"

	"ghost-downloader-go-win32/internal/config"
	"ghost-downloader-go-win32/internal/core"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

type trayController struct {
	mainWindow *walk.MainWindow
	notifyIcon *walk.NotifyIcon
	exiting    bool
}

func installTray(mainWindow *walk.MainWindow, paths config.Paths, scheduler *core.Scheduler, statusLabel *walk.Label) (*trayController, error) {
	if mainWindow == nil {
		return nil, nil
	}
	notifyIcon, err := walk.NewNotifyIcon(mainWindow)
	if err != nil {
		return nil, err
	}

	controller := &trayController{
		mainWindow: mainWindow,
		notifyIcon: notifyIcon,
	}
	if icon := mainWindow.Icon(); icon != nil {
		if err := notifyIcon.SetIcon(icon); err != nil {
			slog.Warn("set tray icon failed", "error", err)
		}
	}
	if err := notifyIcon.SetToolTip("Ghost Downloader Go"); err != nil {
		notifyIcon.Dispose()
		return nil, err
	}

	addTrayAction := func(text string, handler func()) error {
		action := walk.NewAction()
		if err := action.SetText(text); err != nil {
			return err
		}
		action.Triggered().Attach(handler)
		return notifyIcon.ContextMenu().Actions().Add(action)
	}
	if err := addTrayAction("Show", controller.showMainWindow); err != nil {
		notifyIcon.Dispose()
		return nil, err
	}
	if err := addTrayAction("Open Downloads", func() {
		if err := exec.Command("explorer.exe", paths.DownloadDir).Start(); err != nil {
			setStatus(statusLabel, "Open downloads failed: "+err.Error())
		}
	}); err != nil {
		notifyIcon.Dispose()
		return nil, err
	}
	if scheduler != nil {
		if err := addTrayAction("Start All", func() {
			scheduler.StartAll()
			setStatus(statusLabel, "All paused tasks queued.")
		}); err != nil {
			notifyIcon.Dispose()
			return nil, err
		}
		if err := addTrayAction("Pause All", func() {
			scheduler.PauseAll()
			setStatus(statusLabel, "All active tasks paused.")
		}); err != nil {
			notifyIcon.Dispose()
			return nil, err
		}
	}
	if err := notifyIcon.ContextMenu().Actions().Add(walk.NewSeparatorAction()); err != nil {
		notifyIcon.Dispose()
		return nil, err
	}
	if err := addTrayAction("Exit", func() {
		controller.exiting = true
		walk.App().Exit(0)
	}); err != nil {
		notifyIcon.Dispose()
		return nil, err
	}

	notifyIcon.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			controller.showMainWindow()
		}
	})
	mainWindow.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		if controller.exiting {
			return
		}
		*canceled = true
		mainWindow.Hide()
		setStatus(statusLabel, "Still running in the system tray.")
	})
	mainWindow.SizeChanged().Attach(func() {
		if controller.exiting || !win.IsIconic(mainWindow.Handle()) {
			return
		}
		mainWindow.Hide()
		setStatus(statusLabel, "Still running in the system tray.")
	})

	if err := notifyIcon.SetVisible(true); err != nil {
		notifyIcon.Dispose()
		return nil, err
	}
	return controller, nil
}

func (t *trayController) showMainWindow() {
	if t == nil || t.mainWindow == nil {
		return
	}
	t.mainWindow.Show()
	win.ShowWindow(t.mainWindow.Handle(), win.SW_RESTORE)
	_ = win.SetForegroundWindow(t.mainWindow.Handle())
}

func (t *trayController) dispose() {
	if t == nil || t.notifyIcon == nil {
		return
	}
	if err := t.notifyIcon.SetVisible(false); err != nil {
		slog.Warn("hide tray icon failed", "error", err)
	}
	if err := t.notifyIcon.Dispose(); err != nil {
		slog.Warn("dispose tray icon failed", "error", err)
	}
	t.notifyIcon = nil
}

func setStatus(label *walk.Label, text string) {
	if label != nil {
		label.SetText(text)
		return
	}
	if text != "" {
		slog.Info(fmt.Sprintf("ui status: %s", text))
	}
}
