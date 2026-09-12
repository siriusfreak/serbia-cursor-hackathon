// Package oracle compares what a pull request PROMISES against what the same
// pull request actually CHECKS.
//
// It knows no language, no repository and no bug. It reads the author's own
// words for the subjects they put their name behind, reads the tests they
// added in the same change, and reports where the two do not line up. That is
// a coverage gap, not a proof of a bug -- and an empty gap list is not a proof
// the claim holds, which is why "holds": true is never returned.
//
// The rules are about SHAPE rather than content:
//
//  1. untested sibling -- the claim names two things of the same kind and the
//     tests only exercise one of them.
//  2. uneven knob      -- a setting or flag is flipped for some subjects and
//     never for others.
//  3. uneven depth     -- no knob anywhere, but one subject gets a fraction
//     of the assertions its siblings get.
//  4. unwired sibling  -- one path family forwards a claimed name into a
//     request, another family only declares it. A type is not a test.
package oracle

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/sirius/cogdebt/internal/ext"
)

var (
	backticked = regexp.MustCompile("`([^`\n]{2,80})`")
	camel      = regexp.MustCompile(`\b[A-Z][a-z0-9]+(?:[A-Z][a-z0-9]+)+\b`)
	lowerCamel = regexp.MustCompile(`\b[a-z][a-z0-9]*[A-Z][A-Za-z0-9]*\b`)
	snake      = regexp.MustCompile(`\b[a-z][a-z0-9]*(?:_[a-z0-9]+)+\b`)
	assignment = regexp.MustCompile(`(?i)([A-Za-z_][A-Za-z0-9_.]*)\s*(?::=|==|=|:)\s*([A-Za-z0-9_'"-]+)`)
	camelSplit = regexp.MustCompile(`[A-Z][a-z0-9]*`)
	pullPath   = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/pulls?/(\d+)`)
)

const (
	minSubjectLen  = 4
	pervasiveShare = 0.10
	thinFactor     = 3
	thinLines      = 3
	reviewRole     = "same promise"
)

// Ext is the claim-versus-tests comparator.
type Ext struct{}

// New returns the plugin. It holds nothing: every answer is a pure function of
// the pull request it is handed.
func New() *Ext { return &Ext{} }

func (e *Ext) Manifest() ext.Manifest {
	return ext.Manifest{
		Name:       "oracle",
		Version:    "0.1.0",
		ABIVersion: ext.ABIVersion,
		Kind:       ext.KindRetrieval,
		Provides: []ext.ToolSpec{{
			Name: "check",
			Description: "Compares what the pull request promises against what its own tests check, and reports " +
				"the gap. Call this right after vcs_pull, passing that tool's title, claim and files unchanged. " +
				"Never decide yourself whether a claim holds -- this tool is the only verdict, and a gap is a " +
				"question for the reviewer, not a bug report.",
			Schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"url":   {"type": "string", "description": "Pull request URL, used for the citation"},
					"title": {"type": "string", "description": "Pull request title from vcs_pull"},
					"claim": {"type": "string", "description": "Pull request body from vcs_pull: what the author promises"},
					"files": {
						"type": "array",
						"description": "The files array from vcs_pull, unchanged",
						"items": {
							"type": "object",
							"properties": {
								"path":    {"type": "string"},
								"patch":   {"type": "string"},
								"is_test": {"type": "boolean"}
							}
						}
					}
				},
				"required": ["files"]
			}`),
			ReadOnly: true,
		}},
	}
}

type file struct {
	Path   string `json:"path"`
	Patch  string `json:"patch"`
	IsTest bool   `json:"is_test"`
}

func (f file) test() bool { return f.IsTest || isTestPath(f.Path) }

type checkArgs struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Claim string `json:"claim"`
	Files []file `json:"files"`
}

type finding struct {
	Subject  string
	Shape    string
	Why      string
	Observed string
	Covered  string
	Quote    string
	Path     string
}

