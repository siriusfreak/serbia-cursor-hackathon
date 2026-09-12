package fal

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
		// Observed against the real endpoint: a Bearer header makes fal try to
		// decode a JWT. An API key goes in the Key scheme.
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Key ") {
			t.Errorf("Authorization = %q, want the Key scheme", got)
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	e := New()
	e.baseURL, e.token, e.model = srv.URL, "id:secret", "fal-ai/flux/schnell"
	return e
}

func TestIllustrateReturnsURL(t *testing.T) {
	e := stub(t, 200, map[string]any{"images": []map[string]any{{"url": "https://cdn.example/x.jpg"}}})
	exttest.Calls(t, e, exttest.Case{
		Tool: "illustrate",
		Args: map[string]any{"source": "etcd", "target": "feature store", "shared_role": "source_of_truth"},
		Check: func(t *testing.T, raw json.RawMessage) {
			var got struct{ URL string }
			json.Unmarshal(raw, &got)
			if got.URL == "" {
				t.Fatal("no image url returned")
			}
		},
	})
}

// The prompt has to ask for structure. A pretty picture of a database teaches
// nothing about a mapping.
func TestPromptAsksForTheStructure(t *testing.T) {
	p := diagramPrompt("etcd", "feature store", "source_of_truth", "no point-in-time correctness")
	for _, want := range []string{"etcd", "feature store", "source of truth", "diagram"} {
		if !strings.Contains(strings.ToLower(p), strings.ToLower(want)) {
			t.Errorf("prompt does not mention %q:\n%s", want, p)
		}
	}
	if !strings.Contains(p, "broken") && !strings.Contains(p, "dashed") {
		t.Error("a mapping with a breakdown is drawn as if it held everywhere")
	}
}

func TestIllustrateFaults(t *testing.T) {
	t.Run("one-sided", func(t *testing.T) {
		exttest.Calls(t, stub(t, 200, map[string]any{}), exttest.Case{
			Tool: "illustrate", Args: map[string]any{"source": "etcd"}, WantFault: "invalid_args"})
	})
	t.Run("bad key explains the format", func(t *testing.T) {
		e := stub(t, 401, map[string]any{})
		_, err := e.Invoke(t.Context(), "illustrate", json.RawMessage(`{"source":"a","target":"b"}`))
		if err == nil || !strings.Contains(err.Error(), "id:secret") {
			t.Fatalf("the error should name the key format that actually works, got %v", err)
		}
	})
}

func TestFalConformance(t *testing.T) {
	exttest.Conformance(t, stub(t, 200, map[string]any{"images": []map[string]any{{"url": "https://x/y.jpg"}}}))
}
