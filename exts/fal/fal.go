// Package fal generates an image for an analogy.
//
// A structural mapping is a picture: two domains, a shared role, and the seam
// where they come apart. Some learners hold that better as a diagram than as a
// table, and the image is generated from the mapping the system already built.
//
// The auth format here was OBSERVED, not taken from documentation: fal.run
// answers "bearer: unable to decode issuer" to an Authorization: Bearer header
// (it expects a JWT there) and "invalid key credentials" to
// Authorization: Key <id>:<secret>, which is the scheme an API key uses.
package fal

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

// EnvKey is where the API key is read from. fal keys are "<id>:<secret>".
const EnvKey = "FAL_KEY"

// defaultModel is a fast text-to-image model; override with FAL_MODEL.
const defaultModel = "fal-ai/flux/schnell"

// Ext generates images.
type Ext struct {
	client  *http.Client
	baseURL string
	model   string
	token   string
}

// New returns the plugin.
func New() *Ext {
	return &Ext{
		client:  &http.Client{Timeout: 120 * time.Second},
		baseURL: envOr("FAL_BASE_URL", "https://fal.run"),
		model:   envOr("FAL_MODEL", defaultModel),
		token:   os.Getenv(EnvKey),
	}
}

// Configured reports whether a key is present.
func (e *Ext) Configured() bool { return e.token != "" }

func (e *Ext) Manifest() ext.Manifest {
	return ext.Manifest{
		Name:       "fal",
		Version:    "0.1.0",
		ABIVersion: ext.ABIVersion,
		Kind:       ext.KindRetrieval,
		Provides: []ext.ToolSpec{{
			Name: "illustrate",
			Description: "Draws a diagram of one analogy pair. Call this only when the learner asks to see the " +
				"mapping, or says they think visually — an image costs real money and adds nothing to a mapping " +
				"they already understood in words.",
			Schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"source":      {"type": "string", "description": "The concept they already hold"},
					"target":      {"type": "string", "description": "The concept in the new field"},
					"shared_role": {"type": "string", "description": "The role that justifies the pairing"},
					"breakdown":   {"type": "string", "description": "Where the analogy stops holding"}
				},
				"required": ["source", "target"]
			}`),
		}},
	}
}

func (e *Ext) Invoke(ctx context.Context, tool string, in json.RawMessage) (json.RawMessage, error) {
	if tool != "illustrate" {
		return nil, ext.Invalidf("fal has no tool %q; it provides illustrate", tool)
	}
	if e.token == "" {
		return nil, &ext.Fault{Code: ext.FaultDenied, Message: "fal is not configured; add a key in Settings, or describe the mapping in words instead."}
	}

	var a struct {
		Source     string `json:"source"`
		Target     string `json:"target"`
		SharedRole string `json:"shared_role"`
		Breakdown  string `json:"breakdown"`
	}
	if err := ext.Args(in, &a); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Source) == "" || strings.TrimSpace(a.Target) == "" {
		return nil, ext.Invalidf("source and target are both required; an analogy needs two sides")
	}

	body, _ := json.Marshal(map[string]any{
		"prompt":        diagramPrompt(a.Source, a.Target, a.SharedRole, a.Breakdown),
		"image_size":    "landscape_16_9",
		"num_images":    1,
		"output_format": "jpeg",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/"+e.model, bytes.NewReader(body))
	if err != nil {
		return nil, ext.Internalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Key "+e.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, &ext.Fault{Code: ext.FaultUnavailable, Message: "fal is unreachable: " + err.Error(), Retry: true}
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, &ext.Fault{Code: ext.FaultDenied,
			Message: "fal rejected the key. It must be the full \"id:secret\" pair from the fal dashboard, not just one half."}
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, &ext.Fault{Code: ext.FaultUnavailable, Message: "fal rate limit reached.", Retry: true}
	case resp.StatusCode >= 300:
		return nil, &ext.Fault{Code: ext.FaultUnavailable, Message: fmt.Sprintf("fal returned %s", resp.Status)}
	}

	var out struct {
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, ext.Internalf("decode fal response: %v", err)
	}
	if len(out.Images) == 0 || out.Images[0].URL == "" {
		return nil, ext.NotFoundf("fal returned no image")
	}
	return ext.JSON(map[string]any{"url": out.Images[0].URL, "source": a.Source, "target": a.Target})
}

// diagramPrompt asks for a diagram rather than an illustration: the point is
// the structure, and a decorative picture of "a database" teaches nothing.
func diagramPrompt(source, target, role, breakdown string) string {
	var b strings.Builder
	b.WriteString("A clean technical diagram on a near-black background, thin amber lines, no photorealism. ")
	fmt.Fprintf(&b, "Two labelled boxes side by side: %q on the left, %q on the right, ", source, target)
	b.WriteString("joined by an arrow. ")
	if role != "" {
		fmt.Fprintf(&b, "The arrow is labelled %q. ", strings.ReplaceAll(role, "_", " "))
	}
	if breakdown != "" {
		b.WriteString("Below the arrow, a broken or dashed segment marked with a small warning triangle, ")
		b.WriteString("showing that the correspondence fails at one point. ")
	}
	b.WriteString("Minimal, diagrammatic, legible labels, plenty of empty space.")
	return b.String()
}

func (e *Ext) Close() error { return nil }

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

var _ ext.Extension = (*Ext)(nil)
