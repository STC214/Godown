package ui

import (
	"ghost-downloader-go-win32/internal/core"
	"github.com/lxn/walk"
)

// Shared tokens keep the native controls, table cells and custom buttons in
// agreement. Sizes are logical 96-DPI units; Walk scales them for the monitor.
var taskColumnWidths = [...]int{230, 92, 100, 160, 96, 140}

type visualPalette struct {
	canvas, surface, raised, border walk.Color
	text, muted                     walk.Color
	accent, accentHover, onAccent   walk.Color
	success, warning, danger        walk.Color
}

func paletteForDarkMode(dark bool) visualPalette {
	if dark {
		return visualPalette{
			canvas: walk.RGB(17, 20, 27), surface: walk.RGB(24, 28, 36),
			raised: walk.RGB(32, 38, 49), border: walk.RGB(46, 54, 67),
			text: walk.RGB(231, 237, 247), muted: walk.RGB(156, 170, 189),
			accent: walk.RGB(59, 111, 225), accentHover: walk.RGB(47, 93, 199), onAccent: walk.RGB(255, 255, 255),
			success: walk.RGB(94, 214, 143), warning: walk.RGB(251, 191, 36), danger: walk.RGB(248, 113, 113),
		}
	}
	return visualPalette{
		canvas: walk.RGB(241, 245, 249), surface: walk.RGB(255, 255, 255),
		raised: walk.RGB(245, 247, 251), border: walk.RGB(218, 225, 235),
		text: walk.RGB(30, 41, 59), muted: walk.RGB(100, 116, 139),
		accent: walk.RGB(37, 99, 235), accentHover: walk.RGB(29, 78, 216), onAccent: walk.RGB(255, 255, 255),
		success: walk.RGB(21, 128, 61), warning: walk.RGB(180, 83, 9), danger: walk.RGB(185, 28, 49),
	}
}

func (p visualPalette) statusColor(status core.TaskStatus, dark bool) walk.Color {
	switch status {
	case core.StatusRunning:
		if dark {
			return walk.RGB(147, 190, 255)
		}
		return p.accent
	case core.StatusCompleted, core.StatusSeeding:
		return p.success
	case core.StatusPaused:
		return p.warning
	case core.StatusFailed:
		return p.danger
	default:
		return p.muted
	}
}
