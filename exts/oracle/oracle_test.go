package oracle

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sirius/cogdebt/exts/vcs"
	"github.com/sirius/cogdebt/internal/ext/exttest"
)

// A public regression test that happens to be SQL. The oracle must not need
// that: it only sees names in a claim and how a knob is used in the tests.
const siblingToggle = `
CREATE TABLE t_alpha (k UInt32, s Int64)
ENGINE = SummingMergeTree ORDER BY k SETTINGS optimize_on_insert = 0;

SELECT k, s FROM t_alpha FINAL WHERE k GROUP BY k, s ORDER BY k, s LIMIT 5;
SELECT k, s FROM t_alpha FINAL WHERE k GROUP BY k, s ORDER BY k, s LIMIT 5
SETTINGS optimize_move_to_prewhere_if_final = 0;

CREATE TABLE t_beta (k UInt32, s Int64)
ENGINE = CoalescingMergeTree ORDER BY k SETTINGS optimize_on_insert = 0;

SELECT k FROM t_beta FINAL WHERE k GROUP BY k, s ORDER BY k LIMIT 5;
`

func TestCheckReportsGapWhenTestsOmitClaimedName(t *testing.T) {
	raw, err := New().Invoke(t.Context(), "check", json.RawMessage(`{
		"title": "Fix optimizer on CoalescingMergeTree",
		"claim": "Result-preserving on SummingMergeTree and CoalescingMergeTree",
		"files": [
			{"path": "src/optimize.cpp", "patch": "+ move filter on SummingMergeTree"},
			{"path": "tests/summing.sql", "patch": "+ SELECT k FROM t FINAL WHERE k\n+ ENGINE = SummingMergeTree"}
		]
	}`))
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	got := decode(t, raw)
	if got.Holds == nil || *got.Holds {
		t.Fatal("a missing claimed name must set holds=false")
	}
	if !hasGap(got.Gaps, "CoalescingMergeTree") {
		t.Fatalf("gaps = %v, want CoalescingMergeTree", got.Gaps)
	}
	if got.Untested.Subject != "CoalescingMergeTree" {
		t.Fatalf("untested.subject = %q", got.Untested.Subject)
	}
}

