package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// palette is one complete colour set. Defining both variants from the same
// struct keeps light and dark from drifting apart as the UI grows.
type palette struct {
	bg        color.Color // window ground
	surface   color.Color // cards, bubbles
	surfaceHi color.Color // inputs, raised rows
	line      color.Color // separators, borders
	fg        color.Color // primary text
	muted     color.Color // secondary text
	accent    color.Color // the learning accent: also "where it breaks"
	success   color.Color // mastery
	warn      color.Color // breakdown emphasis
	danger    color.Color
}

func rgb(hex uint32) color.NRGBA {
	return color.NRGBA{R: uint8(hex >> 16), G: uint8(hex >> 8), B: uint8(hex), A: 0xff}
}

var darkPalette = palette{
	bg:        rgb(0x14161A),
	surface:   rgb(0x1C1F25),
	surfaceHi: rgb(0x242932),
	line:      rgb(0x2C323C),
	fg:        rgb(0xE6E8EC),
	muted:     rgb(0x8C93A1),
	accent:    rgb(0xE8A33D),
	success:   rgb(0x4FBF8B),
	warn:      rgb(0xE8834A),
	danger:    rgb(0xE05A5A),
}

var lightPalette = palette{
	bg:        rgb(0xFAFAF8),
	surface:   rgb(0xFFFFFF),
	surfaceHi: rgb(0xF0F1F3),
	line:      rgb(0xDFE1E6),
	fg:        rgb(0x1B1E24),
	muted:     rgb(0x6B7280),
	accent:    rgb(0xB8792A),
	success:   rgb(0x2F855A),
	warn:      rgb(0xC2571F),
	danger:    rgb(0xC0392B),
}

// activePalette resolves the palette for a variant.
func activePalette(v fyne.ThemeVariant) palette {
	if v == theme.VariantLight {
		return lightPalette
	}
	return darkPalette
}

// appTheme is the application look. It delegates anything it does not override
// to the stock theme, so future Fyne additions keep working.
type appTheme struct{ base fyne.Theme }

func newTheme() fyne.Theme { return &appTheme{base: theme.DefaultTheme()} }

func (t *appTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	p := activePalette(v)
	switch n {
	case theme.ColorNameBackground:
		return p.bg
	case theme.ColorNameForeground:
		return p.fg
	case theme.ColorNamePlaceHolder, theme.ColorNameDisabled:
		return p.muted
	case theme.ColorNamePrimary, theme.ColorNameFocus, theme.ColorNameHyperlink:
		return p.accent
	case theme.ColorNameButton, theme.ColorNameInputBackground:
		return p.surfaceHi
	case theme.ColorNameSeparator, theme.ColorNameInputBorder:
		return p.line
	case theme.ColorNameHover, theme.ColorNamePressed, theme.ColorNameSelection:
		return p.surfaceHi
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground, theme.ColorNameHeaderBackground:
		return p.surface
	case theme.ColorNameSuccess:
		return p.success
	case theme.ColorNameWarning:
		return p.warn
	case theme.ColorNameError:
		return p.danger
	case theme.ColorNameShadow:
		return color.NRGBA{A: 0x40}
	}
	return t.base.Color(n, v)
}

func (t *appTheme) Font(s fyne.TextStyle) fyne.Resource { return t.base.Font(s) }

func (t *appTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return t.base.Icon(n) }

// Size widens the default spacing: the stock theme is tuned for dense forms,
// and this app is mostly prose the learner has to actually read.
func (t *appTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameText:
		return 14
	case theme.SizeNameHeadingText:
		return 21
	case theme.SizeNameSubHeadingText:
		return 16
	case theme.SizeNameCaptionText:
		return 12
	case theme.SizeNamePadding:
		return 5
	case theme.SizeNameInnerPadding:
		return 10
	case theme.SizeNameLineSpacing:
		return 5
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius:
		return 8
	case theme.SizeNameScrollBar:
		return 10
	}
	return t.base.Size(n)
}

var _ fyne.Theme = (*appTheme)(nil)
