package ui

import (
	"testing"
)

func TestDarkTabThemePaletteIsReadable(t *testing.T) {
	palette := darkTabThemePalette()
	if palette.header == palette.text || palette.selected == palette.text || palette.unselected == palette.text {
		t.Fatalf("dark tab palette has no text contrast: %#v", palette)
	}
	if palette.selected == palette.unselected {
		t.Fatalf("selected and unselected tabs use the same color: %#v", palette)
	}
}
