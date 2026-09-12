package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"unicode"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/sirius/cogdebt/internal/ext"
)

// Config wires the shell to the rest of the system.
type Config struct {
	// Title is the window title.
	Title string
	// Model is shown as a badge in the header.
	Model string
	// Bridge runs the agent. Required.
	Bridge *Bridge
	// Mastery supplies the progress panel after each turn. Optional.
	Mastery func(context.Context) []ext.MasteryItem
	// Analogies supplies every analogy stored for the learner, newest first.
	//
	// It is read from the store rather than from tool results because the
	// analogy agent records its own pairs, and agenttool does not surface a
	// sub-agent's tool events to the parent stream. The store is the one place
	// both transports agree on.
	Analogies func(context.Context) []ext.AnalogyRow
	// Plugins is the loaded plugin summary, listed in the sidebar so the
	// plugin layer is visible without opening a terminal.
	Plugins []string
	// Settings wires the configuration dialog. Zero value hides the button.
	Settings SettingsConfig
	// Scripted marks a run that plays a fixed script instead of calling a
	// model. The footer says so, and typing is inert -- a demo that silently
	// ignored what someone typed into it would be worse than one that says it
	// is a recording.
	Scripted bool
}

// Shell is the desktop window.
type Shell struct {
	cfg Config
	ctx context.Context
	pal palette

	app     fyne.App
	win     fyne.Window
	feed    *fyne.Container
	scroll  *container.Scroll
	input   *widget.Entry
	send    *widget.Button
	side    *fyne.Container
	spinner *widget.ProgressBarInfinite

	// streaming holds the assistant reply being built this turn.
	streaming *strings.Builder
	streamAt  int // index of the streaming widget in feed, -1 when idle
	// shownAnalogies counts rows already drawn, so a turn renders only new ones.
	shownAnalogies int
	// cyrillic is set once the learner writes in Cyrillic. See phrase.
	cyrillic bool
	// liveQuestions retires question cards that are still forms. A learner who
	// answers in the message box below, or who is simply asked something new,
	// leaves the old card live otherwise -- and a live card can be answered a
	// second time, which the assessor would grade as a second attempt at a
	// question it asked once.
	liveQuestions []func()
}

// New builds the window. Call Run to show it.
func New(ctx context.Context, cfg Config) *Shell {
	if cfg.Title == "" {
		cfg.Title = "cogdebt"
	}
	s := &Shell{cfg: cfg, ctx: ctx, streaming: &strings.Builder{}, streamAt: -1}

	s.app = fyneapp.New()
	s.app.Settings().SetTheme(newTheme())
	s.pal = activePalette(s.app.Settings().ThemeVariant())

	s.win = s.app.NewWindow(cfg.Title)
	s.win.Resize(fyne.NewSize(1180, 780))

	s.feed = container.NewVBox()
	// The vertical scrollbar is drawn over the content, so the feed carries its
	// own right-hand clearance; without it the bar sits on top of every card.
	s.scroll = container.NewVScroll(container.New(layoutPadding{h: 8}, s.feed))

	s.win.SetContent(container.NewBorder(
		s.header(),
		s.footer(),
		nil,
		s.sidebar(),
		container.NewPadded(s.scroll),
	))
	s.greet()
	return s
}

// Run shows the window and blocks until it closes.
func (s *Shell) Run() { s.win.ShowAndRun() }

func (s *Shell) header() fyne.CanvasObject {
	title := styledText("cogdebt", theme.ColorNameForeground, theme.SizeNameSubHeadingText, fyne.TextStyle{Bold: true})
	subtitle := muted("learn a new field through the one you already hold")

	var badge fyne.CanvasObject = layoutBlank()
	if s.cfg.Model != "" {
		badge = chip(s.cfg.Model, s.pal.accent, s.pal.surfaceHi)
	}
	right := container.NewHBox(badge, s.settingsButton())

	bar := container.NewBorder(nil, nil, container.NewVBox(title, subtitle), right)
	line := canvas.NewRectangle(s.pal.line)
	line.SetMinSize(fyne.NewSize(0, 1))
	return container.NewVBox(container.NewPadded(bar), line)
}

