// Package e2e drives the whole system the way a learner does.
//
// A unit test proves a plugin answers correctly. It cannot prove the thing this
// project is actually claiming: that a profile turns into an analogy, that the
// analogy turns into a rung of questions, and that answering them moves
// mastery. That is a property of the loop, not of any component, and the only
// way to observe it is to run the loop.
//
// So a scenario here is a learner: what they already hold, what they want, and
// the borrowed intuition they are carrying. It runs against the real model and
// the real plugins, records every tool call, and writes a transcript you can
// read afterwards. Different domains deliberately pull in different plugins --
// a coding task needs a sandbox, biology needs sources, physics wants a
// picture -- so the suite also answers "does this tool get used at all, and by
// the right kind of question?".
//
// Nothing here runs by default: every scenario costs money and wall time.
//
//	COGDEBT_LIVE=1 go test ./e2e/ -run Physics -v -timeout 30m
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/genai"

	"github.com/sirius/cogdebt/internal/app"
	"github.com/sirius/cogdebt/internal/ext"
	"github.com/sirius/cogdebt/internal/obs"
)

// Scenario is one learner working through one field.
type Scenario struct {
	// Name identifies the scenario and names its transcript file.
	Name string
	// Learner is the persona the simulated student plays.
	Learner Learner
	// Opening are scripted messages, sent in order before the student takes
	// over. They are scripted because the first two moves -- stating skills and
	// naming a target -- are the same in every scenario, and because this is
	// where a scenario steers the tutor toward the plugin it means to exercise.
	Opening []string
	// Replies is how many adaptive turns follow, each one the student answering
	// whatever the tutor last put on screen.
	Replies int
	// MustCall are qualified tool names the scenario is pointless without. A
	// biology run that never fetches a source proved nothing about retrieval.
	MustCall []string
	// Needs are environment variables the scenario cannot run without.
	Needs []string
	// Check asserts whatever is specific to this domain.
	Check func(t *testing.T, r *Result)
}

// Turn is one exchange, with everything observable about it.
type Turn struct {
	// Who is "learner" or "tutor".
	Who string
	// Text is what was said. For a tutor turn, the streamed reply concatenated.
	Text string
	// Tools are the qualified tool names called during this turn, in order.
	Tools []string
	// Question is the card the assessor put on screen this turn, if any.
	Question *Question
	// Took is the wall time of the turn.
	Took time.Duration
}

// Question is an assessor_ask result: the rung, and the wording.
type Question struct {
	Concept string `json:"concept"`
	Level   string `json:"level"`
	Prompt  string `json:"prompt"`
}

// Result is everything the scenario produced.
type Result struct {
	Scenario  Scenario
	Turns     []Turn
	Tools     map[string]int
	Questions []Question
	Analogies []ext.AnalogyRow
	Mastery   []ext.MasteryItem
	// Images are urls from any illustration tool, so a picture can be opened.
	Images []string
	Took   time.Duration
	// Model is what actually answered, recorded because a scenario's timing and
	// tool discipline both change with it.
	Model string
	// OK is whether the scenario's assertions all held.
	OK bool
}

// Called reports how many times a tool fired.
func (r *Result) Called(tool string) int { return r.Tools[tool] }

// Levels lists the rungs asked, in order.
func (r *Result) Levels() []string {
	out := make([]string, 0, len(r.Questions))
	for _, q := range r.Questions {
		out = append(out, q.Level)
	}
	return out
}

// MasteryOf returns the recorded level for a concept, matched loosely because
// the model chooses the wording and "динамическое программирование" and
// "dynamic programming" are the same concept to a learner.
func (r *Result) MasteryOf(substr string) (float64, bool) {
	for _, m := range r.Mastery {
		if strings.Contains(strings.ToLower(m.Label), strings.ToLower(substr)) {
			return m.Level, true
		}
	}
	return 0, false
}

