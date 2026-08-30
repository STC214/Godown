package ui

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ghost-downloader-go-win32/internal/browserbridge"
	btdownload "ghost-downloader-go-win32/internal/btruntime"
	"ghost-downloader-go-win32/internal/config"
	"ghost-downloader-go-win32/internal/core"
	httpdownload "ghost-downloader-go-win32/internal/download/http"
	"ghost-downloader-go-win32/internal/ratelimit"
	updatecheck "ghost-downloader-go-win32/internal/update"
	appwin32 "ghost-downloader-go-win32/internal/win32"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type Options struct {
	AppVersion     string
	Paths          config.Paths
	Scheduler      *core.Scheduler
	Limiter        *ratelimit.Limiter
	Settings       config.Settings
	SaveSettings   func(config.Settings) error
	BrowserBridge  *browserbridge.Bridge
	ParseSource    func(context.Context, string, config.Settings, map[string]string) (core.Task, error)
	CheckForUpdate func(context.Context) (updatecheck.Release, error)
	InstallUpdate  func(context.Context, updatecheck.Release) error
}

func Run(options Options) error {
	var mainWindow *walk.MainWindow
	var statusLabel *walk.Label
	var urlEdit *walk.LineEdit
	var taskTable *walk.TableView
	var taskSearchEdit *walk.LineEdit
	var taskFilterBox *walk.ComboBox
	var taskDetail *walk.TextEdit
	var taskSummaryLabel *walk.Label
	var taskHeaderButtons [6]*walk.PushButton
	var selectedTaskID string
	var selectedTaskByID map[string]core.TaskSnapshot
	var applyingTaskFilter bool
	var themeStyle *windowThemeStyle

	app := NewApplicationDispatcher()
	defer app.Clear()
	currentSettings := options.Settings.Normalized(options.Paths)
	taskModel := newTaskTableModel()
	taskModel.SetDarkMode(appwin32.DarkModeEnabled(currentSettings.ThemeMode))
	applyTaskFilter := func(filter taskFilter) {
		applyingTaskFilter = true
		if taskFilterBox != nil {
			_ = taskFilterBox.SetCurrentIndex(int(filter))
		}
		applyingTaskFilter = false
		taskModel.SetFilter(filter)
		refreshTaskSelection(taskTable, taskModel, &selectedTaskID, taskDetail)
		updateTaskSummary(taskSummaryLabel, taskModel)
	}
	headerTitles := [...]string{"Name", "Status", "Progress", "Size", "Speed", "Folder"}
	updateTaskSortHeaders := func() {
		column, order := taskModel.SortState()
		for index, button := range taskHeaderButtons {
			if button == nil {
				continue
			}
			text := headerTitles[index]
			if index == column {
				if order == walk.SortAscending {
					text += " ▲"
				} else {
					text += " ▼"
				}
			}
			button.SetText(text)
		}
	}
	sortTaskColumn := func(column int) {
		if err := taskModel.ToggleSort(column); err != nil {
			statusLabel.SetText("Sort failed: " + err.Error())
			return
		}
		updateTaskSortHeaders()
		refreshTaskSelection(taskTable, taskModel, &selectedTaskID, taskDetail)
	}
	toggleSelectedTask := func() {
		if selectedTaskID == "" {
			statusLabel.SetText("Select a task first.")
			return
		}
		if err := options.Scheduler.TogglePause(selectedTaskID); err != nil {
			statusLabel.SetText("Toggle failed: " + err.Error())
		}
	}
	redownloadSelectedTask := func() {
		taskID := selectedTaskID
		if taskID == "" {
			statusLabel.SetText("Select a task first.")
			return
		}
		statusLabel.SetText("Restarting task...")
		go func() {
			err := options.Scheduler.Redownload(taskID)
			app.Post(func() {
				if err != nil {
					statusLabel.SetText("Redownload failed: " + err.Error())
					return
				}
				statusLabel.SetText("Task restarted.")
			})
		}()
	}
	removeSelectedTask := func() {
		if selectedTaskID == "" {
			statusLabel.SetText("Select a task first.")
			return
		}
		if err := options.Scheduler.Remove(selectedTaskID); err != nil {
			statusLabel.SetText("Remove failed: " + err.Error())
			return
		}
		selectedTaskID = ""
		statusLabel.SetText("Task removed.")
	}
	openSelectedTaskFolder := func() {
		target := currentSettings.DownloadDir
		if selectedTaskID != "" {
			if task, ok := selectedTaskByID[selectedTaskID]; ok {
				target = task.Path
			}
		}
		if err := exec.Command("explorer.exe", target).Start(); err != nil {
			statusLabel.SetText("Open folder failed: " + err.Error())
		}
	}
	openSelectedTaskFile := func() {
		if selectedTaskID == "" {
			statusLabel.SetText("Select a task first.")
			return
		}
		task, ok := selectedTaskByID[selectedTaskID]
		if !ok {
			statusLabel.SetText("Selected task is stale.")
			return
		}
		if task.Status != core.StatusCompleted && task.Status != core.StatusSeeding {
			statusLabel.SetText("File is not completed yet.")
			return
		}
		if err := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", filepath.Join(task.Path, task.Title)).Start(); err != nil {
			statusLabel.SetText("Open file failed: " + err.Error())
		}
	}
	openLogFile := func() {
		if err := exec.Command("explorer.exe", "/select,", options.Paths.LogFile).Start(); err != nil {
			statusLabel.SetText("Open log location failed: " + err.Error())
		}
	}
	checkForUpdates := func() {
		if options.CheckForUpdate == nil {
			statusLabel.SetText("Update checker is unavailable.")
			return
		}
		statusLabel.SetText("Checking for updates...")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			release, err := options.CheckForUpdate(ctx)
			app.Post(func() {
				if err != nil {
					statusLabel.SetText("Update check failed: " + err.Error())
					return
				}
				if !release.Newer {
					statusLabel.SetText("You are running the latest version (" + options.AppVersion + ").")
					return
				}
				if options.InstallUpdate != nil && release.HasPortableUpdate() {
					message := fmt.Sprintf("Ghost Downloader %s is available. Download, verify, replace the portable files, and restart?", release.Version)
					if walk.MsgBox(mainWindow, "Portable Update", message, walk.MsgBoxYesNo|walk.MsgBoxIconInformation) != walk.DlgCmdYes {
						statusLabel.SetText("Update available: " + release.Version)
						return
					}
					statusLabel.SetText("Downloading and verifying update " + release.Version + "...")
					go func(release updatecheck.Release) {
						ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
						defer cancel()
						err := options.InstallUpdate(ctx, release)
						app.Post(func() {
							if err != nil {
								statusLabel.SetText("Portable update failed: " + err.Error())
								return
							}
							statusLabel.SetText("Update verified. Closing to replace files...")
							walk.App().Exit(0)
						})
					}(release)
					return
				}
				message := fmt.Sprintf("Ghost Downloader %s is available. Open the release page?", release.Version)
				if walk.MsgBox(mainWindow, "Update Available", message, walk.MsgBoxYesNo|walk.MsgBoxIconInformation) == walk.DlgCmdYes {
					if openErr := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", release.URL).Start(); openErr != nil {
						statusLabel.SetText("Open release page failed: " + openErr.Error())
						return
					}
				}
				statusLabel.SetText("Update available: " + release.Version)
			})
		}()
	}
	parseAndAddSource := func(source string, clearURL bool) {
		source = strings.TrimSpace(source)
		if source == "" {
			statusLabel.SetText("Enter a URL or choose a torrent file first.")
			return
		}
		settings := currentSettings
		statusLabel.SetText("Resolving download source...")
		go func(source string, settings config.Settings, clearURL bool) {
			headers, err := httpdownload.ParseHeaders(settings.HeadersText)
			if err != nil {
				app.Post(func() {
					statusLabel.SetText("Headers invalid: " + err.Error())
				})
				return
			}
			headers = httpdownload.MergeCookies(headers, settings.CookiesText)

			var task core.Task
			if options.ParseSource != nil {
				task, err = options.ParseSource(context.Background(), source, settings, headers)
			} else {
				task, err = httpdownload.Parse(context.Background(), source, settings.DownloadDir, settings.BlockNum, settings.RetryCount, settings.ProxyURL, headers)
			}

			app.Post(func() {
				if err != nil {
					statusLabel.SetText("Parse failed: " + err.Error())
					return
				}
				if task.PackID == "bt" {
					selectedTask, selected, selectionErr := runBTSelectionDialog(mainWindow, task, currentSettings.ThemeMode)
					if selectionErr != nil {
						statusLabel.SetText("Open torrent selection failed: " + selectionErr.Error())
						return
					}
					if !selected {
						statusLabel.SetText("Torrent task canceled.")
						return
					}
					task = selectedTask
				}
				task.Title = httpdownload.DeduplicateTitle(
					task.Title,
					task.Path,
					options.Scheduler.Snapshot(),
				)
				options.Scheduler.Add(task)
				if clearURL && urlEdit != nil && strings.TrimSpace(urlEdit.Text()) == source {
					urlEdit.SetText("")
				}
				statusLabel.SetText("Task added: " + task.Title)
			})
		}(source, settings, clearURL)
	}

	window := MainWindow{
		AssignTo: &mainWindow,
		Title:    "Ghost Downloader Go " + options.AppVersion,
		MinSize:  Size{Width: 510, Height: 620},
		Size:     Size{Width: 1120, Height: 720},
		Layout:   VBox{MarginsZero: true, SpacingZero: true},
		Children: []Widget{
			Composite{
				Layout: VBox{Margins: Margins{Left: 14, Top: 10, Right: 14, Bottom: 10}, Spacing: 6},
				Children: []Widget{
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							LineEdit{
								AssignTo:      &urlEdit,
								MinSize:       Size{Width: 120, Height: 0},
								StretchFactor: 1,
								Text:          "",
							},
							PushButton{
								Text: "Add URL",
								OnClicked: func() {
									parseAndAddSource(urlEdit.Text(), true)
								},
							},
						},
					},
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							PushButton{
								Text: "Open Torrent",
								OnClicked: func() {
									dialog := new(walk.FileDialog)
									dialog.Title = "Open Torrent File"
									dialog.Filter = "Torrent files (*.torrent)|*.torrent|All files (*.*)|*.*"
									ok, err := dialog.ShowOpen(mainWindow)
									if err != nil {
										statusLabel.SetText("Open torrent failed: " + err.Error())
										return
									}
									if ok {
										parseAndAddSource(dialog.FilePath, false)
									}
								},
							},
							PushButton{
								Text: "Settings",
								OnClicked: func() {
									next, ok, err := runSettingsDialog(mainWindow, currentSettings)
									if err != nil {
										statusLabel.SetText("Open settings failed: " + err.Error())
										return
									}
									if !ok {
										return
									}
									next = next.Normalized(options.Paths)
									if err := saveSettings(options, next); err != nil {
										statusLabel.SetText("Save settings failed: " + err.Error())
										return
									}
									currentSettings = next
									themeStyle.Apply(next.ThemeMode)
									taskModel.SetDarkMode(appwin32.DarkModeEnabled(next.ThemeMode))
									options.Scheduler.SetMaxRunning(next.MaxConcurrent)
									if options.Limiter != nil {
										options.Limiter.SetRate(next.SpeedLimitKiB * 1024)
									}
									statusLabel.SetText("Settings saved.")
								},
							},
							PushButton{Text: "Check Updates", OnClicked: checkForUpdates},
							PushButton{Text: "Open Logs", OnClicked: openLogFile},
							PushButton{
								Text: "Start All",
								OnClicked: func() {
									options.Scheduler.StartAll()
									statusLabel.SetText("All paused tasks queued.")
								},
							},
							PushButton{
								Text: "Pause All",
								OnClicked: func() {
									options.Scheduler.PauseAll()
									statusLabel.SetText("All active tasks paused.")
								},
							},
							HSpacer{},
						},
					},
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							PushButton{
								Text:      "Pause/Resume",
								OnClicked: toggleSelectedTask,
							},
							PushButton{
								Text:      "Redownload",
								OnClicked: redownloadSelectedTask,
							},
							PushButton{
								Text:      "Remove",
								OnClicked: removeSelectedTask,
							},
							PushButton{
								Text:      "Open Folder",
								OnClicked: openSelectedTaskFolder,
							},
							PushButton{
								Text:      "Open File",
								OnClicked: openSelectedTaskFile,
							},
							HSpacer{},
						},
					},
				},
			},
			Composite{
				Layout: VBox{Margins: Margins{Left: 16, Top: 14, Right: 16, Bottom: 14}},
				Children: []Widget{
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							Label{
								Text:      "Tasks",
								Font:      Font{PointSize: 18, Bold: true},
								Alignment: AlignHNearVCenter,
							},
							Label{
								AssignTo: &taskSummaryLabel,
								Text:     "All 0 | Active 0 | Done 0 | Failed 0",
							},
							LineEdit{
								AssignTo:      &taskSearchEdit,
								CueBanner:     "Search tasks",
								MinSize:       Size{Width: 80, Height: 0},
								StretchFactor: 1,
								OnTextChanged: func() {
									taskModel.SetSearchText(taskSearchEdit.Text())
									refreshTaskSelection(taskTable, taskModel, &selectedTaskID, taskDetail)
									updateTaskSummary(taskSummaryLabel, taskModel)
								},
							},
							ComboBox{
								AssignTo:     &taskFilterBox,
								Model:        []string{"All", "Active", "Completed", "Failed"},
								CurrentIndex: 0,
								MinSize:      Size{Width: 90, Height: 0},
								OnCurrentIndexChanged: func() {
									if applyingTaskFilter {
										return
									}
									applyTaskFilter(taskFilter(taskFilterBox.CurrentIndex()))
								},
							},
						},
					},
					Composite{
						MinSize: Size{Width: 0, Height: 24},
						Layout:  HBox{MarginsZero: true, SpacingZero: true},
						Children: []Widget{
							PushButton{AssignTo: &taskHeaderButtons[0], Text: "Name", MinSize: Size{Width: 110}, MaxSize: Size{Width: 110}, OnClicked: func() { sortTaskColumn(0) }},
							PushButton{AssignTo: &taskHeaderButtons[1], Text: "Status", MinSize: Size{Width: 60}, MaxSize: Size{Width: 60}, OnClicked: func() { sortTaskColumn(1) }},
							PushButton{AssignTo: &taskHeaderButtons[2], Text: "Progress", MinSize: Size{Width: 65}, MaxSize: Size{Width: 65}, OnClicked: func() { sortTaskColumn(2) }},
							PushButton{AssignTo: &taskHeaderButtons[3], Text: "Size ▼", MinSize: Size{Width: 75}, MaxSize: Size{Width: 75}, OnClicked: func() { sortTaskColumn(3) }},
							PushButton{AssignTo: &taskHeaderButtons[4], Text: "Speed", MinSize: Size{Width: 70}, MaxSize: Size{Width: 70}, OnClicked: func() { sortTaskColumn(4) }},
							PushButton{AssignTo: &taskHeaderButtons[5], Text: "Folder", StretchFactor: 1, OnClicked: func() { sortTaskColumn(5) }},
						},
					},
					TableView{
						AssignTo:                    &taskTable,
						MinSize:                     Size{Width: 0, Height: 0},
						AlternatingRowBG:            true,
						ColumnsOrderable:            false,
						ColumnsSizable:              false,
						CustomRowHeight:             32,
						HeaderHidden:                true,
						LastColumnStretched:         true,
						Model:                       taskModel,
						SelectionHiddenWithoutFocus: true,
						ContextMenuItems: []MenuItem{
							Action{Text: "Pause/Resume", OnTriggered: toggleSelectedTask},
							Action{Text: "Redownload", OnTriggered: redownloadSelectedTask},
							Separator{},
							Action{Text: "Open Folder", OnTriggered: openSelectedTaskFolder},
							Action{Text: "Open File", OnTriggered: openSelectedTaskFile},
							Separator{},
							Action{Text: "Remove", OnTriggered: removeSelectedTask},
						},
						Columns: []TableViewColumn{
							{Title: "Name", Width: 110},
							{Title: "Status", Width: 60},
							{Title: "Progress", Width: 65, Alignment: AlignFar},
							{Title: "Size", Width: 75, Alignment: AlignFar},
							{Title: "Speed", Width: 70, Alignment: AlignFar},
							{Title: "Folder", Width: 80},
						},
						OnSelectedIndexesChanged: func() {
							refreshTaskSelection(taskTable, taskModel, &selectedTaskID, taskDetail)
						},
					},
					TextEdit{
						AssignTo: &taskDetail,
						ReadOnly: true,
						VScroll:  true,
						MinSize:  Size{Width: 0, Height: 86},
						Text:     "No task selected.",
					},
				},
			},
			Composite{
				Layout: HBox{Margins: Margins{Left: 14, Top: 8, Right: 14, Bottom: 8}},
				Children: []Widget{
					Label{
						AssignTo: &statusLabel,
						Text:     fmt.Sprintf("Ready | Download folder: %s", currentSettings.DownloadDir),
					},
				},
			},
		},
	}

	if err := window.Create(); err != nil {
		return fmt.Errorf("create main window: %w", err)
	}
	// TableView initializes sorter models to its first column while creating the
	// native control. Restore the product default and synchronize the custom
	// dark header before the window is shown.
	if err := taskModel.Sort(3, walk.SortDescending); err != nil {
		return fmt.Errorf("initialize task sorting: %w", err)
	}
	updateTaskSortHeaders()
	themeStyle, err := newWindowThemeStyle(mainWindow, currentSettings.ThemeMode, urlEdit, taskSearchEdit, taskDetail)
	if err != nil {
		return fmt.Errorf("style main window: %w", err)
	}
	defer themeStyle.Dispose()

	if icon, err := loadApplicationIcon(); err != nil {
		slog.Warn("load application icon failed", "error", err)
	} else if err := mainWindow.SetIcon(icon); err != nil {
		icon.Dispose()
		slog.Warn("set application icon failed", "error", err)
	}

	app.SetMainWindow(mainWindow)
	if options.BrowserBridge != nil {
		options.BrowserBridge.SetPairApprovalFunc(pairApprovalFunc(app, mainWindow))
		defer options.BrowserBridge.SetPairApprovalFunc(nil)
	}
	tray, err := installTray(mainWindow, options.Paths, options.Scheduler, statusLabel)
	if err != nil {
		return fmt.Errorf("install tray: %w", err)
	}
	defer tray.dispose()
	var schedulerEventsDone chan struct{}
	if options.Scheduler != nil {
		initialTasks := options.Scheduler.Snapshot()
		notificationTracker := newTaskNotificationTracker(initialTasks)
		selectedTaskByID = indexTasks(initialTasks)
		taskModel.SetTasks(initialTasks)
		refreshTaskSelection(taskTable, taskModel, &selectedTaskID, taskDetail)
		updateTaskSummary(taskSummaryLabel, taskModel)
		schedulerEventsDone = make(chan struct{})
		go func() {
			defer close(schedulerEventsDone)
			for event := range options.Scheduler.Events() {
				tasks := event.Tasks
				notifications := notificationTracker.Update(tasks)
				app.Post(func() {
					selectedTaskByID = indexTasks(tasks)
					taskModel.SetTasks(tasks)
					refreshTaskSelection(taskTable, taskModel, &selectedTaskID, taskDetail)
					updateTaskSummary(taskSummaryLabel, taskModel)
					for _, notification := range notifications {
						showTaskNotification(tray, notification)
					}
				})
			}
		}()
	}
	slog.Info("main window created")
	mainWindow.Run()
	// Stop producers before disposing the dispatcher/window. This also waits
	// for BT resume checkpoints and lets the event pump exit deterministically.
	app.Clear()
	if options.BrowserBridge != nil {
		if err := options.BrowserBridge.Stop(context.Background()); err != nil {
			slog.Warn("stop browser bridge during UI shutdown failed", "error", err)
		}
	}
	if options.Scheduler != nil {
		options.Scheduler.StopAll()
		<-schedulerEventsDone
	}
	slog.Info("main window closed")
	return nil
}

