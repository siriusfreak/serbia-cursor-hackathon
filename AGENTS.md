# AGENTS.md

Instructions for AI agents working in this repository.

`cogdebt` teaches an engineer a new field by mapping it onto one they already
hold, and marking where that map breaks. Go, plugin ABI, Fyne desktop, agents on
Google ADK, embedded SQLite.

Everything in this repository is written in English: code, comments, `README.md`,
`docs/` and this file. Keep it that way.

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
- **`net/rpc` silently refuses methods with unexported argument types.** The
  service still registers and answers calls that use builtin types, so a plugin
  loads, reports its manifest, and fails on every real invocation. This is why
  `subprocess.InvokeArgs` and `InvokeReply` are exported. A smoke test will not
  catch it; `internal/ext/subprocess` has tests that do.
- **Rebuild plugin binaries after changing wire types.** A stale binary in
  `plugins/` speaks the old protocol.
- **`go mod tidy` on a cold cache takes many minutes.** `GOFLAGS=-mod=mod go
  build ./...` resolves only what is imported and is far faster while iterating.

## Logging is structured, and every seam is timed

This pipeline puts an LLM, a subprocess and a GUI in one call path. When a run
misbehaves the only useful question is *which seam, and how long did it sit
there* — and a GUI leaves no scrollback to answer it from afterwards. So:

- **Every log record is structured.** No formatted prose. Use the field and
  event constants in `internal/obs`, never bare strings, so a query written once
  keeps working.
- **Every seam is timed.** `obs.Start()` at the boundary, `timer.Attr()` in the
  record. Durations are milliseconds to one decimal — whole milliseconds hide
  the difference between a 0.2ms in-process call and a 0.9ms one, which is
  exactly the comparison the transport work exists to make.
- **Add new seams to `internal/obs` first.** A new `Event*` constant, then the
  call site. If you find yourself writing `log.Info("did the thing")` with no
  event and no duration, that record will not help anyone at 3am.

What is already instrumented: plugin load and rejection, every tool invocation
(both at the ADK boundary and inside `extTool`, so framework overhead is
separable from plugin cost), every model call with token counts and streamed
chunk counts, agent runs, subprocess spawn/call/exit, and whole UI turns.

```bash
go run ./cmd/cogdebt -cli -log-format json -log-file /tmp/run.jsonl -log-level debug
```

Then sort by `ms` and read the top of the list. That is how the analogy agent
was found to be 89% of a turn.

Two traps in the callbacks themselves, both already handled — do not reintroduce
them:

- **`AfterModelCallback` fires once per streamed chunk, not once per call.**
  Closing the span on the first one emits hundreds of 0ms records and buries the
  real measurement. Check `resp.Partial` and only close on the final response.
- **Runner-level lifecycle plugins do not see inside an `agenttool` sub-agent.**
  Its model calls are invisible from the runner, which left the largest span in
  the system unexplained. `obs.AgentCallbacks` is attached when the sub-agent is
  built, in `Toolset.agentTool`.

## Model choice is per-agent, and it is the biggest lever you have

`AgentSpec.Model` picks a model per agent plugin; `Toolset.ModelFor` builds it.
Use it. Measured on this workload, tracing a single turn:

| | before | after |
|---|---|---|
| whole turn | 120s | 34s |
| the analogy sub-agent | 84s | 7.4s |

The analogy agent follows a procedure that is written out for it in full, so
reasoning tokens buy nothing and cost almost the entire turn — a reply of 4069
tokens at 74 seconds, against an instruction asking for three short pairs. A
non-reasoning model produced cleaner structure in a tenth of the time. An agent
that *decides* what to do next is the opposite case and should keep a reasoning
model; the root agent still runs on `COGDEBT_MODEL`.

`MaxOutputTokens` on the spec is the other half. Prose about length is advice a
model may ignore.

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
internal/ext/subprocess/  out-of-process transport (go-plugin over net/rpc)
exts/             plugins: profile and assessor (host state, in-process),
                  analogy (declarative agent), github (portable, HTTP only)
cmd/ext-github/   github as a standalone plugin process
docs/             PLUGIN_GUIDE.md — written for plugin authors
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

All planned phases are in: plugin ABI and registry, the ADK adapter, the Fyne
shell, the L1-L4 ladder on structured tool results, out-of-process plugins, and
the GitHub scanner that makes cognitive debt real rather than guessed.

Two things about the ladder worth knowing before changing it:

- **The UI draws cards from tool results, not from prose.** `ui.Bridge` surfaces
  `FunctionResponse` parts, and `Shell.appendToolResult` maps the ones it knows
  (`assessor_ask`, `profile_save_analogy`) onto ViewSpecs. Adding a new card
  means adding a case there, not asking the model to emit JSON in its text.
- **The producer records.** The analogy agent calls `profile_save_analogy`
  itself rather than leaving it to its caller. A caller asked to record someone
  else's output skips it whenever the reply is long, and the table never reaches
  the screen.
