package ui

import (
	"errors"
	"fmt"

	btdownload "ghost-downloader-go-win32/internal/btruntime"
	"ghost-downloader-go-win32/internal/core"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type btSelectionRow struct {
	file    btdownload.File
	checked bool
}

type btSelectionModel struct {
	walk.TableModelBase
	rows      []btSelectionRow
	onChanged func()
}

func newBTSelectionModel(files []btdownload.File) *btSelectionModel {
	model := &btSelectionModel{rows: make([]btSelectionRow, len(files))}
	for index, file := range files {
		model.rows[index] = btSelectionRow{file: file, checked: file.Selected}
	}
	return model
}

func (m *btSelectionModel) RowCount() int {
	if m == nil {
		return 0
	}
	return len(m.rows)
}

func (m *btSelectionModel) Value(row, column int) interface{} {
	if m == nil || row < 0 || row >= len(m.rows) {
		return ""
	}
	file := m.rows[row].file
	switch column {
	case 0:
		return file.Path
	case 1:
		return formatBytes(file.Size)
	default:
		return ""
	}
}

func (m *btSelectionModel) Checked(row int) bool {
	return m != nil && row >= 0 && row < len(m.rows) && m.rows[row].checked
}

func (m *btSelectionModel) SetChecked(row int, checked bool) error {
	if m == nil || row < 0 || row >= len(m.rows) {
		return fmt.Errorf("BitTorrent 文件行 %d 超出范围", row)
	}
	if m.rows[row].checked == checked {
		return nil
	}
	m.rows[row].checked = checked
	m.PublishRowChanged(row)
	m.notifyChanged()
	return nil
}

func (m *btSelectionModel) SelectAll() {
	m.setChecks(func(bool) bool { return true })
}

func (m *btSelectionModel) Clear() {
	m.setChecks(func(bool) bool { return false })
}

func (m *btSelectionModel) Invert() {
	m.setChecks(func(current bool) bool { return !current })
}

func (m *btSelectionModel) SelectedIndexes() ([]int, error) {
	if m == nil {
		return nil, errors.New("请至少选择一个 BitTorrent 文件")
	}
	result := make([]int, 0, len(m.rows))
	for _, row := range m.rows {
		if row.checked {
			result = append(result, row.file.Index)
		}
	}
	if len(result) == 0 {
		return nil, errors.New("请至少选择一个 BitTorrent 文件")
	}
	return result, nil
}

func (m *btSelectionModel) SelectedSize() int64 {
	if m == nil {
		return 0
	}
	var total int64
	for _, row := range m.rows {
		if row.checked {
			total += row.file.Size
		}
	}
	return total
}

func (m *btSelectionModel) selectedCount() int {
	if m == nil {
		return 0
	}
	count := 0
	for _, row := range m.rows {
		if row.checked {
			count++
		}
	}
	return count
}

func (m *btSelectionModel) summary() string {
	return fmt.Sprintf("已选择 %d/%d 个文件 | %s", m.selectedCount(), m.RowCount(), formatBytes(m.SelectedSize()))
}

func (m *btSelectionModel) setChecks(next func(bool) bool) {
	if m == nil {
		return
	}
	changed := false
	for index := range m.rows {
		checked := next(m.rows[index].checked)
		if m.rows[index].checked != checked {
			m.rows[index].checked = checked
			changed = true
		}
	}
	if changed {
		m.PublishRowsReset()
		m.notifyChanged()
	}
}

func (m *btSelectionModel) notifyChanged() {
	if m != nil && m.onChanged != nil {
		m.onChanged()
	}
}

func runBTSelectionDialog(owner walk.Form, task core.Task, themeMode ...string) (core.Task, bool, error) {
	files, err := btdownload.FilesFromTask(task)
	if err != nil {
		return task, false, err
	}
	model := newBTSelectionModel(files)

	var dialog *walk.Dialog
	var table *walk.TableView
	var summaryLabel *walk.Label
	var acceptButton, cancelButton *walk.PushButton
	selectedTask := task
	updateSummary := func() {
		if summaryLabel != nil {
			summaryLabel.SetText(model.summary())
		}
	}
	model.onChanged = updateSummary

	definition := Dialog{
		AssignTo:      &dialog,
		Title:         "选择种子文件 - " + task.Title,
		MinSize:       Size{Width: 720, Height: 460},
		Size:          Size{Width: 820, Height: 560},
		Layout:        VBox{Margins: Margins{Left: 14, Top: 14, Right: 14, Bottom: 14}},
		DefaultButton: &acceptButton,
		CancelButton:  &cancelButton,
		Children: []Widget{
			Label{Text: "请选择要下载的文件；勾选的文件将加入任务。"},
			TableView{
				AssignTo:            &table,
				Model:               model,
				CheckBoxes:          true,
				AlternatingRowBG:    true,
				ColumnsSizable:      true,
				LastColumnStretched: false,
				Columns: []TableViewColumn{
					{Title: "路径", Width: 610},
					{Title: "大小", Width: 130, Alignment: AlignFar},
				},
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					PushButton{Text: "全选", OnClicked: model.SelectAll},
					PushButton{Text: "清空", OnClicked: model.Clear},
					PushButton{Text: "反选", OnClicked: model.Invert},
					HSpacer{},
					Label{AssignTo: &summaryLabel, Text: model.summary()},
				},
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{
						AssignTo: &acceptButton,
						Text:     "添加任务",
						OnClicked: func() {
							selectedIndexes, err := model.SelectedIndexes()
							if err != nil {
								walk.MsgBox(dialog, "种子文件", err.Error(), walk.MsgBoxIconWarning)
								return
							}
							selectedTask, err = btdownload.SetSelectedFiles(task, selectedIndexes)
							if err != nil {
								walk.MsgBox(dialog, "种子文件", err.Error(), walk.MsgBoxIconWarning)
								return
							}
							dialog.Accept()
						},
					},
					PushButton{
						AssignTo:  &cancelButton,
						Text:      "取消",
						OnClicked: func() { dialog.Cancel() },
					},
				},
			},
		},
	}

	if err := definition.Create(owner); err != nil {
		return task, false, err
	}
	mode := "system"
	if len(themeMode) > 0 {
		mode = themeMode[0]
	}
	themeStyle, err := newWindowThemeStyle(dialog, mode)
	if err != nil {
		return task, false, err
	}
	defer themeStyle.Dispose()
	result := dialog.Run()
	if result != walk.DlgCmdOK {
		return task, false, nil
	}
	return selectedTask, true, nil
}
