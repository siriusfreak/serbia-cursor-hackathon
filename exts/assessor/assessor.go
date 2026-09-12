// Package assessor runs the question ladder.
//
// The division of labour matters: choosing WHICH concept to probe and at WHICH
// rung is domain logic and stays deterministic here, driven by mastery and debt.
// Only the wording of the question is left to the model. That keeps the climb
// honest -- a model asked to both pick the difficulty and judge the answer will
// drift toward whatever it just explained.
package assessor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sirius/cogdebt/internal/domain"
	"github.com/sirius/cogdebt/internal/ext"
	"github.com/sirius/cogdebt/internal/store"
)

// pendingKey is where the open question is parked between ask and grade.
const pendingKey = "pending"

// Ext is the assessor plugin.
type Ext struct {
	db     *store.Store
	kv     *store.KV
	userID string
}

// New returns an assessor scoped to one learner.
func New(db *store.Store, userID string) *Ext {
	return &Ext{db: db, kv: db.KV("assessor"), userID: userID}
}

func (e *Ext) Manifest() ext.Manifest {
	return ext.Manifest{
		Name:       "assessor",
		Version:    "0.1.0",
		ABIVersion: ext.ABIVersion,
		Kind:       ext.KindAssessor,
		Requires:   []ext.Kind{ext.KindProfile},
		Provides: []ext.ToolSpec{
			{
				Name: "next",
				Description: "Chooses what to ask the learner next and at which rung of the ladder, based on what " +
					"they hold and what they owe. Call this before writing a question; do not pick the topic or the " +
					"difficulty yourself.",
				Schema:   json.RawMessage(`{"type":"object","properties":{}}`),
				ReadOnly: true,
			},
			{
				Name: "ask",
				Description: "Puts your question to the learner on screen. Call this with the wording you wrote for " +
					"the concept and level that assessor_next returned.",
				Schema: json.RawMessage(`{
					"type": "object",
					"properties": {
						"concept": {"type": "string", "description": "Concept from assessor_next"},
						"level":   {"type": "string", "description": "L1, L2, L3 or L4, from assessor_next"},
						"prompt":  {"type": "string", "description": "The question, in the learner's language"}
					},
					"required": ["concept", "level", "prompt"]
				}`),
			},
			{
				Name: "grade",
				Description: "Records how well the learner answered the open question and moves their mastery. " +
					"Call this right after they answer. score is 0 for no grasp, 1 for solid command.",
				Schema: json.RawMessage(`{
					"type": "object",
					"properties": {
						"score": {"type": "number", "description": "0..1"},
						"note":  {"type": "string", "description": "One line on what they did or did not show"}
					},
					"required": ["score"]
				}`),
			},
		},
	}
}

func (e *Ext) Invoke(ctx context.Context, tool string, in json.RawMessage) (json.RawMessage, error) {
	switch tool {
	case "next":
		return e.next(ctx)
	case "ask":
		return e.ask(ctx, in)
	case "grade":
		return e.grade(ctx, in)
	default:
		return nil, ext.Invalidf("assessor has no tool %q; it provides next, ask and grade", tool)
	}
}

// next picks the concept worth the most to probe and the highest rung the
// learner has earned on it.
func (e *Ext) next(ctx context.Context) (json.RawMessage, error) {
	rows, err := e.db.Profile(ctx, e.userID)
	if err != nil {
		return nil, ext.Internalf("read profile: %v", err)
	}
	if len(rows) == 0 {
		return nil, ext.NotFoundf("the profile is empty; ask the learner what they know and call profile_upsert first")
	}

	best, bestScore := rows[0], -1.0
	for _, cm := range rows {
		// Probe what costs the most: heavy debt, or simply the weakest grip
		// when nothing is known about real usage yet.
		score := domain.Debt(cm.Mastery, 1)
		if score == 0 {
			score = 1 - cm.Mastery.Level
		}
		if score > bestScore {
			best, bestScore = cm, score
		}
	}

	level := domain.NextLevel(best.Mastery)
	return ext.JSON(map[string]any{
		"concept":  best.Concept.Name,
		"level":    level.String(),
		"level_is": levelMeaning(level),
		"mastery":  best.Mastery.Level,
		"ask_for":  guidance(level),
		"then":     "Write the question in the learner's language, then call assessor_ask.",
	})
}