func (e *Ext) Invoke(_ context.Context, tool string, in json.RawMessage) (json.RawMessage, error) {
	if tool != "check" {
		return nil, ext.Invalidf("oracle has no tool %q; it provides check", tool)
	}
	var a checkArgs
	if err := ext.Args(in, &a); err != nil {
		return nil, err
	}
	if len(a.Files) == 0 {
		return nil, ext.Invalidf("files is empty; pass the files array from vcs_pull unchanged")
	}

	found := analyse(a.Title, a.Claim, a.Files)
	out := map[string]any{
		"url":     a.URL,
		"subject": subjectOf(a.URL),
		"title":   a.Title,
		"claim":   clip(a.Claim, 320),
		"then":    "Save these rows as the analogy table, word for word. Ask the learner about untested.shape. Do not name a bug.",
	}
	if len(found) == 0 {
		limit := noGapObserved(a.Claim, a.Files)
		out["observed"] = limit
		out["rows"] = []map[string]string{{
			"source":      "this change checks what it promises",
			"target":      "comparing a claim to its own tests",
			"shared_role": reviewRole,
			"carry_over":  "Every subject the author named is exercised the same way by the tests in this change.",
			"breakdown":   limit,
		}}
		return ext.JSON(out)
	}

	observed := make([]string, 0, len(found))
	subjects := make([]string, 0, len(found))
	citations := make([]map[string]string, 0, len(found))
	rows := make([]map[string]string, 0, len(found))
	for _, f := range found {
		observed = append(observed, f.Observed)
		subjects = append(subjects, f.Subject)
		if f.Path != "" {
			citations = append(citations, map[string]string{"path": f.Path, "quote": f.Quote})
		}
		rows = append(rows, map[string]string{
			"source":      clip(f.Covered, 110),
			"target":      clip(f.Subject, 110),
			"shared_role": reviewRole,
			"carry_over":  clip("The change treats them as one case, so whatever it fixed for one plausibly applies to the other.", 300),
			"breakdown":   clip(f.Observed, 700),
		})
	}

	out["holds"] = false
	out["gaps"] = subjects
	out["observed"] = strings.Join(observed, " ")
	out["citations"] = citations
	out["rows"] = rows
	out["untested"] = map[string]string{
		"subject": found[0].Subject,
		"shape":   found[0].Shape,
		"why":     found[0].Why,
	}
	return ext.JSON(out)
}

func analyse(title, claim string, files []file) []finding {
	text := title + "\n" + claim
	diff := allPatches(files)
	if strings.TrimSpace(text) == "" || diff == "" {
		return nil
	}
	subjects := ground(candidates(text), diff, files)
	if len(subjects) == 0 {
		return nil
	}
	var out []finding
	out = append(out, unwiredFamily(subjects, files)...)
	lines, paths := addedTestLines(files)
	if len(lines) == 0 {
		return out
	}
	anchors, counts := anchorsIn(subjects, lines)
	if len(anchors) == 0 {
		return out
	}
	regions := attribute(anchors, lines, paths)
	fromTests := unevenKnob(anchors, regions)
	fromTests = append(fromTests, untestedSibling(subjects, counts, anchors, regions)...)
	if len(fromTests) == 0 {
		fromTests = unevenDepth(anchors, regions)
	}
	return append(out, fromTests...)
}

