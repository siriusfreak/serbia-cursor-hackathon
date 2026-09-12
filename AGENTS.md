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

COGDEBT_LIVE=1 go test ./e2e/ -v -timeout 30m   # every scenario, against the real model and services
COGDEBT_LIVE=1 go test ./e2e/ -run Physics -v   # one of them
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

## The design canvas is checked, not just drawn

`design/` holds one `.dc.html` per artboard plus `canvas.json`. The assembled
canvas is a build artifact and is gitignored; these files are what you edit.

`go test ./design/` reads the palette and type ramp **out of
`internal/ui/theme.go`** and fails on any colour or size in a mockup that the
app cannot render — so the design cannot drift from the code, and the code
cannot drift from the design, without the build saying so. It also asserts the
design's own intent: the prediction screen must not name the target it is
asking for, the reveal screen must state that a limit exists, and every ledger
entry must name the source a false belief was borrowed from.

Changing a theme colour and forgetting the mockups is now a test failure, which
is the point. To re-assemble after editing artboards, re-run the `/design`
skill's seeder over `design/*.dc.html` and republish.

## Keys, and plugins that are off without them

Settings (the header button) writes `.env` — the same file the app reads at
startup, gitignored, one place a key can live. Secrets are write-only in that
dialog: an existing key shows as "set" and is never rendered back, and a blank
field keeps the stored value rather than clearing it.

A plugin with no key is **not loaded at all**. Exposing a tool the model cannot
use costs a turn every time it tries. The gate is a `Configured() bool` method;
`loadPlugins` checks it. Saving a key calls `reload()`, which rebuilds the
registry in place and calls `Toolset.Invalidate()` — and because ADK asks the
Toolset for tools every turn, the new plugin is live on the next message with
nothing restarting. Only the model and the xAI key need a restart.

## Live tests: real services, off by default

Each plugin with an external API has a `live_test.go` guarded by
`COGDEBT_LIVE=1`. They are skipped otherwise, because `go test ./...` must not
boot a sandbox or spend anyone's quota.

```bash
COGDEBT_LIVE=1 go test ./exts/daytona/ -run Live -v
```

These caught what stubs cannot. The Daytona plugin passed its stub suite while
being unable to reach a real sandbox at all.

## End-to-end scenarios are about the loop, not about a plugin

`e2e/` runs the thing the README claims: a profile becomes an analogy, the
analogy becomes a rung of questions, and answering them moves mastery. No unit
test can show that, because it is a property of the loop rather than of any
component.

A scenario is a learner. `e2e.Learner` carries what they already hold, what they
came for, and — the load-bearing field — the **misconception** they are carrying
over from their own field. A simulated student plays that persona for the whole
conversation. Without a misconception the student answers everything correctly,
every grade comes back 1.0, and the run proves only that the happy path is
happy; `Learner.instruction()` is mostly negative instructions for exactly this
reason.

Four fields, four reasons to reach for a different plugin:

| scenario | learner brings | plugin under test | why this field |
|---|---|---|---|
| `algorithms` | Python, pandas, SQL windows | `daytona_run_task` | the answer can be executed, so the grade is not an opinion |
| `biology` | microservices, queues, retries | `exa_search`, `firecrawl_fetch` | the model's recall of a signalling cascade is not trustworthy; ground it |
| `physics` | rate limiting, backpressure | `fal_illustrate` | the mapping is a picture before it is a paragraph |
| `distributed-systems` | Go, Postgres, a GitHub login | `github_scan` | the repos say what they lean on, which self-report misses |

Every scenario asserts the same process invariants in `assertProcess`,
whatever the field: an analogy was recorded, no analogy shipped without a
breakdown, the assessor chose the rung (not the model), and something that was
asked was graded. `MustCall` is per-scenario and is the honest part — a biology
run that never fetched a source proved nothing about retrieval, so it fails.

Each run writes `e2e/out/<scenario>.md`: tools with counts, the rungs asked in
order, the analogies with their breakdowns, final mastery and debt, and the full
transcript. Read it. A green run says the loop held together; only the words say
whether the teaching was any good.

The student uses `COGDEBT_STUDENT_MODEL` (default the non-reasoning model). Do
not "improve" it to a reasoning model: it is playing a part, not solving the
problem, and a stronger model quietly stops being the beginner it was asked to
be.

### What the first runs found

Four defects, in the first two hours of the suite existing. Every one of them
compiled, passed the whole unit suite, and was invisible in a demo.

- **The analogy table was recorded twice.** The analogy agent saves its own
  pairs; the tutor then called `profile_save_analogy` with the same three. There
  was no uniqueness on `(user_id, source_id, target_id)`, so the learner read
  every mapping twice. The first fix was a unique index plus a prohibition in
  the root instruction, and the prohibition immediately made it worse: on the
  next run the agent did not record anything either and the table came back
  empty. The tutor's call is a safety net, not a duplicate. So the store now
  merges instead — a re-save updates the pairing, and any field the second
  writer leaves empty keeps what the first one said — and the instruction asks
  the tutor to save only if the agent did not.
