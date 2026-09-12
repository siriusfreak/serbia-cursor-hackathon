// Package domain holds the learning model: concepts, mastery, analogies and
// cognitive debt.
//
// It imports nothing from the rest of the project -- no ADK, no Fyne, no ext.
// That is deliberate: the learning logic stays testable with no API key, no
// network and no GUI, so it can be fixed at 4am when everything above it is
// on fire.
package domain

import (
	"regexp"
	"strings"
)

// Role is a concept's structural position in its domain. Analogies are matched
// on roles, not on names -- that is what produces "etcd <-> model registry"
// instead of pairing things that merely sound alike.
type Role string

const (
	RoleSourceOfTruth     Role = "source_of_truth"
	RoleUnitOfScheduling  Role = "unit_of_scheduling"
	RoleRollback          Role = "rollback"
	RoleDegradationSignal Role = "degradation_signal"
	RoleQuota             Role = "quota"
	RoleReconcileLoop     Role = "reconcile_loop"
)

// Concept is one idea in some domain.
type Concept struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Roles  []Role `json:"roles,omitempty"`
}

// Mastery is how well a learner holds a concept, on [0,1].
type Mastery struct {
	ConceptID string  `json:"concept_id"`
	Level     float64 `json:"level"`
	// Frequency is how often the concept shows up in the learner's real work,
	// on [0,1]. Filled in by a retrieval plugin scanning their repositories.
	Frequency float64 `json:"frequency"`
}

// ConceptMastery pairs a concept with the learner's grip on it.
type ConceptMastery struct {
	Concept
	Mastery
}

// Mapping is one analogy: a source concept the learner already holds, matched
// to a target concept they do not, through a shared structural role.
type Mapping struct {
	Source     Concept  `json:"source"`
	Target     Concept  `json:"target"`
	SharedRole Role     `json:"shared_role"`
	CarryOver  []string `json:"carry_over"`
	// Breakdown says where the analogy stops holding. It must never be empty:
	// an analogy without a stated limit creates new cognitive debt instead of
	// paying off the old kind.
	Breakdown  []string `json:"breakdown"`
	Confidence float64  `json:"confidence"`
}

// Valid reports whether the mapping is fit to show a learner.
func (m Mapping) Valid() bool {
	return m.Source.Name != "" && m.Target.Name != "" && len(m.Breakdown) > 0
}

// Level is a rung on the question ladder. The learner climbs as mastery grows.
type Level int

const (
	// L1Transfer asks what plays a known role in the new domain.
	L1Transfer Level = iota + 1
	// L2Limit asks where the analogy breaks. The most valuable rung: it is
	// where a borrowed intuition gets corrected before it calcifies.
	L2Limit
	// L3Native asks in the target domain's own terms, with no analogy to lean on.
	L3Native
	// L4Synthesis asks the learner to combine several target concepts.
	L4Synthesis
)

// String names the level as it appears in prompts and the UI.
func (l Level) String() string {
	switch l {
	case L1Transfer:
		return "L1"
	case L2Limit:
		return "L2"
	case L3Native:
		return "L3"
	case L4Synthesis:
		return "L4"
	}
	return "L?"
}

// masteryForLevel is the grip a learner needs before a rung is worth asking.
var masteryForLevel = map[Level]float64{L1Transfer: 0, L2Limit: 0.3, L3Native: 0.55, L4Synthesis: 0.8}

// NextLevel picks the highest rung the learner has earned. Climbing only on
// demonstrated mastery is what keeps the ladder inside their reach instead of
// dropping them into the deep end.
func NextLevel(m Mastery) Level {
	best := L1Transfer
	for _, l := range []Level{L2Limit, L3Native, L4Synthesis} {
		if m.Level >= masteryForLevel[l] {
			best = l
		}
	}
	return best
}

// Debt scores how much a concept is costing the learner right now: something
// they lean on often, that much else depends on, and that they do not hold.
//
// prereqDepth is how many other concepts build on this one.
func Debt(m Mastery, prereqDepth int) float64 {
	depth := float64(prereqDepth)
	if depth < 1 {
		depth = 1
	}
	return m.Frequency * depth * (1 - clamp01(m.Level))
}

// ApplyGrade moves mastery toward a graded answer, damped so one lucky reply
// does not declare a topic learned.
func ApplyGrade(current float64, score float64, weight float64) float64 {
	if weight <= 0 {
		weight = 0.34
	}
	return clamp01(current + weight*(clamp01(score)-clamp01(current)))
}

func clamp01(v float64) float64 {
	return max(0, min(1, v))
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// SlugID derives a stable concept ID from a display name, so the same skill
// typed twice does not become two concepts.
func SlugID(name string) string {
	s := nonSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	return strings.Trim(s, "-")
}
