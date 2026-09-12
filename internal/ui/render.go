// Package ui renders the desktop shell with Fyne.
package ui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/sirius/cogdebt/internal/ext"
)

// Render turns a ViewSpec into Fyne widgets. emit receives interactions and
// may be nil for a read-only view.
//
// An unknown node type renders as a visible placeholder rather than panicking:
// a plugin newer than the host must degrade, not take the window down.
func Render(v ext.ViewSpec, emit func(ext.ViewEvent)) fyne.CanvasObject {
	switch v.Type {
	case ext.ViewStack:
		return renderStack(v, emit)
	case ext.ViewMarkdown:
		return renderMarkdown(v)
	case ext.ViewAnalogyTable:
		return renderAnalogyTable(v, emit)
	case ext.ViewQuestion:
		return renderQuestion(v, emit)
	case ext.ViewMastery:
		return renderMastery(v)
	case ext.ViewImage:
		return renderImage(v)
	default:
		return muted(fmt.Sprintf("[unsupported view %q — update the app]", v.Type))
	}
}

func renderStack(v ext.ViewSpec, emit func(ext.ViewEvent)) fyne.CanvasObject {
	var p ext.StackProps
	_ = v.DecodeProps(&p)

	kids := make([]fyne.CanvasObject, 0, len(v.Children))
	for _, c := range v.Children {
		kids = append(kids, Render(c, emit))
	}
	if p.Dir == "h" {
		return container.NewHBox(kids...)
	}
	return container.NewVBox(kids...)
}

func renderMarkdown(v ext.ViewSpec) fyne.CanvasObject {
	var p ext.MarkdownProps
	_ = v.DecodeProps(&p)
	return Markdown(p.Text)
}

// Markdown builds a word-wrapped rich text block.
func Markdown(text string) fyne.CanvasObject {
	rt := widget.NewRichTextFromMarkdown(text)
	rt.Wrapping = fyne.TextWrapWord
	return rt
}

// currentPalette resolves the palette for the running app.
func currentPalette() palette {
	if a := fyne.CurrentApp(); a != nil {
		return activePalette(a.Settings().ThemeVariant())
	}
	return darkPalette
}

// renderAnalogyTable draws the hero widget of the whole product.
//
// Rows are cards rather than grid cells: a breakdown is a sentence, and three
// columns of wrapped prose is unreadable at any width. The breakdown line is
// coloured and never omitted -- an analogy without a stated limit leaves the
// learner holding a borrowed intuition past the point it holds, which is how
// this tool would create cognitive debt instead of paying it off.
func renderAnalogyTable(v ext.ViewSpec, emit func(ext.ViewEvent)) fyne.CanvasObject {
	var p ext.AnalogyTableProps
	_ = v.DecodeProps(&p)
	if len(p.Rows) == 0 {
		return muted("(no analogies yet)")
	}
	pal := currentPalette()

	rows := make([]fyne.CanvasObject, 0, len(p.Rows)+1)
	rows = append(rows, sectionLabel("ANALOGY"))
	for _, r := range p.Rows {
		rows = append(rows, analogyRow(pal, r, emit))
	}
	return container.NewVBox(rows...)
}

func analogyRow(pal palette, r ext.AnalogyRow, emit func(ext.ViewEvent)) fyne.CanvasObject {
	mapping := widget.NewRichText(
		&widget.TextSegment{Text: r.Source, Style: widget.RichTextStyle{
			ColorName: theme.ColorNameForeground, TextStyle: fyne.TextStyle{Bold: true}, Inline: true}},
		&widget.TextSegment{Text: "  maps to  ", Style: widget.RichTextStyle{
			ColorName: theme.ColorNamePlaceHolder, SizeName: theme.SizeNameCaptionText, Inline: true}},
		&widget.TextSegment{Text: r.Target, Style: widget.RichTextStyle{
			ColorName: theme.ColorNamePrimary, TextStyle: fyne.TextStyle{Bold: true}, Inline: true}},
	)
	mapping.Wrapping = fyne.TextWrapWord

	lines := []fyne.CanvasObject{mapping}
	if r.SharedRole != "" {
		lines = append(lines, muted("shared role · "+strings.ReplaceAll(r.SharedRole, "_", " ")))
	}
	if r.CarryOver != "" {
		lines = append(lines, body(r.CarryOver))
	}
	lines = append(lines, styledText("⚠  "+r.Breakdown,
		theme.ColorNameWarning, theme.SizeNameText, fyne.TextStyle{Italic: true}))
	if doors := analogyDoors(r, emit); doors != nil {
		lines = append(lines, doors)
	}

	return accented(pal.surface, pal.accent, 8, container.NewVBox(lines...))
}

