// Package github scans a learner's public repositories to find which concepts
// they actually work with, and how often.
//
// This is the plugin that turns mastery into cognitive debt: debt is high for
// a concept you lean on often and do not hold, and "often" has to come from
// somewhere real. Self-report cannot supply it -- people do not know what they
// lean on.
//
// It depends on nothing but HTTP, which makes it the reference PORTABLE plugin:
// unlike profile, it holds no handle to host state, so it can run in-process or
// in a subprocess unchanged.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sirius/cogdebt/internal/ext"
)

// Ext is the GitHub scanner.
type Ext struct {
	client  *http.Client
	baseURL string
	token   string
}

// New returns the scanner. A GITHUB_TOKEN in the environment raises the rate
// limit from 60 to 5000 requests an hour; without one a demo scan still fits.
func New() *Ext {
	return &Ext{
		client:  &http.Client{Timeout: 15 * time.Second},
		baseURL: "https://api.github.com",
		token:   os.Getenv("GITHUB_TOKEN"),
	}
}

func (e *Ext) Manifest() ext.Manifest {
	return ext.Manifest{
		Name:       "github",
		Version:    "0.1.0",
		ABIVersion: ext.ABIVersion,
		Kind:       ext.KindRetrieval,
		Provides: []ext.ToolSpec{{
			Name: "scan",
			Description: "Scans a person's public GitHub repositories and reports which technologies they actually " +
				"work with and how often, as frequency between 0 and 1. Call this to find what the learner leans on " +
				"in real work, then pass the result to profile_set_frequency so cognitive debt can be computed.",
			Schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"login": {"type": "string", "description": "GitHub username, e.g. \"torvalds\""},
					"limit": {"type": "integer", "description": "How many recent repositories to consider, default 30"}
				},
				"required": ["login"]
			}`),
			ReadOnly: true,
		}},
	}
}

type scanArgs struct {
	Login string `json:"login"`
	Limit int    `json:"limit"`
}

// repo is the subset of the GitHub repository object this plugin reads. Both
// language and topics come back in the list response, so one request is enough
// -- fetching per-repo language breakdowns would be N+1 and burn the unauthenticated
// rate limit on a single scan.
type repo struct {
	Name     string   `json:"name"`
	Language string   `json:"language"`
	Topics   []string `json:"topics"`
	Fork     bool     `json:"fork"`
	Archived bool     `json:"archived"`
	Stars    int      `json:"stargazers_count"`
	PushedAt string   `json:"pushed_at"`
}

func (e *Ext) Invoke(ctx context.Context, tool string, in json.RawMessage) (json.RawMessage, error) {
	if tool != "scan" {
		return nil, ext.Invalidf("github has no tool %q; it provides scan", tool)
	}

	var a scanArgs
	if err := ext.Args(in, &a); err != nil {
		return nil, err
	}
	login := strings.TrimSpace(a.Login)
	if login == "" {
		return nil, ext.Invalidf("login is empty; pass a GitHub username")
	}
	if strings.ContainsAny(login, "/?&# ") {
		return nil, ext.Invalidf("login %q is not a bare username", login)
	}
	if a.Limit <= 0 || a.Limit > 100 {
		a.Limit = 30
	}

	repos, err := e.fetchRepos(ctx, login, a.Limit)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return nil, ext.NotFoundf("%q has no public repositories to scan; ask the learner what they work with instead", login)
	}
	return ext.JSON(map[string]any{
		"login":    login,
		"scanned":  len(repos),
		"concepts": score(repos),
	})
}

func (e *Ext) fetchRepos(ctx context.Context, login string, limit int) ([]repo, error) {
	url := fmt.Sprintf("%s/users/%s/repos?per_page=%d&sort=pushed&type=owner", e.baseURL, login, limit)
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

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, ext.NotFoundf("GitHub has no user %q; check the spelling", login)
	case http.StatusForbidden, http.StatusTooManyRequests:
		// Anonymous callers get 60 requests an hour. Say so plainly, because
		// the model cannot fix it by retrying.
		return nil, &ext.Fault{
			Code:    ext.FaultUnavailable,
			Message: "GitHub rate limit reached. Set GITHUB_TOKEN to raise it, or ask the learner what they work with.",
		}
	default:
		return nil, &ext.Fault{Code: ext.FaultUnavailable, Message: fmt.Sprintf("GitHub returned %s", resp.Status)}
	}

	var repos []repo
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, ext.Internalf("decode GitHub response: %v", err)
	}
	return repos, nil
}

// concept is one technology and how much of the learner's work touches it.
type concept struct {
	Name      string  `json:"name"`
	Frequency float64 `json:"frequency"`
	Repos     int     `json:"repos"`
}

// score weights each repository by recency and reach, then normalises so the
// most-used technology sits at 1.0.
//
// Forks and archives are skipped: they are things the learner touched, not
// things they work with, and counting them inflates debt for concepts they
// never actually lean on.
func score(repos []repo) []concept {
	weights := map[string]float64{}
	counts := map[string]int{}

	for _, r := range repos {
		if r.Fork || r.Archived {
			continue
		}
		w := recencyWeight(r.PushedAt)
		if r.Stars > 0 {
			w *= 1.2 // work others depend on counts for more
		}
		for _, name := range append([]string{r.Language}, r.Topics...) {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			weights[name] += w
			counts[name]++
		}
	}
	if len(weights) == 0 {
		return nil
	}

	var top float64
	for _, w := range weights {
		top = max(top, w)
	}

	// Repository topics are a mix of real technologies and project names
	// ("tv-backlight"). A low share is the best available signal that a tag is
	// incidental, and passing those through pollutes the debt list with things
	// the learner does not lean on at all.
	const floor = 0.35

	out := make([]concept, 0, len(weights))
	for name, w := range weights {
		if f := w / top; f >= floor {
			out = append(out, concept{Name: name, Frequency: f, Repos: counts[name]})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Frequency != out[j].Frequency {
			return out[i].Frequency > out[j].Frequency
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

// recencyWeight decays toward 0.2 over two years: what someone shipped last
// month describes them better than what they shipped in 2019.
func recencyWeight(pushedAt string) float64 {
	t, err := time.Parse(time.RFC3339, pushedAt)
	if err != nil {
		return 0.5
	}
	years := time.Since(t).Hours() / (24 * 365)
	switch {
	case years <= 0.5:
		return 1.0
	case years >= 2:
		return 0.2
	default:
		return 1.0 - 0.4*years
	}
}

func (e *Ext) Close() error { return nil }

var _ ext.Extension = (*Ext)(nil)
