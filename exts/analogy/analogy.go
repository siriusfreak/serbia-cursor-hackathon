// Package analogy is the reference AGENT plugin.
//
// It does not implement an agent. It describes one, and the host builds it
// with llmagent and exposes it through adk agenttool. That is why a whole new
// specialist agent costs one manifest and zero Go logic -- swap the
// Instruction and you have swapped strategies without recompiling the core.
//
// Two variants ship here on purpose: New() does real structure mapping, and
// NewNaive() does surface-level "X is like Y". Loading one or the other shows
// that the plugin seam is load-bearing rather than decorative.
package analogy

import (
	"context"
	"encoding/json"

	"github.com/sirius/cogdebt/internal/ext"
)

// Ext is a declarative agent plugin.
type Ext struct {
	name string
	spec ext.AgentSpec
}

// New returns the structure-mapping analogy agent.
func New() *Ext {
	return &Ext{
		name: "analogy",
		spec: ext.AgentSpec{
			Description: "Builds an analogy from what the learner already knows to a topic they want to learn, and states where the analogy breaks. Delegate here once the learner's profile has skills in it and they have named a target topic.",
			Instruction: structureMappingInstruction,
			// The parent needs the table intact, and rewriting it costs another
			// LLM call per delegation.
			SkipSummarization: true,
			// The agent that produces the pairs is the one that records them.
			// Leaving that to the caller means it is skipped whenever the reply
			// is long, and the table never reaches the screen.
			ToolRefs: []string{"profile_get", "profile_save_analogy"},
		},
	}
}

// NewNaive returns the shallow variant, for side-by-side comparison.
func NewNaive() *Ext {
	return &Ext{
		name: "analogy_naive",
		spec: ext.AgentSpec{
			Description:       "Builds a quick surface-level analogy between two topics. Use only when explicitly asked for the naive comparison.",
			Instruction:       "Explain the target topic by saying what it is like, in two or three sentences. Keep it light and intuitive.",
			SkipSummarization: true,
		},
	}
}

const structureMappingInstruction = `You map what a learner already knows onto something they do not, using structure mapping.

Answer in the language the learner used. Technical terms stay in their original form.

Method:
1. Call profile_get to see the concepts the learner holds.
2. For each concept, identify its STRUCTURAL ROLE in its own domain -- what job it does. Roles include:
   source_of_truth, unit_of_scheduling, rollback, degradation_signal, quota, reconcile_loop.
3. In the target domain, find the concept holding the SAME role.
4. Pair them on that shared role.

Match on role, never on name or surface resemblance. "Both have a controller" is not a mapping;
"both are the single source of truth that everything else reconciles against" is.

For every pair you must give:
  - source: the concept the learner already holds
  - target: the concept in the new domain
  - shared_role: the role that justifies the pairing
  - carry_over: what their existing intuition gets right, for free
  - breakdown: where that intuition will MISLEAD them

breakdown must never be empty. An analogy without a stated limit does not pay off cognitive debt;
it creates new debt, because the learner keeps the borrowed intuition past the point it holds.
If you cannot name a way the analogy fails, the pairing is too vague -- replace it.

Produce three to five pairs, strongest first. Be concrete: name real mechanisms, not categories.

Then, before you answer, call profile_save_analogy with those pairs. That is what puts the table
on the learner's screen and stores it; skipping it means your work is never shown. After the call,
reply with at most two sentences pointing at the pair that matters most -- do not restate the table,
it is already rendered.`

// Manifest describes the plugin. A declarative kind provides exactly one tool.
func (e *Ext) Manifest() ext.Manifest {
	return ext.Manifest{
		Name:       e.name,
		Version:    "0.1.0",
		ABIVersion: ext.ABIVersion,
		Kind:       ext.KindAgent,
		Requires:   []ext.Kind{ext.KindProfile},
		Provides: []ext.ToolSpec{{
			Name:        ext.DescribeTool,
			Description: "Returns the agent specification the host builds this agent from.",
			Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
			ReadOnly:    true,
		}},
	}
}

// Invoke answers the single declarative call.
func (e *Ext) Invoke(_ context.Context, tool string, _ json.RawMessage) (json.RawMessage, error) {
	if tool != ext.DescribeTool {
		return nil, ext.Invalidf("%s is a declarative agent plugin; it provides only %q", e.name, ext.DescribeTool)
	}
	spec := e.spec
	spec.Name = e.name
	return ext.JSON(spec)
}

// Close releases nothing.
func (e *Ext) Close() error { return nil }

var _ ext.Extension = (*Ext)(nil)
