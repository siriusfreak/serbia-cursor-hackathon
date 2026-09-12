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
exts/             plugins: profile, assessor, analogy (agent), github (retrieval)
cmd/ext-github/   the same github plugin, as a standalone process
docs/             PLUGIN_GUIDE.md
```

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
