package profile_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sirius/cogdebt/exts/profile"
	"github.com/sirius/cogdebt/internal/ext/exttest"
	"github.com/sirius/cogdebt/internal/store"
)

// This file is the template for testing a plugin. Copy it.
//
// exttest.Calls exercises real calls; exttest.Conformance then runs every
// check the host performs at load time. Conformance closes the plugin, so it
// goes last.

func newPlugin(t *testing.T) *profile.Ext {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return profile.New(db, "test-user")
}

func TestProfileCalls(t *testing.T) {
	p := newPlugin(t)

	exttest.Calls(t, p,
		exttest.Case{
			Tool: "get",
			Args: map[string]any{},
			Check: func(t *testing.T, result json.RawMessage) {
				var got struct {
					Concepts []any  `json:"concepts"`
					Hint     string `json:"hint"`
				}
				mustJSON(t, result, &got)
				if len(got.Concepts) != 0 {
					t.Fatalf("fresh profile has %d concepts, want 0", len(got.Concepts))
				}
				// An empty profile must tell the model what to do next, or it
				// stalls instead of asking the learner anything.
				if got.Hint == "" {
					t.Fatal("empty profile returned no hint for the model")
				}
			},
		},
		exttest.Case{
			Tool: "upsert",
			Args: map[string]any{"skills": []string{"Kubernetes", "Go"}, "domain": "infrastructure"},
			Check: func(t *testing.T, result json.RawMessage) {
				var got struct {
					Added int `json:"added"`
				}
				mustJSON(t, result, &got)
				if got.Added != 2 {
					t.Fatalf("added = %d, want 2", got.Added)
				}
			},
		},
		exttest.Case{
			Tool:      "upsert",
			Args:      map[string]any{"skills": []string{}},
			WantFault: "invalid_args",
		},
		exttest.Case{
			Tool:      "mastery_update",
			Args:      map[string]any{"concept": "Kubernetes", "score": 4.2},
			WantFault: "invalid_args", // score outside 0..1
		},
		exttest.Case{
			Tool: "mastery_update",
			Args: map[string]any{"concept": "Kubernetes", "score": 0.8, "note": "named the reconcile loop"},
			Check: func(t *testing.T, result json.RawMessage) {
				var got struct {
					Mastery   float64 `json:"mastery"`
					NextLevel string  `json:"next_level"`
				}
				mustJSON(t, result, &got)
				if got.Mastery <= 0 {
					t.Fatalf("mastery = %v, want > 0", got.Mastery)
				}
				// Grading is damped: one good answer must not jump to mastered.
				if got.Mastery >= 0.8 {
					t.Fatalf("mastery = %v; a single answer should not reach the graded score", got.Mastery)
				}
				if got.NextLevel == "" {
					t.Fatal("next_level is empty")
				}
			},
		},
	)
}

// TestProfileSkillsPersist checks that upsert is actually durable, which the
// generic harness cannot know to check.
func TestProfileSkillsPersist(t *testing.T) {
	p := newPlugin(t)
	ctx := t.Context()

	args, _ := json.Marshal(map[string]any{"skills": []string{"Kubernetes"}})
	if _, err := p.Invoke(ctx, "upsert", args); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	raw, err := p.Invoke(ctx, "get", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var got struct {
		Concepts []struct {
			Concept string `json:"concept"`
		} `json:"concepts"`
	}
	mustJSON(t, raw, &got)
	if len(got.Concepts) != 1 || got.Concepts[0].Concept != "Kubernetes" {
		t.Fatalf("profile = %+v, want one Kubernetes concept", got.Concepts)
	}
}

func TestProfileConformance(t *testing.T) {
	exttest.Conformance(t, newPlugin(t))
}

func mustJSON(t *testing.T, raw json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

// TestAnalogyFieldsHaveCaps covers a live failure: the analogy agent wrote a
// thousand-word deliberation into shared_role, it was stored, and the screen
// rendered it as the role. The fault has to name the field so the model can
// write a shorter one on its next move.
func TestAnalogyFieldsHaveCaps(t *testing.T) {
	p := newPlugin(t)
	essay := strings.Repeat("deliberating about what the role really is, ", 40)

	args, _ := json.Marshal(map[string]any{"rows": []map[string]any{{
		"source": "Postgres primary", "target": "Raft leader",
		"shared_role": essay, "breakdown": "Postgres has no quorum",
	}}})
	_, err := p.Invoke(t.Context(), "save_analogy", args)
	if err == nil {
		t.Fatal("an essay in shared_role was accepted; it will be rendered on screen as the role")
	}
	if !strings.Contains(err.Error(), "shared_role") {
		t.Errorf("the fault does not name the offending field, so the model cannot fix it: %v", err)
	}

	// The same pair with a real role goes through.
	args, _ = json.Marshal(map[string]any{"rows": []map[string]any{{
		"source": "Postgres primary", "target": "Raft leader",
		"shared_role": "source of truth", "breakdown": "Postgres has no quorum or election",
	}}})
	if _, err := p.Invoke(t.Context(), "save_analogy", args); err != nil {
		t.Fatalf("a well-formed pair was refused: %v", err)
	}
}