func (s *Shell) footer() fyne.CanvasObject {
	s.input = widget.NewEntry()
	s.input.SetPlaceHolder("What do you already know?  e.g. Kubernetes, Go, distributed systems")
	s.input.OnSubmitted = func(string) { s.submit() }

	s.send = widget.NewButton("Send", s.submit)
	s.send.Importance = widget.HighImportance

	if s.cfg.Scripted {
		s.input.SetPlaceHolder("Scripted run — no model is being called")
		s.input.Disable()
	}

	s.spinner = widget.NewProgressBarInfinite()
	s.spinner.Hide()

	row := container.NewBorder(nil, nil, nil, s.send, s.input)
	line := canvas.NewRectangle(s.pal.line)
	line.SetMinSize(fyne.NewSize(0, 1))
	return container.NewVBox(line, container.New(layoutHeight{h: 3}, s.spinner), container.NewPadded(row))
}

func (s *Shell) sidebar() fyne.CanvasObject {
	s.side = container.NewVBox(muted("Answer a question and progress appears here."))

	// No heading here: refreshMastery emits its own group labels, and a
	// "PROGRESS" above "COGNITIVE DEBT" reads as a category error.
	blocks := container.NewVBox(s.side)
	if s.cfg.Settings.Fields != nil {
		if note := missingKeysNote(s.cfg.Settings.Fields()); note != "" {
			blocks.Add(widget.NewSeparator())
			blocks.Add(sectionLabel("UNCONFIGURED"))
			blocks.Add(muted(note))
		}
	}
	if len(s.cfg.Plugins) > 0 {
		rows := make([]fyne.CanvasObject, 0, len(s.cfg.Plugins))
		for _, p := range s.cfg.Plugins {
			rows = append(rows, muted(p))
		}
		blocks.Add(widget.NewSeparator())
		blocks.Add(sectionLabel("PLUGINS"))
		blocks.Add(container.NewVBox(rows...))
	}

	// Pad the right edge: the vertical scrollbar overlays content, and without
	// clearance it eats the trailing percentage on every progress row.
	scroll := container.NewVScroll(container.New(layoutPadding{h: 14, v: 10}, blocks))
	scroll.SetMinSize(fyne.NewSize(330, 0))

	line := canvas.NewRectangle(s.pal.line)
	line.SetMinSize(fyne.NewSize(1, 0))
	return container.NewBorder(nil, nil, line, nil, scroll)
}

func (s *Shell) greet() {
	s.feed.Add(container.NewPadded(container.NewVBox(
		heading("Tell me what you already know"),
		body("Then name what you want to learn. I will map the new field onto the one you already hold — "+
			"and mark where that map stops working."),
	)))
	s.feed.Refresh()
}

// submit sends the current input to the agent.
func (s *Shell) submit() {
	text := strings.TrimSpace(s.input.Text)
	if text == "" || s.send.Disabled() || s.cfg.Bridge == nil {
		return
	}
	s.input.SetText("")
	s.appendUser(text)
	s.setBusy(true)

	s.streaming.Reset()
	s.streamAt = -1

	s.cfg.Bridge.Send(s.ctx, text, Handler{
		OnText:       s.streamText,
		OnToolCall:   s.appendToolCall,
		OnToolResult: s.appendToolResult,
		OnError:      s.appendError,
		OnDone:       s.finishTurn,
	})
}

func (s *Shell) appendUser(text string) {
	// Any message from the learner settles the open question: they either
	// answered it or moved past it. Doing this here rather than in submit
	// covers the scripted run too, which appends the learner's turns directly.
	s.retireQuestions()
	s.noteScript(text)
	bubble := panel(s.pal.surfaceHi, 10, body(text))
	// Indent from the left so the learner's own words read as a distinct column.
	s.feed.Add(container.NewBorder(nil, nil, spacer(120), nil, bubble))
	s.bump()
}

// streamText appends a chunk to the reply being built, replacing the widget in
// place so the text grows instead of fragmenting into many blocks.
func (s *Shell) streamText(chunk string) {
	s.streaming.WriteString(chunk)
	md := container.NewPadded(Markdown(s.streaming.String()))

	if s.streamAt >= 0 && s.streamAt < len(s.feed.Objects) {
		s.feed.Objects[s.streamAt] = md
	} else {
		s.streamAt = len(s.feed.Objects)
		s.feed.Add(md)
	}
	s.bump()
}

