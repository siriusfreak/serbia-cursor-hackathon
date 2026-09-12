package ui

import (
	"context"
	"image/color"
	"strings"

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
	// Plugins is the loaded plugin summary, listed in the sidebar so the
	// plugin layer is visible without opening a terminal.
	Plugins []string
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
	s.scroll = container.NewVScroll(s.feed)

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

	var right fyne.CanvasObject = layoutBlank()
	if s.cfg.Model != "" {
		right = chip(s.cfg.Model, s.pal.accent, s.pal.surfaceHi)
	}

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

	s.spinner = widget.NewProgressBarInfinite()
	s.spinner.Hide()

	row := container.NewBorder(nil, nil, nil, s.send, s.input)
	line := canvas.NewRectangle(s.pal.line)
	line.SetMinSize(fyne.NewSize(0, 1))
	return container.NewVBox(line, s.spinner, container.NewPadded(row))
}

func (s *Shell) sidebar() fyne.CanvasObject {
	s.side = container.NewVBox(muted("Answer a question and progress appears here."))

	blocks := container.NewVBox(
		sectionLabel("PROGRESS"),
		s.side,
	)
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
	if text == "" || s.send.Disabled() {
		return
	}
	s.input.SetText("")
	s.appendUser(text)
	s.setBusy(true)

	s.streaming.Reset()
	s.streamAt = -1

	s.cfg.Bridge.Send(s.ctx, text, Handler{
		OnText:     s.streamText,
		OnToolCall: s.appendToolCall,
		OnError:    s.appendError,
		OnDone:     s.finishTurn,
	})
}

func (s *Shell) appendUser(text string) {
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

// AppendView renders a ViewSpec from a UI plugin into the feed.
func (s *Shell) AppendView(spec ext.ViewSpec) {
	s.feed.Add(container.NewPadded(Render(spec, s.onViewEvent)))
	s.bump()
}

func (s *Shell) onViewEvent(ev ext.ViewEvent) {
	var payload struct {
		Answer string `json:"answer"`
	}
	if len(ev.Payload) > 0 {
		_ = ext.ViewSpec{Props: ev.Payload}.DecodeProps(&payload)
	}
	if payload.Answer == "" {
		return
	}
	s.input.SetText(payload.Answer)
	s.submit()
}

func (s *Shell) finishTurn() {
	s.setBusy(false)
	s.refreshMastery()
}

func (s *Shell) refreshMastery() {
	if s.cfg.Mastery == nil {
		return
	}
	items := s.cfg.Mastery(s.ctx)
	if len(items) == 0 {
		return
	}
	rows := make([]fyne.CanvasObject, 0, len(items))
	for _, it := range items {
		rows = append(rows, progressRow(s.pal, it.Label, it.Level, it.Debt))
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
