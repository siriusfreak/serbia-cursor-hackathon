package assessor_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sirius/cogdebt/exts/assessor"
	"github.com/sirius/cogdebt/internal/domain"
	"github.com/sirius/cogdebt/internal/ext/exttest"
	"github.com/sirius/cogdebt/internal/store"
)

func newPlugin(t *testing.T, skills ...string) *assessor.Ext {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cs := make([]domain.Concept, 0, len(skills))
	for _, s := range skills {
		cs = append(cs, domain.Concept{ID: domain.SlugID(s), Name: s})
	}
	if _, _, err := db.UpsertConcepts(t.Context(), "test-user", cs); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	return assessor.New(db, "test-user")
}

// next asks for a probe and returns the concept and rung it chose.
func next(t *testing.T, e *assessor.Ext) (concept, level string, retry bool) {
	t.Helper()
	raw, err := e.Invoke(t.Context(), "next", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	var got struct {
		Concept string `json:"concept"`
		Level   string `json:"level"`
		Retry   bool   `json:"retry"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got.Concept, got.Level, got.Retry
}

func askAndGrade(t *testing.T, e *assessor.Ext, concept, level string, score float64) {
	t.Helper()
	args, _ := json.Marshal(map[string]any{"concept": concept, "level": level, "prompt": "what plays that role?"})
	if _, err := e.Invoke(t.Context(), "ask", args); err != nil {
		t.Fatalf("ask: %v", err)
	}
	args, _ = json.Marshal(map[string]any{"score": score, "note": "test"})
	if _, err := e.Invoke(t.Context(), "grade", args); err != nil {
		t.Fatalf("grade: %v", err)
	}
}

// TestMissedConceptIsAskedAgain is the fix for what a live run exposed: the
// learner gave the same wrong answer three times and was asked about three
// different concepts, so the misconception was never put to them directly.
func TestMissedConceptIsAskedAgain(t *testing.T) {
	e := newPlugin(t, "backpressure", "rate limiting", "queueing theory")

	first, level, retry := next(t, e)
	if retry {
		t.Error("the very first probe cannot be a retry")
	}
	askAndGrade(t, e, first, level, 0.2)

	again, _, retry := next(t, e)
	if again != first {
		t.Errorf("after missing %q the assessor moved to %q; a miss has to be confronted, not rotated past", first, again)
	}
	if !retry {
		t.Error("the retry flag is not set, so the tutor will reword the same question instead of making the miss concrete")
	}
}

// TestRetriesAreBounded keeps the fix from turning into a drill.
func TestRetriesAreBounded(t *testing.T) {
	e := newPlugin(t, "backpressure", "rate limiting", "queueing theory")

	first, _, _ := next(t, e)
	for i := 0; i < 3; i++ {
		concept, level, _ := next(t, e)
		askAndGrade(t, e, concept, level, 0.1)
	}
	moved, _, _ := next(t, e)
	if moved == first {
		t.Errorf("still on %q after three misses; the learner is being drilled rather than taught", first)
	}
}

// TestAGoodAnswerMovesOn is the other half: stickiness must not survive success.
func TestAGoodAnswerMovesOn(t *testing.T) {
	e := newPlugin(t, "backpressure", "rate limiting")

	first, level, _ := next(t, e)
	askAndGrade(t, e, first, level, 0.9)

	moved, _, retry := next(t, e)
	if retry {
		t.Error("a well-answered question was treated as a miss")
	}
	if moved == first {
		t.Errorf("stayed on %q after a strong answer; the weaker concept is the one worth probing", first)
	}
}

// TestLadderClimbsWithMastery pins the rung thresholds to the ladder, so a
// change to one is a deliberate change to the other.
func TestLadderClimbsWithMastery(t *testing.T) {
	e := newPlugin(t, "backpressure")

	if _, level, _ := next(t, e); level != "L1" {
		t.Fatalf("a learner with no recorded mastery starts at %s, want L1", level)
	}
	// Three strong answers in a row: mastery is damped, so this takes a few.
	for i := 0; i < 4; i++ {
		concept, level, _ := next(t, e)
		askAndGrade(t, e, concept, level, 1.0)
	}
	_, level, _ := next(t, e)
	if level == "L1" {
		t.Error("four correct answers and still on L1: the ladder never climbs, so L2 -- the rung that does the " +
			"teaching -- is unreachable")
	}
}

func TestGradeWithoutAnOpenQuestion(t *testing.T) {
	e := newPlugin(t, "backpressure")
	args, _ := json.Marshal(map[string]any{"score": 0.5})
	if _, err := e.Invoke(t.Context(), "grade", args); err == nil {
		t.Error("grading with nothing asked should fault, not silently move mastery")
	}
}

func TestAssessorConformance(t *testing.T) {
	exttest.Conformance(t, newPlugin(t, "backpressure"))
}

// TestOneConceptClimbsTheWholeLadder documents the happy path, and pins the
// number of good answers it takes.
//
// The learner must name ONE skill for this to work. next() probes whatever the
// learner holds least well, so with three skills it rotates and no single
// concept ever reaches the mastery L4 needs — which is correct behaviour, and
// also why a demo that lists three skills never gets past L2.
func TestOneConceptClimbsTheWholeLadder(t *testing.T) {
	e := newPlugin(t, "PostgreSQL")

	var rungs []string
	for i := 0; i < 6; i++ {
		concept, level, _ := next(t, e)
		rungs = append(rungs, level)
		askAndGrade(t, e, concept, level, 1.0)
	}

	got := strings.Join(rungs, " ")
	const want = "L1 L2 L3 L3 L4 L4"
	if got != want {
		t.Errorf("a learner answering perfectly climbs %q, want %q", got, want)
	}
}
