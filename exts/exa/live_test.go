package exa

import (
	"encoding/json"
	"os"
	"testing"
)

// TestLiveSearch talks to the real Exa. Skipped unless COGDEBT_LIVE is set.
func TestLiveSearch(t *testing.T) {
	if os.Getenv("COGDEBT_LIVE") == "" || os.Getenv(EnvKey) == "" {
		t.Skip("set COGDEBT_LIVE=1 and " + EnvKey + " to run against the real service")
	}

	e := New()
	raw, err := e.Invoke(t.Context(), "search",
		json.RawMessage(`{"query":"why feature stores need point-in-time correct joins","results":3}`))
	if err != nil {
		t.Fatalf("search against the real service: %v", err)
	}

	var got struct {
		Results []struct{ Title, URL, Snippet string }
	}
	json.Unmarshal(raw, &got)
	if len(got.Results) == 0 {
		t.Fatal("no results")
	}
	for _, r := range got.Results {
		t.Logf("  %s — %s", r.Title, r.URL)
		if r.URL == "" {
			t.Error("a result has no url, so the tutor cannot follow it up")
		}
	}
}
