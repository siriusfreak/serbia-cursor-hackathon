package exa

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirius/cogdebt/internal/ext/exttest"
)

func stub(t *testing.T, status int, body any) *Ext {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Exa authenticates with its own header, not a bearer token.
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key = %q", got)
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	e := New()
	e.baseURL, e.token = srv.URL, "test-key"
	return e
}

func TestSearchMapsResults(t *testing.T) {
	e := stub(t, 200, map[string]any{"results": []map[string]any{{
		"title": "Point-in-time correctness", "url": "https://example.com/pit",
		"publishedDate": "2024-03-01", "highlights": []string{"what was known at request time", "label leakage"},
	}}})

	exttest.Calls(t, e, exttest.Case{
		Tool: "search", Args: map[string]any{"query": "how feature stores avoid label leakage"},
		Check: func(t *testing.T, raw json.RawMessage) {
			var got struct {
				Results []struct{ Title, URL, Snippet, Published string }
			}
			json.Unmarshal(raw, &got)
			if len(got.Results) != 1 {
				t.Fatalf("got %d results", len(got.Results))
			}
			r := got.Results[0]
			if r.URL == "" || r.Title == "" || r.Snippet == "" {
				t.Fatalf("a result is missing fields the model needs: %+v", r)
			}
		},
	})
}

func TestSearchFaults(t *testing.T) {
	t.Run("empty query", func(t *testing.T) {
		exttest.Calls(t, stub(t, 200, map[string]any{}), exttest.Case{
			Tool: "search", Args: map[string]any{"query": "  "}, WantFault: "invalid_args"})
	})
	t.Run("nothing found", func(t *testing.T) {
		exttest.Calls(t, stub(t, 200, map[string]any{"results": []any{}}), exttest.Case{
			Tool: "search", Args: map[string]any{"query": "x"}, WantFault: "not_found"})
	})
	t.Run("rate limited", func(t *testing.T) {
		exttest.Calls(t, stub(t, 429, map[string]any{}), exttest.Case{
			Tool: "search", Args: map[string]any{"query": "x"}, WantFault: "unavailable"})
	})
}

func TestExaConformance(t *testing.T) {
	exttest.Conformance(t, stub(t, 200, map[string]any{"results": []any{}}))
}