- **The ladder rotated instead of climbing.** `assessor_next` picked the
  weakest concept, so grading one up made the next-weakest the winner. The
  student answered "entropy is just messiness" three times about three
  different concepts and was never once shown where that fails. `next` now
  stays on a concept scored below 0.5, for at most two retries, and returns
  `retry: true` with different guidance — make the miss concrete rather than
  reword the question. `exts/assessor/assessor_test.go` covers both halves:
  it sticks after a miss, and it moves on after a good answer.
- **The sandbox was promised and never run.** The learner asked for a coding
  task outright; the tutor said "after this card I'll give you code and run the
  tests" four turns running and never did, because `daytona_run_task` described
  itself as "use this for L4" and the learner was on L1. The description now
  says to run code whenever a belief can be settled that way, and always in the
  turn it is asked for; the root instruction says the same. A belief that can be
  executed should be.
- **Concept ids erased any language but English.** `SlugID` was
  `[^a-z0-9]+` -> `-`, so "динамическое программирование" slugged to the empty
  string and `profile_upsert` refused it: "none of the skill names contained
  usable characters". Every scenario that had run until then happened to use
  English concept names, which is why unit tests, the demo and three live runs
  all missed it. An id here only has to be stable and distinct — it is not a
  URL — so it now keeps letters and digits in any script.
- **A model wrote its reasoning into a tool argument.** The analogy agent filled
  `shared_role` with a thousand-word deliberation ending "So pairs: ...", which
  was stored and would have rendered on screen as the role. `profile_save_analogy`
  now caps the fields (120 characters for a name or a role, 800 for prose) and
  the fault names the offending field — faults come back as results, so the model
  simply writes a shorter one and carries on.

## Daytona's toolbox does not take the organization key

Worth writing down, because the published spec points the wrong way and the
detour cost an afternoon:

- `POST /sandbox` and the rest of the control plane take
  `Authorization: Bearer $DAYTONA_API_KEY`. That part is as documented.
- `GET /sandbox/{id}/toolbox-proxy-url` returns a **shared** proxy host that
  rejects that key. Its own error is the clue: it wants "a preview access
  token". Sending the org key there yields "Bearer token is invalid" — the
  proxy is trying to parse it as a JWT.
- The working path is `GET /sandbox/{id}/ports/2280/preview-url`, which returns
  a **per-sandbox** host AND a token. Call `POST {url}/process/code-run` on that
  host with `x-daytona-preview-token: {token}`.
- A freshly created sandbox is `creating`; its preview host does not answer
  until `started`, so poll first.

Port 2280 and the header were established by probing, not from documentation.
If code execution starts returning 401, re-probe before assuming the key is bad.

## The pitch deck is part of the product — update it with the code

`presentation/` is the hackathon deck, served by Render from `master`. It is not
a side artifact: it is the version of this project that other people actually
see, and a deck that has drifted from the code is worse than no deck, because it
is confidently wrong in front of an audience.

Ten slides, five minutes. The slide text is written in **Simple Technical
English** — short sentences, one idea per sentence, active voice, no idioms and
no metaphors. Keep new text in that register: the audience is international, and
a projector is a bad place for a long sentence.

**When you change any of the following, change the deck in the same commit:**

| you changed | update |
|---|---|
| the palette in `internal/ui/theme.go` | the `:root` block in `presentation/index.html` |
| the plugin set in `exts/` | the ABI table and the plugin count on the architecture slide |
| `ext.Extension` | the "3 methods" claim and the code block on the ABI slide |
| the debt formula or the L1–L4 ladder | the formula slide and the ladder slide |
| the UI, visibly | re-shoot `presentation/assets/app.png` with `-screenshot` |
| any slide | the matching `## N · ` section in `presentation/SPEECH.md` |
| a headline, a count or a date on a slide | the entry and the link in `presentation/SOURCES.md` |

`design/deck_test.go` enforces most of this and fails `go test ./...` when it
drifts: every colour in the deck must be in the app palette, the plugin table
must match `exts/` exactly in both directions, the ABI method count must match
`abi.go`, every slide must carry `<aside class="notes">`, and `SPEECH.md` must
have one numbered section per slide. It cannot check whether a sentence is still
*true* — that part is yours.

**Every factual claim on a slide needs a link in `presentation/SOURCES.md`.**
A headline on a slide is a claim made in front of judges, and someone will ask
for the proof. The test checks that each headline is cited, that the count the
slide states equals the number of rows in the file, and that the date range on
the slide covers the sources listed. It cannot check that a number is honestly
derived — state the method in `SOURCES.md`, including what the number is not.

Two rules the test cannot express. Numbers on slides are measured, never
estimated: the 120s→34s figures came out of the tracing, so if you re-measure,
change them. And the deck claims six of the ten partner technologies with a
concrete job each — if a plugin stops being used, move it to the "not used" line
rather than leaving the claim standing.

The deck is plain static HTML with no build step and no dependencies: open
`presentation/index.html` in a browser. Arrow keys navigate, `N` shows the
speaker notes, `F` is fullscreen, and printing gives a PDF backup for when the
projector loses the laptop.

