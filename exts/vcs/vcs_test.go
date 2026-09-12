package vcs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirius/cogdebt/internal/ext/exttest"
)

func stub(t *testing.T, pullBody, filesBody string, pullStatus, filesStatus int) *Ext {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/octocat/Hello-World/pulls/42/files", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(filesStatus)
		_, _ = w.Write([]byte(filesBody))
	})
	mux.HandleFunc("/repos/octocat/Hello-World/pulls/42", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(pullStatus)
		_, _ = w.Write([]byte(pullBody))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	e := New()
	e.baseURL = srv.URL
	return e
}

func TestPullSummarisesClaimAndTruncatesDiff(t *testing.T) {
	longPatch := make([]byte, maxPatchBytes+80)
	for i := range longPatch {
		longPatch[i] = 'a'
	}
	e := stub(t,
		`{"title":"Honour the cache flag","body":"UserRepository and OrderRepository.","user":{"login":"dev"},"state":"open","labels":[{"name":"bug"}]}`,
		`[{"filename":"cache.go","status":"modified","additions":12,"deletions":3,"patch":"`+string(longPatch)+`"}]`,
		http.StatusOK, http.StatusOK,
	)

	raw, err := e.Invoke(t.Context(), "pull", json.RawMessage(`{"url":"https://github.com/octocat/Hello-World/pull/42"}`))
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	var got struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Claim  string `json:"claim"`
		Author string `json:"author"`
		Files  []struct {
			Path  string `json:"path"`
			Patch string `json:"patch"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Number != 42 || got.Author != "dev" {
		t.Fatalf("got #%d by %q", got.Number, got.Author)
	}
	if got.Claim == "" || got.Title == "" {
		t.Fatal("claim and title must come from the PR, not from the model")
	}
	if len(got.Files) != 1 || got.Files[0].Path != "cache.go" {
		t.Fatalf("files = %+v", got.Files)
	}
	if len(got.Files[0].Patch) > maxPatchBytes+20 {
		t.Fatalf("patch was not truncated: %d bytes", len(got.Files[0].Patch))
	}
}

func TestPullKeepsLongTestPatch(t *testing.T) {
	body := make([]byte, maxPatchBytes+400)
	for i := range body {
		body[i] = 'x'
	}
	patch := "+enable_cache = false on OrderRepository\n+" + string(body)
	e := stub(t,
		`{"title":"fix","body":"ok","user":{"login":"a"},"state":"open","labels":[]}`,
		`[{"filename":"repo_test.go","status":"added","additions":1,"deletions":0,"patch":`+jsonString(patch)+`}]`,
		http.StatusOK, http.StatusOK,
	)
	raw, err := e.Invoke(t.Context(), "pull", json.RawMessage(`{"url":"https://github.com/octocat/Hello-World/pull/42"}`))
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	var got struct {
		Files []struct {
			Patch string `json:"patch"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 1 || !strings.Contains(got.Files[0].Patch, "OrderRepository") {
		t.Fatal("test patches must keep the tail; that is where the untested sibling often lives")
	}
	if strings.Contains(got.Files[0].Patch, "truncated") {
		t.Fatal("a test patch just over maxPatchBytes must not be truncated")
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestPullFaults(t *testing.T) {
	t.Run("not a pull url", func(t *testing.T) {
		exttest.Calls(t, New(), exttest.Case{
			Tool: "pull", Args: map[string]any{"url": "https://github.com/octocat/Hello-World"}, WantFault: "invalid_args",
		})
	})
	t.Run("missing", func(t *testing.T) {
		e := stub(t, `{}`, `[]`, http.StatusNotFound, http.StatusNotFound)
		exttest.Calls(t, e, exttest.Case{
			Tool: "pull", Args: map[string]any{"url": "https://github.com/octocat/Hello-World/pull/42"}, WantFault: "not_found",
		})
	})
	t.Run("rate limit", func(t *testing.T) {
		e := stub(t, `{}`, `[]`, http.StatusForbidden, http.StatusOK)
		exttest.Calls(t, e, exttest.Case{
			Tool: "pull", Args: map[string]any{"url": "https://github.com/octocat/Hello-World/pull/42"}, WantFault: "unavailable",
		})
	})
}

func TestParsePullURL(t *testing.T) {
	owner, repo, n, err := ParsePullURL("https://github.com/octocat/Hello-World/pull/42/files")
	if err != nil || owner != "octocat" || repo != "Hello-World" || n != 42 {
		t.Fatalf("got %s/%s#%d err=%v", owner, repo, n, err)
	}
}

func TestVCSConformance(t *testing.T) {
	exttest.Conformance(t, stub(t, `{}`, `[]`, http.StatusOK, http.StatusOK))
}
