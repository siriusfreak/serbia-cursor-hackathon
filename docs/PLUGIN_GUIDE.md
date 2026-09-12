# Writing a plugin

You never touch the core. A plugin is a Go package under `exts/` implementing
three methods.

## In five minutes

**1. Copy a reference plugin**

```bash
cp -r exts/profile exts/mytool && rm exts/mytool/*_test.go
```

Two references, pick the closer one:

- [`exts/profile`](../exts/profile/profile.go) — holds host state, so it is
  in-process only
- [`exts/github`](../exts/github/github.go) — depends on nothing but HTTP, so it
  is portable across transports

**2. Describe yourself in `Manifest()`**

```go
func (e *Ext) Manifest() ext.Manifest {
	return ext.Manifest{
		Name:       "mytool",            // snake_case, globally unique
		Version:    "0.1.0",
		ABIVersion: ext.ABIVersion,      // always this, never a literal
		Kind:       ext.KindRetrieval,
		Provides: []ext.ToolSpec{{
			Name:        "search",
			Description: "Searches the docs. Call this when you need a fact from an outside source.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`),
			ReadOnly:    true,
		}},
	}
}
```

The model sees this tool as **`mytool_search`** — `<plugin>_<tool>`.

**3. Implement `Invoke()` — a switch over tool names**

```go
func (e *Ext) Invoke(ctx context.Context, tool string, in json.RawMessage) (json.RawMessage, error) {
	switch tool {
	case "search":
		var a struct{ Q string `json:"q"` }
		if err := ext.Args(in, &a); err != nil {
			return nil, err
		}
		if a.Q == "" {
			return nil, ext.Invalidf("q is empty; pass a query string")
		}
		return ext.JSON(map[string]any{"hits": hits})
	default:
		return nil, ext.Invalidf("mytool has no tool %q", tool)
	}
}
```

**4. `Close()`** — release what you own. Never close something you were handed,
such as a shared `*store.Store`.

**5. Register it in `cmd/cogdebt/main.go`**

```go
reg.MustLoad(
	profile.New(db, userID),
	analogy.New(),
	github.New(),
	mytool.New(),        // ← one line
)
```

Done. The model sees the new tool on its very next turn.

## Testing is mandatory and nearly free

```go
func TestConformance(t *testing.T) {
	exttest.Conformance(t, mytool.New())
}
```

`Conformance` runs exactly what the host runs at load time, plus the failure
modes that only show up in front of an audience: missing required fields, wrong
types, unknown tool names, double `Close`. For real calls use `exttest.Calls` —
see [`exts/profile/profile_test.go`](../exts/profile/profile_test.go).

```bash
go test ./exts/...
```

## Errors are `Fault`, not `error`

A failure has to survive a process boundary, so it is data:

```go
return nil, ext.Invalidf("skills is empty; pass at least one skill name")
return nil, ext.NotFoundf("the profile is empty, call profile_upsert first")
return nil, ext.Internalf("read database: %v", err)
```

**`Message` goes back to the LLM as the tool result.** Write it as an
instruction to the model — "call profile_upsert first", never "nil pointer
dereference". The model reads it and corrects itself on the next turn. A plain
`error` does not abort the run either, but it is logged loudly as breakage.

The host guarantees the bytes you receive are valid JSON. You still own the
schema: missing fields and wrong types arrive at your door, and `ext.Args` turns
both into a `Fault` for you.

## Four rules you cannot break

**1. Only bytes cross the boundary.** No pointers, channels or
`fyne.CanvasObject`. Break this and your plugin is in-process forever.

**2. `Manifest()` is pure, cheap and stable.** The host calls it every turn to
rebuild the tool list. No network, no database.

**3. A plugin never calls another plugin.** Need someone else's capability?
Declare `Requires: []ext.Kind{...}` and let the host resolve it. This is the rule
people break first, and after that "pluggable" is only true on paper.

**4. No unrecoverable state between calls.** A subprocess can be restarted at any
moment. State lives in `store`.

## Where to keep data

Do not add tables — take a namespaced key/value space:

```go
kv := db.KV("mytool")           // namespace is yours alone
kv.SetJSON(ctx, "cache", data)
ok, err := kv.GetJSON(ctx, "cache", &data)
```

If you truly need your own tables, `db.DB()` hands you the `*sql.DB`.

**Note:** a plugin that accepts a `*store.Store` becomes **in-process only**.
That is fine for plugins that *are* the host's storage, like `profile`. A plugin
meant to run in its own process keeps its own state — `github` is the model to
follow.

## Kinds

| Kind | Purpose |
|---|---|
| `profile` | skills and mastery |
| `retrieval` | search, GitHub, docs |
| `assessor` | questions and grading |
| `llm` | describes a model endpoint |
| `ui` | ViewSpec JSON rendered by the host in Fyne |
| `agent` | describes a sub-agent |

## Agent plugins

`llm` and `agent` are **declarative**: they do not implement logic, they describe
it. Exactly one tool, named `ext.DescribeTool`, returning a spec the host builds
from.

```go
func New() *Ext {
	return &Ext{spec: ext.AgentSpec{
		Description: "Builds the analogy. Delegate here once the profile has skills in it.",
		Instruction: "...system prompt...",
		ToolRefs:    []string{"profile_get"},
		SkipSummarization: true,
	}}
}
```

**A new agent is a new manifest and zero lines of logic.** Two strategies side by
side in [`exts/analogy`](../exts/analogy/analogy.go).

Two traps:

- **`SkipSummarization: false`** (the default) adds an **extra LLM call** per
  delegation — cost and latency double invisibly. Set it `true` for deterministic
  agents.
- **Cycles.** Agent A references agent B in `ToolRefs`, B references A. The host
  catches this and caps nesting (`MaxAgentDepth`, default 2), but do not build it.

## UI plugins

A UI plugin **returns no widgets** — it returns declarative ViewSpec JSON and the
host draws it. Otherwise the plugin would have to link against Fyne and would be
in-process forever.

```go
return ext.JSON(map[string]any{"view": ext.Stack(
    ext.Markdown("You know K8s. A pipeline is the same reconcile loop…"),
    ext.View(ext.ViewAnalogyTable, "", ext.AnalogyTableProps{Rows: []ext.AnalogyRow{{
        Source: "etcd", Target: "feature store", SharedRole: "source_of_truth",
        CarryOver: "Dual writes are split-brain.",
        Breakdown: "etcd never had point-in-time correctness.",
    }}}),
    ext.View(ext.ViewQuestion, "q1", ext.QuestionProps{Level: "L2", Prompt: "Where does the analogy break?"}),
)})
```

The vocabulary is closed and small: `stack · markdown · analogy_table · question ·
mastery`. An unknown `type` renders as a placeholder and the window survives — a
plugin newer than the host degrades rather than breaks.

Types live in [`internal/ext/viewspec.go`](../internal/ext/viewspec.go), rendering
in [`internal/ui/render.go`](../internal/ui/render.go).

**See it without spending a model call:**

```bash
go run ./cmd/cogdebt -screenshot /tmp/preview.png
```

## Running your plugin in its own process

If your plugin holds no host state — no `*store.Store`, nothing that cannot be
rebuilt — it can run as a separate binary with no changes to its code:

```go
// cmd/ext-mytool/main.go
func main() {
	subprocess.Serve(mytool.New())
}
```

```bash
go build -o plugins/ext-mytool ./cmd/ext-mytool
go run ./cmd/cogdebt -cli          # the banner now shows mytool as "subprocess"
```

The host scans `plugins/` at startup, and an out-of-process plugin takes
precedence over a built-in one of the same name. A binary that fails to start is
logged and skipped.

Two things to know:

- The handshake pins the protocol to `ext.ABIVersion`, so a plugin built against
  a different ABI fails before any of its code runs.
- **Rebuild the binary whenever the ABI changes.** A stale binary speaks the old
  protocol and will fail at call time.

## Schemas: one trap

`Schema` must be `{"type":"object", ...}` or the plugin is rejected at load. Inside
it, write ordinary lowercase JSON Schema (`"string"`, `"array"`). The host converts
to the genai form (`"STRING"`, `"ARRAY"`) itself, recursively. Never adjust the case
by hand.

## What happens if your plugin is broken

It **does not load**, the reason goes to the log, and every other plugin keeps
working. The host never dies — otherwise one bad plugin kills the demo.

To see what the host sees:

```bash
go run ./cmd/cogdebt -cli -debug
```

The banner prints every loaded plugin and the tools actually exposed to the model.

## The whole contract

One file: [`internal/ext/abi.go`](../internal/ext/abi.go). You need to read
nothing else.
