package ui

import (
	"fmt"
	"sort"
	"strings"

	"ghost-downloader-go-win32/internal/core"

	"github.com/lxn/walk"
)

type taskFilter int

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
		sortColumn: 3,
		sortOrder:  walk.SortDescending,
	}
	model.rebuild()
	return model
}

func (m *taskTableModel) RowCount() int {
	return len(m.rows)
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
}

func (m *taskTableModel) StyleCell(style *walk.CellStyle) {
	if !m.darkMode {
		return
	}
	style.BackgroundColor = walk.RGB(30, 31, 34)
	if style.Row()%2 == 1 {
		style.BackgroundColor = walk.RGB(38, 39, 43)
	}
	style.TextColor = walk.RGB(242, 243, 245)
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
			comparison = left.CreatedAt.Compare(right.CreatedAt)
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
