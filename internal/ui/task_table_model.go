package ui

import (
	"fmt"
	"sort"
	"strings"

	"ghost-downloader-go-win32/internal/core"

	"github.com/lxn/walk"
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

// Walk supplies native selected/focused colors before each subitem draw.
// Preserve them instead of painting dark status text over a blue highlight.
type taskTableCellStyler struct {
	model *taskTableModel
	table **walk.TableView
}

func (s taskTableCellStyler) StyleCell(style *walk.CellStyle) {
	if s.table != nil && *s.table != nil {
		table := *s.table
		if !table.MultiSelection() && table.CurrentIndex() == style.Row() {
			return
		}
		for _, row := range table.SelectedIndexes() {
			if row == style.Row() {
				return
			}
		}
	}
	s.model.StyleCell(style)
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
