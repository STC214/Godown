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
	schedulerActions := newSerialExecutor()
	defer schedulerActions.CloseAndWait()
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
	headerTitles := [...]string{"名称", "状态", "进度", "大小", "速度", "目录"}
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
			statusLabel.SetText("排序失败：" + err.Error())
			return
		}
		updateTaskSortHeaders()
		refreshTaskSelection(taskTable, taskModel, &selectedTaskID, taskDetail)
	}
	toggleSelectedTask := func() {
		taskID := selectedTaskID
		if taskID == "" {
			statusLabel.SetText("请先选择任务。")
			return
		}
		statusLabel.SetText("正在切换任务状态…")
		schedulerActions.Submit(func() {
			err := options.Scheduler.TogglePause(taskID)
			app.Post(func() {
				if err != nil {
					statusLabel.SetText("切换任务状态失败：" + err.Error())
					return
				}
				statusLabel.SetText("任务状态已切换。")
			})
		})
	}
	redownloadSelectedTask := func() {
		taskID := selectedTaskID
		if taskID == "" {
			statusLabel.SetText("请先选择任务。")
			return
		}
		statusLabel.SetText("正在重新下载任务…")
		schedulerActions.Submit(func() {
			err := options.Scheduler.Redownload(taskID)
			app.Post(func() {
				if err != nil {
					statusLabel.SetText("重新下载失败：" + err.Error())
					return
				}
				statusLabel.SetText("任务已重新开始。")
			})
		})
	}
	editSelectedBTFiles := func() {
		taskID := selectedTaskID
		if taskID == "" {
			statusLabel.SetText("请先选择任务。")
			return
		}
		task, ok := options.Scheduler.Task(taskID)
		if !ok {
			statusLabel.SetText("所选任务已失效。")
			return
		}
		if task.PackID != "bt" {
			statusLabel.SetText("只有 BitTorrent 任务可以调整文件选择。")
			return
		}
		edited, accepted, err := runBTSelectionDialog(mainWindow, task, currentSettings.ThemeMode)
		if err != nil {
			statusLabel.SetText("打开种子文件选择窗口失败：" + err.Error())
			return
		}
		if !accepted {
			return
		}
		files, err := btdownload.FilesFromTask(edited)
		if err != nil {
			statusLabel.SetText("读取文件选择失败：" + err.Error())
			return
		}
		priorities := make(map[int]int, len(files))
		for _, file := range files {
			if file.Selected {
				priorities[file.Index] = file.Priority
			}
		}
		statusLabel.SetText("正在应用 BitTorrent 文件选择…")
		schedulerActions.Submit(func() {
			err := options.Scheduler.EditTask(taskID, func(latest core.Task) (core.Task, error) {
				return btdownload.SetFilePriorities(latest, priorities)
			})
			app.Post(func() {
				if err != nil {
					statusLabel.SetText("调整 BitTorrent 文件失败：" + err.Error())
					return
				}
				statusLabel.SetText("BitTorrent 文件选择已更新。")
			})
		})
	}
	removeSelectedTask := func() {
		if selectedTaskID == "" {
			statusLabel.SetText("请先选择任务。")
			return
		}
		if err := options.Scheduler.Remove(selectedTaskID); err != nil {
			statusLabel.SetText("移除任务失败：" + err.Error())
			return
		}
		selectedTaskID = ""
		statusLabel.SetText("任务已移除。")
	}
	openSelectedTaskFolder := func() {
		target := currentSettings.DownloadDir
		if selectedTaskID != "" {
			if task, ok := selectedTaskByID[selectedTaskID]; ok {
				target = task.Path
			}
		}
		if err := exec.Command("explorer.exe", target).Start(); err != nil {
			statusLabel.SetText("打开目录失败：" + err.Error())
		}
	}
	openSelectedTaskFile := func() {
		if selectedTaskID == "" {
			statusLabel.SetText("请先选择任务。")
			return
		}
		task, ok := selectedTaskByID[selectedTaskID]
		if !ok {
			statusLabel.SetText("所选任务已失效。")
			return
		}
		if task.Status != core.StatusCompleted && task.Status != core.StatusSeeding {
			statusLabel.SetText("文件尚未下载完成。")
			return
		}
		if err := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", filepath.Join(task.Path, task.Title)).Start(); err != nil {
			statusLabel.SetText("打开文件失败：" + err.Error())
		}
	}
	openLogFile := func() {
		if err := exec.Command("explorer.exe", "/select,", options.Paths.LogFile).Start(); err != nil {
			statusLabel.SetText("打开日志位置失败：" + err.Error())
		}
	}
	checkForUpdates := func() {
		if options.CheckForUpdate == nil {
			statusLabel.SetText("更新检查功能当前不可用。")
			return
		}
		statusLabel.SetText("正在检查更新…")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			release, err := options.CheckForUpdate(ctx)
			app.Post(func() {
				if err != nil {
					statusLabel.SetText("检查更新失败：" + err.Error())
					return
				}
				if !release.Newer {
					statusLabel.SetText("当前已是最新版本（" + options.AppVersion + "）。")
					return
				}
				if options.InstallUpdate != nil && release.HasPortableUpdate() {
					message := fmt.Sprintf("发现 Ghost Downloader %s。是否下载并校验便携包、替换程序文件后重启？", release.Version)
					if walk.MsgBox(mainWindow, "便携版更新", message, walk.MsgBoxYesNo|walk.MsgBoxIconInformation) != walk.DlgCmdYes {
						statusLabel.SetText("发现新版本：" + release.Version)
						return
					}
					statusLabel.SetText("正在下载并校验更新 " + release.Version + "…")
					go func(release updatecheck.Release) {
						ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
						defer cancel()
						err := options.InstallUpdate(ctx, release)
						app.Post(func() {
							if err != nil {
								statusLabel.SetText("便携版更新失败：" + err.Error())
								return
							}
							statusLabel.SetText("更新已校验，正在关闭程序以替换文件…")
							walk.App().Exit(0)
						})
					}(release)
					return
				}
				message := fmt.Sprintf("发现 Ghost Downloader %s。是否打开发布页面？", release.Version)
				if walk.MsgBox(mainWindow, "发现新版本", message, walk.MsgBoxYesNo|walk.MsgBoxIconInformation) == walk.DlgCmdYes {
					if openErr := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", release.URL).Start(); openErr != nil {
						statusLabel.SetText("打开发布页面失败：" + openErr.Error())
						return
					}
				}
				statusLabel.SetText("发现新版本：" + release.Version)
			})
		}()
	}
	parseAndAddSource := func(source string, clearURL bool) {
		source = strings.TrimSpace(source)
		if source == "" {
			statusLabel.SetText("请先输入下载地址或选择种子文件。")
			return
		}
		settings := currentSettings
		statusLabel.SetText("正在解析下载源…")
		go func(source string, settings config.Settings, clearURL bool) {
			headers, err := httpdownload.ParseHeaders(settings.HeadersText)
			if err != nil {
				app.Post(func() {
					statusLabel.SetText("请求标头格式错误：" + err.Error())
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
					statusLabel.SetText("解析失败：" + err.Error())
					return
				}
				if task.PackID == "bt" {
					selectedTask, selected, selectionErr := runBTSelectionDialog(mainWindow, task, currentSettings.ThemeMode)
					if selectionErr != nil {
						statusLabel.SetText("打开种子文件选择窗口失败：" + selectionErr.Error())
						return
					}
					if !selected {
						statusLabel.SetText("已取消添加种子任务。")
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
				statusLabel.SetText("已添加任务：" + task.Title)
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
								Text: "添加地址",
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
								Text: "打开种子",
								OnClicked: func() {
									dialog := new(walk.FileDialog)
									dialog.Title = "打开种子文件"
									dialog.Filter = "种子文件 (*.torrent)|*.torrent|所有文件 (*.*)|*.*"
									ok, err := dialog.ShowOpen(mainWindow)
									if err != nil {
										statusLabel.SetText("打开种子文件失败：" + err.Error())
										return
									}
									if ok {
										parseAndAddSource(dialog.FilePath, false)
									}
								},
							},
							PushButton{
								Text: "设置",
								OnClicked: func() {
									next, ok, err := runSettingsDialog(mainWindow, currentSettings)
									if err != nil {
										statusLabel.SetText("打开设置失败：" + err.Error())
										return
									}
									if !ok {
										return
									}
									next = next.Normalized(options.Paths)
									if err := saveSettings(options, next); err != nil {
										statusLabel.SetText("保存设置失败：" + err.Error())
										return
									}
									currentSettings = next
									themeStyle.Apply(next.ThemeMode)
									taskModel.SetDarkMode(appwin32.DarkModeEnabled(next.ThemeMode))
									options.Scheduler.SetMaxRunning(next.MaxConcurrent)
									if options.Limiter != nil {
										options.Limiter.SetRate(next.SpeedLimitKiB * 1024)
									}
									statusLabel.SetText("设置已保存。")
								},
							},
							PushButton{Text: "检查更新", OnClicked: checkForUpdates},
							PushButton{Text: "打开日志", OnClicked: openLogFile},
							PushButton{
								Text: "全部开始",
								OnClicked: func() {
									statusLabel.SetText("正在启动全部任务…")
									schedulerActions.Submit(func() {
										options.Scheduler.StartAll()
										app.Post(func() { statusLabel.SetText("所有暂停任务已加入队列。") })
									})
								},
							},
							PushButton{
								Text: "全部暂停",
								OnClicked: func() {
									statusLabel.SetText("正在暂停全部任务…")
									schedulerActions.Submit(func() {
										options.Scheduler.PauseAll()
										app.Post(func() { statusLabel.SetText("所有活动任务已暂停。") })
									})
								},
							},
							HSpacer{},
						},
					},
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							PushButton{
								Text:      "暂停/继续",
								OnClicked: toggleSelectedTask,
							},
							PushButton{
								Text:      "重新下载",
								OnClicked: redownloadSelectedTask,
							},
							PushButton{
								Text:      "BT 文件",
								OnClicked: editSelectedBTFiles,
							},
							PushButton{
								Text:      "移除任务",
								OnClicked: removeSelectedTask,
							},
							PushButton{
								Text:      "打开目录",
								OnClicked: openSelectedTaskFolder,
							},
							PushButton{
								Text:      "打开文件",
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
								Text:      "下载任务",
								Font:      Font{PointSize: 18, Bold: true},
								Alignment: AlignHNearVCenter,
							},
							Label{
								AssignTo: &taskSummaryLabel,
								Text:     "全部 0 | 活动 0 | 完成 0 | 失败 0",
							},
							LineEdit{
								AssignTo:      &taskSearchEdit,
								CueBanner:     "搜索任务",
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
								Model:        []string{"全部", "活动", "已完成", "失败"},
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
							PushButton{AssignTo: &taskHeaderButtons[0], Text: "名称", MinSize: Size{Width: 110}, MaxSize: Size{Width: 110}, OnClicked: func() { sortTaskColumn(0) }},
							PushButton{AssignTo: &taskHeaderButtons[1], Text: "状态", MinSize: Size{Width: 60}, MaxSize: Size{Width: 60}, OnClicked: func() { sortTaskColumn(1) }},
							PushButton{AssignTo: &taskHeaderButtons[2], Text: "进度", MinSize: Size{Width: 65}, MaxSize: Size{Width: 65}, OnClicked: func() { sortTaskColumn(2) }},
							PushButton{AssignTo: &taskHeaderButtons[3], Text: "大小 ▼", MinSize: Size{Width: 75}, MaxSize: Size{Width: 75}, OnClicked: func() { sortTaskColumn(3) }},
							PushButton{AssignTo: &taskHeaderButtons[4], Text: "速度", MinSize: Size{Width: 70}, MaxSize: Size{Width: 70}, OnClicked: func() { sortTaskColumn(4) }},
							PushButton{AssignTo: &taskHeaderButtons[5], Text: "目录", StretchFactor: 1, OnClicked: func() { sortTaskColumn(5) }},
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
							Action{Text: "暂停/继续", OnTriggered: toggleSelectedTask},
							Action{Text: "重新下载", OnTriggered: redownloadSelectedTask},
							Action{Text: "选择 BT 文件", OnTriggered: editSelectedBTFiles},
							Separator{},
							Action{Text: "打开目录", OnTriggered: openSelectedTaskFolder},
							Action{Text: "打开文件", OnTriggered: openSelectedTaskFile},
							Separator{},
							Action{Text: "移除任务", OnTriggered: removeSelectedTask},
						},
						Columns: []TableViewColumn{
							{Title: "名称", Width: 110},
							{Title: "状态", Width: 60},
							{Title: "进度", Width: 65, Alignment: AlignFar},
							{Title: "大小", Width: 75, Alignment: AlignFar},
							{Title: "速度", Width: 70, Alignment: AlignFar},
							{Title: "目录", Width: 80},
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
						Text:     "未选择任务。",
					},
				},
			},
			Composite{
				Layout: HBox{Margins: Margins{Left: 14, Top: 8, Right: 14, Bottom: 8}},
				Children: []Widget{
					Label{
						AssignTo: &statusLabel,
						Text:     fmt.Sprintf("就绪 | 下载目录：%s", currentSettings.DownloadDir),
					},
				},
			},
		},
	}

	if err := window.Create(); err != nil {
		return fmt.Errorf("创建主窗口失败：%w", err)
	}
	// TableView initializes sorter models to its first column while creating the
	// native control. Restore the product default and synchronize the custom
	// dark header before the window is shown.
	if err := taskModel.Sort(3, walk.SortDescending); err != nil {
		return fmt.Errorf("初始化任务排序失败：%w", err)
	}
	updateTaskSortHeaders()
	themeStyle, err := newWindowThemeStyle(mainWindow, currentSettings.ThemeMode, urlEdit, taskSearchEdit, taskDetail)
	if err != nil {
		return fmt.Errorf("设置主窗口样式失败：%w", err)
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
		return fmt.Errorf("创建托盘图标失败：%w", err)
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
	schedulerActions.CloseAndWait()
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
		detail.SetText("未选择任务。")
		return
	}
	var builder strings.Builder
	builder.WriteString(task.Title)
	builder.WriteString("\r\n")
	builder.WriteString("状态：")
	builder.WriteString(displayStatus(task.Status))
	builder.WriteString(" | 进度：")
	builder.WriteString(fmt.Sprintf("%.1f%%", task.Progress))
	builder.WriteString(" | 大小：")
	if task.FileSize > 0 {
		builder.WriteString(formatBytes(task.Received))
		builder.WriteString(" / ")
		builder.WriteString(formatBytes(task.FileSize))
	} else {
		builder.WriteString(formatBytes(task.Received))
	}
	builder.WriteString("\r\nURL: ")
	builder.WriteString(task.URL)
	builder.WriteString("\r\n目录：")
	builder.WriteString(task.Path)
	if task.Detail != "" {
		builder.WriteString("\r\n详情：")
		builder.WriteString(task.Detail)
	}
	if task.Error != "" {
		builder.WriteString("\r\n错误：")
		builder.WriteString(task.Error)
	}
	detail.SetText(builder.String())
}

func updateTaskSummary(label *walk.Label, model *taskTableModel) {
	if label == nil || model == nil {
		return
	}
	all, active, completed, failed := model.Counts()
	label.SetText(fmt.Sprintf("全部 %d | 活动 %d | 完成 %d | 失败 %d", all, active, completed, failed))
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
