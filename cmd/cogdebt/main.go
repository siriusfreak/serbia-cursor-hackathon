// Command cogdebt runs the learning agent.
//
// This is phase 0/1 wiring: a terminal REPL, deliberately without the Fyne UI
// so that agent problems and GUI problems stay separable. The UI lands in
// phase 2a and reuses everything below unchanged.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/openaimodel"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"

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
	"github.com/sirius/cogdebt/internal/ui"
)

const rootInstruction = `You help an engineer learn a new field by building on what they already know.

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
   The analogy agent records and renders its own pairs; do not repeat them as text.

3. LADDER. Call assessor_next to learn which concept to probe and at which rung.
   Never choose the topic or the difficulty yourself. Write the question in the
   learner's language and put it on screen with assessor_ask.

4. GRADE. When they answer, call assessor_grade with a score from 0 to 1, then go
   back to step 3.

The screen already draws the analogy table and the question card from the tool
results. Do not repeat their contents as text -- add only what they do not show.

Never narrate your own mechanics: do not translate the learner's message back to
them, do not announce which tool you are about to call, do not explain what a tool
returned.

Ask one question at a time. Be concrete and brief.`

func main() {
	var (
		dbPath  = flag.String("db", "cogdebt.db", "SQLite database path")
		plugDir = flag.String("plugins", "plugins", "directory scanned for out-of-process plugin binaries")
		user    = flag.String("user", "local", "learner id")
		naive   = flag.Bool("naive", false, "also load the naive analogy plugin, for comparison")
		cli     = flag.Bool("cli", false, "terminal REPL instead of the desktop window")
		shot    = flag.String("screenshot", "", "save a PNG of the window here and exit")
		say     = flag.String("say", "", "submit this message on startup, for a scripted live run")
		openCfg = flag.Bool("open-settings", false, "open the settings dialog on startup, for screenshots")
		after   = flag.Duration("shot-after", 1200*time.Millisecond, "how long to wait before the screenshot")
		debug   = flag.Bool("debug", false, "shorthand for -log-level debug")
		logLvl  = flag.String("log-level", "info", "debug, info, warn or error")
		logFmt  = flag.String("log-format", "text", "text or json; use json when you intend to query the output")
		logFile = flag.String("log-file", "", "also append structured logs to this file")
	)
	flag.Parse()

	if *debug {
		*logLvl = "debug"
	}
	log, closeLog, err := obs.Setup(obs.Config{Level: *logLvl, Format: *logFmt, File: *logFile})
	if err != nil {
		fmt.Fprintf(os.Stderr, "logging: %v\n", err)
		os.Exit(1)
	}
	defer closeLog()

	if err = run(*dbPath, *plugDir, *user, *naive, *cli, *shot, *say, *openCfg, *after, log); err != nil {
		fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		os.Exit(1)
	}
}