// appendToolCall makes a plugin firing visible. On stage this is the proof
// that the plugin layer is real rather than decorative.
func (s *Shell) appendToolCall(name string) {
	s.feed.Add(container.NewPadded(chip("⚙ "+name, s.pal.muted, s.pal.surface)))
	// The next text chunk starts a fresh block, below this marker.
	s.streaming.Reset()
	s.streamAt = -1
	s.bump()
}

func (s *Shell) appendError(err error) {
	card := accented(s.pal.surface, s.pal.danger, 8,
		styledText(err.Error(), theme.ColorNameError, theme.SizeNameText, fyne.TextStyle{}))
	s.feed.Add(container.NewPadded(card))
	s.bump()
}

// appendToolResult turns a known plugin result into a card.
//
// Only results whose shape the host understands are drawn; anything else is
// left to the model to narrate. This is how the ladder and the analogy table
// reach the screen as structured data instead of parsed prose.
func (s *Shell) appendToolResult(name string, result map[string]any) {
	raw, err := json.Marshal(result)
	if err != nil {
		return
	}
	switch {
	case strings.HasSuffix(name, "assessor_ask"), name == "assessor_ask":
		var q ext.QuestionProps
		var id struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &q) != nil || q.Prompt == "" {
			return
		}
		_ = json.Unmarshal(raw, &id)
		s.AppendView(ext.View(ext.ViewQuestion, id.ID, q))

	case strings.HasSuffix(name, "profile_save_analogy"):
		// Draw from the store rather than from this result. The analogy agent
		// records its own pairs and the tutor records them again as a safety
		// net, so the same table arrives twice; the store is the one place that
		// has already collapsed them.
		s.drawAnalogies()

	case strings.HasSuffix(name, "fal_illustrate"):
		var img struct {
			URL    string `json:"url"`
			Source string `json:"source"`
			Target string `json:"target"`
		}
		if json.Unmarshal(raw, &img) != nil || img.URL == "" {
			return
		}
		// The caption repeats the pair in text because the generator garbles
		// labels inside the picture often enough that the image alone cannot be
		// trusted to say which side is which.
		s.AppendView(ext.View(ext.ViewImage, "", ext.ImageProps{
			URL:     img.URL,
			Caption: img.Source + "  maps to  " + img.Target,
			Alt:     "drawing " + img.Source + " against " + img.Target + "…",
		}))
	}
	// A card starts a fresh text block beneath it.
	s.streaming.Reset()
	s.streamAt = -1
}

// AppendView renders a ViewSpec from a UI plugin into the feed.
func (s *Shell) AppendView(spec ext.ViewSpec) {
	if spec.Type == ext.ViewQuestion {
		// Only one question is open at a time, because the assessor only parks
		// one. A new card arriving means the last one is no longer answerable.
		s.retireQuestions()
		card, retire := renderQuestionCard(spec, s.onViewEvent)
		s.liveQuestions = append(s.liveQuestions, retire)
		s.feed.Add(container.NewPadded(card))
		s.bump()
		return
	}
	s.feed.Add(container.NewPadded(Render(spec, s.onViewEvent)))
	s.bump()
}

// retireQuestions turns every open card back into a record of what was asked.
func (s *Shell) retireQuestions() {
	for _, retire := range s.liveQuestions {
		retire()
	}
	s.liveQuestions = nil
}

func (s *Shell) onViewEvent(ev ext.ViewEvent) {
	if text := s.messageFor(ev); text != "" {
		s.input.SetText(text)
		s.submit()
	}
}

// messageFor turns a view event into the learner's next message.
//
// The buttons on an analogy card are shortcuts for something the learner could
// have typed, so that is exactly what they produce: a message, in the feed,
// visible in the transcript. Nothing happens behind their back.
func (s *Shell) messageFor(ev ext.ViewEvent) string {
	switch ev.Event {
	case ext.EventSubmit, "":
		var p struct {
			Answer string `json:"answer"`
		}
		_ = decodePayload(ev.Payload, &p)
		return strings.TrimSpace(p.Answer)

	case ext.EventDigDeeper, ext.EventAskMe:
		var p ext.PairPayload
		if decodePayload(ev.Payload, &p) != nil || p.Source == "" || p.Target == "" {
			return ""
		}
		return phrase(ev.Event, p, s.cyrillic)
	}
	return ""
}

