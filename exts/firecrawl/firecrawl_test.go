package firecrawl

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirius/cogdebt/internal/ext/exttest"
)

func stub(t *testing.T, status int, body any) *Ext {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want a bearer token", got)
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	e := New()
	e.baseURL, e.token = srv.URL, "test-key"
	return e
}

func page(markdown string) map[string]any {
	return map[string]any{"success": true, "data": map[string]any{
		"markdown": markdown,
		"metadata": map[string]any{"title": "Feature stores", "url": "https://example.com/fs"},
	}}
}

func TestFetchReturnsMarkdown(t *testing.T) {
	e := stub(t, 200, page("# Feature stores\n\nA materialized view."))
	exttest.Calls(t, e, exttest.Case{
		Tool: "fetch", Args: map[string]any{"url": "https://example.com/fs"},
		Check: func(t *testing.T, raw json.RawMessage) {
			var got struct{ Title, Markdown string }
			json.Unmarshal(raw, &got)
			if !strings.Contains(got.Markdown, "materialized view") || got.Title == "" {
				t.Fatalf("got %+v", got)
			}
		},
	})
}

// A whole page would crowd out the conversation it is meant to inform.
func TestLongPagesAreTruncated(t *testing.T) {
	e := stub(t, 200, page(strings.Repeat("x", 40000)))
	raw, err := e.Invoke(t.Context(), "fetch", json.RawMessage(`{"url":"https://example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Markdown string }
	json.Unmarshal(raw, &got)
	if len(got.Markdown) > 17000 {
		t.Fatalf("markdown is %d chars; long pages must be cut", len(got.Markdown))
	}
	if !strings.Contains(got.Markdown, "truncated") {
		t.Error("the cut is not signposted, so the model will think it read the whole page")
	}
}

func TestFetchFaults(t *testing.T) {
	t.Run("no scheme", func(t *testing.T) {
		exttest.Calls(t, stub(t, 200, page("x")), exttest.Case{
			Tool: "fetch", Args: map[string]any{"url": "example.com"}, WantFault: "invalid_args"})
	})
	t.Run("bad key", func(t *testing.T) {
		exttest.Calls(t, stub(t, 401, map[string]any{}), exttest.Case{
			Tool: "fetch", Args: map[string]any{"url": "https://example.com"}, WantFault: "denied"})
	})
	t.Run("empty page", func(t *testing.T) {
		exttest.Calls(t, stub(t, 200, page("   ")), exttest.Case{
			Tool: "fetch", Args: map[string]any{"url": "https://example.com"}, WantFault: "not_found"})
	})
}

func TestFirecrawlConformance(t *testing.T) {
	exttest.Conformance(t, stub(t, 200, page("x")))
}