func run(dbPath, pluginDir, userID string, naive, cli bool, shot, say string, openCfg bool, after time.Duration, log *slog.Logger) error {
	loadDotEnv(".env")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	db, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	llm, err := buildModel(ctx)
	if err != nil {
		return err
	}

	// Every capability enters the system here and nowhere else.
	reg := ext.NewRegistry(log)
	transport := map[string]string{}
	defer reg.Close()

	loadPlugins(reg, db, userID, pluginDir, transport, log)
	if naive {
		reg.MustLoad(analogy.NewNaive())
	}

	// Reload rebuilds the plugin set in place. Because ADK asks the Toolset for
	// tools on every turn, a key added in Settings switches its plugin on
	// mid-conversation -- nothing restarts, and the model simply has one more
	// tool on its next move.
	var toolset *ext.Toolset
	reload := func() int {
		reg.Close()
		clear(transport)
		loadPlugins(reg, db, userID, pluginDir, transport, log)
		toolset.Invalidate()
		return len(reg.Manifests())
	}

	toolset = ext.NewToolset(ext.ToolsetConfig{
		Registry: reg,
		Model:    llm,
		ModelFor: func(name string) (model.LLM, error) { return buildNamedModel(ctx, name) },
		Log:      log,
	})

	// The root agent is given one Toolset and never learns that plugins exist.
	root, err := llmagent.New(llmagent.Config{
		Name:        "tutor",
		Description: "Teaches a new field by analogy to what the learner already knows.",
		Instruction: rootInstruction,
		Model:       llm,
		Toolsets:    []tool.Toolset{toolset},
	})
	if err != nil {
		return fmt.Errorf("build root agent: %w", err)
	}

	// The runner owns the agent loop, so model and tool spans are only visible
	// from inside it. This lifecycle plugin is how they get out.
	tracer, err := obs.Tracer(log)
	if err != nil {
		return fmt.Errorf("build tracer: %w", err)
	}

	r, err := runner.New(runner.Config{
		AppName:           "cogdebt",
		Agent:             root,
		SessionService:    session.InMemoryService(),
		AutoCreateSession: true,
		PluginConfig:      runner.PluginConfig{Plugins: []*plugin.Plugin{tracer}},
	})
	if err != nil {
		return fmt.Errorf("build runner: %w", err)
	}

	bridge := &ui.Bridge{Runner: r, UserID: userID, SessionID: "main", Log: log}

	if cli {
		printBanner(reg, toolset, transport)
		return repl(ctx, r, userID)
	}

	shell := ui.New(ctx, ui.Config{
		Model:     modelName(),
		Settings:  settingsConfig(reload, log),
		Bridge:    bridge,
		Mastery:   masteryPanel(db, userID),
		Analogies: analogyPanel(db, userID),
		Plugins:   pluginSummary(reg),
	})
	switch {
	case say != "":
		// A scripted live run: real model, real plugins, no sample content.
		shell.Ask(say, 1200*time.Millisecond)
	case shot != "":
		// A preview: sample content, no model call.
		shell.Seed()
	}
	if openCfg {
		shell.OpenSettings(900 * time.Millisecond)
	}
	if shot != "" {
		return shell.RunAndCapture(shot, after)
	}
	shell.Run()
	return nil
}

// loadPlugins fills the registry. Out-of-process plugins are discovered first
// and win over their built-in twins, so dropping a binary into ./plugins swaps
// the transport with no code change.
//
// A plugin with no key is not loaded at all rather than loaded and failing:
// exposing a tool the model cannot use wastes a turn every time it tries.
func loadPlugins(reg *ext.Registry, db *store.Store, userID, pluginDir string, transport map[string]string, log *slog.Logger) {
	for _, name := range subprocess.LoadDir(reg, pluginDir, log) {
		transport[name] = "subprocess"
	}

	reg.MustLoad(
		profile.New(db, userID),
		assessor.New(db, userID),
		analogy.New(),
	)
	if !reg.Has("github") {
		reg.MustLoad(github.New())
	}

	// Each of these needs a key. Configured() is the gate.
	type gated interface {
		ext.Extension
		Configured() bool
	}
	for _, e := range []gated{firecrawl.New(), exa.New(), fal.New(), daytona.New()} {
		if e.Configured() {
			reg.MustLoad(e)
		}
	}
}

