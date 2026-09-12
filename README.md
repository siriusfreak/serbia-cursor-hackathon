# cogdebt

Teaches you a new field through one you already hold: it builds the analogy from
structural roles, **marks explicitly where that analogy breaks**, and walks you up
a ladder of questions from borrowed intuition to standing on your own.

Plugin architecture in Go, desktop shell in Fyne, agents on Google ADK.

## Running it

```bash
echo 'XAI_API_KEY=<key>' > .env   # .env is gitignored
go run ./cmd/cogdebt
```

| Flag | What it does |
|---|---|
| `-cli` | terminal REPL instead of the window — every tool call is printed |
| `-screenshot out.png` | fills the window with sample content, saves a PNG, exits |
| `-naive` | loads a second analogy strategy alongside the real one |
| `-plugins DIR` | directory scanned for out-of-process plugin binaries (default `plugins`) |
| `-debug` | logs plugin loading and tool calls |

Needs Go 1.27+ (`GOTOOLCHAIN=auto` fetches it) and Xcode CLT for cgo — only the
Fyne binary requires it.

## The idea

**Cognitive debt** is a quantity, not a metaphor:

```
debt(c) = frequency_in_real_work(c) × prereq_depth(c) × (1 − mastery(c))
```

What you lean on often, that much else rests on, and that you do not hold.

**Analogies are matched on structural role, not on names.** Not "both have a
controller", but "both are the single source of truth everything else reconciles
against". That is what produces `etcd ↔ feature store` and
`liveness probe ↔ drift detection`.

**The `breakdown` field is mandatory.** An analogy with no stated limit does not
pay off cognitive debt — it creates more, because the learner carries the
borrowed intuition past the point where it holds. This is the one invariant the
domain layer enforces.

## Architecture

```
Fyne (window, ViewSpec renderer, fyne.Do bridge)
        ↓
ADK (Runner + agents: profiler, analogy, tutor, assessor)
        ↓
ext.Registry implements tool.Toolset     ← the only seam
        ↓
transports: in-process · go-plugin gRPC · WASM
        ↓
domain/ — pure core, imports no ADK, no Fyne, no ext
```

The entire plugin contract is one file: [`internal/ext/abi.go`](internal/ext/abi.go).
Three methods, everything serializable.

The load-bearing detail: `ext.Registry` implements `tool.Toolset`, and ADK asks
for the tool list **on every turn**. So plugins appear and disappear with no
restart, and the root agent never learns that plugins exist at all.

## Layout

```
cmd/cogdebt/      entry point and wiring
internal/ext/     ABI, registry, ADK adapter, ViewSpec
internal/domain/  concepts, mastery, analogies, debt — no external deps
internal/store/   SQLite (modernc, cgo-free) + namespaced KV for plugins
internal/ui/      theme, components, renderer, bridge to the runner
exts/             plugins: profile, assessor, analogy (agent), github, exa,
                  firecrawl, fal, daytona
cmd/ext-github/   the same github plugin, as a standalone process
docs/             PLUGIN_GUIDE.md
design/           design canvas artboards, checked against the app theme
```

## Keys

Everything except xAI is optional, and a plugin with no key is simply not
loaded. Open **Settings** in the header to add one; it writes the gitignored
`.env`, and the plugin is live on your next message without a restart.

| Key | What it turns on |
|---|---|
| `XAI_API_KEY` | required — nothing runs without it |
| `DAYTONA_API_KEY` | coding tasks: your code runs against hidden tests in a sandbox |
| `EXA_API_KEY` | finding sources by meaning rather than keyword |
| `FIRECRAWL_API_KEY` | reading a page as markdown instead of raw HTML |
| `GITHUB_TOKEN` | raises the repository scan limit from 60/hour to 5000 |
| `FAL_KEY` | drawing a mapping as a diagram (`id:secret`) |

## Coding tasks are the one grade that is not an opinion

With Daytona configured, the tutor can set a small problem, run your solution
against tests it wrote, and grade on the result. A model scoring prose drifts
toward whatever it just explained; an assertion does not. And a failing test
names the belief that failed:

```
passed=1 failed=1 in 2252ms
  FAIL is point-in-time correct — AssertionError: used a value from the future
```

That line is a misconception, already worded for the ledger.

## Design

`design/` holds the screens for the learning loop — predict the mapping before
it is revealed, contrast against what you actually said, and track the false
beliefs an analogy lends you.

```bash
go test ./design/
```

That test reads the palette and type ramp straight out of
[`internal/ui/theme.go`](internal/ui/theme.go), so a mockup using a colour the
app cannot render is a build failure — and so is a screen that gives away the
answer it is supposed to be asking for.

## Running a plugin in its own process

```bash
go build -o plugins/ext-github ./cmd/ext-github
go run ./cmd/cogdebt -cli            # github now reports "subprocess"
rm plugins/ext-github
go run ./cmd/cogdebt -cli            # falls back to "in-process"
```

The plugin source is identical in both cases. Because `Extension` moves only
bytes, the transport is a deployment decision, not a code change — the banner
prints which one each plugin is using.

## Adding a plugin

See [docs/PLUGIN_GUIDE.md](docs/PLUGIN_GUIDE.md) — five minutes, and you never
touch the core.

Check your plugin:

```bash
go test ./exts/...
```

`exttest.Conformance` runs exactly what the host runs at load time.

## Status

| | | |
|---|---|---|
| 0 | skeleton, Grok over an OpenAI-compatible endpoint | ✅ |
| 1 | ABI, registry, adapter, `profile` and `analogy` plugins | ✅ |
| 2a | Fyne window, theme, bridge, ViewSpec renderer | ✅ |
| 2b | L1–L4 ladder on structured output instead of prose | ✅ |
| 3 | plugin in its own process (hashicorp/go-plugin) | ✅ |
| 4 | GitHub scanner → frequency → live cognitive debt | ✅ |

Default model is `grok-4.6`; override with `COGDEBT_MODEL`. Any
OpenAI-compatible endpoint works through `XAI_BASE_URL`.

Model choice is per-agent: the analogy agent runs a non-reasoning model
(`COGDEBT_ANALOGY_MODEL`, default `grok-4.20-0309-non-reasoning`) because it
follows a procedure that is spelled out for it, and reasoning there cost 84s of
a 120s turn for no gain in quality. Structured logs are how that was found:

```bash
go run ./cmd/cogdebt -cli -log-format json -log-file /tmp/run.jsonl
```

Every seam is timed — plugin loads, tool calls, model calls with token counts,
subprocess round trips, whole turns. Sort by `ms` and read the top.
