package ext

import "encoding/json"

// AgentSpec is what a KindAgent plugin returns from its DescribeTool. The
// plugin does not implement an agent; it describes one, and the host builds it.
//
// This keeps the ABI narrow: an agent streams events and owns conversation
// state, neither of which survives an Invoke(json) -> json boundary. Describing
// the agent instead means a new agent is a new manifest, with no Go code.
type AgentSpec struct {
	// Name identifies the agent. The host overrides it with the plugin name so
	// the exposed tool is predictable.
	Name string `json:"name"`

	// Description is read by the PARENT agent to decide when to delegate here.
	// This is the single highest-leverage string in an agent plugin.
	Description string `json:"description"`

	// Instruction is the system prompt for this agent.
	Instruction string `json:"instruction"`

	// Model names the model to use. Empty means the host's default model.
	Model string `json:"model,omitempty"`

	// ToolRefs lists qualified tool names ("profile_upsert") this agent may
	// call. Names that resolve to nothing are skipped with a warning rather
	// than failing the load.
	ToolRefs []string `json:"tool_refs,omitempty"`

	// OutputSchema constrains the agent's reply. Informational for now.
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`

	// MaxOutputTokens caps the agent's reply. Zero means no cap.
	//
	// Prose instructions about length are advice a model may ignore; this is
	// the only lever that holds. Tracing showed one analogy reply at 5924
	// tokens and 92 seconds -- 89% of the turn -- against an instruction
	// asking for three short pairs.
	MaxOutputTokens int32 `json:"max_output_tokens,omitempty"`

	// SkipSummarization drops the extra LLM call that rewrites this agent's
	// result for its parent. Set it true for cheap deterministic agents:
	// leaving it false silently doubles cost and latency per delegation.
	SkipSummarization bool `json:"skip_summarization"`
}
