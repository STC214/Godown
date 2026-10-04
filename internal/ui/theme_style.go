package ui

import (
	"syscall"

	appwin32 "ghost-downloader-go-win32/internal/win32"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

const (
	darkWindowColor  = walk.Color(0x001B1411)
	lightWindowColor = walk.Color(0x00F9F5F1)
)

type windowThemeStyle struct {
	form            walk.Form
	lightBackground *walk.SolidColorBrush
	darkBackground  *walk.SolidColorBrush
	inputs          *readableInputStyle
	tabs            []*nativeTabTheme
	buttons         []*nativeButtonStyle
	surfaces        [2]*walk.SolidColorBrush
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
	for i, dark := range []bool{false, true} {
		style.surfaces[i], err = walk.NewSolidColorBrush(paletteForDarkMode(dark).surface)
		if err != nil {
			style.Dispose()
			return nil, err
		}
	}
	style.collectButtons(form)
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
	if dark {
		background = s.darkBackground
	}
	s.form.SetBackground(background)
	appwin32.ApplyTheme(s.form.Handle(), mode)
	appwin32.ApplyControlPalette(s.form.Handle(), dark)
	index := 0
	if dark {
		index = 1
	}
	s.applyChildren(s.form, background, s.surfaces[index], paletteForDarkMode(dark))
	s.inputs.Apply(mode)
	for _, button := range s.buttons {
		color := paletteForDarkMode(dark).canvas
		if brush, ok := button.button.Parent().Background().(*walk.SolidColorBrush); ok {
			color = brush.Color()
		}
		button.Apply(paletteForDarkMode(dark), color)
	}
	for _, tab := range s.tabs {
		tab.Apply(dark)
	}
	_ = s.form.Invalidate()
}

func (s *windowThemeStyle) applyChildren(container walk.Container, background, surface walk.Brush, p visualPalette) {
	children := container.Children()
	for index := 0; index < children.Len(); index++ {
		widget := children.At(index)
		switch typed := widget.(type) {
		case *walk.Label:
			color := p.text
			if typed.Name() == "mutedLabel" {
				color = p.muted
			}
			typed.SetTextColor(color)
		case *walk.TableView:
			typed.SetBackground(surface)
			applyNativeTablePalette(typed.Handle(), p)
		case *walk.ComboBox:
			typed.SetBackground(background)
		case *walk.TabWidget:
			typed.SetBackground(background)
			for pageIndex := 0; pageIndex < typed.Pages().Len(); pageIndex++ {
				page := typed.Pages().At(pageIndex)
				page.SetBackground(background)
				s.applyChildren(page, background, surface, p)
			}
		}
		if childContainer, ok := widget.(walk.Container); ok {
			childBackground := background
			if widget.Name() == "sourceCard" || widget.Name() == "taskCard" {
				childBackground = surface
			}
			childContainer.SetBackground(childBackground)
			s.applyChildren(childContainer, childBackground, surface, p)
		}
	}
}

// Walk's TableView handle is a container, not the normal/frozen ListViews.
// Apply the palette to both native children, including their empty row areas.
func applyNativeTablePalette(parent win.HWND, p visualPalette) {
	for child := win.GetWindow(parent, win.GW_CHILD); child != 0; child = win.GetWindow(child, win.GW_HWNDNEXT) {
		var name [128]uint16
		length, _ := win.GetClassName(child, &name[0], len(name))
		if syscall.UTF16ToString(name[:length]) == "SysListView32" {
			win.SendMessage(child, win.LVM_SETBKCOLOR, 0, uintptr(p.surface))
			win.SendMessage(child, win.LVM_SETTEXTBKCOLOR, 0, uintptr(p.surface))
			win.SendMessage(child, win.LVM_SETTEXTCOLOR, 0, uintptr(p.text))
		}
		applyNativeTablePalette(child, p)
	}
}

func (s *windowThemeStyle) collectButtons(container walk.Container) {
	for i := 0; i < container.Children().Len(); i++ {
		widget := container.Children().At(i)
		if button, ok := widget.(*walk.PushButton); ok && button.Name() != "" {
			if style := installButtonStyle(button); style != nil {
				s.buttons = append(s.buttons, style)
			}
		}
		if child, ok := widget.(walk.Container); ok {
			s.collectButtons(child)
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
	for _, button := range s.buttons {
		button.Dispose()
	}
	s.buttons = nil
	for _, surface := range s.surfaces {
		if surface != nil {
			surface.Dispose()
		}
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
