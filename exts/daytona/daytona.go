// Package daytona runs a learner's code in a disposable sandbox and reports
// which tests passed.
//
// This is the only task type in the system whose grade is not an opinion. A
// model scoring prose drifts toward whatever it just explained; a failing
// assertion does not. And a named failing test is a named misconception —
// "you assumed a rollback restores the data distribution" — which is exactly
// the shape the misconception ledger wants.
//
// Reaching the sandbox took observation, not documentation. The control plane
// at /sandbox creates it, but the /sandbox/{id}/toolbox-proxy-url endpoint
// returns a SHARED proxy host that rejects an organization API key: its own
// error names what it wants, "a preview access token". That token comes from
// /sandbox/{id}/ports/{port}/preview-url, which also returns a PER-SANDBOX
// host — and the toolbox answers there, on port 2280, with the token in an
// x-daytona-preview-token header. Verified end to end against the real
// service: print(6*7) returns {"exitCode":0,"result":"42\n"}.
package daytona

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sirius/cogdebt/internal/ext"
)

// EnvKey is where the API key is read from.
const EnvKey = "DAYTONA_API_KEY"

// marker prefixes the one line of machine-readable output the harness prints,
// so ordinary program output cannot be mistaken for results.
const marker = "__COGDEBT_RESULTS__"

// toolboxPort is where the sandbox's toolbox listens. Observed, not published.
const toolboxPort = 2280

// Ext runs code tasks.
type Ext struct {
	client  *http.Client
	baseURL string
	token   string

	// mu guards the cached sandbox. Creating one per task would pay the boot
	// cost on every question; the id is recoverable, so caching it does not
	// violate the ABI's no-unrecoverable-state rule.
	mu        sync.Mutex
	sandboxID string
	toolbox   string // per-sandbox preview host
	preview   string // preview access token for that host
}

// New returns the plugin. It does not contact Daytona until a task runs.
func New() *Ext {
	return &Ext{
		client:  &http.Client{Timeout: 90 * time.Second},
		baseURL: envOr("DAYTONA_BASE_URL", "https://app.daytona.io/api"),
		token:   os.Getenv(EnvKey),
	}
}

// Configured reports whether a key is present. The host skips loading a plugin
// that cannot work, rather than exposing a tool the model will keep failing.
func (e *Ext) Configured() bool { return e.token != "" }

