package e2e_test

import (
	"strings"
	"testing"
	"time"

	"github.com/sirius/cogdebt/e2e"
	"github.com/sirius/cogdebt/internal/ext"
)

// A run that did everything right. Each test below breaks one thing.
func goodRun() *e2e.Result {
	return &e2e.Result{
		Scenario: e2e.Scenario{Name: "sample", MustCall: []string{"analogy"}},
		Tools:    map[string]int{"analogy": 1, "assessor_next": 2, "assessor_ask": 2, "assessor_grade": 2},
		Questions: []e2e.Question{
			{Concept: "backpressure", Level: "L1", Prompt: "what plays that role?"},
			{Concept: "backpressure", Level: "L2", Prompt: "where does it stop holding?"},
		},
		Analogies: []ext.AnalogyRow{{
			Source: "backpressure", Target: "irreversibility",
			SharedRole: "degradation_signal", Breakdown: "a queue can be drained; entropy cannot",
		}},
		Mastery: []ext.MasteryItem{{Label: "backpressure", Level: 0.4}},
		Took:    90 * time.Second,
		Model:   "grok-4.6",
	}
}

func hasProblem(t *testing.T, r *e2e.Result, substr string) {
	t.Helper()
	for _, p := range e2e.Problems(r) {
		if strings.Contains(p, substr) {
			return
		}
	}
	t.Errorf("no problem mentioning %q; got %v", substr, e2e.Problems(r))
}

func TestAGoodRunHasNoProblems(t *testing.T) {
	if got := e2e.Problems(goodRun()); len(got) != 0 {
		t.Fatalf("a clean run was flagged: %v", got)
	}
}

// An analogy with no stated limit is the failure mode this whole product is
// built to avoid, so the harness has to catch it.
func TestAnAnalogyWithoutABreakdownIsAProblem(t *testing.T) {
	r := goodRun()
	r.Analogies[0].Breakdown = "   "
	hasProblem(t, r, "no stated breakdown")
}

func TestMappingSomethingOntoItselfIsAProblem(t *testing.T) {
	r := goodRun()
	r.Analogies[0].Target = "Backpressure"
	hasProblem(t, r, "onto itself")
}

func TestAMissingMustCallIsAProblem(t *testing.T) {
	r := goodRun()
	r.Scenario.MustCall = []string{"daytona_run_task"}
	hasProblem(t, r, "daytona_run_task never fired")
}

func TestARungOffTheLadderIsAProblem(t *testing.T) {
	r := goodRun()
	r.Questions[1].Level = "L7"
	hasProblem(t, r, "not on the ladder")
}

// The model choosing its own difficulty is the one thing the assessor exists
// to prevent, so a run where assessor_next never fired is not a pass.
func TestSkippingTheAssessorIsAProblem(t *testing.T) {
	r := goodRun()
	delete(r.Tools, "assessor_next")
	hasProblem(t, r, "chose the difficulty itself")
}

func TestAskingWithoutGradingIsAProblem(t *testing.T) {
	r := goodRun()
	delete(r.Tools, "assessor_grade")
	hasProblem(t, r, "none graded")
}

func TestTranscriptCarriesWhatAReviewerNeeds(t *testing.T) {
	r := goodRun()
	r.Turns = []e2e.Turn{
		{Who: "learner", Text: "I know backpressure"},
		{Who: "tutor", Text: "mapping it now", Tools: []string{"analogy"}, Took: 4 * time.Second},
	}
	md := r.Markdown()
	for _, want := range []string{
		"# sample",
		"grok-4.6",
		"`analogy` × 1",       // which plugins actually ran
		"**L2**",              // the rungs, in order
		"breaks down:",        // the part that does the teaching
		"I know backpressure", // the learner's own words
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the transcript is missing %q, so a reviewer cannot judge the run from it", want)
		}
	}
}

func TestLevelsAndMasteryLookup(t *testing.T) {
	r := goodRun()
	if got := strings.Join(r.Levels(), " "); got != "L1 L2" {
		t.Errorf("Levels() = %q, want the rungs in the order they were asked", got)
	}
	// Loose matching: the model names the concept, and "Backpressure" and
	// "backpressure" are the same thing to a learner.
	if lvl, ok := r.MasteryOf("BACKPRESSURE"); !ok || lvl != 0.4 {
		t.Errorf("MasteryOf = %v, %v; want a case-insensitive hit", lvl, ok)
	}
	if _, ok := r.MasteryOf("raft"); ok {
		t.Error("MasteryOf matched a concept that was never probed")
	}
}
