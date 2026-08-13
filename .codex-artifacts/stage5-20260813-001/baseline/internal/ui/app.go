package ui

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"ghost-downloader-go-win32/internal/browserbridge"
	"ghost-downloader-go-win32/internal/config"
	"ghost-downloader-go-win32/internal/core"
	httpdownload "ghost-downloader-go-win32/internal/download/http"
	m3u8download "ghost-downloader-go-win32/internal/download/m3u8"
	"ghost-downloader-go-win32/internal/ratelimit"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type Options struct {
	Paths         config.Paths
	Scheduler     *core.Scheduler
	Limiter       *ratelimit.Limiter
	Settings      config.Settings
	SaveSettings  func(config.Settings) error
	BrowserBridge *browserbridge.Bridge
}

func Run(options Options) error {
	var mainWindow *walk.MainWindow
	var statusLabel *walk.Label
	var urlEdit *walk.LineEdit
	var downloadDirEdit *walk.LineEdit
	var proxyEdit *walk.LineEdit
	var headersEdit *walk.TextEdit
	var cookiesEdit *walk.TextEdit
	var blockEdit *walk.LineEdit
	var retryEdit *walk.LineEdit
	var limitEdit *walk.LineEdit
	var concurrentEdit *walk.LineEdit
	var taskTable *walk.TableView
	var taskSearchEdit *walk.LineEdit
	var taskFilterBox *walk.ComboBox
	var taskDetail *walk.TextEdit
	var taskSummaryLabel *walk.Label
	var selectedTaskID string
	var selectedTaskByID map[string]core.TaskSnapshot
	var applyingTaskFilter bool

	app := NewApplicationDispatcher()
	defer app.Clear()
	currentSettings := options.Settings.Normalized(options.Paths)
	taskModel := newTaskTableModel()
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
		if task.Status != core.StatusCompleted {
			statusLabel.SetText("File is not completed yet.")
			return
		}
		if err := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", filepath.Join(task.Path, task.Title)).Start(); err != nil {
			statusLabel.SetText("Open file failed: " + err.Error())
		}
	}

	window := MainWindow{
		AssignTo: &mainWindow,
		Title:    "Ghost Downloader Go",
		MinSize:  Size{Width: 980, Height: 620},
		Size:     Size{Width: 1120, Height: 720},
		Layout:   VBox{MarginsZero: true, SpacingZero: true},
		Children: []Widget{
			Composite{
				Layout: HBox{Margins: Margins{Left: 14, Top: 10, Right: 14, Bottom: 10}},
				Children: []Widget{
					Label{
						Text:      "Ghost Downloader",
						Font:      Font{PointSize: 14, Bold: true},
						Alignment: AlignHNearVCenter,
					},
					HSpacer{},
					LineEdit{
						AssignTo: &urlEdit,
						MinSize:  Size{Width: 420, Height: 0},
						Text:     "",
					},
					PushButton{
						Text: "Add URL",
						OnClicked: func() {
							rawURL := strings.TrimSpace(urlEdit.Text())
							if rawURL == "" {
								statusLabel.SetText("Enter a URL first.")
								return
							}
							settings := currentSettings
							statusLabel.SetText("Parsing URL...")
							go func(settings config.Settings) {
								headers, err := httpdownload.ParseHeaders(settings.HeadersText)
								if err != nil {
									app.Post(func() {
										statusLabel.SetText("Headers invalid: " + err.Error())
									})
									return
								}
								headers = httpdownload.MergeCookies(headers, settings.CookiesText)
								var task core.Task
								if m3u8download.IsManifestSource(rawURL) {
									result, parseErr := m3u8download.Parse(context.Background(), rawURL, settings, headers)
									if parseErr != nil {
										err = parseErr
									} else {
										task = result.Task
									}
								} else {
									task, err = httpdownload.Parse(
										context.Background(),
										rawURL,
										settings.DownloadDir,
										settings.BlockNum,
										settings.RetryCount,
										settings.ProxyURL,
										headers,
									)
								}
								app.Post(func() {
									if err != nil {
										statusLabel.SetText("Parse failed: " + err.Error())
										return
									}
									task.Title = httpdownload.DeduplicateTitle(
										task.Title,
										task.Path,
										options.Scheduler.Snapshot(),
									)
									options.Scheduler.Add(task)
									urlEdit.SetText("")
									statusLabel.SetText("Task added: " + task.Title)
								})
							}(settings)
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
							options.Scheduler.SetMaxRunning(next.MaxConcurrent)
							if options.Limiter != nil {
								options.Limiter.SetRate(next.SpeedLimitKiB * 1024)
							}
							syncSettingsControls(
								next,
								downloadDirEdit,
								proxyEdit,
								headersEdit,
								cookiesEdit,
								blockEdit,
								concurrentEdit,
								retryEdit,
								limitEdit,
							)
							statusLabel.SetText("Settings saved.")
						},
					},
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
					LineEdit{
						AssignTo: &limitEdit,
						MinSize:  Size{Width: 92, Height: 0},
						Text:     strconv.FormatInt(currentSettings.SpeedLimitKiB, 10),
					},
					PushButton{
						Text: "Limit KiB/s",
						OnClicked: func() {
							value := strings.TrimSpace(limitEdit.Text())
							kib, err := strconv.ParseInt(value, 10, 64)
							if err != nil || kib < 0 {
								statusLabel.SetText("Limit must be 0 or a positive KiB/s value.")
								return
							}
							if options.Limiter != nil {
								options.Limiter.SetRate(kib * 1024)
							}
							currentSettings.SpeedLimitKiB = kib
							if err := saveSettings(options, currentSettings); err != nil {
								statusLabel.SetText("Save limit failed: " + err.Error())
								return
							}
							if kib == 0 {
								statusLabel.SetText("Global speed limit disabled.")
							} else {
								statusLabel.SetText(fmt.Sprintf("Global speed limit set to %d KiB/s.", kib))
							}
						},
					},
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
				},
			},
			HSplitter{
				Children: []Widget{
					Composite{
						MinSize: Size{Width: 190, Height: 0},
						MaxSize: Size{Width: 240, Height: 0},
						Layout:  VBox{Margins: Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}},
						Children: []Widget{
							Label{Text: "Download Folder"},
							LineEdit{
								AssignTo: &downloadDirEdit,
								Text:     currentSettings.DownloadDir,
							},
							PushButton{
								Text: "Choose Folder",
								OnClicked: func() {
									dialog := new(walk.FileDialog)
									dialog.Title = "Choose Download Folder"
									if ok, err := dialog.ShowBrowseFolder(mainWindow); err != nil {
										statusLabel.SetText("Choose folder failed: " + err.Error())
									} else if ok {
										currentSettings.DownloadDir = dialog.FilePath
										downloadDirEdit.SetText(dialog.FilePath)
										if err := saveSettings(options, currentSettings); err != nil {
											statusLabel.SetText("Save folder failed: " + err.Error())
											return
										}
										statusLabel.SetText("Download folder updated.")
									}
								},
							},
							Label{Text: "Proxy URL"},
							LineEdit{
								AssignTo: &proxyEdit,
								Text:     currentSettings.ProxyURL,
							},
							PushButton{
								Text: "Save Proxy",
								OnClicked: func() {
									currentSettings.ProxyURL = strings.TrimSpace(proxyEdit.Text())
									if err := saveSettings(options, currentSettings); err != nil {
										statusLabel.SetText("Save proxy failed: " + err.Error())
										return
									}
									if currentSettings.ProxyURL == "" {
										statusLabel.SetText("Proxy disabled.")
									} else {
										statusLabel.SetText("Proxy saved.")
									}
								},
							},
							Label{Text: "Request Headers"},
							TextEdit{
								AssignTo: &headersEdit,
								Text:     currentSettings.HeadersText,
								VScroll:  true,
								MinSize:  Size{Width: 0, Height: 84},
							},
							PushButton{
								Text: "Save Headers",
								OnClicked: func() {
									next := strings.TrimSpace(headersEdit.Text())
									if _, err := httpdownload.ParseHeaders(next); err != nil {
										statusLabel.SetText("Headers invalid: " + err.Error())
										return
									}
									currentSettings.HeadersText = next
									if err := saveSettings(options, currentSettings); err != nil {
										statusLabel.SetText("Save headers failed: " + err.Error())
										return
									}
									statusLabel.SetText("Headers saved.")
								},
							},
							Label{Text: "Cookies"},
							TextEdit{
								AssignTo: &cookiesEdit,
								Text:     currentSettings.CookiesText,
								VScroll:  true,
								MinSize:  Size{Width: 0, Height: 70},
							},
							PushButton{
								Text: "Save Cookies",
								OnClicked: func() {
									next := httpdownload.NormalizeCookies(cookiesEdit.Text())
									currentSettings.CookiesText = next
									cookiesEdit.SetText(next)
									if err := saveSettings(options, currentSettings); err != nil {
										statusLabel.SetText("Save cookies failed: " + err.Error())
										return
									}
									if next == "" {
										statusLabel.SetText("Cookies cleared.")
									} else {
										statusLabel.SetText("Cookies saved.")
									}
								},
							},
							Label{Text: "Blocks / Max / Retries"},
							Composite{
								Layout: HBox{MarginsZero: true},
								Children: []Widget{
									LineEdit{
										AssignTo: &blockEdit,
										Text:     strconv.Itoa(currentSettings.BlockNum),
									},
									LineEdit{
										AssignTo: &concurrentEdit,
										Text:     strconv.Itoa(currentSettings.MaxConcurrent),
									},
									LineEdit{
										AssignTo: &retryEdit,
										Text:     strconv.Itoa(currentSettings.RetryCount),
									},
									PushButton{
										Text: "Save",
										OnClicked: func() {
											blockNum, err := strconv.Atoi(strings.TrimSpace(blockEdit.Text()))
											if err != nil || blockNum <= 0 {
												statusLabel.SetText("Blocks must be positive.")
												return
											}
											maxConcurrent, err := strconv.Atoi(strings.TrimSpace(concurrentEdit.Text()))
											if err != nil || maxConcurrent <= 0 {
												statusLabel.SetText("Max tasks must be positive.")
												return
											}
											retryCount, err := strconv.Atoi(strings.TrimSpace(retryEdit.Text()))
											if err != nil || retryCount < 0 {
												statusLabel.SetText("Retries must be 0 or a positive number.")
												return
											}
											currentSettings.BlockNum = blockNum
											currentSettings.MaxConcurrent = maxConcurrent
											currentSettings.RetryCount = retryCount
											options.Scheduler.SetMaxRunning(maxConcurrent)
											if err := saveSettings(options, currentSettings); err != nil {
												statusLabel.SetText("Save settings failed: " + err.Error())
												return
											}
											statusLabel.SetText("Download settings saved.")
										},
									},
								},
							},
							Label{
								AssignTo: &taskSummaryLabel,
								Text:     "All 0 | Active 0 | Done 0 | Failed 0",
							},
							PushButton{
								Text:      "All Tasks",
								OnClicked: func() { applyTaskFilter(taskFilterAll) },
							},
							PushButton{
								Text:      "Active",
								OnClicked: func() { applyTaskFilter(taskFilterActive) },
							},
							PushButton{
								Text:      "Completed",
								OnClicked: func() { applyTaskFilter(taskFilterCompleted) },
							},
							PushButton{
								Text:      "Failed",
								OnClicked: func() { applyTaskFilter(taskFilterFailed) },
							},
							VSpacer{},
							Label{Text: "Browser Extension"},
							Label{Text: "Runtimes"},
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
									HSpacer{},
									LineEdit{
										AssignTo:  &taskSearchEdit,
										CueBanner: "Search tasks",
										MinSize:   Size{Width: 220, Height: 0},
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
										MinSize:      Size{Width: 120, Height: 0},
										OnCurrentIndexChanged: func() {
											if applyingTaskFilter {
												return
											}
											applyTaskFilter(taskFilter(taskFilterBox.CurrentIndex()))
										},
									},
								},
							},
							TableView{
								AssignTo:                    &taskTable,
								AlternatingRowBG:            true,
								ColumnsOrderable:            true,
								ColumnsSizable:              true,
								CustomRowHeight:             32,
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
									{Title: "Name", Width: 260},
									{Title: "Status", Width: 92},
									{Title: "Progress", Width: 90, Alignment: AlignFar},
									{Title: "Size", Width: 150, Alignment: AlignFar},
									{Title: "Speed", Width: 110, Alignment: AlignFar},
									{Title: "Folder", Width: 220},
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
	if options.Scheduler != nil {
		initialTasks := options.Scheduler.Snapshot()
		notificationTracker := newTaskNotificationTracker(initialTasks)
		selectedTaskByID = indexTasks(initialTasks)
		taskModel.SetTasks(initialTasks)
		refreshTaskSelection(taskTable, taskModel, &selectedTaskID, taskDetail)
		updateTaskSummary(taskSummaryLabel, taskModel)
		go func() {
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
	slog.Info("main window closed")
	return nil
}

func saveSettings(options Options, settings config.Settings) error {
	if options.SaveSettings == nil {
		return nil
	}
	return options.SaveSettings(settings)
}

func syncSettingsControls(settings config.Settings, downloadDirEdit, proxyEdit *walk.LineEdit, headersEdit, cookiesEdit *walk.TextEdit, blockEdit, concurrentEdit, retryEdit, limitEdit *walk.LineEdit) {
	if downloadDirEdit != nil {
		downloadDirEdit.SetText(settings.DownloadDir)
	}
	if proxyEdit != nil {
		proxyEdit.SetText(settings.ProxyURL)
	}
	if headersEdit != nil {
		headersEdit.SetText(settings.HeadersText)
	}
	if cookiesEdit != nil {
		cookiesEdit.SetText(settings.CookiesText)
	}
	if blockEdit != nil {
		blockEdit.SetText(strconv.Itoa(settings.BlockNum))
	}
	if concurrentEdit != nil {
		concurrentEdit.SetText(strconv.Itoa(settings.MaxConcurrent))
	}
	if retryEdit != nil {
		retryEdit.SetText(strconv.Itoa(settings.RetryCount))
	}
	if limitEdit != nil {
		limitEdit.SetText(strconv.FormatInt(settings.SpeedLimitKiB, 10))
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
