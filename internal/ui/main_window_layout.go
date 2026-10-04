package ui

import (
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

// The layout is separate from scheduler actions so native layout/visual tests
// can exercise the very same window without downloads, storage or a tray icon.
type mainWindowControls struct {
	window          **walk.MainWindow
	status, summary **walk.Label
	url, search     **walk.LineEdit
	filter          **walk.ComboBox
	table           **walk.TableView
	detail          **walk.TextEdit
	headers         *[6]*walk.PushButton
	model           *taskTableModel
}

type mainWindowActions struct {
	add, torrent, settings, updates, logs func()
	startAll, pauseAll                    func()
	toggle, redownload, btFiles, remove   func()
	openFolder, openFile                  func()
	search, filter, selection             func()
	sort                                  func(int)
}

func secondaryButton(text string, onClick func()) PushButton {
	return PushButton{Name: "secondaryButton", Text: text, MinSize: Size{Width: 76, Height: 32}, OnClicked: onClick}
}

func trackStatusTooltip(label *walk.Label) {
	label.SetToolTipText(label.Text())
	label.AsWindowBase().Property("Text").Changed().Attach(func() { label.SetToolTipText(label.Text()) })
}

func mainWindowLayout(version, downloadDir string, c mainWindowControls, a mainWindowActions) MainWindow {
	headerTitles := [...]string{"名称", "状态", "进度", "大小", "速度", "目录"}
	headers := make([]Widget, len(headerTitles))
	for i, title := range headerTitles {
		column := i
		button := PushButton{
			Name: "sortButton", AssignTo: &c.headers[i], Text: title,
			MinSize: Size{Width: taskColumnWidths[i], Height: 30},
			MaxSize: Size{Width: taskColumnWidths[i]},
			OnClicked: func() {
				if a.sort != nil {
					a.sort(column)
				}
			},
		}
		if i == len(headers)-1 {
			button.MinSize.Width, button.MaxSize.Width, button.StretchFactor = 0, 0, 1
		}
		headers[i] = button
	}
	return MainWindow{
		AssignTo: c.window,
		Title:    "Ghost Downloader · " + version,
		Font:     Font{Family: "Microsoft YaHei UI", PointSize: 9},
		MinSize:  Size{Width: 860, Height: 650}, Size: Size{Width: 1160, Height: 780},
		Layout: VBox{Margins: Margins{Left: 20, Top: 16, Right: 20, Bottom: 14}, Spacing: 14},
		Children: []Widget{
			Composite{
				Name: "brandHeader", Layout: HBox{MarginsZero: true, Spacing: 10},
				Children: []Widget{
					Composite{
						Layout: VBox{MarginsZero: true, Spacing: 2},
						Children: []Widget{
							Label{Name: "brandTitle", Text: "Ghost Downloader", Font: Font{Family: "Segoe UI", PointSize: 22, Bold: true}},
							Label{Name: "mutedLabel", Text: "轻松管理每一次下载"},
						},
					},
					HSpacer{},
					PushButton{Name: "utilityButton", Text: "设置", MinSize: Size{Width: 64, Height: 32}, OnClicked: a.settings},
					PushButton{Name: "utilityButton", Text: "检查更新", MinSize: Size{Width: 88, Height: 32}, OnClicked: a.updates},
					PushButton{Name: "utilityButton", Text: "日志", MinSize: Size{Width: 64, Height: 32}, OnClicked: a.logs},
				},
			},
			Composite{
				Name: "sourceCard", Layout: VBox{Margins: Margins{Left: 18, Top: 14, Right: 18, Bottom: 16}, Spacing: 10},
				Children: []Widget{
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							Label{Text: "新建下载", Font: Font{Family: "Microsoft YaHei UI", PointSize: 11, Bold: true}},
							HSpacer{},
							Label{Name: "mutedLabel", Text: "HTTP / FTP / Magnet / Torrent"},
						},
					},
					Composite{
						Layout: HBox{MarginsZero: true, Spacing: 10},
						Children: []Widget{
							LineEdit{Name: "sourceInput", AssignTo: c.url, CueBanner: "粘贴下载地址，开始新的任务…", MinSize: Size{Width: 160, Height: 38}, StretchFactor: 1},
							PushButton{Name: "primaryButton", Text: "+ 添加任务", MinSize: Size{Width: 112, Height: 38}, OnClicked: a.add},
							PushButton{Name: "secondaryButton", Text: "导入种子", MinSize: Size{Width: 96, Height: 38}, OnClicked: a.torrent},
						},
					},
				},
			},
			Composite{
				Name: "taskCard", StretchFactor: 1,
				Layout: VBox{Margins: Margins{Left: 18, Top: 16, Right: 18, Bottom: 14}, Spacing: 10},
				Children: []Widget{
					Composite{
						Layout: HBox{MarginsZero: true, Spacing: 10},
						Children: []Widget{
							Label{Text: "下载任务", Font: Font{Family: "Microsoft YaHei UI", PointSize: 16, Bold: true}},
							HSpacer{},
							LineEdit{Name: "searchInput", AssignTo: c.search, CueBanner: "搜索名称、链接或目录", MinSize: Size{Width: 220, Height: 32}, MaxSize: Size{Width: 280}, OnTextChanged: a.search},
							ComboBox{AssignTo: c.filter, Model: []string{"全部", "活动", "已完成", "失败"}, CurrentIndex: 0, MinSize: Size{Width: 100, Height: 32}, OnCurrentIndexChanged: a.filter},
						},
					},
					Label{Name: "mutedLabel", AssignTo: c.summary, Text: "全部 0  ·  活动 0  ·  完成 0  ·  失败 0"},
					Composite{
						Layout: HBox{MarginsZero: true, Spacing: 8},
						Children: []Widget{
							secondaryButton("暂停/继续", a.toggle), secondaryButton("重新下载", a.redownload),
							secondaryButton("BT 文件", a.btFiles), secondaryButton("打开目录", a.openFolder),
							secondaryButton("打开文件", a.openFile), secondaryButton("移除任务", a.remove),
							HSpacer{}, secondaryButton("全部开始", a.startAll), secondaryButton("全部暂停", a.pauseAll),
						},
					},
					Composite{Name: "tableHeader", Layout: HBox{MarginsZero: true, SpacingZero: true}, Children: headers},
					TableView{
						AssignTo: c.table, MinSize: Size{Height: 160}, StretchFactor: 1,
						AlternatingRowBG: false, ColumnsOrderable: false, ColumnsSizable: false,
						CustomRowHeight: 42, HeaderHidden: true, LastColumnStretched: true,
						Model: c.model, CellStyler: taskTableCellStyler{model: c.model, table: c.table},
						SelectionHiddenWithoutFocus: false,
						ContextMenuItems: []MenuItem{
							Action{Text: "暂停/继续", OnTriggered: a.toggle}, Action{Text: "重新下载", OnTriggered: a.redownload},
							Action{Text: "选择 BT 文件", OnTriggered: a.btFiles}, Separator{},
							Action{Text: "打开目录", OnTriggered: a.openFolder}, Action{Text: "打开文件", OnTriggered: a.openFile},
							Separator{}, Action{Text: "移除任务", OnTriggered: a.remove},
						},
						Columns: []TableViewColumn{
							{Title: headerTitles[0], Width: taskColumnWidths[0]}, {Title: headerTitles[1], Width: taskColumnWidths[1]},
							{Title: headerTitles[2], Width: taskColumnWidths[2], Alignment: AlignFar}, {Title: headerTitles[3], Width: taskColumnWidths[3], Alignment: AlignFar},
							{Title: headerTitles[4], Width: taskColumnWidths[4], Alignment: AlignFar}, {Title: headerTitles[5], Width: taskColumnWidths[5]},
						},
						OnCurrentIndexChanged: a.selection,
					},
					Composite{
						Layout:   HBox{MarginsZero: true},
						Children: []Widget{Label{Text: "任务详情", Font: Font{Family: "Microsoft YaHei UI", PointSize: 10, Bold: true}}, HSpacer{}, Label{Name: "mutedLabel", Text: "右键任务可查看更多操作"}},
					},
					TextEdit{Name: "detailInput", AssignTo: c.detail, ReadOnly: true, VScroll: true, MinSize: Size{Height: 84}, MaxSize: Size{Height: 100}, Text: "选择任务以查看下载进度和详细信息。"},
				},
			},
			Composite{
				Name: "statusBar", Layout: HBox{MarginsZero: true},
				Children: []Widget{Label{
					Name: "mutedLabel", AssignTo: c.status, Text: "就绪  ·  下载目录：" + downloadDir,
					EllipsisMode: EllipsisEnd, NoPrefix: true, StretchFactor: 1,
				}},
			},
		},
	}
}
