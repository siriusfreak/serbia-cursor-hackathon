// Package obs is the observability layer: one logger, one set of field names,
// and timings on every seam.
//
// Everything here emits STRUCTURED records. When a run goes wrong -- and with
// an LLM, a plugin process and a GUI in the same pipeline it will -- the
// question is always "which seam, and how long did it sit there". Prose logs
// cannot answer that; fields can be filtered, counted and sorted.
//
// Field names are declared as constants so that a query written once keeps
// working. Every record carries Event and, where it means anything, Ms.
package obs

import (
	"context"
	"io"
	"log/slog"
	"os"
	"time"
)

// Event names. Stable strings, so `| grep event=tool.invoke` keeps working.
const (
	EventPluginLoad   = "plugin.load"
	EventPluginReject = "plugin.reject"
	EventToolInvoke   = "tool.invoke"
	EventModelCall    = "model.call"
	EventAgentRun     = "agent.run"
	EventTurn         = "turn"
	EventSubprocSpawn = "subprocess.spawn"
	EventSubprocCall  = "subprocess.call"
	EventSubprocExit  = "subprocess.exit"
	EventRenderCard   = "ui.card"
	EventStoreQuery   = "store.query"
)

// Field names, for the same reason.
const (
	FEvent      = "event"
	FMs         = "ms"
	FPlugin     = "plugin"
	FTool       = "tool"
	FAgent      = "agent"
	FModel      = "model"
	FTransport  = "transport"
	FKind       = "kind"
	FVersion    = "version"
	FSession    = "session"
	FInvocation = "invocation"
	FUser       = "user"
	FOK         = "ok"
	FFault      = "fault"
	FErr        = "err"
	FBytesIn    = "bytes_in"
	FBytesOut   = "bytes_out"
	FCount      = "count"
	FPath       = "path"
)

// Config describes where and how to log.
type Config struct {
	// Level is "debug", "info", "warn" or "error". Empty means info.
	Level string
	// Format is "text" (default) or "json". Use json when you intend to query
	// the output rather than read it.
	Format string
	// File, when set, receives the log in addition to stderr. A run that only
	// misbehaves in the GUI leaves no scrollback to read afterwards.
	File string
}

// Setup builds the process logger and returns it alongside a closer for any
// log file it opened.
func Setup(cfg Config) (*slog.Logger, func() error, error) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(orDefault(cfg.Level, "info"))); err != nil {
		return nil, nil, err
	}

	out := io.Writer(os.Stderr)
	closer := func() error { return nil }
	if cfg.File != "" {
		f, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, err
		}
		out, closer = io.MultiWriter(os.Stderr, f), f.Close
	}

	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler = slog.NewTextHandler(out, opts)
	if cfg.Format == "json" {
		h = slog.NewJSONHandler(out, opts)
	}
	return slog.New(h), closer, nil
}

// Timer measures a span.
type Timer struct{ start time.Time }

// Start begins a span.
func Start() Timer { return Timer{start: time.Now()} }

// Ms returns elapsed milliseconds, rounded to a tenth. Whole milliseconds hide
// the difference between a 0.2ms in-process call and a 0.9ms one, which is
// exactly the comparison the transport work exists to make.
func (t Timer) Ms() float64 {
	return float64(time.Since(t.start).Microseconds()) / 1000
}

// Attr returns the elapsed time as a log field.
func (t Timer) Attr() slog.Attr { return slog.Float64(FMs, t.Ms()) }

// Nop is a logger that discards everything, for tests and for callers that
// were given no logger.
func Nop() *slog.Logger { return slog.New(slog.DiscardHandler) }

// Or returns log, or a discarding logger when it is nil.
func Or(log *slog.Logger) *slog.Logger {
	if log == nil {
		return Nop()
	}
	return log
}

// Enabled reports whether a level would be recorded, so callers can skip
// building expensive fields.
func Enabled(log *slog.Logger, lvl slog.Level) bool {
	return log != nil && log.Enabled(context.Background(), lvl)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
