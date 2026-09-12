// Command cogdebt runs the learning agent.
//
// Everything the program is made of is assembled in internal/app; this file is
// only the entry point: flags, logging, the desktop shell and the terminal
// REPL. Keeping construction out of main is what lets the end-to-end scenarios
// exercise the same object graph the learner runs.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"github.com/sirius/cogdebt/exts/daytona"
	"github.com/sirius/cogdebt/exts/exa"
	"github.com/sirius/cogdebt/exts/fal"
	"github.com/sirius/cogdebt/exts/firecrawl"
	"github.com/sirius/cogdebt/internal/app"
	"github.com/sirius/cogdebt/internal/ext"
	"github.com/sirius/cogdebt/internal/obs"
	"github.com/sirius/cogdebt/internal/ui"
)

func main() {
	var (
		dbPath  = flag.String("db", "cogdebt.db", "SQLite database path")
		plugDir = flag.String("plugins", "plugins", "directory scanned for out-of-process plugin binaries")
		user    = flag.String("user", "local", "learner id")
		naive   = flag.Bool("naive", false, "also load the naive analogy plugin, for comparison")
		cli     = flag.Bool("cli", false, "terminal REPL instead of the desktop window")
		demo    = flag.Bool("demo", false, "play the scripted happy path: no model, no network, same result every time")
		speed   = flag.Float64("demo-speed", 1, "playback speed for -demo; 2 is twice as fast")
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

	if *demo {
		// Deliberately before anything that needs a key or a network: the whole
		// point is a run that cannot fail for a reason outside this binary.
		runDemo(*speed, *shot, *after)
		return
	}

	if err = run(*dbPath, *plugDir, *user, *naive, *cli, *shot, *say, *openCfg, *after, log); err != nil {
		fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		os.Exit(1)
	}
}

func run(dbPath, pluginDir, userID string, naive, cli bool, shot, say string, openCfg bool, after time.Duration, log *slog.Logger) error {
	app.LoadDotEnv(".env")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	a, err := app.New(ctx, app.Config{
		DBPath:    dbPath,
		PluginDir: pluginDir,
		UserID:    userID,
		Naive:     naive,
		Log:       log,
	})
	if err != nil {
		return err
	}
	defer a.Close()

	if cli {
		printBanner(a)
		return repl(ctx, a.Runner, userID)
	}

	shell := ui.New(ctx, ui.Config{
		Model:     a.Model,
		Settings:  settingsConfig(a.Reload, log),
		Bridge:    &ui.Bridge{Runner: a.Runner, UserID: userID, SessionID: "main", Log: log},
		Mastery:   a.Mastery,
		Analogies: a.Analogies,
		Plugins:   a.PluginSummary(),
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

// runDemo plays the scripted happy path.
//
// A live demo depends on a model, a network and four external services, and a
// stage is the worst place to find out one of them is having a bad minute. Every
// card here is drawn by the same renderer the live app uses, so the room sees
// the product rather than a video of it -- only the content is fixed.
func runDemo(speed float64, shot string, after time.Duration) {
	shell := ui.New(context.Background(), ui.Config{
		Model:    "scripted",
		Scripted: true,
	})
	shell.Play(ui.HappyPath(), speed)
	if shot != "" {
		if err := shell.RunAndCapture(shot, after); err != nil {
			fmt.Fprintf(os.Stderr, "screenshot: %v\n", err)
		}
		return
	}
	shell.Run()
}

// settingsConfig describes what is configurable and what saving does.
func settingsConfig(reload func() int, log *slog.Logger) ui.SettingsConfig {
	return ui.SettingsConfig{
		Fields: func() []ui.SettingField {
			return []ui.SettingField{
				{Section: "Model", Key: "COGDEBT_MODEL", Label: "Main model",
					Help:  "Runs the conversation and decides which tools to call.",
					Value: app.ModelName(), RestartRequired: true},
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

// envOr is FirstNonEmpty over an environment variable.
func envOr(key, def string) string { return app.FirstNonEmpty(os.Getenv(key), def) }

func printBanner(a *app.App) {
	fmt.Println("cogdebt — learn by analogy")
	fmt.Println()
	for _, m := range a.Registry.Manifests() {
		names := make([]string, 0, len(m.Provides))
		for _, p := range m.Provides {
			names = append(names, ext.QualifiedName(m.Name, p.Name))
		}
		where := a.Transport[m.Name]
		if where == "" {
			where = "in-process"
		}
		fmt.Printf("  plugin %-10s %-9s %-12s %s\n", m.Name, m.Kind, where, strings.Join(names, " "))
	}
	fmt.Printf("\n  exposed to the model: %s\n", strings.Join(a.ToolNames(), " "))
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