// Run plays the scenario end to end.
func Run(t *testing.T, s Scenario) *Result {
	t.Helper()
	requireLive(t, s.Needs)

	log := obs.Nop()
	if testing.Verbose() {
		if l, closeLog, err := obs.Setup(obs.Config{Level: "info", Format: "text"}); err == nil {
			log = l
			defer closeLog()
		}
	}

	// Eight minutes is roughly twice the slowest healthy run. A scenario that
	// has gone wrong tends to go wrong by looping, and a generous timeout means
	// paying twenty minutes to find that out.
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()

	// No PluginDir: a scenario runs the in-process plugins only. A stale binary
	// left in ./plugins would otherwise silently replace one of them, and the
	// run would be reporting on code that is not in this tree. The subprocess
	// transport has its own tests.
	a, err := app.New(ctx, app.Config{
		DBPath: filepath.Join(t.TempDir(), "e2e.db"),
		UserID: "e2e",
		Log:    log,
	})
	if err != nil {
		t.Fatalf("build the system: %v", err)
	}
	defer a.Close()

	student, err := NewStudent(ctx, s.Learner)
	if err != nil {
		t.Fatalf("build the student: %v", err)
	}

	r := &Result{Scenario: s, Tools: map[string]int{}, Model: a.Model}
	started := time.Now()
	sessionID := "e2e-" + s.Name

	say := func(text string) {
		r.Turns = append(r.Turns, Turn{Who: "learner", Text: text})
		turn := tutorTurn(ctx, t, a, sessionID, text, r)
		r.Turns = append(r.Turns, turn)
	}

	for _, msg := range s.Opening {
		say(msg)
	}
	for i := 0; i < s.Replies; i++ {
		reply, err := student.Answer(ctx, lastTutorTurn(r))
		if err != nil {
			t.Errorf("the student could not answer on turn %d: %v", i+1, err)
			break
		}
		say(reply)
	}

	r.Took = time.Since(started)
	r.Analogies = a.Analogies(ctx)
	r.Mastery = a.Mastery(ctx)

	writeTranscript(t, r)
	assertProcess(t, r)
	if s.Check != nil {
		s.Check(t, r)
	}
	r.OK = !t.Failed()
	record(r)
	return r
}

// completed collects results across a whole run, for the index.
var (
	mu        sync.Mutex
	completed []*Result
)

func record(r *Result) {
	mu.Lock()
	defer mu.Unlock()
	completed = append(completed, r)
}

