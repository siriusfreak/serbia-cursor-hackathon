package firecrawl

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestLiveFetch talks to the real Firecrawl. Skipped unless COGDEBT_LIVE is set.
func TestLiveFetch(t *testing.T) {
	if os.Getenv("COGDEBT_LIVE") == "" || os.Getenv(EnvKey) == "" {
		t.Skip("set COGDEBT_LIVE=1 and " + EnvKey + " to run against the real service")
	}

	e := New()
	raw, err := e.Invoke(t.Context(), "fetch",
		json.RawMessage(`{"url":"https://docs.feast.dev/getting-started/concepts/point-in-time-joins"}`))
	if err != nil {
		t.Fatalf("fetch against the real service: %v", err)
	}

	var got struct{ Title, Markdown, URL string }
	json.Unmarshal(raw, &got)
	t.Logf("title=%q  %d chars of markdown", got.Title, len(got.Markdown))
	if len(got.Markdown) < 200 {
		t.Fatalf("got %d chars; the page did not come back as readable content", len(got.Markdown))
	}
	// Markdown, not HTML: the whole reason this plugin exists.
	if strings.Contains(got.Markdown, "<div") || strings.Contains(got.Markdown, "<script") {
		t.Error("raw HTML leaked into the markdown")
	}
	t.Logf("first line: %s", strings.SplitN(strings.TrimSpace(got.Markdown), "\n", 2)[0])
}
