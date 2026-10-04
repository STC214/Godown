package ui

import (
	"fmt"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"ghost-downloader-go-win32/internal/core"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

type taskFilter int

// Creation time is a default ordering, not the visible size column.
const taskSortCreatedAt = -1

const (
	taskFilterAll taskFilter = iota
	taskFilterActive
	taskFilterCompleted
	taskFilterFailed
)

type taskTableModel struct {
	walk.TableModelBase
	walk.SorterBase

	all        []core.TaskSnapshot
	rows       []core.TaskSnapshot
	searchText string
	filter     taskFilter
	sortColumn int
	sortOrder  walk.SortOrder
	darkMode   bool
}

func newTaskTableModel() *taskTableModel {
	model := &taskTableModel{
		sortColumn: taskSortCreatedAt,
		sortOrder:  walk.SortDescending,
	}
	_ = model.SorterBase.Sort(model.sortColumn, model.sortOrder)
	model.rebuild()
	return model
}

func (m *taskTableModel) RowCount() int {
	return len(m.rows)
}

// ID lets Walk restore the selected task after a reset or sort rather than
// retaining an obsolete row index (or dropping the selection on progress).
func (m *taskTableModel) ID(row int) interface{} {
	if task, ok := m.TaskAt(row); ok {
		return task.ID
	}
	return nil
}

func (m *taskTableModel) Value(row, col int) interface{} {
	if row < 0 || row >= len(m.rows) {
		return ""
	}
	task := m.rows[row]
	switch col {
	case 0:
		return task.Title
	case 1:
		return displayStatus(task.Status)
	case 2:
		return fmt.Sprintf("%.1f%%", task.Progress)
	case 3:
		if task.FileSize <= 0 {
			return formatBytes(task.Received)
		}
		return fmt.Sprintf("%s / %s", formatBytes(task.Received), formatBytes(task.FileSize))
	case 4:
		if task.Speed <= 0 {
			return "-"
		}
		return formatBytes(task.Speed) + "/s"
	case 5:
		return task.Path
	}
	return ""
}

func (m *taskTableModel) Sort(col int, order walk.SortOrder) error {
	m.sortColumn, m.sortOrder = col, order
	m.sortRows()
	return m.SorterBase.Sort(col, order)
}

func (m *taskTableModel) ToggleSort(col int) error {
	order := walk.SortAscending
	if m.sortColumn == col && m.sortOrder == walk.SortAscending {
		order = walk.SortDescending
	}
	return m.Sort(col, order)
}

func (m *taskTableModel) SortState() (int, walk.SortOrder) {
	return m.sortColumn, m.sortOrder
}

func (m *taskTableModel) SetTasks(tasks []core.TaskSnapshot) {
	m.all = append(m.all[:0], tasks...)
	m.rebuild()
}

func (m *taskTableModel) SetSearchText(text string) {
	text = strings.ToLower(strings.TrimSpace(text))
	if m.searchText == text {
		return
	}
	m.searchText = text
	m.rebuild()
}

func (m *taskTableModel) SetFilter(filter taskFilter) {
	if m.filter == filter {
		return
	}
	m.filter = filter
	m.rebuild()
}

func (m *taskTableModel) SetDarkMode(enabled bool) {
	if m.darkMode == enabled {
		return
	}
	m.darkMode = enabled
	m.PublishRowsReset()
	if len(m.rows) > 0 {
		m.PublishRowsChanged(0, len(m.rows)-1)
	}
}

func (m *taskTableModel) StyleCell(style *walk.CellStyle) {
	p := paletteForDarkMode(m.darkMode)
	style.BackgroundColor = p.surface
	if style.Row()%2 == 1 {
		style.BackgroundColor = p.raised
	}
	style.TextColor = p.text
	if style.Col() == 5 {
		style.TextColor = p.muted
	}
	if task, ok := m.TaskAt(style.Row()); ok && style.Col() == 1 {
		style.TextColor = p.statusColor(task.Status, m.darkMode)
	}
}

// Draw selected cells explicitly: native focused/unfocused highlight colors
// vary with Windows themes and can have poor contrast in the dark UI.
type taskTableCellStyler struct {
	model *taskTableModel
	table **walk.TableView
}

// Call directly through syscall's uintptr-escape-aware API. A RECT address
// passed through win.SendMessage's unannotated uintptr parameter may stay on
// a Go stack that moves when the native ListView re-enters Walk's Go callback.
var taskCellRectMessage = syscall.NewLazyDLL("user32.dll").NewProc("SendMessageW")

func (s taskTableCellStyler) StyleCell(style *walk.CellStyle) {
	if s.table != nil && *s.table != nil {
		table := *s.table
		if !table.MultiSelection() && table.CurrentIndex() == style.Row() {
			s.drawSelectedCell(style, table)
			return
		}
		for _, row := range table.SelectedIndexes() {
			if row == style.Row() {
				s.drawSelectedCell(style, table)
				return
			}
		}
	}
	s.model.StyleCell(style)
}

func (s taskTableCellStyler) drawSelectedCell(style *walk.CellStyle, table *walk.TableView) {
	p := paletteForDarkMode(s.model.darkMode)
	style.BackgroundColor, style.TextColor = p.selectedBackground, p.selectedText
	col := style.Col()
	if col < 0 || col >= table.Columns().Len() {
		return // Row-level custom draw has no canvas; subitems paint below.
	}
	lv := selectedTaskListView(table)
	if lv == 0 {
		return
	}
	// Nmcd.Rc may cover the entire row, especially for column zero. Query
	// native cell geometry so text, clipping, DPI and horizontal scrolling agree.
	rect := win.RECT{Top: int32(col), Left: win.LVIR_BOUNDS}
	result, _, _ := taskCellRectMessage.Call(uintptr(lv), win.LVM_GETSUBITEMRECT, uintptr(style.Row()), uintptr(unsafe.Pointer(&rect)))
	if result == 0 || rect.Bottom <= rect.Top {
		return
	}
	rect.Right = rect.Left + int32(win.SendMessage(lv, win.LVM_GETCOLUMNWIDTH, uintptr(col), 0))
	canvas := style.Canvas()
	if canvas == nil {
		return
	}
	fillNativeRect(canvas.HDC(), &rect, win.COLORREF(p.selectedBackground))
	padding := int32(walk.IntFrom96DPI(6, table.DPI()))
	rect.Left += padding
	rect.Right -= padding
	format := walk.TextVCenter | walk.TextSingleLine | walk.TextEndEllipsis | walk.TextNoPrefix
	if table.Columns().At(col).Alignment() == walk.AlignFar {
		format |= walk.TextRight
	}
	_ = canvas.DrawTextPixels(fmt.Sprint(s.model.Value(style.Row(), col)), table.Font(), p.selectedText,
		walk.Rectangle{X: int(rect.Left), Y: int(rect.Top), Width: int(rect.Right - rect.Left), Height: int(rect.Bottom - rect.Top)}, format)
}

// The main task table has no frozen columns; select its non-zero-width native
// ListView, not Walk's outer container or the hidden frozen-column sibling.
func selectedTaskListView(table *walk.TableView) win.HWND {
	for child := win.GetWindow(table.Handle(), win.GW_CHILD); child != 0; child = win.GetWindow(child, win.GW_HWNDNEXT) {
		var name [128]uint16
		length, _ := win.GetClassName(child, &name[0], len(name))
		var rect win.RECT
		if syscall.UTF16ToString(name[:length]) == "SysListView32" && win.GetClientRect(child, &rect) && rect.Right > 0 {
			return child
		}
	}
	return 0
}

func (m *taskTableModel) TaskAt(row int) (core.TaskSnapshot, bool) {
	if row < 0 || row >= len(m.rows) {
		return core.TaskSnapshot{}, false
	}
	return m.rows[row], true
}

func (m *taskTableModel) TaskByID(taskID string) (core.TaskSnapshot, bool) {
	for _, task := range m.all {
		if task.ID == taskID {
			return task, true
		}
	}
	return core.TaskSnapshot{}, false
}

func (m *taskTableModel) Counts() (all, active, completed, failed int) {
	all = len(m.all)
	for _, task := range m.all {
		switch task.Status {
		case core.StatusCompleted:
			completed++
		case core.StatusFailed:
			failed++
			active++
		default:
			active++
		}
	}
	return all, active, completed, failed
}

func (m *taskTableModel) rebuild() {
	m.rows = m.rows[:0]
	for _, task := range m.all {
		if !m.matchesFilter(task) || !m.matchesSearch(task) {
			continue
		}
		m.rows = append(m.rows, task)
	}
	m.sortRows()
	m.PublishRowsReset()
	// Walk resets the virtual ListView count with LVSICF_NOINVALIDATEALL.
	// Existing row pixels must also be refreshed after insertion or reordering.
	if len(m.rows) > 0 {
		m.PublishRowsChanged(0, len(m.rows)-1)
	}
}

func (m *taskTableModel) sortRows() {
	ascending := m.sortOrder == walk.SortAscending
	sort.SliceStable(m.rows, func(i, j int) bool {
		left, right := m.rows[i], m.rows[j]
		comparison := 0
		switch m.sortColumn {
		case 0:
			comparison = strings.Compare(strings.ToLower(left.Title), strings.ToLower(right.Title))
		case 1:
			comparison = strings.Compare(string(left.Status), string(right.Status))
		case 2:
			comparison = compareFloat(left.Progress, right.Progress)
		case 3:
			leftSize, rightSize := left.FileSize, right.FileSize
			if leftSize <= 0 {
				leftSize = left.Received
			}
			if rightSize <= 0 {
				rightSize = right.Received
			}
			comparison = compareInt64(leftSize, rightSize)
		case 4:
			comparison = compareInt64(left.Speed, right.Speed)
		case 5:
			comparison = strings.Compare(strings.ToLower(left.Path), strings.ToLower(right.Path))
		default:
			comparison = left.CreatedAt.Compare(right.CreatedAt)
		}
		if ascending {
			return comparison < 0
		}
		return comparison > 0
	})
}

func compareInt64(left, right int64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func compareFloat(left, right float64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func (m *taskTableModel) matchesSearch(task core.TaskSnapshot) bool {
	if m.searchText == "" {
		return true
	}
	return strings.Contains(strings.ToLower(task.Title), m.searchText) ||
		strings.Contains(strings.ToLower(task.URL), m.searchText) ||
		strings.Contains(strings.ToLower(task.Path), m.searchText)
}

func (m *taskTableModel) matchesFilter(task core.TaskSnapshot) bool {
	switch m.filter {
	case taskFilterActive:
		return task.Status != core.StatusCompleted
	case taskFilterCompleted:
		return task.Status == core.StatusCompleted
	case taskFilterFailed:
		return task.Status == core.StatusFailed
	default:
		return true
	}
}

func displayStatus(status core.TaskStatus) string {
	switch status {
	case core.StatusWaiting:
		return "等待中"
	case core.StatusRunning:
		return "下载中"
	case core.StatusSeeding:
		return "做种中"
	case core.StatusPaused:
		return "已暂停"
	case core.StatusCompleted:
		return "已完成"
	case core.StatusFailed:
		return "失败"
	case core.StatusCanceled:
		return "已取消"
	default:
		return string(status)
	}
}