// WriteIndex writes one table covering every scenario that ran. Call it from
// TestMain after m.Run(): the per-scenario transcripts say what happened, and
// this says whether the shape of it is the same across fields.
func WriteIndex() {
	mu.Lock()
	defer mu.Unlock()
	if len(completed) == 0 {
		return
	}
	dir := os.Getenv("COGDEBT_E2E_OUT")
	if dir == "" {
		dir = "out"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	var b strings.Builder
	b.WriteString("# Scenario runs\n\n")
	fmt.Fprintf(&b, "%s\n\n", time.Now().Format(time.RFC3339))
	b.WriteString("| scenario | | time | rungs | analogies | tools |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, r := range completed {
		mark := "ok"
		if !r.OK {
			mark = "**failed**"
		}
		rungs := strings.Join(r.Levels(), " ")
		if rungs == "" {
			rungs = "-"
		}
		fmt.Fprintf(&b, "| [%s](%s.md) | %s | %s | %s | %d | %s |\n",
			r.Scenario.Name, r.Scenario.Name, mark, r.Took.Round(time.Second),
			rungs, len(r.Analogies), strings.Join(sortedKeys(r.Tools), " "))
	}
	b.WriteString("\nRungs are the ladder in the order it was climbed. A row of `L1 L1 L1` " +
		"means the learner never earned L2, which is worth reading the transcript over.\n")

	_ = os.WriteFile(filepath.Join(dir, "index.md"), []byte(b.String()), 0o644)
}

// tutorTurn sends one learner message and drains the agent's event stream.
func tutorTurn(ctx context.Context, t *testing.T, a *app.App, sessionID, text string, r *Result) Turn {
	t.Helper()
	started := time.Now()
	turn := Turn{Who: "tutor"}
	var reply strings.Builder
	// A model emits a short preamble before each tool call, as its own text
	// part. Concatenating them runs two sentences together ("...on screen.Next
	// question...") and makes the transcript read as one garbled paragraph, so
	// a part that follows a tool call starts a new one -- which is also what the
	// UI does.
	brokeForTool := false

	msg := genai.NewContentFromText(text, genai.RoleUser)
	for ev, err := range a.Runner.Run(ctx, a.UserID, sessionID, msg, agent.RunConfig{}) {
		if err != nil {
			t.Errorf("turn failed: %v", err)
			break
		}
		if ev == nil || ev.Content == nil {
			continue
		}
		for _, part := range ev.Content.Parts {
			if part.Thought {
				continue // model scratchpad, not the answer
			}
			switch {
			case part.Text != "":
				if brokeForTool && reply.Len() > 0 {
					reply.WriteString("\n\n")
				}
				brokeForTool = false
				reply.WriteString(part.Text)
			case part.FunctionCall != nil:
				name := part.FunctionCall.Name
				turn.Tools = append(turn.Tools, name)
				r.Tools[name]++
				brokeForTool = true
			case part.FunctionResponse != nil:
				absorb(r, &turn, part.FunctionResponse.Name, part.FunctionResponse.Response)
			}
		}
	}
	turn.Text = strings.TrimSpace(reply.String())
	turn.Took = time.Since(started)
	return turn
}

// absorb pulls the structured results the UI would have drawn as cards. The
// scenario reads exactly what the screen reads, so an assertion here is an
// assertion about what the learner sees.
func absorb(r *Result, turn *Turn, name string, resp map[string]any) {
	raw, err := json.Marshal(resp)
	if err != nil {
		return
	}
	switch {
	case strings.HasSuffix(name, "assessor_ask"):
		var q Question
		if json.Unmarshal(raw, &q) == nil && q.Prompt != "" {
			turn.Question = &q
			r.Questions = append(r.Questions, q)
		}
	case strings.HasSuffix(name, "fal_illustrate"):
		var img struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(raw, &img) == nil && img.URL != "" {
			r.Images = append(r.Images, img.URL)
		}
	}
}

// lastTutorTurn is what the student is answering: the tutor's prose plus,
// when there is one, the question card. The card matters -- the tutor is told
// not to repeat it as text, so prose alone often contains no question at all.
func lastTutorTurn(r *Result) string {
	for i := len(r.Turns) - 1; i >= 0; i-- {
		if r.Turns[i].Who != "tutor" {
			continue
		}
		t := r.Turns[i]
		if t.Question == nil {
			return t.Text
		}
		return strings.TrimSpace(t.Text + "\n\n" + t.Question.Prompt)
	}
	return ""
}

// Problems lists everything wrong with a run, whatever the field.
//
// It is a pure function over the Result so that the rules can be tested without
// spending a model call. The checks are deliberately about the LOOP rather than
// about content: a model words an analogy differently every run, but it must
// always record one, must never skip the breakdown, and must let the assessor
// choose the rung.
func Problems(r *Result) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }

	for _, want := range r.Scenario.MustCall {
		if r.Tools[want] == 0 {
			add("%s never fired; the scenario exercised nothing of what it was written for (called: %s)",
				want, strings.Join(sortedKeys(r.Tools), " "))
		}
	}

	if len(r.Analogies) == 0 {
		add("no analogy was recorded, so there was nothing to learn from")
	}
	for _, a := range r.Analogies {
		if strings.TrimSpace(a.Breakdown) == "" {
			add("analogy %q maps to %q with no stated breakdown; that is how this tool would CREATE "+
				"cognitive debt instead of paying it off", a.Source, a.Target)
		}
		if strings.EqualFold(a.Source, a.Target) {
			add("analogy maps %q onto itself", a.Source)
		}
	}

	if len(r.Questions) == 0 {
		add("the learner was never asked anything, so no mastery could be measured")
	}
	for _, q := range r.Questions {
		switch strings.ToUpper(q.Level) {
		case "L1", "L2", "L3", "L4":
		default:
			add("question on concept %q came back at rung %q, which is not on the ladder", q.Concept, q.Level)
		}
	}

	if r.Tools["assessor_next"] == 0 {
		add("assessor_next never fired: the model chose the difficulty itself, which is the one thing the " +
			"ladder exists to prevent")
	}
	if asks, grades := r.Tools["assessor_ask"], r.Tools["assessor_grade"]; asks > 0 && grades == 0 {
		add("%d questions asked and none graded; nothing the learner said was recorded", asks)
	}
	return out
}

