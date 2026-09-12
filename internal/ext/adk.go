package ext

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/agenttool"
	"google.golang.org/adk/v2/tool/toolutils"
)

// SchemaFromJSON converts a plugin's JSON Schema into the schema type the model
// APIs expect.
//
// The field names line up one-to-one, so the conversion is a plain unmarshal --
// except for "type". JSON Schema spells it "object"/"string"; genai spells it
// "OBJECT"/"STRING". Skipping that normalization produces a schema that
// marshals fine and is silently wrong at the API, so it is done recursively
// here, once, for everyone.
func SchemaFromJSON(raw json.RawMessage) (*genai.Schema, error) {
	if len(raw) == 0 {
		return &genai.Schema{Type: genai.TypeObject}, nil
	}
	var s genai.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("schema is not valid JSON: %w", err)
	}
	normalizeSchemaTypes(&s)
	return &s, nil
}

func normalizeSchemaTypes(s *genai.Schema) {
	if s == nil {
		return
	}
	s.Type = genai.Type(strings.ToUpper(string(s.Type)))
	for _, p := range s.Properties {
		normalizeSchemaTypes(p)
	}
	normalizeSchemaTypes(s.Items)
	for _, a := range s.AnyOf {
		normalizeSchemaTypes(a)
	}
}

// extTool adapts one plugin tool to the ADK tool interface. It satisfies ADK's
// unexported runnableTool structurally: Name/Description/IsLongRunning plus
// Declaration and Run.
type extTool struct {
	ext      Extension
	plugin   string
	spec     ToolSpec
	declared *genai.Schema
	log      *slog.Logger
}

func (t *extTool) Name() string        { return QualifiedName(t.plugin, t.spec.Name) }
func (t *extTool) Description() string { return t.spec.Description }
func (t *extTool) IsLongRunning() bool { return false }

// ProcessRequest registers this tool in the outgoing LLM request. ADK requires
// every tool to pack itself; without it the runner rejects the tool at call
// time rather than at build time.
func (t *extTool) ProcessRequest(_ agent.Context, req *model.LLMRequest) error {
	return toolutils.PackTool(req, t)
}

func (t *extTool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.spec.Description,
		Parameters:  t.declared,
	}
}

// Run marshals the model's arguments, hands them to the plugin, and turns the
// answer back into a map.
//
// Failures are returned as a RESULT, not as an error: the model then sees what
// went wrong and can correct itself on the next turn, where a Go error would
// abort the whole invocation.
func (t *extTool) Run(ctx agent.Context, args any) (map[string]any, error) {
	in, err := json.Marshal(args)
	if err != nil {
		return faultResult(Internalf("could not encode arguments: %v", err)), nil
	}

	out, err := t.ext.Invoke(ctx, t.spec.Name, in)
	if err != nil {
		var f *Fault
		if errors.As(err, &f) {
			return faultResult(f), nil
		}
		// Not a Fault: transport or programming breakage. Tell the model
		// something useful, but make sure a human sees it too.
		t.log.Error("plugin invoke failed", "tool", t.Name(), "err", err)
		return faultResult(Internalf("tool %q failed: %v", t.Name(), err)), nil
	}

	var res map[string]any
	if err := json.Unmarshal(out, &res); err == nil {
		return res, nil
	}
	// A plugin may legitimately return a non-object (list, string). Wrap it
	// rather than rejecting it.
	var scalar any
	if err := json.Unmarshal(out, &scalar); err == nil {
		return map[string]any{"result": scalar}, nil
	}
	return faultResult(Internalf("tool %q returned unusable JSON", t.Name())), nil
}

func faultResult(f *Fault) map[string]any {
	return map[string]any{
		"error":   f.Code,
		"message": f.Message,
		"retry":   f.Retry,
	}
}

// ToolsetConfig wires a Registry into ADK.
type ToolsetConfig struct {
	// Registry holds the loaded plugins. Required.
	Registry *Registry
	// Model is the default model for agent plugins that do not name one.
	Model model.LLM
	// MaxAgentDepth caps nesting of agent plugins. Zero means 2. Without a cap,
	// agent plugins referencing each other recurse until the token budget is
	// gone.
	MaxAgentDepth int
	// Log receives rejections and warnings.
	Log *slog.Logger
}

// Toolset exposes every loaded plugin to ADK as a single tool.Toolset.
//
// ADK calls Tools on each turn, so plugins loaded or dropped since the last
// turn appear or vanish with no restart: hot-reload for free. Agents are
// configured with this one Toolset and never learn that plugins exist.
type Toolset struct {
	cfg ToolsetConfig
	log *slog.Logger

	mu     sync.Mutex
	agents map[string]tool.Tool // plugin name -> built agent tool, cached by version
	built  map[string]string    // plugin name -> version the cache was built from
}

// NewToolset returns a Toolset over the registry.
func NewToolset(cfg ToolsetConfig) *Toolset {
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.MaxAgentDepth <= 0 {
		cfg.MaxAgentDepth = 2
	}
	return &Toolset{
		cfg:    cfg,
		log:    cfg.Log,
		agents: map[string]tool.Tool{},
		built:  map[string]string{},
	}
}

// Name implements tool.Toolset.
func (ts *Toolset) Name() string { return "plugins" }

