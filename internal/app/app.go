// Package app assembles the whole system: store, model, plugins, agent, runner.
//
// It exists so that there is exactly one construction path. The desktop shell,
// the terminal REPL and the end-to-end scenarios all build the same object
// graph from here, which means a scenario that passes is evidence about the
// program the learner actually runs rather than about a test-only rig.
package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/openaimodel"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"

	"github.com/sirius/cogdebt/exts/analogy"
	"github.com/sirius/cogdebt/exts/assessor"
	"github.com/sirius/cogdebt/exts/daytona"
	"github.com/sirius/cogdebt/exts/exa"
	"github.com/sirius/cogdebt/exts/fal"
	"github.com/sirius/cogdebt/exts/firecrawl"
	"github.com/sirius/cogdebt/exts/github"
	"github.com/sirius/cogdebt/exts/profile"
	"github.com/sirius/cogdebt/internal/domain"
	"github.com/sirius/cogdebt/internal/ext"
	"github.com/sirius/cogdebt/internal/ext/subprocess"
	"github.com/sirius/cogdebt/internal/obs"
	"github.com/sirius/cogdebt/internal/store"
)

// RootInstruction is the tutor's standing brief.
//
// It is a constant rather than a flag because the loop it describes IS the
// product: the ladder is chosen by the assessor, not by the model, and the
// prohibition on narrating mechanics is what keeps the transcript readable.
const RootInstruction = `You help an engineer learn a new field by building on what they already know.

Always reply in the language the learner writes in.

The loop:

1. SKILLS. If you do not know what they know, ask -- then call profile_upsert.
   If they give you a GitHub username, call github_scan and pass its concepts
   straight to profile_set_frequency. That is what makes cognitive debt real
   rather than guessed: it measures what they lean on, which self-report misses.

2. ANALOGY. Once skills are saved and they have named a target topic, delegate to
   the analogy agent immediately. Do NOT call profile_get first -- the analogy agent
   reads the profile itself, and calling it in the same turn as profile_upsert races
   the write and returns an empty profile.
   The agent records its own pairs and the screen draws them, so do not repeat
   them as text. Then call profile_save_analogy yourself with exactly the pairs it
   returned, in its wording -- do not translate or reword them. A pair saved twice
   under the same names collapses into one row, so the repeat costs nothing, and
   it is the only thing standing between the learner and an empty analogy table.
   If they ask to see the mapping, or say they think in pictures, call fal_illustrate
   once for the single pair that carries the most weight. The screen shows the
   picture; do not describe it.

3. LADDER. Call assessor_next to learn which concept to probe and at which rung.
   Never choose the topic or the difficulty yourself. Write the question in the
   learner's language and put it on screen with assessor_ask.

4. GRADE. When they answer, call assessor_grade with a score from 0 to 1, then go
   back to step 3.
   If they ask for a coding task, or make a claim that running code would settle,
   set the problem and run it with daytona_run_task in that same turn. Do not
   promise it for a later rung -- a belief that can be executed should be.

The screen already draws the analogy table and the question card from the tool
results. Do not repeat their contents as text -- add only what they do not show.

Never narrate your own mechanics: do not translate the learner's message back to
them, do not announce which tool you are about to call, do not explain what a tool
returned. Say nothing at all before a tool call -- write only once you have the
result, and write it to the learner rather than about yourself.

Ask one question at a time. Be concrete and brief.`

// Config is what varies between the shell, the REPL and a scenario.
type Config struct {
	// DBPath is the SQLite file. Required.
	DBPath string
	// PluginDir is scanned for out-of-process plugin binaries. Optional.
	PluginDir string
	// UserID scopes the profile, mastery and analogies. Defaults to "local".
	UserID string
	// Instruction overrides RootInstruction. Optional.
	Instruction string
	// Naive also loads the naive analogy plugin, for side-by-side comparison.
	Naive bool
	// Log receives structured events. Defaults to a discarding logger.
	Log *slog.Logger
}