func candidates(text string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.Trim(strings.TrimSpace(s), "`.,;:()[]")
		if len([]rune(s)) < minSubjectLen || seen[strings.ToLower(s)] {
			return
		}
		if strings.ContainsAny(s, " \t") {
			return
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	for _, m := range backticked.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	for _, m := range camel.FindAllString(text, -1) {
		add(m)
	}
	for _, m := range lowerCamel.FindAllString(text, -1) {
		add(m)
	}
	for _, m := range snake.FindAllString(text, -1) {
		add(m)
	}
	return out
}

func ground(subjects []string, _ string, files []file) []string {
	// A name that is only the path of a changed file is the class being
	// edited, not a subject the tests could mention. A name that appears
	// only in the claim is the opposite: that is exactly an untested sibling.
	var paths strings.Builder
	for _, f := range files {
		paths.WriteString(strings.ToLower(f.Path))
		paths.WriteByte('\n')
	}
	pathText := paths.String()
	out := make([]string, 0, len(subjects))
	for _, s := range subjects {
		if strings.Contains(pathText, strings.ToLower(s)) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func addedTestLines(files []file) (lines, paths []string) {
	for _, f := range files {
		if !f.test() {
			continue
		}
		for _, raw := range strings.Split(f.Patch, "\n") {
			body, ok := addedLine(raw)
			if !ok {
				continue
			}
			lines = append(lines, body)
			paths = append(paths, f.Path)
		}
	}
	return lines, paths
}

func addedLine(raw string) (string, bool) {
	switch {
	case strings.HasPrefix(raw, "+++"), strings.HasPrefix(raw, "---"),
		strings.HasPrefix(raw, "@@"), strings.HasPrefix(raw, "diff "),
		strings.HasPrefix(raw, "index "):
		return "", false
	case strings.HasPrefix(raw, "+"):
		body := strings.TrimSpace(raw[1:])
		return body, body != ""
	case strings.HasPrefix(raw, "-"):
		return "", false
	}
	body := strings.TrimSpace(raw)
	return body, body != ""
}

func anchorsIn(subjects []string, lines []string) ([]string, map[string]int) {
	counts := map[string]int{}
	for _, s := range subjects {
		key := strings.ToLower(s)
		for _, l := range lines {
			if strings.Contains(strings.ToLower(l), key) {
				counts[s]++
			}
		}
	}
	limit := int(float64(len(lines)) * pervasiveShare)
	if limit < 2 {
		limit = 2
	}
	assigned := assignmentNames(lines)
	var out []string
	for _, s := range subjects {
		if assigned[strings.ToLower(s)] {
			continue
		}
		if n := counts[s]; n >= 1 && n <= limit {
			out = append(out, s)
		}
	}
	return out, counts
}

func assignmentNames(lines []string) map[string]bool {
	out := map[string]bool{}
	for _, l := range lines {
		for _, m := range assignment.FindAllStringSubmatch(l, -1) {
			out[strings.ToLower(lastField(m[1]))] = true
		}
	}
	return out
}

type region struct {
	Lines []string
	Path  string
	Quote string
}

func attribute(anchors []string, lines, paths []string) map[string]*region {
	out := map[string]*region{}
	for _, a := range anchors {
		out[a] = &region{}
	}
	current := ""
	for i, line := range lines {
		lower := strings.ToLower(line)
		for _, a := range anchors {
			if strings.Contains(lower, strings.ToLower(a)) {
				current = a
				if out[a].Quote == "" {
					out[a].Quote = clip(line, 160)
					out[a].Path = paths[i]
				}
				break
			}
		}
		if current == "" {
			continue
		}
		out[current].Lines = append(out[current].Lines, line)
	}
	return out
}

// familyOf groups files that ship as one client. "apps/js-sdk/..." and
// "apps/python-sdk/..." are different families; two files under js-sdk are not.
func familyOf(path string) string {
	for _, seg := range strings.Split(path, "/") {
		s := strings.ToLower(seg)
		if strings.HasSuffix(s, "-sdk") || strings.HasSuffix(s, "_sdk") || s == "sdk" {
			return seg
		}
	}
	parts := strings.Split(path, "/")
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return path
}

func lineMentions(line, subject string) bool {
	return strings.Contains(strings.ToLower(line), strings.ToLower(subject))
}

func isForwardLine(line, subject string) bool {
	if strings.Contains(line, `"`+subject+`"`) || strings.Contains(line, `'`+subject+`'`) {
		return true
	}
	return strings.Contains(line, "request."+subject) ||
		strings.Contains(line, "payload."+subject) ||
		strings.Contains(line, "body."+subject)
}

func isDeclareLine(line, subject string) bool {
	if isForwardLine(line, subject) {
		return false
	}
	if !lineMentions(line, subject) {
		return false
	}
	if strings.Contains(line, subject+"?:") {
		return true
	}
	lower := strings.ToLower(line)
	if strings.Contains(lower, "private ") || strings.Contains(lower, "public ") ||
		strings.Contains(lower, "protected ") {
		return true
	}
	// "name: boolean" / "name: string | null" is a type. "name: true" is not.
	idx := strings.Index(line, subject+":")
	if idx < 0 {
		return false
	}
	rest := strings.TrimSpace(line[idx+len(subject)+1:])
	rest = strings.TrimSuffix(rest, ";")
	rest = strings.ToLower(rest)
	return strings.HasPrefix(rest, "boolean") || strings.HasPrefix(rest, "string") ||
		strings.HasPrefix(rest, "number") || strings.HasPrefix(rest, "bool") ||
		strings.Contains(rest, "|")
}

type familyUse struct {
	declares bool
	forwards bool
	path     string
	quote    string
}

func classifyFamilies(subject string, files []file) map[string]*familyUse {
	out := map[string]*familyUse{}
	for _, f := range files {
		if f.test() {
			continue
		}
		fam := familyOf(f.Path)
		u := out[fam]
		if u == nil {
			u = &familyUse{}
			out[fam] = u
		}
		for _, raw := range strings.Split(f.Patch, "\n") {
			body, ok := addedLine(raw)
			if !ok || !lineMentions(body, subject) {
				continue
			}
			if u.quote == "" {
				u.quote = clip(body, 160)
				u.path = f.Path
			}
			if isForwardLine(body, subject) {
				u.forwards = true
			} else if isDeclareLine(body, subject) {
				u.declares = true
			}
		}
	}
	return out
}

func unwiredFamily(subjects []string, files []file) []finding {
	var out []finding
	for _, s := range subjects {
		uses := classifyFamilies(s, files)
		var wired, declared []string
		var weak *familyUse
		for fam, u := range uses {
			switch {
			case u.forwards:
				wired = append(wired, fam)
			case u.declares:
				declared = append(declared, fam)
				if weak == nil {
					weak = u
				}
			}
		}
		if len(wired) == 0 || len(declared) == 0 {
			continue
		}
		sort.Strings(wired)
		sort.Strings(declared)
		strong := strings.Join(wired, ", ")
		weakNames := strings.Join(declared, " and ")
		out = append(out, finding{
			Subject: s,
			Shape:   "forward " + s + " from " + weakNames + " the way " + wired[0] + " already does",
			Why:     strong + " puts " + s + " on the wire; " + weakNames + " only declares it",
			Covered: strong,
			Path:    weak.path,
			Quote:   weak.quote,
			Observed: "The claim names " + s + " across " + strong + " and " + weakNames +
				", but " + weakNames + " only adds a type. " + strong +
				" already forwards it in the request. A type is not a proof the option is sent.",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out
}

func unevenKnob(anchors []string, regions map[string]*region) []finding {
	type hit struct {
		anchor string
		values []string
	}
	byName := map[string][]hit{}
	anchorSet := map[string]bool{}
	for _, a := range anchors {
		anchorSet[strings.ToLower(a)] = true
	}
	for _, a := range anchors {
		for name, values := range assignmentsIn(regions[a].Lines) {
			if len(name) < minSubjectLen || anchorSet[strings.ToLower(name)] {
				continue
			}
			if valuesAreAnchors(values, anchorSet) {
				continue
			}
			byName[name] = append(byName[name], hit{anchor: a, values: values})
		}
	}
	var out []finding
	for name, hits := range byName {
		seen := map[string]bool{}
		var with []string
		for _, h := range hits {
			if !seen[h.anchor] {
				with = append(with, h.anchor)
				seen[h.anchor] = true
			}
		}
		if len(with) == 0 || len(with) == len(anchors) {
			continue
		}
		var without []string
		for _, a := range anchors {
			if !seen[a] {
				without = append(without, a)
			}
		}
		if len(without) == 0 {
			continue
		}
		sort.Strings(with)
		sort.Strings(without)
		weak, strong := without[0], strings.Join(with, ", ")
		out = append(out, finding{
			Subject: weak,
			Shape:   "the same check on " + weak + " with " + name + " flipped the other way",
			Why:     strong + " is checked with " + name + "; " + weak + " is not",
			Covered: strong,
			Path:    regions[weak].Path,
			Quote:   regions[weak].Quote,
			Observed: "The change promises the same thing for " + strong + " and " + weak +
				", but only " + strong + " is checked with " + name + " set both ways. " +
				"Nothing here runs " + weak + " with " + name + " flipped.",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out
}

func untestedSibling(subjects []string, counts map[string]int, anchors []string, regions map[string]*region) []finding {
	var out []finding
	for _, s := range subjects {
		if counts[s] > 0 {
			continue
		}
		sibling := ""
		for _, a := range anchors {
			if kindOf(a) == kindOf(s) {
				sibling = a
				break
			}
		}
		if sibling == "" {
			continue
		}
		out = append(out, finding{
			Subject: s,
			Shape:   "any test that exercises " + s,
			Why:     "the claim names it beside " + sibling + ", which the tests do exercise",
			Covered: sibling,
			Path:    regions[sibling].Path,
			Quote:   regions[sibling].Quote,
			Observed: "The claim names " + s + " beside " + sibling +
				", but no test added by this change mentions " + s + " at all.",
		})
	}
	return out
}

func unevenDepth(anchors []string, regions map[string]*region) []finding {
	if len(anchors) < 2 {
		return nil
	}
	best, worst := anchors[0], anchors[0]
	for _, a := range anchors {
		if len(regions[a].Lines) > len(regions[best].Lines) {
			best = a
		}
		if len(regions[a].Lines) < len(regions[worst].Lines) {
			worst = a
		}
	}
	deep, thin := len(regions[best].Lines), len(regions[worst].Lines)
	if best == worst || thin > thinLines || deep < thin*thinFactor {
		return nil
	}
	return []finding{{
		Subject: worst,
		Shape:   "the checks " + best + " gets, applied to " + worst,
		Why:     best + " gets " + plural(deep) + "; " + worst + " gets " + plural(thin),
		Covered: best,
		Path:    regions[worst].Path,
		Quote:   regions[worst].Quote,
		Observed: "The claim covers " + best + " and " + worst + " alike, but the tests give " + best + " " +
			plural(deep) + " and " + worst + " only " + plural(thin) + ".",
	}}
}

func assignmentsIn(lines []string) map[string][]string {
	values := map[string]map[string]bool{}
	for _, l := range lines {
		for _, m := range assignment.FindAllStringSubmatch(l, -1) {
			name := lastField(m[1])
			if values[name] == nil {
				values[name] = map[string]bool{}
			}
			values[name][strings.Trim(m[2], `'"`)] = true
		}
	}
	out := map[string][]string{}
	for name, set := range values {
		vs := make([]string, 0, len(set))
		for v := range set {
			vs = append(vs, v)
		}
		sort.Strings(vs)
		out[name] = vs
	}
	return out
}

func valuesAreAnchors(values []string, anchors map[string]bool) bool {
	for _, v := range values {
		if anchors[strings.ToLower(v)] {
			return true
		}
	}
	return false
}

func kindOf(s string) string {
	if segs := camelSplit.FindAllString(s, -1); len(segs) > 1 {
		return strings.ToLower(segs[len(segs)-1])
	}
	if parts := strings.Split(s, "_"); len(parts) > 1 {
		return strings.ToLower(parts[len(parts)-1])
	}
	return strings.ToLower(s)
}

func noGapObserved(claim string, files []file) string {
	if strings.TrimSpace(claim) == "" {
		return "This pull request states no promise to compare against, so there is nothing to check it with. " +
			"Read the change itself before approving."
	}
	if !hasTests(files) {
		return "This change adds no tests, so there is nothing in it that could contradict the claim. " +
			"Absence of a test is not evidence the claim holds."
	}
	return "Every subject the claim names is exercised the same way by the tests in this change. " +
		"That is a comparison of a promise against its own tests, not a run: it cannot show the promise is true."
}

func hasTests(files []file) bool {
	for _, f := range files {
		if f.test() {
			return true
		}
	}
	return false
}

func isTestPath(path string) bool {
	p := strings.ToLower(path)
	return strings.Contains(p, "test") || strings.Contains(p, "/spec/") ||
		strings.HasSuffix(p, "_spec.rb") || strings.HasSuffix(p, ".feature")
}

func allPatches(files []file) string {
	var b strings.Builder
	for _, f := range files {
		b.WriteString(f.Patch)
		b.WriteByte('\n')
	}
	return b.String()
}

func subjectOf(url string) string {
	m := pullPath.FindStringSubmatch(url)
	if m == nil {
		return ""
	}
	return m[1] + "/" + m[2] + "#" + m[3]
}

func lastField(s string) string {
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return "1 line"
	}
	return itoa(n) + " lines"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

func (e *Ext) Close() error { return nil }

var _ ext.Extension = (*Ext)(nil)
