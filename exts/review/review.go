// Package review is the agent for the second way into the same tutor.
//
// The analogy agent starts from a topic the learner names. This one starts
// from a pull request someone else wrote. The loop underneath is unchanged:
// something they believe, something measured, and the seam between the two.
//
// Like analogy it implements no agent. It describes one, and the host builds
// it. The agent does not look for problems -- oracle_check does that, in
// ordinary Go, deterministically. This agent only carries facts between tools
// and asks the learner one question.
package review

import (
	"context"
	"encoding/json"
	"os"

	"github.com/sirius/cogdebt/internal/ext"
)

var defaultReviewModel = envOr("COGDEBT_REVIEW_MODEL", "grok-4.20-0309-non-reasoning")

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Ext is a declarative agent plugin.
type Ext struct {
	spec ext.AgentSpec
}

// New returns the pull-request review agent.
func New() *Ext {
	return &Ext{spec: ext.AgentSpec{
		Description: "Reviews a pull request the learner pasted, by checking what its author promises against " +
			"what its own tests check. Delegate here as soon as a message contains a pull-request URL, instead " +
			"of the analogy agent. Do not summarise the diff yourself.",
		Instruction:       instruction,
		SkipSummarization: true,
		Model:             defaultReviewModel,
		MaxOutputTokens:   900,
		ToolRefs: []string{
			"vcs_pull",
			"oracle_check",
			"profile_get",
			"profile_save_analogy",
			"assessor_next",
			"assessor_ask",
		},
	}}
}

const instruction = `You walk a learner through one pull request. You do not review it for them.

Answer in the language the learner writes in.

Method:
1. Call vcs_pull with the URL they pasted.
2. Call oracle_check with that result's title, claim, files and url, unchanged.
   oracle_check is the verdict. You never decide whether a claim holds, and you
   never call a gap a bug.
3. Call profile_save_analogy with oracle_check.rows as-is. Every pair's
   breakdown must never be empty. Do not reword breakdown -- it is the
   measurement. Do not invent extra pairs out of the learner's skills.
4. Call assessor_next, then assessor_ask with one short question about
   untested.shape and three clickable options. One option is the untested
   check oracle_check named. Two are plausible and wrong -- reading the diff
   again, or trusting a test because it mentions the name.

Then stop. They answer on the next turn and the tutor grades it.

Never paste the diff. Never restate the cards; the screen already drew them.
Reply with at most two sentences: what the gap is, and that they should pick one.`

func (e *Ext) Manifest() ext.Manifest {
	return ext.Manifest{
		Name:       "review",
		Version:    "0.1.0",
		ABIVersion: ext.ABIVersion,
		Kind:       ext.KindAgent,
		Requires:   []ext.Kind{ext.KindProfile, ext.KindAssessor, ext.KindRetrieval},
		Provides: []ext.ToolSpec{{
			Name:        ext.DescribeTool,
			Description: "Returns the agent specification the host builds this agent from.",
			Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
			ReadOnly:    true,
		}},
	}
}

func (e *Ext) Invoke(_ context.Context, tool string, _ json.RawMessage) (json.RawMessage, error) {
	if tool != ext.DescribeTool {
		return nil, ext.Invalidf("review is a declarative agent plugin; it provides only %q", ext.DescribeTool)
	}
	spec := e.spec
	spec.Name = "review"
	return ext.JSON(spec)
}

func (e *Ext) Close() error { return nil }

var _ ext.Extension = (*Ext)(nil)
