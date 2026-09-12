package daytona

import (
	"encoding/json"
	"os"
	"testing"
)

// TestLiveRunTask talks to the real Daytona. It is skipped unless COGDEBT_LIVE
// is set, because it boots a sandbox and costs money — nobody should pay for
// it by running `go test ./...`.
//
//	COGDEBT_LIVE=1 go test ./exts/daytona/ -run Live -v
func TestLiveRunTask(t *testing.T) {
	if os.Getenv("COGDEBT_LIVE") == "" || os.Getenv(EnvKey) == "" {
		t.Skip("set COGDEBT_LIVE=1 and " + EnvKey + " to run against the real service")
	}

	e := New()
	t.Cleanup(func() { e.Close() })

	// A deliberately wrong solution: it looks up the CURRENT value, ignoring
	// the time the request was made. That is exactly the borrowed intuition an
	// etcd-shaped mental model produces, and the second test catches it while
	// the first does not.
	args, _ := json.Marshal(map[string]any{
		"language": "python",
		"code": `
ROWS = [
    {"user": 1, "ts": 10, "ltv": 100},
    {"user": 1, "ts": 20, "ltv": 250},
]

def lookup(user, as_of):
    hits = [r for r in ROWS if r["user"] == user]
    return hits[-1]["ltv"]
`,
		"tests": []map[string]string{
			{"name": "returns a value", "source": "assert lookup(1, 15) == 100 or lookup(1, 15) == 250"},
			{"name": "is point-in-time correct", "source": "assert lookup(1, 15) == 100, 'used a value from the future'"},
		},
	})

	raw, err := e.Invoke(t.Context(), "run_task", args)
	if err != nil {
		t.Fatalf("run_task against the real service: %v", err)
	}

	var got struct {
		Passed   int `json:"passed"`
		Failed   int `json:"failed"`
		Failures []struct {
			Name string `json:"name"`
			Err  string `json:"err"`
		} `json:"failures"`
		MS int64 `json:"ms"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	t.Logf("passed=%d failed=%d in %dms", got.Passed, got.Failed, got.MS)
	for _, f := range got.Failures {
		t.Logf("  FAIL %s — %s", f.Name, f.Err)
	}

	if got.Passed != 1 || got.Failed != 1 {
		t.Fatalf("passed=%d failed=%d; the wrong solution should pass the loose test and fail the precise one", got.Passed, got.Failed)
	}
	if got.Failures[0].Name != "is point-in-time correct" {
		t.Fatalf("the wrong test was blamed: %q", got.Failures[0].Name)
	}
}
