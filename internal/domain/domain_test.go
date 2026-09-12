package domain_test

import (
	"math"
	"testing"

	"github.com/sirius/cogdebt/internal/domain"
)

// TestSlugIDKeepsAnyScript is the fix for a live run: the tutor replied in
// Russian, named the concept in Russian, and profile_upsert refused it with
// "none of the skill names contained usable characters" — an ASCII-only slug
// had erased every character of the name. A tutor whose first promise is to
// answer in the learner's language cannot be unable to remember what they said.
func TestSlugIDKeepsAnyScript(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"Kubernetes", "kubernetes"},
		{"SQL window functions", "sql-window-functions"},
		{"  Feature Store  ", "feature-store"},
		{"point-in-time correctness", "point-in-time-correctness"},
		{"C++", "c"},
		{"динамическое программирование", "динамическое-программирование"},
		{"Каскад MAPK", "каскад-mapk"},
		{"Go 1.27", "go-1-27"},
	} {
		if got := domain.SlugID(tc.name); got != tc.want {
			t.Errorf("SlugID(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A slug is an identity, so the same skill written two ways is one concept.
func TestSlugIDIsStable(t *testing.T) {
	for _, pair := range [][2]string{
		{"Feature Store", "feature store"},
		{"feature-store", "Feature  Store"},
		{"Динамическое Программирование", "динамическое программирование"},
	} {
		if a, b := domain.SlugID(pair[0]), domain.SlugID(pair[1]); a != b {
			t.Errorf("%q and %q slug differently (%q vs %q), so they become two concepts", pair[0], pair[1], a, b)
		}
	}
	if domain.SlugID("!!!") != "" {
		t.Error("a name with no letters or digits should slug to nothing, so the caller can refuse it")
	}
}

// TestAnalogyWithoutALimitIsInvalid guards the product's central invariant.
func TestAnalogyWithoutALimitIsInvalid(t *testing.T) {
	full := domain.Mapping{
		Source:    domain.Concept{Name: "etcd"},
		Target:    domain.Concept{Name: "feature store"},
		Breakdown: []string{"point-in-time correctness is an invariant etcd never had"},
	}
	if !full.Valid() {
		t.Fatal("a complete mapping was rejected")
	}
	noLimit := full
	noLimit.Breakdown = nil
	if noLimit.Valid() {
		t.Error("a mapping with no breakdown was accepted; that is how this tool creates cognitive debt " +
			"instead of paying it off")
	}
}

func TestLadderClimbsOnDemonstratedMastery(t *testing.T) {
	for _, tc := range []struct {
		level float64
		want  string
	}{
		{0, "L1"}, {0.29, "L1"}, {0.3, "L2"}, {0.54, "L2"}, {0.55, "L3"}, {0.79, "L3"}, {0.8, "L4"}, {1, "L4"},
	} {
		if got := domain.NextLevel(domain.Mastery{Level: tc.level}).String(); got != tc.want {
			t.Errorf("mastery %.2f puts the learner on %s, want %s", tc.level, got, tc.want)
		}
	}
}

// TestDebtIsWhatYouLeanOnAndDoNotHold pins the formula the sidebar draws.
func TestDebtIsWhatYouLeanOnAndDoNotHold(t *testing.T) {
	// Never used, never held: no debt. Debt is a cost, not an absence.
	if got := domain.Debt(domain.Mastery{Frequency: 0, Level: 0}, 1); got != 0 {
		t.Errorf("debt on something never used = %v, want 0", got)
	}
	// Leaned on constantly and not held at all: maximum cost.
	if got := domain.Debt(domain.Mastery{Frequency: 1, Level: 0}, 1); got != 1 {
		t.Errorf("debt on an unheld daily dependency = %v, want 1", got)
	}
	// Leaned on constantly and fully held: paid off.
	if got := domain.Debt(domain.Mastery{Frequency: 1, Level: 1}, 1); got != 0 {
		t.Errorf("debt on a mastered daily dependency = %v, want 0", got)
	}
	// Depth multiplies: a concept others build on costs more to owe.
	shallow := domain.Debt(domain.Mastery{Frequency: 0.5, Level: 0}, 1)
	deep := domain.Debt(domain.Mastery{Frequency: 0.5, Level: 0}, 3)
	if deep <= shallow {
		t.Errorf("depth 3 (%v) does not cost more than depth 1 (%v)", deep, shallow)
	}
	// Depth below 1 must not zero the formula out.
	if got := domain.Debt(domain.Mastery{Frequency: 1, Level: 0}, 0); got != 1 {
		t.Errorf("debt at depth 0 = %v, want the depth to be floored at 1", got)
	}
}

// TestApplyGradeIsDamped is why one lucky answer does not declare a topic learned.
func TestApplyGradeIsDamped(t *testing.T) {
	first := domain.ApplyGrade(0, 1, 0)
	if first >= 1 {
		t.Fatalf("one perfect answer took mastery from 0 to %v", first)
	}
	if first <= 0 {
		t.Fatalf("a perfect answer moved mastery to %v", first)
	}
	// Repeated success converges upward rather than jumping.
	level := 0.0
	for i := 0; i < 20; i++ {
		level = domain.ApplyGrade(level, 1, 0)
	}
	if math.Abs(level-1) > 0.01 {
		t.Errorf("twenty perfect answers land at %v, want close to 1", level)
	}
	// A bad answer pulls back down.
	if down := domain.ApplyGrade(0.8, 0, 0); down >= 0.8 {
		t.Errorf("a failed answer moved mastery from 0.80 to %v", down)
	}
	// Scores outside the range are clamped, not trusted.
	if got := domain.ApplyGrade(0.5, 42, 0); got > 1 {
		t.Errorf("a score of 42 produced mastery %v", got)
	}
}