// settingsConfig describes what is configurable and what saving does.
func settingsConfig(reload func() int, log *slog.Logger) ui.SettingsConfig {
	return ui.SettingsConfig{
		Fields: func() []ui.SettingField {
			return []ui.SettingField{
				{Section: "Model", Key: "COGDEBT_MODEL", Label: "Main model",
					Help:  "Runs the conversation and decides which tools to call.",
					Value: modelName(), RestartRequired: true},
				{Section: "Model", Key: "COGDEBT_ANALOGY_MODEL", Label: "Analogy model",
					Help:  "Follows a written procedure, so a non-reasoning model is both faster and cleaner here.",
					Value: envOr("COGDEBT_ANALOGY_MODEL", "grok-4.20-0309-non-reasoning"), RestartRequired: true},

				{Section: "Keys", Key: "XAI_API_KEY", Label: "xAI", Secret: true,
					Help: "Without this nothing runs.", Value: os.Getenv("XAI_API_KEY"), RestartRequired: true},
				{Section: "Keys", Key: "GITHUB_TOKEN", Label: "GitHub", Secret: true,
					Help: "Raises the scan rate limit from 60 requests an hour to 5000.", Value: os.Getenv("GITHUB_TOKEN")},
				{Section: "Keys", Key: daytona.EnvKey, Label: "Daytona", Secret: true,
					Help:  "Turns on coding tasks: your code runs against hidden tests, and the grade is the result rather than an opinion.",
					Value: os.Getenv(daytona.EnvKey)},
				{Section: "Keys", Key: exa.EnvKey, Label: "Exa", Secret: true,
					Help: "Lets the tutor find sources by meaning when it does not already have a URL.", Value: os.Getenv(exa.EnvKey)},
				{Section: "Keys", Key: firecrawl.EnvKey, Label: "Firecrawl", Secret: true,
					Help: "Reads a web page as clean markdown instead of raw HTML.", Value: os.Getenv(firecrawl.EnvKey)},
				{Section: "Keys", Key: fal.EnvKey, Label: "fal", Secret: true,
					Help: "Draws a mapping as a diagram. The key is the full id:secret pair.", Value: os.Getenv(fal.EnvKey)},
			}
		},
		Save: func(changed map[string]string) (string, error) {
			if err := saveDotEnv(".env", changed); err != nil {
				return "", err
			}
			var restart []string
			for k, v := range changed {
				os.Setenv(k, v)
				if strings.HasPrefix(k, "COGDEBT_") || k == "XAI_API_KEY" {
					restart = append(restart, k)
				}
			}

			n := reload()
			log.Info("settings saved", obs.FEvent, "settings.save", obs.FCount, len(changed), "plugins", n)

			msg := fmt.Sprintf("Saved. %d plugins active — new tools are available on your next message.", n)
			if len(restart) > 0 {
				sort.Strings(restart)
				msg += " " + strings.Join(restart, ", ") + " applies after a restart."
			}
			return msg, nil
		},
	}
}

// saveDotEnv merges values into the env file, preserving anything else in it.
// The file is the same one the app reads at startup, so there is one place a
// key can live and it is already gitignored.
func saveDotEnv(path string, changed map[string]string) error {
	existing, _ := os.ReadFile(path)
	lines := strings.Split(string(existing), "\n")

	seen := map[string]bool{}
	for i, line := range lines {
		key, _, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || strings.HasPrefix(key, "#") {
			continue
		}
		if v, want := changed[key]; want {
			lines[i] = key + "=" + v
			seen[key] = true
		}
	}
	keys := make([]string, 0, len(changed))
	for k := range changed {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, k+"="+changed[k])
	}

	out := strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	return os.WriteFile(path, []byte(out), 0o600)
}

// envOr is firstNonEmpty over an environment variable.
func envOr(key, def string) string { return firstNonEmpty(os.Getenv(key), def) }

// masteryPanel feeds the progress sidebar from the database after each turn.
func masteryPanel(db *store.Store, userID string) func(context.Context) []ext.MasteryItem {
	return func(ctx context.Context) []ext.MasteryItem {
		rows, err := db.Profile(ctx, userID)
		if err != nil {
			return nil
		}
		items := make([]ext.MasteryItem, 0, len(rows))
		for _, cm := range rows {
			debt := domain.Debt(cm.Mastery, 1)
			// A scan turns up plenty of incidental tags. Show only what the
			// learner has started on or is actually paying for; the rest is
			// noise in a panel that is meant to be read at a glance.
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
}

// analogyPanel reads stored analogies for the learner, newest first.
func analogyPanel(db *store.Store, userID string) func(context.Context) []ext.AnalogyRow {
	return func(ctx context.Context) []ext.AnalogyRow {
		saved, err := db.Mappings(ctx, userID)
		if err != nil {
			return nil
		}
		rows := make([]ext.AnalogyRow, 0, len(saved))
		for _, m := range saved {
			rows = append(rows, ext.AnalogyRow{
				Source:     m.Source.Name,
				Target:     m.Target.Name,
				SharedRole: string(m.SharedRole),
				CarryOver:  first(m.CarryOver),
				Breakdown:  first(m.Breakdown),
			})
		}
		return rows
	}
}

func first(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return ss[0]
}

// pluginSummary lists what loaded, shown in the window footer so the plugin
// layer is visible without opening a terminal.
func pluginSummary(reg *ext.Registry) []string {
	ms := reg.Manifests()
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, fmt.Sprintf("%s %s (%s)", m.Name, m.Version, m.Kind))
	}
	return out
}