func saveSettings(options Options, settings config.Settings) error {
	if options.SaveSettings == nil {
		return nil
	}
	return options.SaveSettings(settings)
}

func btOptionsFromSettings(settings config.Settings, headers map[string]string) btdownload.Options {
	return btdownload.Options{
		DownloadDir:           settings.DownloadDir,
		ProxyURL:              settings.ProxyURL,
		Headers:               headers,
		MetadataTimeout:       time.Duration(settings.BTMetadataTimeoutSec) * time.Second,
		ListenPort:            settings.BTListenPort,
		ConnectionsLimit:      settings.BTConnectionsLimit,
		DownloadRateLimit:     settings.BTDownloadRateLimitKiB * 1024,
		UploadRateLimit:       settings.BTUploadRateLimitKiB * 1024,
		EnableDHT:             settings.BTEnableDHT,
		EnableLSD:             settings.BTEnableLSD,
		EnableUPnP:            settings.BTEnableUPnP,
		EnableNATPMP:          settings.BTEnableNATPMP,
		SequentialDownload:    settings.BTSequentialDownload,
		SeedRatioLimitPercent: settings.BTSeedRatioLimitPercent,
		SeedTimeLimitMinutes:  settings.BTSeedTimeLimitMinutes,
		ExtraTrackers:         btdownload.ParseTrackers(settings.BTTrackersText),
		SaveMagnetTorrentFile: settings.BTSaveMagnetTorrentFile,
	}
}