## Releasing binaries

The interface uses Fyne, which needs cgo and OpenGL. A plain
`GOOS=linux go build` produces something that does not start, so cross-builds go
through containers.

```bash
# macOS, on a Mac
CGO_ENABLED=1 GOARCH=arm64 go build -ldflags '-s -w' -o cogdebt-darwin-arm64 ./cmd/cogdebt
CGO_ENABLED=1 GOARCH=amd64 go build -ldflags '-s -w' -o cogdebt-darwin-amd64 ./cmd/cogdebt
lipo -create -output cogdebt-macos-universal cogdebt-darwin-arm64 cogdebt-darwin-amd64

# Linux and Windows, needs Docker running
go install github.com/fyne-io/fyne-cross@latest
fyne-cross linux   -arch=amd64,arm64,386 -env GOTOOLCHAIN=auto -app-id=dev.cogdebt.app -name=cogdebt ./cmd/cogdebt
fyne-cross windows -arch=amd64,arm64,386 -env GOTOOLCHAIN=auto -app-id=dev.cogdebt.app -name=cogdebt ./cmd/cogdebt
```

`-env GOTOOLCHAIN=auto` is required, not optional. The fyne-cross images ship an
older Go with `GOTOOLCHAIN=local`, so without it every target fails with
"requires go >= 1.27.1" before it compiles a line.

Two things fyne-cross leaves in the working tree: `cmd/cogdebt/fyne_metadata_init.go`,
which embeds a placeholder Fyne logo and must never be committed, and a stray
`tmp-pkg/` and `cogdebt.tar.xz`. All of them are gitignored — check `git status`
after a build run anyway.

Upload with `gh release upload <tag> --clobber`, and regenerate `SHA256SUMS.txt`
over the whole set rather than appending to it. Nothing is code-signed, so the
release notes have to tell people how to get past Gatekeeper and SmartScreen;
a download that silently refuses to open is the same as no download.

## Layout

```
cmd/cogdebt/      entry point: flags, logging, the shell, the CLI REPL
internal/app/     the ONE construction path — store, model, plugins, agent,
                  runner, and the root instruction. Shell, REPL and scenarios
                  all build the same object graph from here.
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
                  analogy and review (declarative agents), github and vcs
                  (portable, HTTP), oracle (claim vs tests)
cmd/ext-github/   github as a standalone plugin process
e2e/              scenarios: a simulated learner works through a field
                  against the real model and the real plugins
presentation/     the hackathon deck (static HTML, no build) + SPEECH.md;
                  deployed to Render from master, checked by design/deck_test.go
docs/             PLUGIN_GUIDE.md — written for plugin authors
```

## Making changes

- **New plugin**: follow `docs/PLUGIN_GUIDE.md`. Copy `exts/profile`. Add
  `exttest.Conformance` — it enforces what the host enforces.
- **New `ViewSpec` type**: add the constant and props struct to
  `internal/ext/viewspec.go`, a case to `ui.Render`, and seed it in
  `ui/demo.go` so `-screenshot` covers it. If it needs bytes from somewhere,
  the host fetches them — see `image`, where the plugin returns only a URL.
- **Changing `ext.Extension`, `Manifest` or `ToolSpec` incompatibly**: bump
  `ext.ABIVersion` and say so in `docs/PLUGIN_GUIDE.md`. Old plugins then fail
  to load with a clear message instead of misbehaving.
- **Prompt changes**: the root instruction is `app.RootInstruction` in
  `internal/app/app.go`; the
  analogy strategy is `structureMappingInstruction` in `exts/analogy`; the
  review strategy is `instruction` in `exts/review`. All three must keep the
  "reply in the learner's language" rule.
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
  (`assessor_ask`, `profile_save_analogy`, `oracle_check`) onto ViewSpecs. Adding a new card
  means adding a case there, not asking the model to emit JSON in its text.
- **The producer records.** The analogy agent calls `profile_save_analogy`
  itself rather than leaving it to its caller. A caller asked to record someone
  else's output skips it whenever the reply is long, and the table never reaches
  the screen.
- **`appendToolResult` also knows `fal_illustrate`.** The plugin returns a URL
  and nothing else; the host downloads it. Bytes do not cross the ABI — see the
  note on `ext.ImageProps`.

## Not built yet

- **A cheap diagram plugin.** `fal` generates a picture per call, which costs
  money and garbles its own labels — the caption under the image exists to
  compensate. A Graphviz-backed plugin would render the same `AnalogyRow` set
  deterministically, for free, with labels that are correct by construction. It
  would emit the same `image` ViewSpec, so nothing in the UI changes; only the
  source of the bytes does (a local `dot` writes a file, so the node would need
  a `file://` or data URL path, or the plugin returns SVG and the host renders
  it). Keep `fal` for the cases where an evocative picture beats a correct box
  diagram.
- **The extraction loop.** `design/` has the screens — predict before reveal,
  contrast against what the learner actually said, the misconception ledger —
  and they are designed and checked but not implemented.
