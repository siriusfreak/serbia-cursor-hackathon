// Package vcs fetches a public pull request as facts for the review agent.
//
// It answers one question: what did this change CLAIM, and what did it touch.
// That is not the question github_scan answers -- that one measures what a
// person works with over time, to weigh cognitive debt. Two questions, two
// plugins, so neither grows a mode flag.
//
// Like github it holds no host state and speaks only HTTP, so it runs
// in-process or in a subprocess unchanged.
package vcs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sirius/cogdebt/internal/ext"
)

const (
	maxFiles          = 15
	maxPatchBytes     = 2500
	maxTestPatchBytes = 24_000
	maxBodyRunes      = 1600
)

var pullURL = regexp.MustCompile(`(?i)(?:https?://)?(?:www\.)?github\.com/([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+)/pulls?/(\d+)`)

// Ext is the pull-request retrieval plugin.
type Ext struct {
	client  *http.Client
	baseURL string
	token   string
}

// New returns the plugin. GITHUB_TOKEN is optional: public pull requests work
// without one, at 60 requests an hour instead of 5000.
func New() *Ext {
	return &Ext{
		client:  &http.Client{Timeout: 20 * time.Second},
		baseURL: "https://api.github.com",
		token:   os.Getenv("GITHUB_TOKEN"),
	}
}

func (e *Ext) Manifest() ext.Manifest {
	return ext.Manifest{
		Name:       "vcs",
		Version:    "0.1.0",
		ABIVersion: ext.ABIVersion,
		Kind:       ext.KindRetrieval,
		Provides: []ext.ToolSpec{{
			Name: "pull",
			Description: "Fetches a public GitHub pull request: the author's claim, the files it touches and " +
				"their diffs. Call this first whenever the learner pastes a pull-request URL, and pass the result " +
				"straight to oracle_check. Do not summarise the diff to the learner.",
			Schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"url": {"type": "string", "description": "Pull request URL, e.g. https://github.com/owner/repo/pull/42"}
				},
				"required": ["url"]
			}`),
			ReadOnly: true,
		}},
	}
}

func (e *Ext) Invoke(ctx context.Context, tool string, in json.RawMessage) (json.RawMessage, error) {
	if tool != "pull" {
		return nil, ext.Invalidf("vcs has no tool %q; it provides pull", tool)
	}
	var a struct {
		URL string `json:"url"`
	}
	if err := ext.Args(in, &a); err != nil {
		return nil, err
	}
	owner, repo, number, err := ParsePullURL(a.URL)
	if err != nil {
		return nil, err
	}

	pr, err := e.object(ctx, fmt.Sprintf("%s/repos/%s/%s/pulls/%d", e.baseURL, owner, repo, number))
	if err != nil {
		return nil, err
	}
	files, err := e.list(ctx, fmt.Sprintf("%s/repos/%s/%s/pulls/%d/files", e.baseURL, owner, repo, number))
	if err != nil {
		return nil, err
	}
	return ext.JSON(Summarise(owner, repo, number, pr, files))
}

// ParsePullURL reads owner, repo and number out of a pull-request URL. It is
// exported so anything else accepting a URL accepts exactly the same shapes.
func ParsePullURL(raw string) (owner, repo string, number int, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", 0, ext.Invalidf("url is empty; pass a GitHub pull request URL")
	}
	m := pullURL.FindStringSubmatch(raw)
	if m == nil {
		return "", "", 0, ext.Invalidf("url %q is not a GitHub pull request; expected github.com/owner/repo/pull/N", raw)
	}
	owner, repo = m[1], m[2]
	if strings.Contains(owner, "..") || strings.Contains(repo, "..") {
		return "", "", 0, ext.Invalidf("url %q is not a GitHub pull request", raw)
	}
	n, convErr := strconv.Atoi(m[3])
	if convErr != nil || n <= 0 {
		return "", "", 0, ext.Invalidf("url %q has no pull request number", raw)
	}
	return owner, repo, n, nil
}

// Summarise reduces the GitHub payloads to the facts a review needs. It is
// exported so a test can drive it from recorded JSON without a network call.
func Summarise(owner, repo string, number int, pr map[string]any, files []map[string]any) map[string]any {
	labels := make([]string, 0)
	if raw, ok := pr["labels"].([]any); ok {
		for _, item := range raw {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if name := str(obj["name"]); name != "" {
				labels = append(labels, name)
			}
		}
	}

	out := make([]map[string]any, 0, min(len(files), maxFiles))
	for i, f := range files {
		if i >= maxFiles {
			break
		}
		path := str(f["filename"])
		patch := str(f["patch"])
		limit := maxPatchBytes
		if IsTestPath(path) {
			limit = maxTestPatchBytes
		}
		if len(patch) > limit {
			patch = patch[:limit] + "\n…(truncated)"
		}
		out = append(out, map[string]any{
			"path":      path,
			"status":    f["status"],
			"additions": f["additions"],
			"deletions": f["deletions"],
			"is_test":   IsTestPath(path),
			"patch":     patch,
		})
	}

	return map[string]any{
		"url":           fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, number),
		"owner":         owner,
		"repo":          repo,
		"number":        number,
		"title":         str(pr["title"]),
		"claim":         clip(str(pr["body"]), maxBodyRunes),
		"author":        nested(pr, "user", "login"),
		"state":         str(pr["state"]),
		"labels":        labels,
		"files":         out,
		"files_omitted": max(0, len(files)-len(out)),
		"then":          "Call oracle_check with this title, claim and files. Do not judge the change yourself.",
	}
}

// IsTestPath guesses whether a path holds tests. It is a guess on purpose:
// every ecosystem spells it differently, and the cost of a wrong guess is a
// patch truncated at the wrong length, not a wrong verdict.
func IsTestPath(path string) bool {
	p := strings.ToLower(path)
	switch {
	case strings.Contains(p, "test"), strings.Contains(p, "/spec/"),
		strings.HasSuffix(p, "_spec.rb"), strings.HasSuffix(p, ".feature"):
		return true
	}
	return false
}

func (e *Ext) object(ctx context.Context, url string) (map[string]any, error) {
	body, err := e.get(ctx, url)
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, ext.Internalf("decode GitHub response: %v", err)
	}
	return obj, nil
}

func (e *Ext) list(ctx context.Context, url string) ([]map[string]any, error) {
	body, err := e.get(ctx, url)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, ext.Internalf("decode GitHub file list: %v", err)
	}
	return out, nil
}

func (e *Ext) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, ext.Internalf("build request: %v", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if e.token != "" {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, &ext.Fault{Code: ext.FaultUnavailable, Message: "GitHub is unreachable: " + err.Error(), Retry: true}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	switch resp.StatusCode {
	case http.StatusOK:
		return body, nil
	case http.StatusNotFound:
		return nil, ext.NotFoundf("GitHub has no pull request at that URL; check the owner, repository and number")
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, &ext.Fault{Code: ext.FaultUnavailable,
			Message: "GitHub rate limit reached. Add a GitHub token in Settings to raise it from 60 requests an hour to 5000."}
	default:
		return nil, &ext.Fault{Code: ext.FaultUnavailable, Message: fmt.Sprintf("GitHub returned %s", resp.Status)}
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func nested(obj map[string]any, keys ...string) string {
	cur := any(obj)
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[k]
	}
	return str(cur)
}

func clip(s string, runes int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= runes {
		return string(r)
	}
	return string(r[:runes]) + "…"
}

func (e *Ext) Close() error { return nil }

var _ ext.Extension = (*Ext)(nil)
