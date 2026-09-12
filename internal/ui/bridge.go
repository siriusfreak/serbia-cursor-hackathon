package ui

import (
	"context"

	"fyne.io/fyne/v2"
	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
)

// Handler receives agent output. Every callback is invoked on the Fyne UI
// goroutine, so implementations may touch widgets freely.
type Handler struct {
	// OnText appends a chunk of assistant text.
	OnText func(string)
	// OnToolCall reports a plugin tool firing, which is what makes the plugin
	// layer visible during a demo.
	OnToolCall func(name string)
	// OnError reports a failed run.
	OnError func(error)
	// OnDone reports the run finishing, successfully or not.
	OnDone func()
}

// Bridge runs the agent off the UI goroutine and marshals its event stream
// back onto it.
//
// This is the one place the two worlds meet. Fyne requires widget access from
// the UI goroutine (fyne.Do since 2.6) while the ADK runner streams from its
// own; getting that wrong produces intermittent freezes that surface exactly
// once, on stage. Centralizing it here means shell code cannot forget.
type Bridge struct {
	Runner    *runner.Runner
	UserID    string
	SessionID string
}

// Send delivers the learner's message and streams the reply. It returns
// immediately; the run continues in the background until ctx is cancelled.
func (b *Bridge) Send(ctx context.Context, text string, h Handler) {
	go func() {
		defer post(h.OnDone)

		msg := genai.NewContentFromText(text, genai.RoleUser)
		for ev, err := range b.Runner.Run(ctx, b.UserID, b.SessionID, msg, agent.RunConfig{}) {
			if err != nil {
				if ctx.Err() == nil {
					postErr(h.OnError, err)
				}
				return
			}
			if ev == nil || ev.Content == nil {
				continue
			}
			for _, part := range ev.Content.Parts {
				// Reasoning models stream their scratchpad as thought parts.
				// It is not the answer and must never reach the learner.
				if part.Thought {
					continue
				}
				switch {
				case part.Text != "":
					text := part.Text
					post(func() {
						if h.OnText != nil {
							h.OnText(text)
						}
					})
				case part.FunctionCall != nil:
					name := part.FunctionCall.Name
					post(func() {
						if h.OnToolCall != nil {
							h.OnToolCall(name)
						}
					})
				}
			}
		}
	}()
}

// post runs fn on the UI goroutine.
func post(fn func()) {
	if fn == nil {
		return
	}
	fyne.Do(fn)
}

func postErr(fn func(error), err error) {
	if fn == nil {
		return
	}
	fyne.Do(func() { fn(err) })
}
