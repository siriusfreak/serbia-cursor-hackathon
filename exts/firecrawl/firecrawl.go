// Package firecrawl turns a web page into clean markdown.
//
// The analogy agent is only as good as the material it has about the target
// domain. A raw HTML dump is mostly navigation; markdown of the main content
// is what a model can actually reason over.
package firecrawl

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
const EnvKey = "FIRECRAWL_API_KEY"

// Ext scrapes pages.
type Ext struct {
	client  *http.Client
	baseURL string
	token   string
}

// New returns the plugin.
func New() *Ext {
	return &Ext{
		client:  &http.Client{Timeout: 60 * time.Second},
		baseURL: envOr("FIRECRAWL_BASE_URL", "https://api.firecrawl.dev/v2"),
		token:   os.Getenv(EnvKey),
	}
}

// Configured reports whether a key is present.
func (e *Ext) Configured() bool { return e.token != "" }

func (e *Ext) Manifest() ext.Manifest {
	return ext.Manifest{
		Name:       "firecrawl",
		Version:    "0.1.0",
		ABIVersion: ext.ABIVersion,
		Kind:       ext.KindRetrieval,
		Provides: []ext.ToolSpec{{
			Name: "fetch",
			Description: "Fetches a web page as clean markdown. Call this when you have a specific URL and need " +
				"what the page actually says — documentation for the target domain, a post describing a mechanism.",
			Schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"url": {"type": "string", "description": "Full URL including scheme"}
				},
				"required": ["url"]
			}`),
			ReadOnly: true,
		}},
	}
}

func (e *Ext) Invoke(ctx context.Context, tool string, in json.RawMessage) (json.RawMessage, error) {
	if tool != "fetch" {
		return nil, ext.Invalidf("firecrawl has no tool %q; it provides fetch", tool)
	}
	if e.token == "" {
		return nil, &ext.Fault{Code: ext.FaultDenied, Message: "Firecrawl is not configured; add a key in Settings or work from what you already know."}
	}

	var a struct {
		URL string `json:"url"`
	}
	if err := ext.Args(in, &a); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(a.URL, "http://") && !strings.HasPrefix(a.URL, "https://") {
		return nil, ext.Invalidf("url %q needs a scheme, e.g. https://", a.URL)
	}

	body, _ := json.Marshal(map[string]any{
		"url": a.URL, "formats": []string{"markdown"}, "onlyMainContent": true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/scrape", bytes.NewReader(body))
	if err != nil {
		return nil, ext.Internalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, &ext.Fault{Code: ext.FaultUnavailable, Message: "Firecrawl is unreachable: " + err.Error(), Retry: true}
	}
	defer resp.Body.Close()

	if err := statusFault("Firecrawl", resp.StatusCode, resp.Status); err != nil {
		return nil, err
	}

	var out struct {
		Success bool `json:"success"`
		Data    struct {
			Markdown string `json:"markdown"`
			Metadata struct {
				Title      string `json:"title"`
				URL        string `json:"url"`
				StatusCode int    `json:"statusCode"`
			} `json:"metadata"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, ext.Internalf("decode Firecrawl response: %v", err)
	}
	if strings.TrimSpace(out.Data.Markdown) == "" {
		return nil, ext.NotFoundf("%s returned no readable content; try another source", a.URL)
	}

	// Whole pages blow the context budget for no gain; the model is reading for
	// mechanisms, not archiving.
	md := out.Data.Markdown
	if len(md) > 16000 {
		md = md[:16000] + "\n\n…(truncated)"
	}
	return ext.JSON(map[string]any{
		"url": firstNonEmpty(out.Data.Metadata.URL, a.URL), "title": out.Data.Metadata.Title, "markdown": md,
	})
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

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

var _ ext.Extension = (*Ext)(nil)