// analogyDoors are the two ways out of a pair and into more learning.
//
// Without them an analogy is something the learner reads and moves past, and
// the only path onward is whatever question the assessor happens to pick next.
// With them the pair the learner actually cares about becomes the next turn:
// "dig deeper" asks for more of the mapping, "ask me" asks to be tested on it
// rather than told about it — which is the harder and more useful of the two,
// because a pair you can be questioned on is one you have to hold yourself.
//
// A read-only render (a preview, a screenshot) passes no emit and gets no
// buttons, so the card stays a card.
func analogyDoors(r ext.AnalogyRow, emit func(ext.ViewEvent)) fyne.CanvasObject {
	if emit == nil {
		return nil
	}
	pair, _ := json.Marshal(ext.PairPayload{Source: r.Source, Target: r.Target})

	dig := widget.NewButton("Dig deeper", func() {
		emit(ext.ViewEvent{Event: ext.EventDigDeeper, Payload: pair})
	})
	ask := widget.NewButton("Ask me", func() {
		emit(ext.ViewEvent{Event: ext.EventAskMe, Payload: pair})
	})
	ask.Importance = widget.HighImportance

	return container.NewHBox(dig, ask)
}

func renderQuestion(v ext.ViewSpec, emit func(ext.ViewEvent)) fyne.CanvasObject {
	var p ext.QuestionProps
	_ = v.DecodeProps(&p)
	pal := currentPalette()

	header := sectionLabel("QUESTION")
	if p.Level != "" {
		header = sectionLabel(strings.ToUpper(p.Level) + " · " + strings.ToUpper(ladderName(p.Level)))
	}
	prompt := styledText(p.Prompt, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})

	var control fyne.CanvasObject
	var read func() string

	if len(p.Options) > 0 {
		opts := widget.NewRadioGroup(p.Options, nil)
		control, read = opts, func() string { return opts.Selected }
	} else {
		entry := widget.NewMultiLineEntry()
		entry.SetPlaceHolder("Your answer…")
		entry.Wrapping = fyne.TextWrapWord
		control, read = entry, func() string {
			text := entry.Text
			entry.SetText("")
			return text
		}
	}

	send := widget.NewButton("Answer", func() {
		if emit == nil {
			return
		}
		if answer := strings.TrimSpace(read()); answer != "" {
			emit(answerEvent(v.ID, answer))
		}
	})
	send.Importance = widget.HighImportance

	// L2 is the rung that does the teaching, so it is marked differently.
	bar := pal.success
	if strings.EqualFold(p.Level, "L2") {
		bar = pal.accent
	}
	// The button sits in an HBox so it keeps its own width. A VBox stretches
	// its children, and a full-width slab of accent colour under every question
	// shouts louder than the question does.
	return accented(pal.surface, bar, 8,
		container.NewVBox(header, prompt, control, container.NewHBox(send)))
}

func answerEvent(nodeID, answer string) ext.ViewEvent {
	payload, _ := json.Marshal(map[string]string{"answer": answer})
	return ext.ViewEvent{NodeID: nodeID, Event: "submit", Payload: payload}
}

// ladderName spells out a rung so the learner can see the climb.
func ladderName(level string) string {
	switch strings.ToUpper(level) {
	case "L1":
		return "what maps to what"
	case "L2":
		return "where the analogy breaks"
	case "L3":
		return "on its own terms"
	case "L4":
		return "put it together"
	}
	return "question"
}

func renderMastery(v ext.ViewSpec) fyne.CanvasObject {
	var p ext.MasteryProps
	_ = v.DecodeProps(&p)
	if len(p.Items) == 0 {
		return muted("(nothing tracked yet)")
	}
	pal := currentPalette()

	rows := make([]fyne.CanvasObject, 0, len(p.Items))
	for _, it := range p.Items {
		// Debt and mastery are different scales: draw whichever one this row
		// is actually about, so the bar's length and its label agree.
		if it.Debt > 0.4 {
			rows = append(rows, progressRow(pal, it.Label, it.Debt,
				strconv.FormatFloat(it.Debt, 'f', 1, 64), pal.accent))
			continue
		}
		rows = append(rows, progressRow(pal, it.Label, it.Level,
			strconv.Itoa(int(it.Level*100))+"%", pal.success))
	}
	return container.NewVBox(rows...)
}
