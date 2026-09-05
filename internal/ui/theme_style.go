package ui

import (
	appwin32 "ghost-downloader-go-win32/internal/win32"

	"github.com/lxn/walk"
)

const (
	darkWindowColor  = walk.Color(0x00222120) // RGB(32, 33, 34)
	lightWindowColor = walk.Color(0x00F0F0F0) // RGB(240, 240, 240)
)

type windowThemeStyle struct {
	form            walk.Form
	lightBackground *walk.SolidColorBrush
	darkBackground  *walk.SolidColorBrush
	inputs          *readableInputStyle
	tabs            []*nativeTabTheme
}

func newWindowThemeStyle(form walk.Form, mode string, inputs ...textInput) (*windowThemeStyle, error) {
	lightBackground, err := walk.NewSolidColorBrush(lightWindowColor)
	if err != nil {
		return nil, err
	}
	darkBackground, err := walk.NewSolidColorBrush(darkWindowColor)
	if err != nil {
		lightBackground.Dispose()
		return nil, err
	}
	inputStyle, err := newReadableInputStyle(mode, inputs...)
	if err != nil {
		lightBackground.Dispose()
		darkBackground.Dispose()
		return nil, err
	}
	style := &windowThemeStyle{
		form:            form,
		lightBackground: lightBackground,
		darkBackground:  darkBackground,
		inputs:          inputStyle,
	}
	for _, tab := range findTabWidgets(form) {
		if tabStyle := installNativeTabTheme(tab); tabStyle != nil {
			style.tabs = append(style.tabs, tabStyle)
		}
	}
	style.Apply(mode)
	return style, nil
}

func (s *windowThemeStyle) Apply(mode string) {
	if s == nil || s.form == nil {
		return
	}
	dark := appwin32.DarkModeEnabled(mode)
	background := walk.Brush(s.lightBackground)
	foreground := walk.RGB(17, 17, 17)
	if dark {
		background = s.darkBackground
		foreground = walk.RGB(242, 243, 245)
	}
	s.form.SetBackground(background)
	applyThemeToChildren(s.form, background, foreground)
	appwin32.ApplyTheme(s.form.Handle(), mode)
	s.inputs.Apply(mode)
	appwin32.ApplyControlPalette(s.form.Handle(), dark)
	for _, tab := range s.tabs {
		tab.Apply(dark)
	}
	_ = s.form.Invalidate()
}

func applyThemeToChildren(container walk.Container, background walk.Brush, foreground walk.Color) {
	children := container.Children()
	for index := 0; index < children.Len(); index++ {
		widget := children.At(index)
		switch typed := widget.(type) {
		case *walk.Label:
			typed.SetTextColor(foreground)
		case *walk.TableView:
			typed.SetBackground(background)
		case *walk.ComboBox:
			typed.SetBackground(background)
		case *walk.TabWidget:
			typed.SetBackground(background)
			for pageIndex := 0; pageIndex < typed.Pages().Len(); pageIndex++ {
				page := typed.Pages().At(pageIndex)
				page.SetBackground(background)
				applyThemeToChildren(page, background, foreground)
			}
		}
		if childContainer, ok := widget.(walk.Container); ok {
			childContainer.SetBackground(background)
			applyThemeToChildren(childContainer, background, foreground)
		}
	}
}

func findTabWidgets(container walk.Container) []*walk.TabWidget {
	var result []*walk.TabWidget
	children := container.Children()
	for index := 0; index < children.Len(); index++ {
		widget := children.At(index)
		if tab, ok := widget.(*walk.TabWidget); ok {
			result = append(result, tab)
			continue
		}
		if childContainer, ok := widget.(walk.Container); ok {
			result = append(result, findTabWidgets(childContainer)...)
		}
	}
	return result
}

func (s *windowThemeStyle) Dispose() {
	if s == nil {
		return
	}
	if s.inputs != nil {
		s.inputs.Dispose()
	}
	for _, tab := range s.tabs {
		tab.Dispose()
	}
	s.tabs = nil
	if s.lightBackground != nil {
		s.lightBackground.Dispose()
	}
	if s.darkBackground != nil {
		s.darkBackground.Dispose()
	}
}
