package obs

import (
	"log/slog"
	"sync"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/tool"
)

// Tracer builds the ADK lifecycle plugin that times every model call, tool call
// and agent run.
//
// These are the spans no amount of application code can see from the outside:
// the runner owns the loop. Wiring this in is what makes "it was slow" into
// "the analogy sub-agent spent 94s in one model call", which is a different
// conversation.
//
// Note the name collision: an ADK "plugin" is a lifecycle callback set, not one
// of our loadable Extensions.
func Tracer(log *slog.Logger) (*plugin.Plugin, error) {
	t := &tracer{log: Or(log)}
	return plugin.New(plugin.Config{
		Name:                "obs",
		BeforeRunCallback:   t.beforeRun,
		AfterRunCallback:    t.afterRun,
		BeforeModelCallback: t.beforeModel,
		AfterModelCallback:  t.afterModel,
		BeforeToolCallback:  t.beforeTool,
		AfterToolCallback:   t.afterTool,
	})
}

// AgentCallbacks returns model callbacks to attach to an agent built outside
// the runner's reach.
//
// A runner-level lifecycle plugin does not see inside an agenttool sub-agent:
// its model calls are invisible, which leaves the largest span in the system --
// the analogy agent -- as an unexplained block of time. Attaching these at
// construction closes that hole.
func AgentCallbacks(log *slog.Logger) (llmagent.BeforeModelCallback, llmagent.AfterModelCallback) {
	t := &tracer{log: Or(log)}
	return t.beforeModel, t.afterModel
}

type tracer struct {
	log *slog.Logger
	// spans holds start times between the before/after halves of a callback
	// pair. Keys are per-invocation, so concurrent sessions do not collide.
	spans sync.Map
}

// span tracks one measured stretch. Chunks counts streamed partials, which is
// how you tell "the model was slow" from "the stream stalled".
type span struct {
	timer  Timer
	chunks int
}

func (t *tracer) start(key string) { t.spans.Store(key, &span{timer: Start()}) }

// peek returns the open span, if any.
func (t *tracer) peek(key string) (*span, bool) {
	v, ok := t.spans.Load(key)
	if !ok {
		return nil, false
	}
	return v.(*span), true
}

func (t *tracer) stop(key string) (float64, int) {
	v, ok := t.spans.LoadAndDelete(key)
	if !ok {
		return 0, 0
	}
	sp := v.(*span)
	return sp.timer.Ms(), sp.chunks
}

func (t *tracer) beforeRun(ictx agent.InvocationContext) (*genai.Content, error) {
	t.start("run/" + ictx.InvocationID())
	t.log.Info("agent run started",
		FEvent, EventAgentRun,
		FInvocation, ictx.InvocationID(),
		FAgent, ictx.Agent().Name(),
	)
	return nil, nil
}

func (t *tracer) afterRun(ictx agent.InvocationContext) {
	t.log.Info("agent run finished",
		FEvent, EventAgentRun,
		FInvocation, ictx.InvocationID(),
		FAgent, ictx.Agent().Name(),
		FMs, firstOf(t.stop("run/"+ictx.InvocationID())),
	)
}

// modelKey scopes a model span to one agent inside one invocation. Calls within
// that scope are sequential, so a single key cannot overlap itself.
func modelKey(ctx agent.Context) string {
	return "model/" + ctx.InvocationID() + "/" + ctx.AgentName()
}

func (t *tracer) beforeModel(ctx agent.Context, req *model.LLMRequest) (*model.LLMResponse, error) {
	t.start(modelKey(ctx))
	t.log.Debug("model call started",
		FEvent, EventModelCall,
		FInvocation, ctx.InvocationID(),
		FAgent, ctx.AgentName(),
		FModel, req.Model,
		"tools", len(req.Tools),
		"contents", len(req.Contents),
	)
	return nil, nil
}

func (t *tracer) afterModel(ctx agent.Context, resp *model.LLMResponse, respErr error) (*model.LLMResponse, error) {
	key := modelKey(ctx)

	// Streaming calls this once per chunk. Only the final response closes the
	// span: logging partials would bury the one measurement that matters under
	// hundreds of zero-length records.
	if respErr == nil && resp != nil && resp.Partial {
		if sp, ok := t.peek(key); ok {
			sp.chunks++
		}
		return nil, nil
	}

	ms, chunks := t.stop(key)
	attrs := []any{
		FEvent, EventModelCall,
		FInvocation, ctx.InvocationID(),
		FAgent, ctx.AgentName(),
		FMs, ms,
		"chunks", chunks,
		FOK, respErr == nil,
	}
	if respErr != nil {
		attrs = append(attrs, FErr, respErr.Error())
	}
	if resp != nil {
		attrs = append(attrs, "parts", countParts(resp), "calls", countCalls(resp))
		if u := resp.UsageMetadata; u != nil {
			attrs = append(attrs, "tokens_in", u.PromptTokenCount, "tokens_out", u.CandidatesTokenCount)
		}
	}
	if respErr != nil {
		t.log.Error("model call failed", attrs...)
	} else {
		t.log.Info("model call finished", attrs...)
	}
	return nil, nil
}

func (t *tracer) beforeTool(ctx agent.Context, tl tool.Tool, args map[string]any) (map[string]any, error) {
	t.start("tool/" + ctx.FunctionCallID())
	t.log.Debug("tool call started",
		FEvent, EventToolInvoke,
		FInvocation, ctx.InvocationID(),
		FAgent, ctx.AgentName(),
		FTool, tl.Name(),
		"args", len(args),
	)
	return nil, nil
}

func (t *tracer) afterTool(ctx agent.Context, tl tool.Tool, _, result map[string]any, err error) (map[string]any, error) {
	attrs := []any{
		FEvent, EventToolInvoke,
		FInvocation, ctx.InvocationID(),
		FAgent, ctx.AgentName(),
		FTool, tl.Name(),
		FMs, firstOf(t.stop("tool/" + ctx.FunctionCallID())),
	}
	switch {
	case err != nil:
		attrs = append(attrs, FOK, false, FErr, err.Error())
		t.log.Error("tool call failed", attrs...)
	default:
		// A plugin reports an expected failure as a successful call carrying a
		// fault. Counting those as successes would hide the most common way a
		// run goes wrong: the model repeatedly calling a tool that keeps
		// refusing it.
		if code, ok := result["error"].(string); ok && code != "" {
			msg, _ := result["message"].(string)
			attrs = append(attrs, FOK, false, FFault, code, "message", msg)
			t.log.Warn("tool call faulted", attrs...)
			return nil, nil
		}
		attrs = append(attrs, FOK, true, "keys", len(result))
		t.log.Info("tool call finished", attrs...)
	}
	return nil, nil
}

// firstOf drops the chunk count where only the duration is wanted.
func firstOf(ms float64, _ int) float64 { return ms }

func countParts(resp *model.LLMResponse) int {
	if resp.Content == nil {
		return 0
	}
	return len(resp.Content.Parts)
}

func countCalls(resp *model.LLMResponse) int {
	if resp.Content == nil {
		return 0
	}
	n := 0
	for _, p := range resp.Content.Parts {
		if p.FunctionCall != nil {
			n++
		}
	}
	return n
}

var _ llmagent.BeforeModelCallback = (&tracer{}).beforeModel
