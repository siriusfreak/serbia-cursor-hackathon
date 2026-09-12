package ui

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/sirius/cogdebt/internal/ext"
)

// TestHappyPathRunsAboutTwoMinutes pins the length. The script is meant to fit
// a demo slot, and a step added without thinking about pacing would stretch it
// past the point where a room keeps watching.
func TestHappyPathRunsAboutTwoMinutes(t *testing.T) {
	var total time.Duration
	for _, s := range HappyPath() {
		total += s.Wait
	}
	if total < 105*time.Second || total > 140*time.Second {
		t.Errorf("the scripted run takes %s; it is meant to be about two minutes", total.Round(time.Second))
	}
	t.Logf("scripted run: %s", total.Round(time.Second))
}

// TestHappyPathClimbsTheWholeLadder: the point of the demo is the climb, so if
// a rung goes missing the demo has quietly stopped making its own argument.
func TestHappyPathClimbsTheWholeLadder(t *testing.T) {
	var levels []string
	var images, analogies, learnerTurns int
	tools := map[string]bool{}

	for _, s := range HappyPath() {
		if s.Tool != "" {
			tools[s.Tool] = true
		}
		if s.Learner != "" {
			learnerTurns++
		}
		if s.View == nil {
			continue
		}
		switch s.View.Type {
		case ext.ViewQuestion:
			var q ext.QuestionProps
			_ = s.View.DecodeProps(&q)
			levels = append(levels, q.Level)
		case ext.ViewImage:
			images++
		case ext.ViewAnalogyTable:
			analogies++
		}
	}

	// The same sequence the real assessor produces for five good answers on one
	// concept — see exts/assessor.TestOneConceptClimbsTheWholeLadder.
	if got := strings.Join(levels, " "); got != "L1 L2 L3 L3 L4" {
		t.Errorf("the demo climbs %q, want the sequence the real ladder produces", got)
	}
	if analogies != 1 {
		t.Errorf("%d analogy tables; the demo is built around one", analogies)
	}
	if images != 1 {
		t.Errorf("%d images; the generated diagram is part of what is being shown", images)
	}
	if learnerTurns < 6 {
		t.Errorf("only %d learner turns: the demo has to look like a conversation", learnerTurns)
	}
	for _, want := range []string{"github_scan", "analogy", "fal_illustrate", "daytona_run_task", "assessor_grade"} {
		if !tools[want] {
			t.Errorf("%s never appears, so the demo never shows that plugin", want)
		}
	}
}

// TestTheDemoDiagramIsEmbedded: the whole point of a scripted run is that it
// cannot fail for a reason outside this binary.
func TestTheDemoDiagramIsEmbedded(t *testing.T) {
	if len(demoDiagram) < 1000 {
		t.Fatalf("the embedded diagram is %d bytes", len(demoDiagram))
	}
	HappyPath()
	raw, ok := imageCache.Load(demoDiagramURL)
	if !ok {
		t.Fatal("the diagram is not in the cache, so the demo would try to fetch a url that resolves to nothing")
	}
	if _, _, err := decodeImage(raw.([]byte)); err != nil {
		t.Fatalf("the embedded diagram does not decode: %v", err)
	}
}

// TestNoStaleFormsInTheScriptedRun replays every step into a real feed and
// checks the invariant the eye caught twice: the moment the learner says
// anything, no question on screen is still a form.
//
// A live card below an answered question invites answering it again, and the
// assessor would record that as a second attempt at something it asked once.
func TestNoStaleFormsInTheScriptedRun(t *testing.T) {
	test.NewApp()
	s := newTestShell()

	for i, step := range HappyPath() {
		s.applyStep(step)

		open := openCards(s.feed)
		if open > 1 {
			t.Fatalf("step %d: %d answerable cards on screen at once", i, open)
		}
		if step.Learner != "" && open != 0 {
			t.Fatalf("step %d: the learner said %q with a question still open as a form",
				i, truncate(step.Learner, 40))
		}
	}

	// Every question asked is still readable at the end: retiring spends the
	// form, it does not erase the history.
	if asked := strings.Count(allText(s.feed), "·"); asked == 0 {
		t.Error("no question headers survive in the feed; the exchange left no record")
	}
}

// newTestShell is the smallest shell that can take a step: a feed to draw into
// and the two widgets setBusy touches.
func newTestShell() *Shell {
	feed := container.NewVBox()
	spinner := widget.NewProgressBarInfinite()
	spinner.Hide()
	return &Shell{
		feed:      feed,
		scroll:    container.NewVScroll(feed),
		send:      widget.NewButton("Send", nil),
		spinner:   spinner,
		side:      container.NewVBox(),
		streaming: &strings.Builder{},
		streamAt:  -1,
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
