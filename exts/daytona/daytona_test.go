package daytona

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirius/cogdebt/internal/ext/exttest"
)

// stub stands in for both halves of Daytona: the control plane that creates
// sandboxes and mints preview tokens, and the sandbox's own toolbox that runs
// code behind that token. run decides what the fake interpreter "prints".
func stub(t *testing.T, run func(code string) (string, int)) *Ext {
	t.Helper()
	const previewToken = "preview-token-1"
	var host string
	mux := http.NewServeMux()

	mux.HandleFunc("POST /sandbox", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": "sb-1", "state": "creating"})
	})
	mux.HandleFunc("GET /sandbox/{id}", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": "sb-1", "state": "started"})
	})
	mux.HandleFunc("GET /sandbox/{id}/ports/{port}/preview-url", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("port") != "2280" {
			t.Errorf("asked for port %s, want the toolbox port", r.PathValue("port"))
		}
		json.NewEncoder(w).Encode(map[string]any{"url": host, "token": previewToken})
	})
	mux.HandleFunc("DELETE /sandbox/{id}", func(w http.ResponseWriter, r *http.Request) {})

	mux.HandleFunc("POST /process/code-run", func(w http.ResponseWriter, r *http.Request) {
		// The toolbox takes the preview token, never the organization key —
		// sending the wrong one is the mistake that cost an afternoon.
		if got := r.Header.Get("x-daytona-preview-token"); got != previewToken {
			t.Errorf("x-daytona-preview-token = %q, want the minted preview token", got)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("the organization key was sent to the sandbox; the toolbox rejects it")
		}
		var body struct {
			Code     string `json:"code"`
			Language string `json:"language"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		out, exit := run(body.Code)
		json.NewEncoder(w).Encode(map[string]any{"result": out, "exitCode": exit})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	host = srv.URL

	e := New()
	e.baseURL, e.token = srv.URL, "test-key"
	return e
}

func runArgsJSON(t *testing.T, code string, tests ...[2]string) json.RawMessage {
	t.Helper()
	var ts []map[string]string
	for _, x := range tests {
		ts = append(ts, map[string]string{"name": x[0], "source": x[1]})
	}
	raw, err := json.Marshal(map[string]any{"language": "python", "code": code, "tests": ts})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestPerTestResults is the whole point: a grade that names WHICH belief
// failed, not a single score.
func TestPerTestResults(t *testing.T) {
	e := stub(t, func(code string) (string, int) {
		// The harness must carry both tests and the learner's own output.
		if !strings.Contains(code, "def lookup") {
			t.Errorf("harness dropped the learner's code:\n%s", code)
		}
		results := `[{"name":"returns a value","ok":true},` +
			`{"name":"is point-in-time correct","ok":false,"err":"AssertionError: leaked the future"}]`
		return "some program output\n" + marker + results, 0
	})

	raw, err := e.Invoke(t.Context(), "run_task", runArgsJSON(t, "def lookup():\n    return 1\n",
		[2]string{"returns a value", "assert lookup() == 1"},
		[2]string{"is point-in-time correct", "assert False"}))
	if err != nil {
		t.Fatalf("run_task: %v", err)
	}

	var got struct {
		Passed   int `json:"passed"`
		Failed   int `json:"failed"`
		Failures []struct {
			Name string `json:"name"`
			Err  string `json:"err"`
		} `json:"failures"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}

	if got.Passed != 1 || got.Failed != 1 {
		t.Fatalf("passed=%d failed=%d, want 1/1", got.Passed, got.Failed)
	}
	if len(got.Failures) != 1 || got.Failures[0].Name != "is point-in-time correct" {
		t.Fatalf("failures = %+v; a failing test must name the belief it checked", got.Failures)
	}
	if got.Failures[0].Err == "" {
		t.Error("the failure carries no message; the learner cannot see what went wrong")
	}
	// The harness line must not leak into what the learner reads.
	if strings.Contains(got.Output, marker) {
		t.Error("the machine-readable line leaked into the learner-visible output")
	}
	if !strings.Contains(got.Output, "some program output") {
		t.Error("the program's own output was dropped")
	}
}

// TestSubmissionThatNeverRuns: a syntax error means no harness line at all,
// which must read as "all failed" rather than "all passed".
func TestSubmissionThatNeverRuns(t *testing.T) {
	e := stub(t, func(string) (string, int) { return "SyntaxError: invalid syntax", 1 })

	raw, err := e.Invoke(t.Context(), "run_task", runArgsJSON(t, "def broken(:",
		[2]string{"a", "assert True"}, [2]string{"b", "assert True"}))
	if err != nil {
		t.Fatalf("run_task: %v", err)
	}
	var got struct {
		Passed int    `json:"passed"`
		Failed int    `json:"failed"`
		Output string `json:"output"`
	}
	json.Unmarshal(raw, &got)

	if got.Passed != 0 || got.Failed != 2 {
		t.Fatalf("passed=%d failed=%d; code that never ran must not count as passing", got.Passed, got.Failed)
	}
	if !strings.Contains(got.Output, "SyntaxError") {
		t.Error("the error the learner needs to see was dropped")
	}
}

// TestLearnerTextCannotBreakTheHarness: quotes and newlines in a submission are
// ordinary, and must not be able to escape into the generated program.
func TestLearnerTextCannotBreakTheHarness(t *testing.T) {
	var seen string
	e := stub(t, func(code string) (string, int) {
		seen = code
		return marker + `[{"name":"t","ok":true}]`, 0
	})

	nasty := `name = "he said \"hi\"" + '''triple'''`
	if _, err := e.Invoke(t.Context(), "run_task",
		runArgsJSON(t, "x = 1", [2]string{nasty, nasty})); err != nil {
		t.Fatalf("run_task: %v", err)
	}
	// The real invariant is reversibility: whatever the harness carries must
	// decode back to exactly what the learner wrote. Checking for a substring
	// only proves what the escaping looked like on one input.
	line := ""
	for _, l := range strings.Split(seen, "\n") {
		if strings.HasPrefix(l, "__check(") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no __check call in the harness:\n%s", seen)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(line, "__check("), ")")
	var name, source string
	dec := json.NewDecoder(strings.NewReader(strings.ReplaceAll(inner, ", ", "\n")))
	if err := dec.Decode(&name); err != nil {
		t.Fatalf("the name argument is not a valid literal: %v\n%s", err, line)
	}
	if err := dec.Decode(&source); err != nil {
		t.Fatalf("the source argument is not a valid literal: %v\n%s", err, line)
	}
	if name != nasty || source != nasty {
		t.Fatalf("the harness altered the learner's text:\n got name:   %q\n got source: %q\n want:       %q", name, source, nasty)
	}
	// And the raw text must never appear unquoted, which is what would let it
	// escape into the surrounding program.
	if strings.Contains(seen, "__check(name =") {
		t.Fatal("test text was pasted into source instead of being encoded")
	}
}

func TestNoTestsIsRefused(t *testing.T) {
	e := stub(t, func(string) (string, int) { return "", 0 })
	exttest.Calls(t, e, exttest.Case{
		Tool: "run_task",
		Args: map[string]any{"language": "python", "code": "x = 1", "tests": []any{}},
		// Running ungraded code is the failure mode this plugin exists to avoid.
		WantFault: "invalid_args",
	})
}

func TestUnconfiguredSaysSo(t *testing.T) {
	e := New()
	e.token = ""
	_, err := e.Invoke(t.Context(), "run_task", runArgsJSON(t, "x = 1", [2]string{"t", "assert True"}))
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("want a clear not-configured fault, got %v", err)
	}
}

func TestDaytonaConformance(t *testing.T) {
	exttest.Conformance(t, stub(t, func(string) (string, int) { return marker + "[]", 0 }))
}
