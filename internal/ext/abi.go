// Package ext defines the plugin ABI: the single narrow contract that every
// capability in the system crosses.
//
// Everything crossing this boundary is bytes. No pointers, no channels, no
// framework types. That constraint is the whole point: the same Extension can
// run in-process today and out-of-process (hashicorp/go-plugin, WASM) tomorrow
// without the core noticing.
//
// If you are writing a plugin, this file is the only one you must read.
// See docs/PLUGIN_GUIDE.md for a walkthrough.
package ext

import (
	"context"
	"encoding/json"
	"fmt"
)

// ABIVersion is the contract version the host speaks. A plugin whose
// Manifest.ABIVersion differs is rejected at load time rather than crashing
// the host.
const ABIVersion = 1

// Kind classifies what a plugin contributes to the system.
type Kind string

const (
	// KindProfile stores learner skills and mastery.
	KindProfile Kind = "profile"
	// KindRetrieval fetches outside knowledge (search, GitHub, docs).
	KindRetrieval Kind = "retrieval"
	// KindAssessor generates questions and grades answers.
	KindAssessor Kind = "assessor"
	// KindLLM describes a model endpoint. Declarative: it provides a single
	// "describe" tool the host calls once at load, then builds the model
	// itself. Token streaming never crosses the ABI.
	KindLLM Kind = "llm"
	// KindUI returns declarative ViewSpec JSON that the host renders with Fyne.
	// Widgets never cross the ABI.
	KindUI Kind = "ui"
	// KindAgent describes a sub-agent. Like KindLLM it is declarative: the
	// plugin provides "describe" returning an AgentSpec, and the host builds
	// the agent and exposes it through adk agenttool.
	KindAgent Kind = "agent"
)

// Valid reports whether k is a Kind the host understands.
func (k Kind) Valid() bool {
	switch k {
	case KindProfile, KindRetrieval, KindAssessor, KindLLM, KindUI, KindAgent:
		return true
	}
	return false
}

// Declarative reports whether the Kind is described rather than invoked per
// call. Declarative kinds expose exactly one tool, DescribeTool, which the host
// calls once at load time.
func (k Kind) Declarative() bool {
	return k == KindLLM || k == KindAgent
}

// DescribeTool is the tool name every declarative plugin must provide.
const DescribeTool = "describe"

// ToolSpec describes one callable the plugin exposes. The host turns each
// ToolSpec into a function declaration the LLM can call.
type ToolSpec struct {
	// Name is unique within the plugin, snake_case. The host registers it
	// globally as "<plugin>_<name>", which must match ^[a-zA-Z0-9_-]{1,64}$.
	Name string `json:"name"`

	// Description is read by the LLM to decide whether to call this tool.
	// Write it for the model, not for a changelog: say when to call it.
	Description string `json:"description"`

	// Schema is a JSON Schema for the arguments object. It MUST be
	// {"type":"object", ...} — anything else is rejected at load time.
	Schema json.RawMessage `json:"schema"`

	// Returns documents the result shape. Informational only; not validated.
	Returns json.RawMessage `json:"returns,omitempty"`

	// ReadOnly marks the tool free of side effects, so the host may retry it
	// and call it speculatively.
	ReadOnly bool `json:"read_only"`
}

// Manifest is a plugin's self-description, read once at load time.
type Manifest struct {
	// Name is the globally unique plugin name, snake_case.
	Name string `json:"name"`
	// Version is the plugin's own semver. Unrelated to ABIVersion.
	Version string `json:"version"`
	// ABIVersion must equal ext.ABIVersion or the plugin is rejected.
	ABIVersion int `json:"abi_version"`
	// Kind classifies the contribution.
	Kind Kind `json:"kind"`
	// Provides lists the callables. Declarative kinds list exactly one,
	// named DescribeTool.
	Provides []ToolSpec `json:"provides"`
	// Requires names Kinds this plugin depends on. The host resolves them;
	// a plugin never calls another plugin directly.
	Requires []Kind `json:"requires,omitempty"`
}

// Extension is the entire plugin ABI. Three methods, all serializable.
type Extension interface {
	// Manifest describes the plugin. It must be cheap, pure and stable:
	// the host may call it on every turn to rebuild the tool list.
	Manifest() Manifest

	// Invoke runs one tool. in is the arguments object matching the tool's
	// Schema; the result is any JSON value, conventionally an object.
	//
	// The host guarantees in is syntactically valid JSON, so a plugin never
	// has to defend against garbage bytes. It must still defend against valid
	// JSON that does not match its schema: missing required fields and wrong
	// types both arrive here, and ext.Args turns them into a Fault for you.
	//
	// Return a *Fault for an expected failure (bad args, missing data): its
	// Message is handed back to the LLM so it can correct itself. Return any
	// other error only for genuine transport or internal breakage.
	Invoke(ctx context.Context, tool string, in json.RawMessage) (json.RawMessage, error)

	// Close releases resources. Called on shutdown and on hot-unload. It must
	// be safe to call more than once.
	Close() error
}

// Fault is an expected, recoverable failure. It crosses the process boundary
// as data, which is why it is not a plain Go error.
type Fault struct {
	// Code is one of the Fault* constants below.
	Code string `json:"code"`

	// Message is fed back to the LLM as the tool result. Write it as an
	// instruction to the model ("profile is empty, call profile_upsert first"),
	// never as a stack trace.
	Message string `json:"message"`

	// Retry hints that calling again with the same arguments may succeed.
	Retry bool `json:"retry"`
}

// Fault codes.
const (
	FaultInvalidArgs = "invalid_args"
	FaultNotFound    = "not_found"
	FaultUnavailable = "unavailable"
	FaultDenied      = "denied"
	FaultInternal    = "internal"
)

func (f *Fault) Error() string {
	return fmt.Sprintf("%s: %s", f.Code, f.Message)
}

// Invalidf returns a Fault telling the model its arguments were wrong.
func Invalidf(format string, a ...any) *Fault {
	return &Fault{Code: FaultInvalidArgs, Message: fmt.Sprintf(format, a...)}
}

// NotFoundf returns a Fault telling the model the data does not exist yet.
func NotFoundf(format string, a ...any) *Fault {
	return &Fault{Code: FaultNotFound, Message: fmt.Sprintf(format, a...)}
}

// Internalf returns a Fault for breakage the model cannot fix.
func Internalf(format string, a ...any) *Fault {
	return &Fault{Code: FaultInternal, Message: fmt.Sprintf(format, a...)}
}

// JSON marshals v into a plugin result, converting a marshalling failure into
// an internal Fault. Plugins use it to return results in one line.
func JSON(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, Internalf("encode result: %v", err)
	}
	return b, nil
}

// Args decodes a tool's arguments into v, reporting a well-formed Fault the
// model can act on when the payload does not match.
func Args(in json.RawMessage, v any) error {
	if len(in) == 0 {
		in = json.RawMessage("{}")
	}
	if err := json.Unmarshal(in, v); err != nil {
		return Invalidf("arguments do not match the declared schema: %v", err)
	}
	return nil
}