// phrase writes the message a door produces.
//
// It is written in the learner's own script because the tutor answers in the
// language it is addressed in: an English button that emits English prose would
// silently switch a Russian conversation over, mid-lesson. Looking at what the
// learner has actually been typing is crude, and it is the whole of what is
// needed to keep that from happening.
func phrase(event string, p ext.PairPayload, cyrillic bool) string {
	if event == ext.EventAskMe {
		if cyrillic {
			return fmt.Sprintf("Задай мне вопрос по паре «%s — %s». Не объясняй, спрашивай.", p.Source, p.Target)
		}
		return fmt.Sprintf("Ask me a question about %q mapping to %q. Don't explain it, test me on it.", p.Source, p.Target)
	}
	if cyrillic {
		return fmt.Sprintf("Копни глубже в пару «%s — %s»: что ещё переносится и где именно это ломается?", p.Source, p.Target)
	}
	return fmt.Sprintf("Dig deeper into %q mapping to %q: what else carries over, and where exactly does it break?", p.Source, p.Target)
}

// decodePayload reads an event payload, tolerating an absent one.
func decodePayload(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// noteScript records which script the learner writes in, so the doors can
// answer in it. Latin is the default because everything else in the UI is.
func (s *Shell) noteScript(text string) {
	for _, r := range text {
		if unicode.Is(unicode.Cyrillic, r) {
			s.cyrillic = true
			return
		}
	}
}

func (s *Shell) finishTurn() {
	s.setBusy(false)
	// A sub-agent's tool calls do not reach this stream, so an analogy recorded
	// by the analogy agent alone is only discoverable once the turn is over.
	s.drawAnalogies()
	s.refreshMastery()
}

// drawAnalogies appends a card for anything recorded since the last time it ran.
//
// It is called both when a save is seen and at the end of the turn, and must be
// idempotent: counting what has already been drawn is what stops the same table
// appearing twice. Calling it on the save matters for reading order -- the
// analogy has to be on screen above the question it is the basis for, and the
// question card is appended in the middle of the same turn.
func (s *Shell) drawAnalogies() {
	if s.cfg.Analogies == nil {
		return
	}
	rows := s.cfg.Analogies(s.ctx)
	if len(rows) <= s.shownAnalogies {
		return
	}
	fresh := rows[:len(rows)-s.shownAnalogies] // newest first
	s.shownAnalogies = len(rows)
	s.AppendView(ext.View(ext.ViewAnalogyTable, "", ext.AnalogyTableProps{Rows: fresh}))
}

func (s *Shell) refreshMastery() {
	if s.cfg.Mastery == nil {
		return
	}
	items := s.cfg.Mastery(s.ctx)
	if len(items) == 0 {
		return
	}
	var debt, mastery []fyne.CanvasObject
	for _, it := range items {
		switch {
		case it.Debt > 0.4:
			// Bar shows the debt itself, so its length and its label agree.
			debt = append(debt, progressRow(s.pal, it.Label, it.Debt,
				strconv.FormatFloat(it.Debt, 'f', 1, 64), s.pal.accent))
		case it.Level > 0:
			mastery = append(mastery, progressRow(s.pal, it.Label, it.Level,
				strconv.Itoa(int(it.Level*100))+"%", s.pal.success))
		}
	}

	rows := make([]fyne.CanvasObject, 0, len(items)+4)
	if len(debt) > 0 {
		rows = append(rows, sectionLabel("COGNITIVE DEBT"))
		rows = append(rows, debt...)
		rows = append(rows, muted("what you lean on and do not hold"))
	}
	if len(mastery) > 0 {
		if len(rows) > 0 {
			rows = append(rows, widget.NewSeparator())
		}
		rows = append(rows, sectionLabel("MASTERY"))
		rows = append(rows, mastery...)
	}
	s.side.Objects = rows
	s.side.Refresh()
}

func (s *Shell) setBusy(busy bool) {
	if busy {
		s.send.Disable()
		s.spinner.Show()
		s.spinner.Start()
		return
	}
	s.send.Enable()
	s.spinner.Stop()
	s.spinner.Hide()
}

// bump refreshes the feed and keeps the newest content in view.
func (s *Shell) bump() {
	s.feed.Refresh()
	s.scroll.ScrollToBottom()
}

// spacer is a fixed-width invisible strut.
func spacer(w float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(w, 0))
	return r
}

func layoutBlank() fyne.CanvasObject { return spacer(0) }