func indexTasks(tasks []core.TaskSnapshot) map[string]core.TaskSnapshot {
	result := make(map[string]core.TaskSnapshot, len(tasks))
	for _, task := range tasks {
		result[task.ID] = task
	}
	return result
}

func refreshTaskSelection(table *walk.TableView, model *taskTableModel, selectedTaskID *string, detail *walk.TextEdit) {
	if model == nil {
		return
	}
	row := -1
	if table != nil {
		indexes := table.SelectedIndexes()
		if len(indexes) > 0 {
			row = indexes[0]
		}
	}
	task, ok := model.TaskAt(row)
	if !ok && selectedTaskID != nil && *selectedTaskID != "" {
		task, ok = model.TaskByID(*selectedTaskID)
	}
	if !ok {
		if selectedTaskID != nil {
			*selectedTaskID = ""
		}
		renderTaskDetail(detail, core.TaskSnapshot{}, false)
		return
	}
	if selectedTaskID != nil {
		*selectedTaskID = task.ID
	}
	renderTaskDetail(detail, task, true)
}

func renderTaskDetail(detail *walk.TextEdit, task core.TaskSnapshot, ok bool) {
	if detail == nil {
		return
	}
	if !ok {
		detail.SetText("No task selected.")
		return
	}
	var builder strings.Builder
	builder.WriteString(task.Title)
	builder.WriteString("\r\n")
	builder.WriteString("Status: ")
	builder.WriteString(displayStatus(task.Status))
	builder.WriteString(" | Progress: ")
	builder.WriteString(fmt.Sprintf("%.1f%%", task.Progress))
	builder.WriteString(" | Size: ")
	if task.FileSize > 0 {
		builder.WriteString(formatBytes(task.Received))
		builder.WriteString(" / ")
		builder.WriteString(formatBytes(task.FileSize))
	} else {
		builder.WriteString(formatBytes(task.Received))
	}
	builder.WriteString("\r\nURL: ")
	builder.WriteString(task.URL)
	builder.WriteString("\r\nFolder: ")
	builder.WriteString(task.Path)
	if task.Detail != "" {
		builder.WriteString("\r\nDetails: ")
		builder.WriteString(task.Detail)
	}
	if task.Error != "" {
		builder.WriteString("\r\nError: ")
		builder.WriteString(task.Error)
	}
	detail.SetText(builder.String())
}

func updateTaskSummary(label *walk.Label, model *taskTableModel) {
	if label == nil || model == nil {
		return
	}
	all, active, completed, failed := model.Counts()
	label.SetText(fmt.Sprintf("All %d | Active %d | Done %d | Failed %d", all, active, completed, failed))
}

func formatBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := int64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}