func (e *Ext) Manifest() ext.Manifest {
	return ext.Manifest{
		Name:       "daytona",
		Version:    "0.1.0",
		ABIVersion: ext.ABIVersion,
		Kind:       ext.KindAssessor,
		Provides: []ext.ToolSpec{{
			Name: "run_task",
			Description: "Runs the learner's code against your tests in a disposable sandbox and reports which " +
				"tests passed. Reach for it whenever a belief can be settled by running something, and ALWAYS in " +
				"the same turn the learner asks for a coding task -- whatever rung they are on. Give them a small " +
				"problem in the target domain and write tests that fail specifically when a borrowed intuition is " +
				"wrong. Each failing test names a misconception, worded better than you could word it.",
			Schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"language": {"type": "string", "enum": ["python", "javascript", "typescript"]},
					"code":     {"type": "string", "description": "The learner's solution, verbatim"},
					"tests": {
						"type": "array",
						"description": "Each test runs against the learner's code. Name it after the belief it checks.",
						"items": {
							"type": "object",
							"properties": {
								"name":   {"type": "string", "description": "e.g. \"rollback does not restore training data\""},
								"source": {"type": "string", "description": "Statements that raise on failure, e.g. an assert"}
							},
							"required": ["name", "source"]
						}
					},
					"timeout": {"type": "integer", "description": "Seconds, default 30"}
				},
				"required": ["language", "code", "tests"]
			}`),
		}},
	}
}

type runArgs struct {
	Language string `json:"language"`
	Code     string `json:"code"`
	Tests    []struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	} `json:"tests"`
	Timeout int `json:"timeout"`
}

type testResult struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	Err  string `json:"err,omitempty"`
}

func (e *Ext) Invoke(ctx context.Context, tool string, in json.RawMessage) (json.RawMessage, error) {
	if tool != "run_task" {
		return nil, ext.Invalidf("daytona has no tool %q; it provides run_task", tool)
	}
	if e.token == "" {
		return nil, &ext.Fault{Code: ext.FaultDenied,
			Message: "Daytona is not configured. Tell the learner to add a Daytona API key in Settings, and grade this answer by reading it instead."}
	}

	var a runArgs
	if err := ext.Args(in, &a); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Code) == "" {
		return nil, ext.Invalidf("code is empty; pass the learner's solution")
	}
	if len(a.Tests) == 0 {
		return nil, ext.Invalidf("tests is empty; a task with no tests cannot be graded objectively, which is the only reason to run it")
	}
	if a.Language == "" {
		a.Language = "python"
	}
	if a.Timeout <= 0 || a.Timeout > 120 {
		a.Timeout = 30
	}

	harness, err := buildHarness(a)
	if err != nil {
		return nil, err
	}

	started := time.Now()
	out, exit, err := e.codeRun(ctx, a.Language, harness, a.Timeout)
	if err != nil {
		return nil, err
	}

	results, stdout := parseResults(out)
	if results == nil {
		// The harness never reported, so the code died before the tests ran.
		return ext.JSON(map[string]any{
			"passed":    0,
			"failed":    len(a.Tests),
			"exit_code": exit,
			"output":    tail(stdout, 2000),
			"note":      "The submission did not run to the point of being tested. Treat the error above as the finding.",
			"ms":        time.Since(started).Milliseconds(),
		})
	}

	var passed int
	var failures []testResult
	for _, r := range results {
		if r.OK {
			passed++
			continue
		}
		failures = append(failures, r)
	}
	return ext.JSON(map[string]any{
		"passed":    passed,
		"failed":    len(failures),
		"failures":  failures,
		"exit_code": exit,
		"output":    tail(stdout, 1200),
		"ms":        time.Since(started).Milliseconds(),
	})
}

// buildHarness wraps the learner's code so each test reports independently.
// Every piece of learner text is injected as a JSON string literal, never
// pasted into source, so quoting in their answer cannot break the harness or
// escape into it.
func buildHarness(a runArgs) (string, error) {
	type t struct {
		Name, Source string
	}
	tests := make([]t, 0, len(a.Tests))
	for _, x := range a.Tests {
		if strings.TrimSpace(x.Source) == "" {
			return "", ext.Invalidf("test %q has no source", x.Name)
		}
		tests = append(tests, t{x.Name, x.Source})
	}

	switch a.Language {
	case "python":
		var b strings.Builder
		b.WriteString(a.Code)
		b.WriteString("\n\nimport json as __j\n__r = []\n")
		b.WriteString("def __check(__n, __s):\n")
		b.WriteString("    try:\n        exec(__s, globals())\n        __r.append({'name': __n, 'ok': True})\n")
		b.WriteString("    except Exception as __e:\n        __r.append({'name': __n, 'ok': False, 'err': '%s: %s' % (type(__e).__name__, __e)})\n")
		for _, x := range tests {
			n, _ := json.Marshal(x.Name)
			s, _ := json.Marshal(x.Source)
			fmt.Fprintf(&b, "__check(%s, %s)\n", n, s)
		}
		fmt.Fprintf(&b, "print(%q + __j.dumps(__r))\n", marker)
		return b.String(), nil

	case "javascript", "typescript":
		var b strings.Builder
		b.WriteString(a.Code)
		b.WriteString("\n\nconst __r = [];\n")
		b.WriteString("const __check = (n, s) => { try { (0, eval)(s); __r.push({name: n, ok: true}); }\n")
		b.WriteString("  catch (e) { __r.push({name: n, ok: false, err: String(e && e.message || e)}); } };\n")
		for _, x := range tests {
			n, _ := json.Marshal(x.Name)
			s, _ := json.Marshal(x.Source)
			fmt.Fprintf(&b, "__check(%s, %s);\n", n, s)
		}
		fmt.Fprintf(&b, "console.log(%q + JSON.stringify(__r));\n", marker)
		return b.String(), nil
	}
	return "", ext.Invalidf("language %q is not supported; use python, javascript or typescript", a.Language)
}

// parseResults pulls the harness line out of the output and returns the rest
// as the learner-visible stdout.
func parseResults(out string) ([]testResult, string) {
	var results []testResult
	var plain []string
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, marker); ok {
			_ = json.Unmarshal([]byte(rest), &results)
			continue
		}
		plain = append(plain, line)
	}
	return results, strings.TrimSpace(strings.Join(plain, "\n"))
}

func (e *Ext) codeRun(ctx context.Context, language, code string, timeout int) (string, int, error) {
	toolbox, err := e.ensureSandbox(ctx)
	if err != nil {
		return "", 0, err
	}

	body := map[string]any{"code": code, "language": language, "timeout": timeout}
	var resp struct {
		ExitCode int    `json:"exitCode"`
		Result   string `json:"result"`
	}
	e.mu.Lock()
	token := e.preview
	e.mu.Unlock()

	if err := e.post(ctx, toolbox+"/process/code-run", token, body, &resp); err != nil {
		// A dead sandbox is recoverable: drop it and let the next call rebuild.
		e.mu.Lock()
		e.sandboxID, e.toolbox, e.preview = "", "", ""
		e.mu.Unlock()
		return "", 0, err
	}
	return resp.Result, resp.ExitCode, nil
}

// post talks to the sandbox's own toolbox, which authenticates with a preview
// token rather than the organization key the control plane takes.
func (e *Ext) post(ctx context.Context, url, previewToken string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return ext.Internalf("encode request: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return ext.Internalf("build request: %v", err)
	}
	req.Header.Set("x-daytona-preview-token", previewToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return &ext.Fault{Code: ext.FaultUnavailable, Message: "the sandbox is unreachable: " + err.Error(), Retry: true}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return &ext.Fault{Code: ext.FaultUnavailable, Message: fmt.Sprintf("the sandbox returned %s", resp.Status), Retry: true}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ensureSandbox returns the toolbox URL, creating the sandbox on first use.
func (e *Ext) ensureSandbox(ctx context.Context) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.toolbox != "" {
		return e.toolbox, nil
	}

	var created struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	create := map[string]any{
		"labels": map[string]string{"app": "cogdebt"},
		// Stop paying for it a few minutes after the learner stops answering.
		"autoStopInterval": 5,
	}
	if err := e.call(ctx, http.MethodPost, e.baseURL+"/sandbox", create, &created); err != nil {
		return "", err
	}
	if created.ID == "" {
		return "", ext.Internalf("Daytona created a sandbox with no id")
	}

	// A freshly created sandbox is "creating"; its preview host does not answer
	// until it is started.
	if err := e.waitStarted(ctx, created.ID); err != nil {
		return "", err
	}

	var preview struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	path := fmt.Sprintf("%s/sandbox/%s/ports/%d/preview-url", e.baseURL, created.ID, toolboxPort)
	if err := e.call(ctx, http.MethodGet, path, nil, &preview); err != nil {
		return "", err
	}
	if preview.URL == "" || preview.Token == "" {
		return "", ext.Internalf("Daytona returned no preview url or token for sandbox %s", created.ID)
	}

	e.sandboxID = created.ID
	e.toolbox = strings.TrimRight(preview.URL, "/")
	e.preview = preview.Token
	return e.toolbox, nil
}

// waitStarted polls until the sandbox is running, or gives up with a message
// that says which it is — a sandbox stuck in "creating" and one that errored
// need different responses from the tutor.
func (e *Ext) waitStarted(ctx context.Context, id string) error {
	deadline := time.Now().Add(60 * time.Second)
	for {
		var sb struct {
			State string `json:"state"`
		}
		if err := e.call(ctx, http.MethodGet, e.baseURL+"/sandbox/"+id, nil, &sb); err != nil {
			return err
		}
		switch sb.State {
		case "started":
			return nil
		case "error", "destroyed", "build_failed":
			return &ext.Fault{Code: ext.FaultUnavailable,
				Message: "the sandbox failed to start (" + sb.State + "); grade this answer by reading it instead."}
		}
		if time.Now().After(deadline) {
			return &ext.Fault{Code: ext.FaultUnavailable,
				Message: "the sandbox did not start in time; grade this answer by reading it instead.", Retry: true}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (e *Ext) call(ctx context.Context, method, url string, body, out any) error {
	var rdr *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return ext.Internalf("encode request: %v", err)
		}
		rdr = bytes.NewReader(raw)
	} else {
		rdr = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return ext.Internalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return &ext.Fault{Code: ext.FaultUnavailable, Message: "Daytona is unreachable: " + err.Error(), Retry: true}
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return &ext.Fault{Code: ext.FaultDenied,
			Message: "Daytona rejected the API key. Ask the learner to check it in Settings."}
	case resp.StatusCode >= 300:
		return &ext.Fault{Code: ext.FaultUnavailable,
			Message: fmt.Sprintf("Daytona returned %s", resp.Status)}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return ext.Internalf("decode Daytona response: %v", err)
	}
	return nil
}

// Close deletes the sandbox. Leaking one costs the user money, so this runs
// even when the host is shutting down in a hurry.
func (e *Ext) Close() error {
	e.mu.Lock()
	id := e.sandboxID
	e.sandboxID, e.toolbox, e.preview = "", "", ""
	e.mu.Unlock()
	if id == "" || e.token == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return e.call(ctx, http.MethodDelete, e.baseURL+"/sandbox/"+id, nil, nil)
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

var _ ext.Extension = (*Ext)(nil)
