package ui

import (
	"fmt"
	"log/slog"

	"ghost-downloader-go-win32/internal/core"
)

type taskNotificationKind int

const (
	taskNotificationInfo taskNotificationKind = iota
	taskNotificationError
)

type taskNotification struct {
	Kind    taskNotificationKind
	Title   string
	Message string
}

type taskNotificationTracker struct {
	statusByID map[string]core.TaskStatus
}

func newTaskNotificationTracker(initial []core.TaskSnapshot) *taskNotificationTracker {
	tracker := &taskNotificationTracker{statusByID: make(map[string]core.TaskStatus, len(initial))}
	for _, task := range initial {
		tracker.statusByID[task.ID] = task.Status
	}
	return tracker
}

func (t *taskNotificationTracker) Update(tasks []core.TaskSnapshot) []taskNotification {
	if t == nil {
		return nil
	}
	current := make(map[string]core.TaskStatus, len(tasks))
	var notifications []taskNotification
	for _, task := range tasks {
		previous, seen := t.statusByID[task.ID]
		current[task.ID] = task.Status
		if !seen || previous == task.Status || !isTerminalNotificationStatus(task.Status) {
			continue
		}
		switch task.Status {
		case core.StatusCompleted:
			notifications = append(notifications, taskNotification{
				Kind:    taskNotificationInfo,
				Title:   "下载完成",
				Message: task.Title,
			})
		case core.StatusFailed:
			message := task.Title
			if task.Error != "" {
				message = fmt.Sprintf("%s\r\n%s", task.Title, task.Error)
			}
			notifications = append(notifications, taskNotification{
				Kind:    taskNotificationError,
				Title:   "下载失败",
				Message: message,
			})
		}
	}
	t.statusByID = current
	return notifications
}

func isTerminalNotificationStatus(status core.TaskStatus) bool {
	return status == core.StatusCompleted || status == core.StatusFailed
}

func showTaskNotification(tray *trayController, notification taskNotification) {
	if tray == nil || tray.notifyIcon == nil {
		return
	}
	var err error
	switch notification.Kind {
	case taskNotificationError:
		err = tray.notifyIcon.ShowError(notification.Title, notification.Message)
	default:
		err = tray.notifyIcon.ShowInfo(notification.Title, notification.Message)
	}
	if err != nil {
		slog.Warn("show task notification failed", "error", err)
	}
}
