package store_test

import (
	"testing"

	"github.com/sirius/cogdebt/internal/domain"
	"github.com/sirius/cogdebt/internal/store"
)

func open(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mapping(source, target, breakdown string) domain.Mapping {
	return domain.Mapping{
		Source:     domain.Concept{ID: domain.SlugID(source), Name: source},
		Target:     domain.Concept{ID: domain.SlugID(target), Name: target},
		SharedRole: "degradation_signal",
		Breakdown:  []string{breakdown},
	}
}

// TestSavingAPairTwiceUpdatesIt is the fix for a live run in which the analogy
// agent recorded three pairs and the tutor re-recorded the same three, leaving
// the learner reading each mapping twice.
func TestSavingAPairTwiceUpdatesIt(t *testing.T) {
	db := open(t)
	ctx := t.Context()

	if err := db.SaveMapping(ctx, "u", mapping("backpressure", "irreversibility", "first wording")); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveMapping(ctx, "u", mapping("backpressure", "irreversibility", "refined wording")); err != nil {
		t.Fatal(err)
	}

	got, err := db.Mappings(ctx, "u")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("stored %d rows for one pairing; the analogy table draws each one", len(got))
	}
	if got[0].Breakdown[0] != "refined wording" {
		t.Errorf("breakdown = %q, want the later statement to win", got[0].Breakdown[0])
	}
}

// TestResavingDoesNotErase is the second half of the same story: the caller
// that re-records a pair usually has less to say than the agent that built it,
// and a restatement must not be able to delete what was already there.
func TestResavingDoesNotErase(t *testing.T) {
	db := open(t)
	ctx := t.Context()

	full := mapping("backpressure", "irreversibility", "a queue can be drained")
	full.CarryOver = []string{"both say the system is past a reversible point"}
	if err := db.SaveMapping(ctx, "u", full); err != nil {
		t.Fatal(err)
	}

	// The same pair, restated with only a breakdown.
	thin := domain.Mapping{
		Source:    domain.Concept{ID: domain.SlugID("backpressure"), Name: "backpressure"},
		Target:    domain.Concept{ID: domain.SlugID("irreversibility"), Name: "irreversibility"},
		Breakdown: []string{"entropy cannot be drained"},
	}
	if err := db.SaveMapping(ctx, "u", thin); err != nil {
		t.Fatal(err)
	}

	got, err := db.Mappings(ctx, "u")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	if got[0].Breakdown[0] != "entropy cannot be drained" {
		t.Errorf("breakdown = %q, want the newer wording", got[0].Breakdown[0])
	}
	if len(got[0].CarryOver) == 0 {
		t.Error("the carry_over was erased by a restatement that simply did not mention it")
	}
	if got[0].SharedRole == "" {
		t.Error("the shared role was erased by a restatement that simply did not mention it")
	}
}

func TestDifferentPairsCoexist(t *testing.T) {
	db := open(t)
	ctx := t.Context()

	for _, m := range []domain.Mapping{
		mapping("backpressure", "irreversibility", "a"),
		mapping("rate limiting", "entropy", "b"),
		mapping("backpressure", "entropy", "c"),
	} {
		if err := db.SaveMapping(ctx, "u", m); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.Mappings(ctx, "u")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d mappings, want 3: deduplication is collapsing distinct pairings", len(got))
	}
}

func TestMappingsAreScopedToTheLearner(t *testing.T) {
	db := open(t)
	ctx := t.Context()

	if err := db.SaveMapping(ctx, "alice", mapping("backpressure", "irreversibility", "a")); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveMapping(ctx, "bob", mapping("backpressure", "irreversibility", "b")); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"alice", "bob"} {
		got, err := db.Mappings(ctx, user)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Errorf("%s has %d mappings, want 1; the unique index must be per learner", user, len(got))
		}
	}
}

// TestMasteryIsDamped guards the property the ladder depends on: one good
// answer must not declare a topic learned.
func TestMasteryIsDamped(t *testing.T) {
	db := open(t)
	ctx := t.Context()

	if _, _, err := db.UpsertConcepts(ctx, "u", []domain.Concept{{ID: "raft", Name: "Raft"}}); err != nil {
		t.Fatal(err)
	}
	level, err := db.UpdateMastery(ctx, "u", "raft", 1.0, "nailed it")
	if err != nil {
		t.Fatal(err)
	}
	if level >= 1 {
		t.Errorf("one perfect answer took mastery to %.2f; it is meant to be damped", level)
	}
	if level <= 0 {
		t.Errorf("a perfect answer moved mastery to %.2f", level)
	}
}
