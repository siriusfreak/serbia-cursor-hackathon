// Package profile is the reference data plugin: it stores what a learner knows.
//
// Copy this file to start a new plugin. The shape is always the same:
//
//  1. a struct holding the plugin's dependencies
//  2. Manifest() -- pure, cheap, stable
//  3. Invoke() -- a switch over tool names
//  4. Close()
//
// This plugin takes a *store.Store, which makes it in-process only. That is
// deliberate: it IS the host's storage. A plugin meant to be portable across
// transports keeps its own state instead -- see docs/PLUGIN_GUIDE.md.
package profile

import (
	"context"
	"encoding/json"

	"github.com/sirius/cogdebt/internal/domain"
	"github.com/sirius/cogdebt/internal/ext"
	"github.com/sirius/cogdebt/internal/store"
)

// Ext is the profile plugin.
type Ext struct {
	db     *store.Store
	userID string
}

// New returns a profile plugin backed by db, scoped to one learner.
func New(db *store.Store, userID string) *Ext {
	return &Ext{db: db, userID: userID}
}

// Manifest describes the plugin. Note how each Description is written for the
// model: it says WHEN to call the tool, not what the code does.
func (e *Ext) Manifest() ext.Manifest {
	return ext.Manifest{
		Name:       "profile",
		Version:    "0.1.0",
		ABIVersion: ext.ABIVersion,
		Kind:       ext.KindProfile,
		Provides: []ext.ToolSpec{
			{
				Name:        "get",
				Description: "Returns everything known about the learner: their concepts, how well they hold each one, and how often it appears in their real work. Call this before giving advice or choosing a question.",
				Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
				ReadOnly:    true,
			},
			{
				Name: "upsert",
				Description: "Saves skills the learner says they have. Call this as soon as they describe what they know, " +
					"even in passing.",
				Schema: json.RawMessage(`{
					"type": "object",
					"properties": {
						"skills": {
							"type": "array",
							"description": "Skill or technology names, e.g. [\"Kubernetes\", \"Go\"]",
							"items": {"type": "string"}
						},
						"domain": {
							"type": "string",
							"description": "Field these skills belong to, e.g. \"infrastructure\""
						}
					},
					"required": ["skills"]
				}`),
			},
			{
				Name: "mastery_update",
				Description: "Records how well the learner answered about one concept. Call this after grading an answer. " +
					"score is 0 for no grasp, 1 for solid command.",
				Schema: json.RawMessage(`{
					"type": "object",
					"properties": {
						"concept": {"type": "string", "description": "Concept name or id the answer was about"},
						"score":   {"type": "number", "description": "0..1"},
						"note":    {"type": "string", "description": "One line on what they did or did not show"}
					},
					"required": ["concept", "score"]
				}`),
			},
		},
	}
}

type upsertArgs struct {
	Skills []string `json:"skills"`
	Domain string   `json:"domain"`
}

type masteryArgs struct {
	Concept string  `json:"concept"`
	Score   float64 `json:"score"`
	Note    string  `json:"note"`
}

// Invoke dispatches one tool call. Every failure the model could fix is
// returned as a *ext.Fault whose message tells it what to do instead.
func (e *Ext) Invoke(ctx context.Context, tool string, in json.RawMessage) (json.RawMessage, error) {
	switch tool {
	case "get":
		return e.get(ctx)
	case "upsert":
		return e.upsert(ctx, in)
	case "mastery_update":
		return e.masteryUpdate(ctx, in)
	default:
		return nil, ext.Invalidf("profile has no tool %q; it provides get, upsert and mastery_update", tool)
	}
}

func (e *Ext) get(ctx context.Context) (json.RawMessage, error) {
	rows, err := e.db.Profile(ctx, e.userID)
	if err != nil {
		return nil, ext.Internalf("read profile: %v", err)
	}
	if len(rows) == 0 {
		// Not an error: an empty profile is the normal starting state. Say so
		// in a way that tells the model what to do next.
		return ext.JSON(map[string]any{
			"concepts": []any{},
			"hint":     "The profile is empty. Ask the learner what they already know, then call profile_upsert.",
		})
	}

	type row struct {
		Concept   string  `json:"concept"`
		Domain    string  `json:"domain,omitempty"`
		Mastery   float64 `json:"mastery"`
		Frequency float64 `json:"frequency,omitempty"`
		NextLevel string  `json:"next_level"`
		Debt      float64 `json:"debt"`
	}
	out := make([]row, 0, len(rows))
	for _, cm := range rows {
		out = append(out, row{
			Concept:   cm.Concept.Name,
			Domain:    cm.Concept.Domain,
			Mastery:   cm.Mastery.Level,
			Frequency: cm.Mastery.Frequency,
			NextLevel: domain.NextLevel(cm.Mastery).String(),
			Debt:      domain.Debt(cm.Mastery, 1),
		})
	}
	return ext.JSON(map[string]any{"concepts": out})
}

func (e *Ext) upsert(ctx context.Context, in json.RawMessage) (json.RawMessage, error) {
	var a upsertArgs
	if err := ext.Args(in, &a); err != nil {
		return nil, err
	}
	if len(a.Skills) == 0 {
		return nil, ext.Invalidf("skills is empty; pass at least one skill name")
	}

	cs := make([]domain.Concept, 0, len(a.Skills))
	for _, s := range a.Skills {
		if id := domain.SlugID(s); id != "" {
			cs = append(cs, domain.Concept{ID: id, Name: s, Domain: a.Domain})
		}
	}
	if len(cs) == 0 {
		return nil, ext.Invalidf("none of the skill names contained usable characters")
	}

	added, updated, err := e.db.UpsertConcepts(ctx, e.userID, cs)
	if err != nil {
		return nil, ext.Internalf("save profile: %v", err)
	}
	return ext.JSON(map[string]any{"added": added, "updated": updated})
}

func (e *Ext) masteryUpdate(ctx context.Context, in json.RawMessage) (json.RawMessage, error) {
	var a masteryArgs
	if err := ext.Args(in, &a); err != nil {
		return nil, err
	}
	id := domain.SlugID(a.Concept)
	if id == "" {
		return nil, ext.Invalidf("concept is empty; name the concept the answer was about")
	}
	if a.Score < 0 || a.Score > 1 {
		return nil, ext.Invalidf("score is %v; it must be between 0 and 1", a.Score)
	}

	level, err := e.db.UpdateMastery(ctx, e.userID, id, a.Score, a.Note)
	if err != nil {
		return nil, ext.Internalf("update mastery: %v", err)
	}
	return ext.JSON(map[string]any{
		"concept":    a.Concept,
		"mastery":    level,
		"next_level": domain.NextLevel(domain.Mastery{Level: level}).String(),
	})
}

// Close releases nothing: the store is owned by the host, and closing a
// borrowed dependency here would break every other plugin sharing it.
func (e *Ext) Close() error { return nil }

// compile-time proof that the plugin satisfies the ABI.
var _ ext.Extension = (*Ext)(nil)
