package review_test

import (
	"strings"
	"testing"

	"github.com/sirius/cogdebt/exts/review"
	"github.com/sirius/cogdebt/internal/ext"
	"github.com/sirius/cogdebt/internal/ext/exttest"
)

func TestReviewSpec(t *testing.T) {
	spec := exttest.Describe[ext.AgentSpec](t, review.New())

	if spec.Description == "" {
		t.Fatal("description is empty; the parent agent reads it to decide when to delegate")
	}
	if !strings.Contains(spec.Instruction, "breakdown must never be empty") {
		t.Fatal("instruction no longer requires a non-empty breakdown")
	}
	if !strings.Contains(spec.Instruction, "vcs_pull") || !strings.Contains(spec.Instruction, "oracle_check") {
		t.Fatal("instruction must send the agent to vcs_pull and oracle_check, not to invent a verdict")
	}
	if !strings.Contains(spec.Instruction, "untested.shape") {
		t.Fatal("instruction must ask about the oracle's untested shape, not a generic summary")
	}
	if !strings.Contains(spec.Instruction, "options") {
		t.Fatal("instruction must give clickable options; the learner should not have to invent a paragraph")
	}
	if !spec.SkipSummarization {
		t.Fatal("SkipSummarization is false: every delegation would pay for an extra LLM call")
	}
	want := map[string]bool{
		"vcs_pull": true, "oracle_check": true, "profile_save_analogy": true,
	}
	for _, ref := range spec.ToolRefs {
		delete(want, ref)
	}
	for missing := range want {
		t.Errorf("ToolRefs missing %q; the host will not hand the agent that tool", missing)
	}
}

func TestReviewConformance(t *testing.T) {
	exttest.Conformance(t, review.New())
}