// buildNamedModel builds a model an agent plugin asked for by name, against the
// same endpoint as the default.
func buildNamedModel(ctx context.Context, name string) (model.LLM, error) {
	key := firstNonEmpty(os.Getenv("XAI_API_KEY"), os.Getenv("OPENAI_API_KEY"))
	if key == "" {
		return nil, errors.New("no API key")
	}
	base := firstNonEmpty(os.Getenv("XAI_BASE_URL"), "https://api.x.ai/v1")
	return openaimodel.NewModel(ctx, name, &openaimodel.ClientConfig{APIKey: key, BaseURL: base})
}

// modelName is the model id, overridable from the environment.
func modelName() string {
	return firstNonEmpty(os.Getenv("COGDEBT_MODEL"), "grok-4.6")
}

// buildModel points the OpenAI-compatible adapter at xAI. Any OpenAI-shaped
// endpoint works the same way; only the two env vars change.
func buildModel(ctx context.Context) (model.LLM, error) {
	key := firstNonEmpty(os.Getenv("XAI_API_KEY"), os.Getenv("OPENAI_API_KEY"))
	if key == "" {
		return nil, errors.New("no API key: put XAI_API_KEY in .env (it is gitignored)")
	}
	name := modelName()
	base := firstNonEmpty(os.Getenv("XAI_BASE_URL"), "https://api.x.ai/v1")

	llm, err := openaimodel.NewModel(ctx, name, &openaimodel.ClientConfig{APIKey: key, BaseURL: base})
	if err != nil {
		return nil, fmt.Errorf("build model %q: %w", name, err)
	}
	return llm, nil
}

func printBanner(reg *ext.Registry, ts *ext.Toolset, transport map[string]string) {
	fmt.Println("cogdebt — learn by analogy")
	fmt.Println()
	for _, m := range reg.Manifests() {
		names := make([]string, 0, len(m.Provides))
		for _, p := range m.Provides {
			names = append(names, ext.QualifiedName(m.Name, p.Name))
		}
		where := transport[m.Name]
		if where == "" {
			where = "in-process"
		}
		fmt.Printf("  plugin %-10s %-9s %-12s %s\n", m.Name, m.Kind, where, strings.Join(names, " "))
	}
	if tools, err := ts.Tools(nil); err == nil {
		names := make([]string, 0, len(tools))
		for _, t := range tools {
			names = append(names, t.Name())
		}
		fmt.Printf("\n  exposed to the model: %s\n", strings.Join(names, " "))
	}
	fmt.Println("\nType what you already know. Ctrl-C to quit.")
	fmt.Println()
}

func repl(ctx context.Context, r *runner.Runner, userID string) error {
	const sessionID = "cli"
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1<<20)

	for {
		fmt.Print("> ")
		if !in.Scan() {
			fmt.Println()
			return in.Err()
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}

		msg := genai.NewContentFromText(line, genai.RoleUser)
		for ev, err := range r.Run(ctx, userID, sessionID, msg, agent.RunConfig{}) {
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				fmt.Printf("\n[error] %v\n", err)
				break
			}
			printEvent(ev)
		}
		fmt.Println()
	}
}

// printEvent shows assistant text and makes tool calls visible, which is the
// whole point of the CLI phase: you can see the plugins firing.
func printEvent(ev *session.Event) {
	if ev == nil || ev.Content == nil {
		return
	}
	for _, part := range ev.Content.Parts {
		if part.Thought {
			continue // model scratchpad, not the answer
		}
		switch {
		case part.Text != "":
			fmt.Print(part.Text)
		case part.FunctionCall != nil:
			fmt.Printf("\n  [tool] %s\n", part.FunctionCall.Name)
		}
	}
}

// loadDotEnv reads KEY=VALUE lines, without pulling in a dependency for it.
// Existing environment variables win.
func loadDotEnv(path string) {
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
