# AGENTS.md

Instructions for AI agents working in this repository.

`cogdebt` teaches an engineer a new field by mapping it onto one they already
hold, and marking where that map breaks. Go, plugin ABI, Fyne desktop, agents on
Google ADK, embedded SQLite.

Audience split, keep it: **code comments in English**, **`README.md` and
`docs/` in Russian**, this file in English.

## Commands

```bash
go build ./...                                  # `ld: warning: -lobjc` is benign on macOS
go vet ./...
go test ./...
go run ./cmd/cogdebt                            # desktop window
go run ./cmd/cogdebt -cli -debug                # REPL; every tool call is printed
go run ./cmd/cogdebt -screenshot /tmp/p.png     # sample content -> PNG -> exit, no model call
```

Go 1.27+ via `GOTOOLCHAIN=auto`. A model key lives in `.env` (gitignored):
`XAI_API_KEY`, optionally `XAI_BASE_URL` and `COGDEBT_MODEL` (default
`grok-4.6`). Any OpenAI-compatible endpoint works — `openaimodel.NewModel` with
a `BaseURL`.

Use `-screenshot` to check any visual change. Do not ask the user to look at the
window when you can capture it yourself.

## Invariants — do not break these

1. **`internal/domain/` imports nothing from this project.** No ADK, no Fyne, no
   `ext`. It must stay testable with no key, no network, no GUI.
2. **Only bytes cross the ABI.** `Extension.Invoke` takes and returns
   `json.RawMessage`. No pointers, channels, `*sql.DB` or `fyne.CanvasObject` in
   a signature that a plugin implements. Break this and out-of-process transport
   becomes impossible, which is the entire point of the design.
3. **A plugin never calls another plugin.** It declares `Requires`; the host
   resolves. This keeps the call graph flat.
4. **`Mapping.Breakdown` is never empty.** An analogy without a stated limit
   creates cognitive debt instead of paying it off. This is the product, not a
   detail — `exts/analogy` has a test asserting the instruction still says so.
5. **A bad plugin never takes the host down.** Validation rejects and logs;
   `MustLoad` continues. An unknown `ViewSpec` type renders a placeholder.
6. **Touch widgets only on the UI goroutine.** Everything goes through
   `ui.Bridge`, which wraps callbacks in `fyne.Do`. Do not call widget methods
   from a goroutine you spawned.

## Traps already paid for — do not rediscover these

- **`genai.Schema.Type` is uppercase** (`"OBJECT"`), JSON Schema is lowercase
  (`"object"`). A direct unmarshal produces a schema that marshals fine and is
  silently wrong at the API. `ext.SchemaFromJSON` normalizes recursively; there
  is a test. Never hand-build a `genai.Schema` from plugin JSON without it.
- **ADK tools must implement `ProcessRequest(agent.Context, *model.LLMRequest)`**
  (`toolutils.PackTool`), on top of the unexported `runnableTool`. Missing it
  fails at *call* time, not build time. `internal/ext/adk.go` has a structural
  compile-time assertion; keep it.
- **Reasoning models stream their scratchpad as parts with `Thought: true`.**
  Skip those parts or the learner reads the model's internal monologue. No
  prompt fixes this. Filtered in `ui/bridge.go` and `cmd/cogdebt` `printEvent`.
- **`fyne.TextWrapWord` collapses a widget's `MinSize` to its widest word.** A
  wrapped multi-word value in a `container.Border` trailing slot gets clipped.
  Use `ui.tag()` for short trailing text.
- **`agenttool` summarizes by default**, costing an extra LLM call per
  delegation. Set `SkipSummarization: true` for deterministic agents.
- **SQLite runs with `SetMaxOpenConns(1)`** on purpose — concurrent tool calls
  otherwise hit "database is locked". Do not raise it.
- **`go mod tidy` on a cold cache takes many minutes.** `GOFLAGS=-mod=mod go
  build ./...` resolves only what is imported and is far faster while iterating.

## Layout

```
cmd/cogdebt/      entry point, wiring, CLI REPL
internal/ext/     THE CONTRACT. abi.go is the whole plugin ABI.
                  registry.go validates and holds; adk.go adapts to ADK;
                  viewspec.go and agentspec.go are declarative payloads;
                  exttest/ is the conformance harness plugin authors run.
internal/domain/  concepts, roles, mastery, analogy, ladder, debt — pure
internal/store/   SQLite (modernc, cgo-free) + per-plugin namespaced KV
internal/ui/      theme.go, components.go (visual vocabulary), render.go
                  (ViewSpec -> Fyne), bridge.go (the one fyne.Do seam),
                  shell.go, demo.go (seeding + screenshot)
exts/             plugins: profile (data), analogy (declarative agent)
docs/             PLUGIN_GUIDE.md — written for the teammate, in Russian
```

## Making changes

- **New plugin**: follow `docs/PLUGIN_GUIDE.md`. Copy `exts/profile`. Add
  `exttest.Conformance` — it enforces what the host enforces.
- **New `ViewSpec` type**: add the constant and props struct to
  `internal/ext/viewspec.go`, a case to `ui.Render`, and seed it in
  `ui/demo.go` so `-screenshot` covers it.
- **Changing `ext.Extension`, `Manifest` or `ToolSpec` incompatibly**: bump
  `ext.ABIVersion` and say so in `docs/PLUGIN_GUIDE.md`. Old plugins then fail
  to load with a clear message instead of misbehaving.
- **Prompt changes**: the root instruction is in `cmd/cogdebt/main.go`; the
  analogy strategy is `structureMappingInstruction` in `exts/analogy`. Both
  must keep the "reply in the learner's language" rule.
- **Verify before reporting.** Build, vet, test, and for anything user-visible
  run `-cli` against the real model or `-screenshot` for the UI. This project
  has repeatedly had defects that compile and test clean and only appear in a
  live run.

## Status

Phases 0, 1 and 2a are done. Not yet built: the L1–L4 ladder on structured
output (2b), out-of-process plugins via hashicorp/go-plugin (3), and the GitHub
scanner that populates `Mastery.Frequency` (4). Until 4 lands, `domain.Debt`
computes against `frequency = 0`, so the sidebar's debt figures are real only
for the seeded demo data.
