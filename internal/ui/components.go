package ui

import (
	"image/color"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The visual vocabulary. Everything on screen is built from these, so spacing
// and colour stay consistent as the UI grows instead of drifting per screen.

// styledText is wrapped text in a theme colour and size. Plain widget.Label
// cannot carry colour, and canvas.Text cannot wrap -- rich text does both.
func styledText(s string, col fyne.ThemeColorName, size fyne.ThemeSizeName, style fyne.TextStyle) *widget.RichText {
	rt := widget.NewRichText(&widget.TextSegment{
		Text:  s,
		Style: widget.RichTextStyle{ColorName: col, SizeName: size, TextStyle: style},
	})
	rt.Wrapping = fyne.TextWrapWord
	return rt
}

func body(s string) *widget.RichText {
	return styledText(s, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
}

// tag is short trailing text that must never wrap. Word wrapping collapses a
// widget's MinSize to its widest single word, so a wrapped two-word value in a
// Border's trailing slot gets silently clipped.
func tag(s string, col fyne.ThemeColorName, style fyne.TextStyle) *widget.RichText {
	rt := widget.NewRichText(&widget.TextSegment{
		Text:  s,
		Style: widget.RichTextStyle{ColorName: col, SizeName: theme.SizeNameCaptionText, TextStyle: style},
	})
	rt.Wrapping = fyne.TextWrapOff
	return rt
}

func muted(s string) *widget.RichText {
	return styledText(s, theme.ColorNamePlaceHolder, theme.SizeNameCaptionText, fyne.TextStyle{})
}

func heading(s string) *widget.RichText {
	return styledText(s, theme.ColorNameForeground, theme.SizeNameSubHeadingText, fyne.TextStyle{Bold: true})
}

// sectionLabel is the small all-caps header above a sidebar block.
func sectionLabel(s string) *widget.RichText {
	return styledText(s, theme.ColorNamePlaceHolder, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})
}

// panel draws content on a rounded filled surface.
func panel(fill color.Color, radius float32, content fyne.CanvasObject) fyne.CanvasObject {
	r := canvas.NewRectangle(fill)
	r.CornerRadius = radius
	return container.NewStack(r, container.NewPadded(content))
}

// accented draws content on a surface with a coloured bar down its left edge,
// which is how a card signals its kind without needing an icon.
func accented(fill, bar color.Color, radius float32, content fyne.CanvasObject) fyne.CanvasObject {
	stripe := canvas.NewRectangle(bar)
	stripe.SetMinSize(fyne.NewSize(3, 0))
	stripe.CornerRadius = 1.5
	return container.NewBorder(nil, nil, stripe, nil, panel(fill, radius, content))
}

// chip is a small inline pill, used to make a plugin tool call visible.
func chip(text string, fg color.Color, bg color.Color) fyne.CanvasObject {
	label := canvas.NewText(text, fg)
	label.TextSize = theme.Size(theme.SizeNameCaptionText)
	label.TextStyle = fyne.TextStyle{Monospace: true}

	r := canvas.NewRectangle(bg)
	r.CornerRadius = 9
	inner := container.New(layoutPadding{h: 9, v: 3}, label)
	return container.NewHBox(container.NewStack(r, inner), layout.NewSpacer())
}

// progressRow is one concept's mastery bar plus its debt marker.
func progressRow(p palette, label string, level, debt float64) fyne.CanvasObject {
	track := canvas.NewRectangle(p.surfaceHi)
	track.CornerRadius = 3
	track.SetMinSize(fyne.NewSize(0, 6))

	fillColor := p.success
	if level < 0.34 {
		fillColor = p.accent
	}
	fill := canvas.NewRectangle(fillColor)
	fill.CornerRadius = 3
	fill.SetMinSize(fyne.NewSize(0, 6))

	bar := container.New(&barLayout{fraction: clamp01(level)}, track, fill)

	name := styledText(label, theme.ColorNameForeground, theme.SizeNameCaptionText, fyne.TextStyle{})
	right := tag(pct(level), theme.ColorNamePlaceHolder, fyne.TextStyle{})
	if debt > 0.35 {
		right = tag("debt "+strconv.FormatFloat(debt, 'f', 1, 64), theme.ColorNameWarning, fyne.TextStyle{Bold: true})
	}
	head := container.NewBorder(nil, nil, nil, right, name)
	return container.NewVBox(head, bar)
}

// barLayout sizes a progress fill to a fraction of the track.
type barLayout struct{ fraction float64 }

func (b *barLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) != 2 {
		return
	}
	objs[0].Resize(size)
	objs[0].Move(fyne.NewPos(0, 0))
	objs[1].Resize(fyne.NewSize(size.Width*float32(b.fraction), size.Height))
	objs[1].Move(fyne.NewPos(0, 0))
}

func (b *barLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(80, 6)
}

// layoutPadding pads a single child by fixed amounts.
type layoutPadding struct{ h, v float32 }

func (p layoutPadding) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(p.h, p.v))
		o.Resize(fyne.NewSize(size.Width-2*p.h, size.Height-2*p.v))
	}
}

func (p layoutPadding) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var w, h float32
	for _, o := range objs {
		m := o.MinSize()
		w = max(w, m.Width)
		h = max(h, m.Height)
	}
	return fyne.NewSize(w+2*p.h, h+2*p.v)
}

func clamp01(v float64) float64 { return max(0, min(1, v)) }

func pct(v float64) string {
	return strconv.Itoa(int(clamp01(v)*100)) + "%"
}

func short(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
