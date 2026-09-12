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
	"strings"

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
			{
				Name: "set_frequency",
				Description: "Records how often each concept appears in the learner's real work, as a value between " +
					"0 and 1. Call this with the output of github_scan. Without it cognitive debt cannot be computed, " +
					"because debt weighs what they lean on against what they hold.",
				Schema: json.RawMessage(`{
					"type": "object",
					"properties": {
						"concepts": {
							"type": "array",
							"items": {
								"type": "object",
								"properties": {
									"name":      {"type": "string"},
									"frequency": {"type": "number", "description": "0..1"}
								},
								"required": ["name", "frequency"]
							}
						}
					},
					"required": ["concepts"]
				}`),
			},
			{
				Name: "save_analogy",
				Description: "Stores the analogy pairs you built for the learner and shows them on screen. Call this " +
					"once per analogy, immediately after the analogy agent answers. Every pair must say where the " +
					"analogy breaks.",
				Schema: json.RawMessage(`{
					"type": "object",
					"properties": {
						"rows": {
							"type": "array",
							"items": {
								"type": "object",
								"properties": {
									"source":      {"type": "string", "description": "Concept the learner already holds"},
									"target":      {"type": "string", "description": "Concept in the new field"},
									"shared_role": {"type": "string", "description": "Short noun phrase naming the role both play, e.g. \"source of truth\". Not a sentence, and never your reasoning."},
									"carry_over":  {"type": "string", "description": "What their intuition gets right. One or two sentences."},
									"breakdown":   {"type": "string", "description": "Where that intuition will mislead them. One or two sentences."}
								},
								"required": ["source", "target", "breakdown"]
							}
						}
					},
					"required": ["rows"]
				}`),
			},
		},
	}
}

type freqArgs struct {
	Concepts []struct {
		Name      string  `json:"name"`
		Frequency float64 `json:"frequency"`
	} `json:"concepts"`
}

type analogyArgs struct {
	Rows []struct {
		Source     string `json:"source"`
		Target     string `json:"target"`
		SharedRole string `json:"shared_role"`
		CarryOver  string `json:"carry_over"`
		Breakdown  string `json:"breakdown"`
	} `json:"rows"`
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
	case "set_frequency":
		return e.setFrequency(ctx, in)
	case "save_analogy":
		return e.saveAnalogy(ctx, in)
	default:
		return nil, ext.Invalidf("profile has no tool %q; it provides get, upsert, mastery_update, set_frequency and save_analogy", tool)
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

// setFrequency records how much of the learner's real work touches each
// concept. Concepts it has never seen are created, because the point is to
// surface what they lean on but never named as a skill.
func (e *Ext) setFrequency(ctx context.Context, in json.RawMessage) (json.RawMessage, error) {
	var a freqArgs
	if err := ext.Args(in, &a); err != nil {
		return nil, err
	}
	if len(a.Concepts) == 0 {
		return nil, ext.Invalidf("concepts is empty; pass the output of github_scan")
	}

	fresh := make([]domain.Concept, 0, len(a.Concepts))
	for _, c := range a.Concepts {
		if id := domain.SlugID(c.Name); id != "" {
			fresh = append(fresh, domain.Concept{ID: id, Name: c.Name})
		}
	}
	if _, _, err := e.db.UpsertConcepts(ctx, e.userID, fresh); err != nil {
		return nil, ext.Internalf("record concepts: %v", err)
	}

	updated := 0
	for _, c := range a.Concepts {
		id := domain.SlugID(c.Name)
		if id == "" {
			continue
		}
		if c.Frequency < 0 || c.Frequency > 1 {
			return nil, ext.Invalidf("frequency for %q is %v; it must be between 0 and 1", c.Name, c.Frequency)
		}
		if err := e.db.SetFrequency(ctx, e.userID, id, c.Frequency); err != nil {
			return nil, ext.Internalf("set frequency for %q: %v", c.Name, err)
		}
		updated++
	}
	return ext.JSON(map[string]any{"updated": updated})
}

// saveAnalogy persists the pairs and hands them back in the shape the UI
// renders, so the table on screen comes from structured data rather than from
// parsing the model's prose.
func (e *Ext) saveAnalogy(ctx context.Context, in json.RawMessage) (json.RawMessage, error) {
	var a analogyArgs
	if err := ext.Args(in, &a); err != nil {
		return nil, err
	}
	if len(a.Rows) == 0 {
		return nil, ext.Invalidf("rows is empty; pass the analogy pairs you built")
	}

	rows := make([]ext.AnalogyRow, 0, len(a.Rows))
	for _, r := range a.Rows {
		if err := checkLengths(r.Source, r.Target, r.SharedRole, r.CarryOver, r.Breakdown); err != nil {
			return nil, err
		}
		m := domain.Mapping{
			Source:     domain.Concept{ID: domain.SlugID(r.Source), Name: r.Source},
			Target:     domain.Concept{ID: domain.SlugID(r.Target), Name: r.Target},
			SharedRole: domain.Role(r.SharedRole),
			Breakdown:  splitNonEmpty(r.Breakdown),
			CarryOver:  splitNonEmpty(r.CarryOver),
		}
		// The domain refuses a mapping with no stated limit; surface that as
		// something the model can fix rather than silently storing a half pair.
		if !m.Valid() {
			return nil, ext.Invalidf("the pair %q -> %q has no breakdown; an analogy without a stated limit creates cognitive debt instead of paying it off", r.Source, r.Target)
		}
		if err := e.db.SaveMapping(ctx, e.userID, m); err != nil {
			return nil, ext.Internalf("save analogy: %v", err)
		}
		rows = append(rows, ext.AnalogyRow{
			Source: r.Source, Target: r.Target, SharedRole: r.SharedRole,
			CarryOver: r.CarryOver, Breakdown: r.Breakdown,
		})
	}
	return ext.JSON(map[string]any{"saved": len(rows), "rows": rows})
}

// Field caps. A model that has been asked for a structural role will sometimes
// write its whole train of thought into that one string -- a live run produced
// a thousand-word deliberation, ending in "So pairs: ...", stored as the role
// and rendered on screen as one. Nothing downstream can recover from that, so
// it is refused here where the fault text can tell the model what the field is
// for; faults come back as results, so it simply writes a shorter one.
const (
	maxNameLen  = 120 // a concept name
	maxRoleLen  = 120 // a role is a noun phrase, not an argument
	maxProseLen = 800 // carry_over and breakdown are a sentence or two
)

func checkLengths(source, target, role, carryOver, breakdown string) error {
	for _, f := range []struct {
		name, value, want string
		max               int
	}{
		{"source", source, "the name of a concept the learner holds", maxNameLen},
		{"target", target, "the name of a concept in the new field", maxNameLen},
		{"shared_role", role, "a short noun phrase naming the role both play, like \"source of truth\"", maxRoleLen},
		{"carry_over", carryOver, "one or two sentences", maxProseLen},
		{"breakdown", breakdown, "one or two sentences", maxProseLen},
	} {
		if len([]rune(f.value)) > f.max {
			return ext.Invalidf("%s is %d characters; it must be at most %d, because it is %s. "+
				"Write the pair again with a shorter %s -- do not put your reasoning in it",
				f.name, len([]rune(f.value)), f.max, f.want, f.name)
		}
	}
	return nil
}

func splitNonEmpty(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return []string{s}
}

// Close releases nothing: the store is owned by the host, and closing a
// borrowed dependency here would break every other plugin sharing it.
func (e *Ext) Close() error { return nil }

// compile-time proof that the plugin satisfies the ABI.
var _ ext.Extension = (*Ext)(nil)
