package ui

import (
	"time"

	"fyne.io/fyne/v2"

	"github.com/sirius/cogdebt/internal/ext"
)

// A scripted run plays a fixed conversation through the real rendering path.
//
// It exists because a live demo depends on a model, a network and four external
// services, and a stage is the worst place to discover that any one of them is
// having a bad minute. Every card here is drawn by the same Render the live app
// uses, so what the room sees is the product rather than a video of it -- but
// the content is fixed, so it cannot go wrong and it cannot go slowly.
//
// The numbers are not invented. Mastery moves 0 -> 0.34 -> 0.56 -> 0.71 -> 0.81
// and the rungs run L1 L2 L3 L3 L4 because that is what the real assessor does
// with five good answers on one concept; exts/assessor has the test that pins
// it. A demo that showed a prettier climb than the code produces would be a lie
// told to the people most likely to check.

// Step is one beat of a scripted run. Fields are applied in the order below,
// so a single step can fire a tool, draw its card and update the sidebar.
type Step struct {
	// Wait is the pause before this step, scaled by the playback speed.
	Wait time.Duration
	// Learner is a message from the learner, drawn as their own bubble.
	Learner string
	// Tool is a plugin chip, the proof that the plugin layer is real.
	Tool string
	// Thinking shows the busy indicator until the next step that clears it.
	Thinking bool
	// Text is assistant prose.
	Text string
	// View is a card: an analogy table, a question, an image.
	View *ext.ViewSpec
	// Mastery replaces the sidebar, when this step moved it.
	Mastery []ext.MasteryItem
}

// Play runs a script. It returns immediately; playback continues in the
// background until the script ends or the window closes.
func (s *Shell) Play(steps []Step, speed float64) {
	if speed <= 0 {
		speed = 1
	}
	go func() {
		for _, step := range steps {
			if d := time.Duration(float64(step.Wait) / speed); d > 0 {
				time.Sleep(d)
			}
			s.playStep(step)
		}
		fyne.Do(func() { s.setBusy(false) })
	}()
}

// playStep applies one beat on the UI goroutine.
func (s *Shell) playStep(step Step) {
	fyne.Do(func() {
		s.setBusy(step.Thinking)
		if step.Learner != "" {
			s.appendUser(step.Learner)
		}
		if step.Tool != "" {
			s.appendToolCall(step.Tool)
		}
		if step.Text != "" {
			// Reset first: each scripted line is its own block, the way a real
			// reply is after a tool call.
			s.streaming.Reset()
			s.streamAt = -1
			s.streamText(step.Text)
		}
		if step.View != nil {
			s.AppendView(*step.View)
		}
		if len(step.Mastery) > 0 {
			s.refreshMasteryWith(step.Mastery)
		}
	})
}