// App is the assembled system.
type App struct {
	Runner   *runner.Runner
	Registry *ext.Registry
	Store    *store.Store
	Toolset  *ext.Toolset
	UserID   string
	// Model is the model id in use, for the header badge and the transcript.
	Model string
	// Transport records which plugins arrived out-of-process, keyed by name.
	Transport map[string]string

	cfg Config
	log *slog.Logger
}

// New builds everything. Close it when done.
func New(ctx context.Context, cfg Config) (*App, error) {
	if cfg.UserID == "" {
		cfg.UserID = "local"
	}
	if cfg.Instruction == "" {
		cfg.Instruction = RootInstruction
	}
	log := obs.Or(cfg.Log)

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}

	llm, err := BuildModel(ctx)
	if err != nil {
		db.Close()
		return nil, err
	}

	a := &App{
		Store:     db,
		Registry:  ext.NewRegistry(log),
		UserID:    cfg.UserID,
		Model:     ModelName(),
		Transport: map[string]string{},
		cfg:       cfg,
		log:       log,
	}

	a.loadPlugins()
	if cfg.Naive {
		a.Registry.MustLoad(analogy.NewNaive())
	}

	a.Toolset = ext.NewToolset(ext.ToolsetConfig{
		Registry: a.Registry,
		Model:    llm,
		ModelFor: func(name string) (model.LLM, error) { return BuildNamedModel(ctx, name) },
		Log:      log,
	})

	// The root agent is given one Toolset and never learns that plugins exist.
	root, err := llmagent.New(llmagent.Config{
		Name:        "tutor",
		Description: "Teaches a new field by analogy to what the learner already knows.",
		Instruction: cfg.Instruction,
		Model:       llm,
		Toolsets:    []tool.Toolset{a.Toolset},
	})
	if err != nil {
		a.Close()
		return nil, fmt.Errorf("build root agent: %w", err)
	}

	// The runner owns the agent loop, so model and tool spans are only visible
	// from inside it. This lifecycle plugin is how they get out.
	tracer, err := obs.Tracer(log)
	if err != nil {
		a.Close()
		return nil, fmt.Errorf("build tracer: %w", err)
	}

	a.Runner, err = runner.New(runner.Config{
		AppName:           "cogdebt",
		Agent:             root,
		SessionService:    session.InMemoryService(),
		AutoCreateSession: true,
		PluginConfig:      runner.PluginConfig{Plugins: []*plugin.Plugin{tracer}},
	})
	if err != nil {
		a.Close()
		return nil, fmt.Errorf("build runner: %w", err)
	}
	return a, nil
}

// Close releases plugins and the database.
func (a *App) Close() {
	if a.Registry != nil {
		a.Registry.Close()
	}
	if a.Store != nil {
		a.Store.Close()
	}
}

// Reload rebuilds the plugin set in place and reports how many are active.
//
// Because ADK asks the Toolset for tools on every turn, a key added in Settings
// switches its plugin on mid-conversation -- nothing restarts, and the model
// simply has one more tool on its next move.
func (a *App) Reload() int {
	a.Registry.Close()
	clear(a.Transport)
	a.loadPlugins()
	a.Toolset.Invalidate()
	return len(a.Registry.Manifests())
}

// loadPlugins fills the registry. Out-of-process plugins are discovered first
// and win over their built-in twins, so dropping a binary into ./plugins swaps
// the transport with no code change.
//
// A plugin with no key is not loaded at all rather than loaded and failing:
// exposing a tool the model cannot use wastes a turn every time it tries.
func (a *App) loadPlugins() {
	if a.cfg.PluginDir != "" {
		for _, name := range subprocess.LoadDir(a.Registry, a.cfg.PluginDir, a.log) {
			a.Transport[name] = "subprocess"
		}
	}

	a.Registry.MustLoad(
		profile.New(a.Store, a.UserID),
		assessor.New(a.Store, a.UserID),
		analogy.New(),
	)
	if !a.Registry.Has("github") {
		a.Registry.MustLoad(github.New())
	}

	// Each of these needs a key. Configured() is the gate.
	type gated interface {
		ext.Extension
		Configured() bool
	}
	for _, e := range []gated{firecrawl.New(), exa.New(), fal.New(), daytona.New()} {
		if e.Configured() {
			a.Registry.MustLoad(e)
		}
	}
}

