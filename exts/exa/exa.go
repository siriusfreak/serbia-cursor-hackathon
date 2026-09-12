// Package exa finds sources by meaning rather than by keyword.
//
// It matters for this system specifically: mapping a learner onto a new field
// means finding how that field describes its own mechanisms, which is a
// semantic question. Keyword search returns the pages that repeat your words
// back at you.
package exa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sirius/cogdebt/internal/ext"
)

// EnvKey is where the API key is read from.
const EnvKey = "EXA_API_KEY"

// Ext searches.
type Ext struct {
	client  *http.Client
	baseURL string
	token   string
}

// New returns the plugin.
func New() *Ext {
	return &Ext{
		client:  &http.Client{Timeout: 45 * time.Second},
		baseURL: envOr("EXA_BASE_URL", "https://api.exa.ai"),
		token:   os.Getenv(EnvKey),
	}
}

// Configured reports whether a key is present.
func (e *Ext) Configured() bool { return e.token != "" }

func (e *Ext) Manifest() ext.Manifest {
	return ext.Manifest{
		Name:       "exa",
		Version:    "0.1.0",
		ABIVersion: ext.ABIVersion,
		Kind:       ext.KindRetrieval,
		Provides: []ext.ToolSpec{{
			Name: "search",
			Description: "Finds sources by meaning, not keyword. Call this when you need to learn how the target " +
				"field describes a mechanism and you do not already have a URL. Describe what you want to find, " +
				"in a sentence, rather than listing keywords.",
			Schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query":   {"type": "string", "description": "A sentence describing what you are looking for"},
					"results": {"type": "integer", "description": "1..10, default 5"}
				},
				"required": ["query"]
			}`),
			ReadOnly: true,
		}},
	}
}

func (e *Ext) Invoke(ctx context.Context, tool string, in json.RawMessage) (json.RawMessage, error) {
	if tool != "search" {
		return nil, ext.Invalidf("exa has no tool %q; it provides search", tool)
	}
	if e.token == "" {
		return nil, &ext.Fault{Code: ext.FaultDenied, Message: "Exa is not configured; add a key in Settings or work from what you already know."}
	}

	var a struct {
		Query   string `json:"query"`
		Results int    `json:"results"`
	}
	if err := ext.Args(in, &a); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Query) == "" {
		return nil, ext.Invalidf("query is empty")
	}
	if a.Results <= 0 || a.Results > 10 {
		a.Results = 5
	}

	body, _ := json.Marshal(map[string]any{
		"query": a.Query, "type": "auto", "numResults": a.Results,
		"contents": map[string]any{"highlights": true},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/search", bytes.NewReader(body))
	if err != nil {
		return nil, ext.Internalf("build request: %v", err)
	}
	req.Header.Set("x-api-key", e.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, &ext.Fault{Code: ext.FaultUnavailable, Message: "Exa is unreachable: " + err.Error(), Retry: true}
	}
	defer resp.Body.Close()

	if err := statusFault("Exa", resp.StatusCode, resp.Status); err != nil {
		return nil, err
	}

	var out struct {
		Results []struct {
			Title         string   `json:"title"`
			URL           string   `json:"url"`
			PublishedDate string   `json:"publishedDate"`
			Highlights    []string `json:"highlights"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, ext.Internalf("decode Exa response: %v", err)
	}
	if len(out.Results) == 0 {
		return nil, ext.NotFoundf("nothing found for %q; try describing the mechanism differently", a.Query)
	}

	type hit struct {
		Title     string `json:"title"`
		URL       string `json:"url"`
		Snippet   string `json:"snippet,omitempty"`
		Published string `json:"published,omitempty"`
	}
	hits := make([]hit, 0, len(out.Results))
	for _, r := range out.Results {
		hits = append(hits, hit{r.Title, r.URL, strings.Join(r.Highlights, " … "), r.PublishedDate})
	}
	return ext.JSON(map[string]any{"results": hits})
}

func (e *Ext) Close() error { return nil }

func statusFault(who string, code int, status string) error {
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return &ext.Fault{Code: ext.FaultDenied, Message: who + " rejected the API key. Ask the learner to check it in Settings."}
	case code == http.StatusTooManyRequests:
		return &ext.Fault{Code: ext.FaultUnavailable, Message: who + " rate limit reached.", Retry: true}
	case code >= 300:
		return &ext.Fault{Code: ext.FaultUnavailable, Message: fmt.Sprintf("%s returned %s", who, status)}
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

var _ ext.Extension = (*Ext)(nil)
