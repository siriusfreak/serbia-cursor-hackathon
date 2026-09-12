package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirius/cogdebt/internal/ext/exttest"
)

// stub serves a canned repository list, so the tests exercise the real decoding
// and scoring path without touching the network or the rate limit.
func stub(t *testing.T, status int, body string) *Ext {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	e := New()
	e.baseURL = srv.URL
	return e
}

func recent(daysAgo int) string {
	return time.Now().AddDate(0, 0, -daysAgo).Format(time.RFC3339)
}

func TestScanScoresAndNormalises(t *testing.T) {
	body := `[
		{"name":"infra","language":"Go","topics":["kubernetes","grpc"],"pushed_at":"` + recent(10) + `","stargazers_count":5},
		{"name":"tools","language":"Go","topics":["cli"],"pushed_at":"` + recent(30) + `"},
		{"name":"old","language":"Perl","topics":[],"pushed_at":"` + recent(1200) + `"},
		{"name":"someone-elses","language":"Rust","topics":["rust"],"pushed_at":"` + recent(5) + `","fork":true},
		{"name":"retired","language":"PHP","topics":[],"pushed_at":"` + recent(5) + `","archived":true}
	]`
	e := stub(t, http.StatusOK, body)

	raw, err := e.Invoke(t.Context(), "scan", json.RawMessage(`{"login":"someone"}`))
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	var got struct {
		Scanned  int `json:"scanned"`
		Concepts []struct {
			Name      string  `json:"name"`
			Frequency float64 `json:"frequency"`
		} `json:"concepts"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	freq := map[string]float64{}
	for _, c := range got.Concepts {
		freq[c.Name] = c.Frequency
	}

	// Forks and archives are things the learner touched, not things they work
	// with; counting them would inflate debt for concepts they never lean on.
	if _, ok := freq["Rust"]; ok {
		t.Error("a fork was counted")
	}
	if _, ok := freq["PHP"]; ok {
		t.Error("an archived repo was counted")
	}

	// Go appears in two live repos, so it must top the list at exactly 1.0.
	if freq["Go"] != 1 {
		t.Errorf("Go frequency = %v, want 1 (the most-used concept anchors the scale)", freq["Go"])
	}
	// Recency decay must rank a stale language below a current one.
	if freq["Perl"] >= freq["Go"] {
		t.Errorf("Perl %v is not below Go %v; recency decay is not applied", freq["Perl"], freq["Go"])
	}
	for name, f := range freq {
		if f < 0 || f > 1 {
			t.Errorf("%s frequency %v is outside 0..1, which profile_set_frequency rejects", name, f)
		}
	}
}

func TestScanFaults(t *testing.T) {
	t.Run("unknown user", func(t *testing.T) {
		e := stub(t, http.StatusNotFound, `{}`)
		exttest.Calls(t, e, exttest.Case{
			Tool: "scan", Args: map[string]any{"login": "nope"}, WantFault: "not_found",
		})
	})

	t.Run("rate limited", func(t *testing.T) {
		e := stub(t, http.StatusForbidden, `{}`)
		exttest.Calls(t, e, exttest.Case{
			Tool: "scan", Args: map[string]any{"login": "someone"}, WantFault: "unavailable",
		})
	})

	t.Run("no public repos", func(t *testing.T) {
		e := stub(t, http.StatusOK, `[]`)
		exttest.Calls(t, e, exttest.Case{
			Tool: "scan", Args: map[string]any{"login": "someone"}, WantFault: "not_found",
		})
	})

	t.Run("login is not a path", func(t *testing.T) {
		e := stub(t, http.StatusOK, `[]`)
		exttest.Calls(t, e, exttest.Case{
			Tool: "scan", Args: map[string]any{"login": "a/../b"}, WantFault: "invalid_args",
		})
	})
}

func TestGitHubConformance(t *testing.T) {
	exttest.Conformance(t, stub(t, http.StatusOK, `[]`))
}