// Tools implements tool.Toolset, returning one ADK tool per plugin tool plus
// one tool per agent plugin.
func (ts *Toolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	var out []tool.Tool
	for _, m := range ts.cfg.Registry.Manifests() {
		if m.Kind == KindAgent {
			at, err := ts.agentTool(ctx, m, nil)
			if err != nil {
				ts.log.Error("agent plugin unavailable", "plugin", m.Name, "err", err)
				continue
			}
			out = append(out, at)
			continue
		}
		if m.Kind.Declarative() {
			continue // KindLLM is read by the host at startup, not by the model
		}
		e, ok := ts.cfg.Registry.Lookup(m.Name)
		if !ok {
			continue
		}
		for _, spec := range m.Provides {
			schema, err := SchemaFromJSON(spec.Schema)
			if err != nil {
				ts.log.Error("tool schema rejected", "tool", QualifiedName(m.Name, spec.Name), "err", err)
				continue
			}
			out = append(out, &extTool{ext: e, plugin: m.Name, spec: spec, declared: schema, log: ts.log})
		}
	}
	return out, nil
}

// agentTool builds (or reuses) the ADK tool for an agent plugin. path carries
// the agent plugins already being built, which is how cycles are caught.
func (ts *Toolset) agentTool(ctx agent.ReadonlyContext, m Manifest, path []string) (tool.Tool, error) {
	for _, seen := range path {
		if seen == m.Name {
			return nil, fmt.Errorf("agent cycle: %s -> %s", strings.Join(path, " -> "), m.Name)
		}
	}
	if len(path) >= ts.cfg.MaxAgentDepth {
		return nil, fmt.Errorf("agent nesting deeper than %d (%s)", ts.cfg.MaxAgentDepth, strings.Join(append(path, m.Name), " -> "))
	}

	ts.mu.Lock()
	if v, ok := ts.built[m.Name]; ok && v == m.Version {
		cached := ts.agents[m.Name]
		ts.mu.Unlock()
		return cached, nil
	}
	ts.mu.Unlock()

	e, ok := ts.cfg.Registry.Lookup(m.Name)
	if !ok {
		return nil, fmt.Errorf("plugin not loaded")
	}
	var spec AgentSpec
	if err := Describe(ctx, e, &spec); err != nil {
		return nil, err
	}
	if strings.TrimSpace(spec.Description) == "" {
		return nil, fmt.Errorf("agent spec has no description; the parent agent needs it to delegate")
	}

	tools, err := ts.resolveRefs(ctx, spec.ToolRefs, append(path, m.Name))
	if err != nil {
		return nil, err
	}

	ag, err := llmagent.New(llmagent.Config{
		Name:        m.Name, // plugin name wins, so the exposed tool is predictable
		Description: spec.Description,
		Instruction: spec.Instruction,
		Model:       ts.cfg.Model,
		Tools:       tools,
	})
	if err != nil {
		return nil, fmt.Errorf("build agent: %w", err)
	}
	at := agenttool.New(ag, &agenttool.Config{SkipSummarization: spec.SkipSummarization})

	ts.mu.Lock()
	ts.agents[m.Name] = at
	ts.built[m.Name] = m.Version
	ts.mu.Unlock()
	return at, nil
}

// resolveRefs turns qualified tool names into ADK tools. An unresolvable name
// is warned about and skipped: a typo in one ref must not sink the agent.
func (ts *Toolset) resolveRefs(ctx agent.ReadonlyContext, refs []string, path []string) ([]tool.Tool, error) {
	var out []tool.Tool
	for _, ref := range refs {
		found := false
		for _, m := range ts.cfg.Registry.Manifests() {
			if m.Kind == KindAgent && m.Name == ref {
				at, err := ts.agentTool(ctx, m, path)
				if err != nil {
					return nil, err // cycles and depth are hard errors
				}
				out, found = append(out, at), true
				break
			}
			e, ok := ts.cfg.Registry.Lookup(m.Name)
			if !ok {
				continue
			}
			for _, spec := range m.Provides {
				if QualifiedName(m.Name, spec.Name) != ref {
					continue
				}
				schema, err := SchemaFromJSON(spec.Schema)
				if err != nil {
					return nil, fmt.Errorf("tool %q: %w", ref, err)
				}
				out, found = append(out, &extTool{ext: e, plugin: m.Name, spec: spec, declared: schema, log: ts.log}), true
				break
			}
			if found {
				break
			}
		}
		if !found {
			ts.log.Warn("agent tool_ref does not resolve", "ref", ref, "agent", strings.Join(path, " -> "))
		}
	}
	return out, nil
}

// compile-time proof that the adapters satisfy what ADK expects.
var (
	_ tool.Toolset = (*Toolset)(nil)
	_ tool.Tool    = (*extTool)(nil)
	// extTool must also satisfy ADK's unexported runnableTool and
	// RequestProcessor. Neither is exported, so assert structurally: a missed
	// method here fails at call time, not at build time.
	_ interface {
		tool.Tool
		Declaration() *genai.FunctionDeclaration
		Run(agent.Context, any) (map[string]any, error)
		ProcessRequest(agent.Context, *model.LLMRequest) error
	} = (*extTool)(nil)
)
