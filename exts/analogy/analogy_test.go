package analogy_test

import (
	"strings"
	"testing"

	"github.com/sirius/cogdebt/exts/analogy"
	"github.com/sirius/cogdebt/internal/ext"
	"github.com/sirius/cogdebt/internal/ext/exttest"
)

func TestAnalogySpec(t *testing.T) {
	spec := exttest.Describe[ext.AgentSpec](t, analogy.New())

	if spec.Description == "" {
		t.Fatal("description is empty; the parent agent reads it to decide when to delegate")
	}
	// The breakdown rule is the product, not a detail. If it ever falls out of
	// the instruction the analogies keep rendering and quietly stop teaching.
	if !strings.Contains(spec.Instruction, "breakdown must never be empty") {
		t.Fatal("instruction no longer requires a non-empty breakdown")
	}
	if !spec.SkipSummarization {
		t.Fatal("SkipSummarization is false: every delegation would pay for an extra LLM call")
	}
}

func TestAnalogyConformance(t *testing.T) {
	exttest.Conformance(t, analogy.New())
}

func TestNaiveVariantIsSeparatePlugin(t *testing.T) {
	if a, b := analogy.New().Manifest().Name, analogy.NewNaive().Manifest().Name; a == b {
		t.Fatalf("both variants are named %q; they must load side by side", a)
	}
	exttest.Conformance(t, analogy.NewNaive())
}