// assertProcess reports every problem as a separate failure, so one run says
// everything that went wrong rather than only the first thing.
func assertProcess(t *testing.T, r *Result) {
	t.Helper()
	for _, p := range Problems(r) {
		t.Error(p)
	}
}

// requireLive skips unless the scenario can actually reach what it needs.
func requireLive(t *testing.T, needs []string) {
	t.Helper()
	if os.Getenv("COGDEBT_LIVE") == "" {
		t.Skip("set COGDEBT_LIVE=1 to run scenarios against the real model and services")
	}
	for _, key := range append([]string{"XAI_API_KEY"}, needs...) {
		if os.Getenv(key) == "" {
			t.Skipf("%s is not set, and this scenario is about the plugin it unlocks", key)
		}
	}
}

// writeTranscript saves the run as readable markdown.
//
// A pass/fail line says the loop held together; it does not say whether the
// teaching was any good. That judgement is a human's, and it needs the words.
func writeTranscript(t *testing.T, r *Result) {
	t.Helper()
	dir := os.Getenv("COGDEBT_E2E_OUT")
	if dir == "" {
		dir = "out"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("transcript directory: %v", err)
		return
	}
	path := filepath.Join(dir, r.Scenario.Name+".md")
	if err := os.WriteFile(path, []byte(r.Markdown()), 0o644); err != nil {
		t.Logf("write transcript: %v", err)
		return
	}
	t.Logf("%s · %s · %d turns · rungs %s · transcript %s",
		r.Scenario.Name, r.Took.Round(time.Second), len(r.Turns)/2,
		strings.Join(r.Levels(), " "), path)
}

// Markdown renders the run.
func (r *Result) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", r.Scenario.Name)
	fmt.Fprintf(&b, "%s · %s · model `%s`\n\n", time.Now().Format(time.RFC3339), r.Took.Round(time.Second), r.Model)

	fmt.Fprintf(&b, "## Tools\n\n")
	for _, name := range sortedKeys(r.Tools) {
		fmt.Fprintf(&b, "- `%s` × %d\n", name, r.Tools[name])
	}

	fmt.Fprintf(&b, "\n## Ladder\n\n")
	if len(r.Questions) == 0 {
		b.WriteString("_nothing asked_\n")
	}
	for _, q := range r.Questions {
		fmt.Fprintf(&b, "- **%s** · %s — %s\n", q.Level, q.Concept, q.Prompt)
	}

	fmt.Fprintf(&b, "\n## Analogies recorded\n\n")
	for _, a := range r.Analogies {
		fmt.Fprintf(&b, "- **%s** maps to **%s**", a.Source, a.Target)
		if a.SharedRole != "" {
			fmt.Fprintf(&b, " · role `%s`", a.SharedRole)
		}
		b.WriteString("\n")
		if a.CarryOver != "" {
			fmt.Fprintf(&b, "  - carries over: %s\n", a.CarryOver)
		}
		fmt.Fprintf(&b, "  - breaks down: %s\n", a.Breakdown)
	}

	fmt.Fprintf(&b, "\n## Mastery at the end\n\n")
	for _, m := range r.Mastery {
		fmt.Fprintf(&b, "- %s — level %.2f, debt %.2f\n", m.Label, m.Level, m.Debt)
	}

	if len(r.Images) > 0 {
		fmt.Fprintf(&b, "\n## Images\n\n")
		for _, u := range r.Images {
			fmt.Fprintf(&b, "- %s\n", u)
		}
	}

	fmt.Fprintf(&b, "\n## Transcript\n\n")
	for _, t := range r.Turns {
		if t.Who == "learner" {
			fmt.Fprintf(&b, "**learner** — %s\n\n", t.Text)
			continue
		}
		fmt.Fprintf(&b, "**tutor** _(%s", t.Took.Round(time.Millisecond*100))
		if len(t.Tools) > 0 {
			fmt.Fprintf(&b, ", tools: %s", strings.Join(t.Tools, ", "))
		}
		b.WriteString(")_\n\n")
		if t.Text != "" {
			fmt.Fprintf(&b, "%s\n\n", t.Text)
		}
		if t.Question != nil {
			fmt.Fprintf(&b, "> **%s · %s** %s\n\n", t.Question.Level, t.Question.Concept, t.Question.Prompt)
		}
	}
	return b.String()
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
