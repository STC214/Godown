package ui

import (
	"math"
	"testing"

	"ghost-downloader-go-win32/internal/core"
	"github.com/lxn/walk"
)

func colorLuminance(color walk.Color) float64 {
	channels := []float64{float64(color & 255), float64((color >> 8) & 255), float64((color >> 16) & 255)}
	for i, channel := range channels {
		channel /= 255
		if channel <= 0.04045 {
			channels[i] = channel / 12.92
		} else {
			channels[i] = math.Pow((channel+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2]
}

func TestVisualPaletteTextContrast(t *testing.T) {
	for _, dark := range []bool{false, true} {
		p := paletteForDarkMode(dark)
		pairs := [][2]walk.Color{{p.text, p.surface}, {p.muted, p.surface}, {p.text, p.raised}, {p.onAccent, p.accent}, {p.onAccent, p.accentHover}}
		for _, status := range []core.TaskStatus{core.StatusRunning, core.StatusCompleted, core.StatusSeeding, core.StatusFailed, core.StatusPaused, core.StatusWaiting} {
			pairs = append(pairs, [2]walk.Color{p.statusColor(status, dark), p.surface})
		}
		for _, pair := range pairs {
			left, right := colorLuminance(pair[0]), colorLuminance(pair[1])
			contrast := (math.Max(left, right) + 0.05) / (math.Min(left, right) + 0.05)
			if contrast < 4.5 {
				t.Errorf("dark=%v foreground=%x background=%x contrast=%.2f, want >=4.5", dark, pair[0], pair[1], contrast)
			}
		}
	}
}

func TestSelectedTaskContrast(t *testing.T) {
	for _, dark := range []bool{false, true} {
		p := paletteForDarkMode(dark)
		a, b := colorLuminance(p.selectedText), colorLuminance(p.selectedBackground)
		ratio := (math.Max(a, b) + 0.05) / (math.Min(a, b) + 0.05)
		if ratio < 7 {
			t.Errorf("dark=%v selected text contrast=%.2f want >=7", dark, ratio)
		}
		t.Logf("dark=%v selected text contrast=%.2f:1", dark, ratio)
	}
}
