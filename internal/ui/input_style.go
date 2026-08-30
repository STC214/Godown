package ui

import (
	"syscall"

	appwin32 "ghost-downloader-go-win32/internal/win32"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// textInput is implemented by Walk's LineEdit and TextEdit.  Windows' dark
// Explorer theme can otherwise paint an unfocused edit control black while
// Walk still supplies the light-theme text color.
type textInput interface {
	Handle() win.HWND
	SetBackground(walk.Brush)
	SetTextColor(walk.Color)
	Invalidate() error
}

type readableInputStyle struct {
	lightBackground *walk.SolidColorBrush
	darkBackground  *walk.SolidColorBrush
	inputs          []textInput
}

func newReadableInputStyle(mode string, inputs ...textInput) (*readableInputStyle, error) {
	lightBackground, err := walk.NewSolidColorBrush(walk.RGB(255, 255, 255))
	if err != nil {
		return nil, err
	}
	darkBackground, err := walk.NewSolidColorBrush(walk.RGB(43, 45, 49))
	if err != nil {
		lightBackground.Dispose()
		return nil, err
	}
	style := &readableInputStyle{lightBackground: lightBackground, darkBackground: darkBackground, inputs: inputs}
	style.Apply(mode)
	return style, nil
}

func (s *readableInputStyle) Apply(mode string) {
	if s == nil {
		return
	}
	dark := appwin32.DarkModeEnabled(mode)
	background := walk.Brush(s.lightBackground)
	text := walk.RGB(17, 17, 17)
	if dark {
		background = s.darkBackground
		text = walk.RGB(242, 243, 245)
	}
	// An empty sub-app theme disables themed EDIT painting. Walk can then honor
	// the explicit background and text colors consistently before first focus.
	empty := syscall.StringToUTF16Ptr("")
	for _, input := range s.inputs {
		if input == nil {
			continue
		}
		_ = win.SetWindowTheme(input.Handle(), empty, empty)
		input.SetBackground(background)
		input.SetTextColor(text)
		_ = input.Invalidate()
	}
}

func (s *readableInputStyle) Dispose() {
	if s == nil {
		return
	}
	if s.lightBackground != nil {
		s.lightBackground.Dispose()
	}
	if s.darkBackground != nil {
		s.darkBackground.Dispose()
	}
}