// PluginSummary lists what loaded, shown in the sidebar so the plugin layer is
// visible without opening a terminal.
func (a *App) PluginSummary() []string {
	ms := a.Registry.Manifests()
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, fmt.Sprintf("%s %s (%s)", m.Name, m.Version, m.Kind))
	}
	return out
}

// ToolNames is every qualified tool the model can currently see.
func (a *App) ToolNames() []string {
	tools, err := a.Toolset.Tools(nil)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name())
	}
	sort.Strings(names)
	return names
}

// Mastery feeds the progress sidebar from the database after each turn.
func (a *App) Mastery(ctx context.Context) []ext.MasteryItem {
	rows, err := a.Store.Profile(ctx, a.UserID)
	if err != nil {
		return nil
	}
	items := make([]ext.MasteryItem, 0, len(rows))
	for _, cm := range rows {
		debt := domain.Debt(cm.Mastery, 1)
		// A scan turns up plenty of incidental tags. Show only what the learner
		// has started on or is actually paying for; the rest is noise in a panel
		// that is meant to be read at a glance.
		if cm.Mastery.Level == 0 && debt < 0.4 {
			continue
		}
		items = append(items, ext.MasteryItem{Label: cm.Concept.Name, Level: cm.Mastery.Level, Debt: debt})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Debt != items[j].Debt {
			return items[i].Debt > items[j].Debt
		}
		return items[i].Level > items[j].Level
	})
	if len(items) > 8 {
		items = items[:8]
	}
	return items
}

// Analogies reads stored analogies for the learner, newest first.
func (a *App) Analogies(ctx context.Context) []ext.AnalogyRow {
	saved, err := a.Store.Mappings(ctx, a.UserID)
	if err != nil {
		return nil
	}
	rows := make([]ext.AnalogyRow, 0, len(saved))
	for _, m := range saved {
		rows = append(rows, ext.AnalogyRow{
			Source:     m.Source.Name,
			Target:     m.Target.Name,
			SharedRole: string(m.SharedRole),
			CarryOver:  firstOf(m.CarryOver),
			Breakdown:  firstOf(m.Breakdown),
		})
	}
	return rows
}

func firstOf(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return ss[0]
}

// ModelName is the model id, overridable from the environment.
func ModelName() string {
	return FirstNonEmpty(os.Getenv("COGDEBT_MODEL"), "grok-4.6")
}

// BuildModel points the OpenAI-compatible adapter at xAI. Any OpenAI-shaped
// endpoint works the same way; only the two env vars change.
func BuildModel(ctx context.Context) (model.LLM, error) {
	key := FirstNonEmpty(os.Getenv("XAI_API_KEY"), os.Getenv("OPENAI_API_KEY"))
	if key == "" {
		return nil, errors.New("no API key: put XAI_API_KEY in .env (it is gitignored)")
	}
	name := ModelName()
	base := FirstNonEmpty(os.Getenv("XAI_BASE_URL"), "https://api.x.ai/v1")

	llm, err := openaimodel.NewModel(ctx, name, &openaimodel.ClientConfig{APIKey: key, BaseURL: base})
	if err != nil {
		return nil, fmt.Errorf("build model %q: %w", name, err)
	}
	return llm, nil
}

// BuildNamedModel builds a model an agent plugin asked for by name, against the
// same endpoint as the default.
func BuildNamedModel(ctx context.Context, name string) (model.LLM, error) {
	key := FirstNonEmpty(os.Getenv("XAI_API_KEY"), os.Getenv("OPENAI_API_KEY"))
	if key == "" {
		return nil, errors.New("no API key")
	}
	base := FirstNonEmpty(os.Getenv("XAI_BASE_URL"), "https://api.x.ai/v1")
	return openaimodel.NewModel(ctx, name, &openaimodel.ClientConfig{APIKey: key, BaseURL: base})
}

// LoadDotEnv reads KEY=VALUE lines, without pulling in a dependency for it.
// Existing environment variables win.
func LoadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
}

// FirstNonEmpty returns the first value that is not "".
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
