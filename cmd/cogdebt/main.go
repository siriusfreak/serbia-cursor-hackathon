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
	"strings"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/openaimodel"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"

	"github.com/sirius/cogdebt/exts/analogy"
	"github.com/sirius/cogdebt/exts/assessor"
	"github.com/sirius/cogdebt/exts/github"
	"github.com/sirius/cogdebt/exts/profile"
	"github.com/sirius/cogdebt/internal/domain"
	"github.com/sirius/cogdebt/internal/ext"
	"github.com/sirius/cogdebt/internal/ext/subprocess"
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
		shot    = flag.String("screenshot", "", "fill the window with sample content, save a PNG here and exit")
		debug   = flag.Bool("debug", false, "log plugin loading and tool calls")
	)
	flag.Parse()

	level := slog.LevelWarn
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if err := run(*dbPath, *plugDir, *user, *naive, *cli, *shot, log); err != nil {
		fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		os.Exit(1)
	}
}

func run(dbPath, pluginDir, userID string, naive, cli bool, shot string, log *slog.Logger) error {
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

	// Out-of-process plugins are discovered first and win over their built-in
	// twins, so dropping a binary into ./plugins swaps the transport with no
	// code change and no restart of anything else.
	out := subprocess.LoadDir(reg, pluginDir, log)
	for _, name := range out {
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
	if naive {
		reg.MustLoad(analogy.NewNaive())
	}

	toolset := ext.NewToolset(ext.ToolsetConfig{Registry: reg, Model: llm, Log: log})

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

	r, err := runner.New(runner.Config{
		AppName:           "cogdebt",
		Agent:             root,
		SessionService:    session.InMemoryService(),
		AutoCreateSession: true,
	})
	if err != nil {
		return fmt.Errorf("build runner: %w", err)
	}

	bridge := &ui.Bridge{Runner: r, UserID: userID, SessionID: "main"}

	if cli {
		printBanner(reg, toolset, transport)
		return repl(ctx, r, userID)
	}

	shell := ui.New(ctx, ui.Config{
		Model:   modelName(),
		Bridge:  bridge,
		Mastery: masteryPanel(db, userID),
		Plugins: pluginSummary(reg),
	})
	if shot != "" {
		shell.Seed()
		return shell.RunAndCapture(shot, 1200*time.Millisecond)
	}
	shell.Run()
	return nil
}

// masteryPanel feeds the progress sidebar from the database after each turn.
func masteryPanel(db *store.Store, userID string) func(context.Context) []ext.MasteryItem {
	return func(ctx context.Context) []ext.MasteryItem {
		rows, err := db.Profile(ctx, userID)
		if err != nil {
			return nil
		}
		items := make([]ext.MasteryItem, 0, len(rows))
		for _, cm := range rows {
			items = append(items, ext.MasteryItem{
				Label: cm.Concept.Name,
				Level: cm.Mastery.Level,
				Debt:  domain.Debt(cm.Mastery, 1),
			})
		}
		return items
	}
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
