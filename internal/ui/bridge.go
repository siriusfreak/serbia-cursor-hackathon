package ui

import (
	"context"
	"log/slog"

	"fyne.io/fyne/v2"
	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"

	"github.com/sirius/cogdebt/internal/obs"
)

// Handler receives agent output. Every callback is invoked on the Fyne UI
// goroutine, so implementations may touch widgets freely.
type Handler struct {
	// OnText appends a chunk of assistant text.
	OnText func(string)
	// OnToolCall reports a plugin tool firing, which is what makes the plugin
	// layer visible during a demo.
	OnToolCall func(name string)
	// OnToolResult carries a plugin's structured answer. The UI draws cards
	// from this rather than from the model's prose: a tool result has a schema,
	// prose does not.
	OnToolResult func(name string, result map[string]any)
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
	// Log records turn timings. A GUI leaves no scrollback, so without this a
	// slow or failed turn is unexplainable after the fact.
	Log *slog.Logger
}

// Send delivers the learner's message and streams the reply. It returns
// immediately; the run continues in the background until ctx is cancelled.
func (b *Bridge) Send(ctx context.Context, text string, h Handler) {
	go func() {
		defer post(h.OnDone)

		log := obs.Or(b.Log)
		timer := obs.Start()
		var tools, chunks int
		var failed bool

		log.Info("turn started",
			obs.FEvent, obs.EventTurn, obs.FUser, b.UserID, obs.FSession, b.SessionID,
			obs.FBytesIn, len(text))
		defer func() {
			log.Info("turn finished",
				obs.FEvent, obs.EventTurn, obs.FUser, b.UserID, obs.FSession, b.SessionID,
				timer.Attr(), "tool_calls", tools, "chunks", chunks, obs.FOK, !failed)
		}()

		msg := genai.NewContentFromText(text, genai.RoleUser)
		for ev, err := range b.Runner.Run(ctx, b.UserID, b.SessionID, msg, agent.RunConfig{}) {
			if err != nil {
				if ctx.Err() == nil {
					failed = true
					log.Error("turn failed",
						obs.FEvent, obs.EventTurn, obs.FSession, b.SessionID,
						timer.Attr(), obs.FErr, err.Error())
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
					chunks++
					text := part.Text
					post(func() {
						if h.OnText != nil {
							h.OnText(text)
						}
					})
				case part.FunctionCall != nil:
					tools++
					name := part.FunctionCall.Name
					post(func() {
						if h.OnToolCall != nil {
							h.OnToolCall(name)
						}
					})
				case part.FunctionResponse != nil:
					name, result := part.FunctionResponse.Name, part.FunctionResponse.Response
					post(func() {
						if h.OnToolResult != nil {
							h.OnToolResult(name, result)
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