func (e *Ext) ask(ctx context.Context, in json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Concept string `json:"concept"`
		Level   string `json:"level"`
		Prompt  string `json:"prompt"`
	}
	if err := ext.Args(in, &a); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Prompt) == "" {
		return nil, ext.Invalidf("prompt is empty; write the question itself")
	}
	if domain.SlugID(a.Concept) == "" {
		return nil, ext.Invalidf("concept is empty; pass the concept assessor_next returned")
	}
	level := parseLevel(a.Level)
	if level == 0 {
		return nil, ext.Invalidf("level %q is not one of L1, L2, L3, L4", a.Level)
	}

	id := fmt.Sprintf("q-%s-%s", domain.SlugID(a.Concept), level)
	pending := map[string]any{"id": id, "concept": a.Concept, "level": level.String(), "prompt": a.Prompt}
	if err := e.kv.SetJSON(ctx, pendingKey, pending); err != nil {
		return nil, ext.Internalf("park question: %v", err)
	}
	// The host renders this shape directly, so the card on screen comes from
	// structured data rather than from parsing the model's prose.
	return ext.JSON(pending)
}

func (e *Ext) grade(ctx context.Context, in json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Score float64 `json:"score"`
		Note  string  `json:"note"`
	}
	if err := ext.Args(in, &a); err != nil {
		return nil, err
	}
	if a.Score < 0 || a.Score > 1 {
		return nil, ext.Invalidf("score is %v; it must be between 0 and 1", a.Score)
	}

	var pending struct {
		Concept string `json:"concept"`
		Level   string `json:"level"`
	}
	ok, err := e.kv.GetJSON(ctx, pendingKey, &pending)
	if err != nil {
		return nil, ext.Internalf("read open question: %v", err)
	}
	if !ok || pending.Concept == "" {
		return nil, ext.NotFoundf("there is no open question to grade; call assessor_next and assessor_ask first")
	}

	level, err := e.db.UpdateMastery(ctx, e.userID, domain.SlugID(pending.Concept), a.Score, a.Note)
	if err != nil {
		return nil, ext.Internalf("update mastery: %v", err)
	}
	if err := e.kv.Set(ctx, pendingKey, "{}"); err != nil {
		return nil, ext.Internalf("clear open question: %v", err)
	}

	next := domain.NextLevel(domain.Mastery{Level: level})
	return ext.JSON(map[string]any{
		"concept":    pending.Concept,
		"answered":   pending.Level,
		"mastery":    level,
		"next_level": next.String(),
		"next_is":    levelMeaning(next),
	})
}

func parseLevel(s string) domain.Level {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "L1":
		return domain.L1Transfer
	case "L2":
		return domain.L2Limit
	case "L3":
		return domain.L3Native
	case "L4":
		return domain.L4Synthesis
	}
	return 0
}

func levelMeaning(l domain.Level) string {
	switch l {
	case domain.L1Transfer:
		return "what maps to what"
	case domain.L2Limit:
		return "where the analogy breaks"
	case domain.L3Native:
		return "the target field on its own terms"
	case domain.L4Synthesis:
		return "combining several ideas"
	}
	return "unknown"
}

// guidance tells the model what shape of question the rung calls for. The rungs
// are not difficulty tiers -- L2 exists because a borrowed intuition has to be
// corrected before it hardens, which is the entire point of the ladder.
func guidance(l domain.Level) string {
	switch l {
	case domain.L1Transfer:
		return "Ask which thing in the new field plays the role they already know from their own field."
	case domain.L2Limit:
		return "Ask where the analogy stops holding. This is the rung that does the teaching: make them find the seam, do not name it for them."
	case domain.L3Native:
		return "Ask in the target field's own vocabulary, with no analogy to lean on."
	case domain.L4Synthesis:
		return "Give a small problem that needs two or three target concepts combined."
	}
	return "Ask something concrete."
}

func (e *Ext) Close() error { return nil }

var _ ext.Extension = (*Ext)(nil)