func TestCheckReportsUnevenKnobOnSiblingSubjects(t *testing.T) {
	in, err := json.Marshal(map[string]any{
		"title": "Fix wrong results on SummingMergeTree FINAL",
		"claim": "Result-preserving on SummingMergeTree and CoalescingMergeTree",
		"files": []map[string]string{{
			"path":  "tests/queries/04545_prewhere_final.sql",
			"patch": siblingToggle,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := New().Invoke(t.Context(), "check", in)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	got := decode(t, raw)
	if got.Holds == nil || *got.Holds {
		t.Fatal("unequal knob coverage must set holds=false")
	}
	if got.Untested.Subject != "CoalescingMergeTree" {
		t.Fatalf("untested.subject = %q, want CoalescingMergeTree; observed=%q", got.Untested.Subject, got.Observed)
	}
	if !strings.Contains(strings.ToLower(got.Observed), "coalescing") {
		t.Fatalf("observed = %q, want it to name Coalescing", got.Observed)
	}
	if len(got.Rows) == 0 || got.Rows[0].Breakdown == "" {
		t.Fatal("rows must be drawable as an analogy table, with a breakdown")
	}
	if got.Rows[0].SharedRole != reviewRole {
		t.Fatalf("shared_role = %q, want %q so the UI can tell a review card from a skill", got.Rows[0].SharedRole, reviewRole)
	}
}

func TestCheckReportsUnevenKnobOnAGoFlag(t *testing.T) {
	// Same shape, different language: two repositories, one feature flag
	// flipped for only one of them. If this fails, the detector learned SQL.
	in, err := json.Marshal(map[string]any{
		"title": "Disable enable_cache for UserRepository and OrderRepository",
		"claim": "UserRepository and OrderRepository now honour enable_cache the same way",
		"files": []map[string]string{{
			"path": "repo_test.go",
			"patch": `
func TestUserRepository(t *testing.T) {
    r := NewUserRepository()
    r.Get(ctx, enable_cache = true)
    r.Get(ctx, enable_cache = false)
}
func TestOrderRepository(t *testing.T) {
    r := NewOrderRepository()
    r.Get(ctx)
}
`,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := New().Invoke(t.Context(), "check", in)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	got := decode(t, raw)
	if got.Holds == nil || *got.Holds {
		t.Fatal("a Go flag used on one sibling only must set holds=false")
	}
	if got.Untested.Subject != "OrderRepository" {
		t.Fatalf("untested.subject = %q, want OrderRepository; observed=%q", got.Untested.Subject, got.Observed)
	}
}

func TestCheckReportsTypeOnlySDKWhenSiblingForwards(t *testing.T) {
	// Firecrawl #3375 shape: the claim adds one option to several SDKs,
	// Python puts it on the wire, JS only extends the type.
	in, err := json.Marshal(map[string]any{
		"title": "feat(sdk): add ignoreRobotsTxt and robotsUserAgent to JS, Python, and Java SDKs",
		"claim": "Adds `ignoreRobotsTxt` and `robotsUserAgent` crawl parameters to the JS/TS, Python, and Java SDKs",
		"files": []map[string]string{
			{
				"path":  "apps/js-sdk/firecrawl/src/v2/types.ts",
				"patch": "+  ignoreRobotsTxt?: boolean;\n+  robotsUserAgent?: string | null;",
			},
			{
				"path": "apps/python-sdk/firecrawl/v2/methods/crawl.py",
				"patch": "+        \"allow_subdomains\": \"allowSubdomains\",\n" +
					"+        \"ignore_robots_txt\": \"ignoreRobotsTxt\",\n" +
					"+        \"robots_user_agent\": \"robotsUserAgent\",",
			},
			{
				"path":  "apps/java-sdk/src/main/java/com/firecrawl/models/CrawlOptions.java",
				"patch": "+    private Boolean ignoreRobotsTxt;\n+    private String robotsUserAgent;",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := New().Invoke(t.Context(), "check", in)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	got := decode(t, raw)
	if got.Holds == nil || *got.Holds {
		t.Fatal("a type-only SDK next to a wired sibling must set holds=false")
	}
	if got.Untested.Subject != "ignoreRobotsTxt" {
		t.Fatalf("untested.subject = %q, want ignoreRobotsTxt; observed=%q", got.Untested.Subject, got.Observed)
	}
	if !strings.Contains(got.Untested.Shape, "js-sdk") {
		t.Fatalf("untested.shape = %q, want it to name the JS family", got.Untested.Shape)
	}
	if !strings.Contains(strings.ToLower(got.Observed), "type") {
		t.Fatalf("observed = %q, want it to say a type is not a forward", got.Observed)
	}
}

func TestCheckAgainstFirecrawlPR(t *testing.T) {
	if os.Getenv("COGDEBT_LIVE") == "" {
		t.Skip("set COGDEBT_LIVE=1 to fetch a real public pull request")
	}
	raw, err := vcs.New().Invoke(t.Context(), "pull", json.RawMessage(
		`{"url":"https://github.com/firecrawl/firecrawl/pull/3375"}`))
	if err != nil {
		t.Fatalf("vcs_pull: %v", err)
	}
	var pr struct {
		Title string `json:"title"`
		Claim string `json:"claim"`
		Files []file `json:"files"`
	}
	if err := json.Unmarshal(raw, &pr); err != nil {
		t.Fatal(err)
	}
	in, err := json.Marshal(map[string]any{
		"title": pr.Title, "claim": pr.Claim, "files": pr.Files, "url": "https://github.com/firecrawl/firecrawl/pull/3375",
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := New().Invoke(t.Context(), "check", in)
	if err != nil {
		t.Fatalf("oracle_check: %v", err)
	}
	got := decode(t, out)
	if got.Holds == nil || *got.Holds {
		t.Fatalf("Firecrawl #3375 must surface the JS type-only gap; observed=%q", got.Observed)
	}
	if got.Untested.Subject != "ignoreRobotsTxt" && got.Untested.Subject != "robotsUserAgent" {
		t.Fatalf("untested.subject = %q, want a claimed crawl option; observed=%q", got.Untested.Subject, got.Observed)
	}
}

func TestCheckNeverReturnsHoldsTrue(t *testing.T) {
	in, err := json.Marshal(map[string]any{
		"title": "Fix FINAL",
		"claim": "Result-preserving on SummingMergeTree and CoalescingMergeTree",
		"files": []map[string]string{{
			"path": "tests/final.sql",
			"patch": `
CREATE TABLE a (k UInt32, s Int64) ENGINE = SummingMergeTree ORDER BY k;
SELECT s FROM a FINAL WHERE k;
SELECT s FROM a FINAL WHERE k SETTINGS optimize_x = 0;

CREATE TABLE b (k UInt32, s Int64) ENGINE = CoalescingMergeTree ORDER BY k;
SELECT s FROM b FINAL WHERE k;
SELECT s FROM b FINAL WHERE k SETTINGS optimize_x = 0;
`,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := New().Invoke(t.Context(), "check", in)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	got := decode(t, raw)
	if got.Holds != nil && *got.Holds {
		t.Fatal("holds:true is never a legal answer; matching tests are not a proof")
	}
	if got.Observed == "" {
		t.Fatal("even a clean comparison must say what was and was not established")
	}
}

func TestCheckAgainstPublicPR(t *testing.T) {
	if os.Getenv("COGDEBT_LIVE") == "" {
		t.Skip("set COGDEBT_LIVE=1 to fetch a real public pull request")
	}
	raw, err := vcs.New().Invoke(t.Context(), "pull", json.RawMessage(
		`{"url":"https://github.com/ClickHouse/ClickHouse/pull/111342"}`))
	if err != nil {
		t.Fatalf("vcs_pull: %v", err)
	}
	var pr struct {
		Title string `json:"title"`
		Claim string `json:"claim"`
		Files []file `json:"files"`
	}
	if err := json.Unmarshal(raw, &pr); err != nil {
		t.Fatal(err)
	}
	in, err := json.Marshal(map[string]any{
		"title": pr.Title, "claim": pr.Claim, "files": pr.Files,
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := New().Invoke(t.Context(), "check", in)
	if err != nil {
		t.Fatalf("oracle_check: %v", err)
	}
	got := decode(t, out)
	if got.Holds == nil || *got.Holds {
		t.Fatalf("live PR must surface the coverage gap; observed=%q", got.Observed)
	}
	if got.Untested.Subject != "CoalescingMergeTree" {
		t.Fatalf("untested.subject = %q, want CoalescingMergeTree; observed=%q", got.Untested.Subject, got.Observed)
	}
}

func TestCheckEmptyFilesFault(t *testing.T) {
	exttest.Calls(t, New(), exttest.Case{
		Tool: "check", Args: map[string]any{"files": []any{}}, WantFault: "invalid_args",
	})
}

func TestOracleConformance(t *testing.T) {
	exttest.Conformance(t, New())
}

type checkOut struct {
	Holds    *bool    `json:"holds"`
	Gaps     []string `json:"gaps"`
	Observed string   `json:"observed"`
	Untested struct {
		Subject string `json:"subject"`
		Shape   string `json:"shape"`
	} `json:"untested"`
	Rows []struct {
		Breakdown  string `json:"breakdown"`
		SharedRole string `json:"shared_role"`
	} `json:"rows"`
}

func decode(t *testing.T, raw json.RawMessage) checkOut {
	t.Helper()
	var got checkOut
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

func hasGap(gaps []string, want string) bool {
	for _, g := range gaps {
		if g == want {
			return true
		}
	}
	return false
}
