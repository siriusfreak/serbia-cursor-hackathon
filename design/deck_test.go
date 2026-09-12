package design

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The pitch deck is checked the same way the artboards are, and for the same
// reason: a presentation that has drifted from the product is worse than no
// presentation, because it is confidently wrong in front of an audience.
//
// Three things are enforced here.
//
//  1. The deck's colours are the app's colours. A screenshot next to a slide
//     in a different orange reads as two different products.
//  2. Every slide has speaker notes, and SPEECH.md has a section per slide.
//     Adding a slide without a line to say is how a deck gets presented badly;
//     this makes it a build failure instead of a surprise on stage.
//  3. The claims the deck makes about the code are still true of the code.
//
// The type ramp is deliberately NOT checked against the app. A desktop window
// reads at 14px and a projector does not; the deck has its own scale.
const (
	deckSrc    = "../presentation/index.html"
	speechSrc  = "../presentation/SPEECH.md"
	sourcesSrc = "../presentation/SOURCES.md"
)

func deck(t *testing.T) string {
	t.Helper()
	return readFile(t, deckSrc)
}

// TestDeckColoursComeFromTheTheme keeps the slides and the product visually
// the same thing. Tints are written as rgba over a palette colour, so any bare
// hex here has to be one the app actually renders.
func TestDeckColoursComeFromTheTheme(t *testing.T) {
	palette := paletteFromTheme(t)
	hex := regexp.MustCompile(`#([0-9A-Fa-f]{3,8})\b`)

	for _, m := range hex.FindAllStringSubmatch(deck(t), -1) {
		got := strings.ToUpper(m[1])
		if len(got) != 6 {
			t.Errorf("%s: colour %s is not a 6-digit hex; the theme has no shorthand or alpha colours", deckSrc, m[0])
			continue
		}
		if _, ok := palette[got]; !ok {
			t.Errorf("%s: colour #%s is not in the app palette (%s) — use rgba() over a palette colour for a tint",
				deckSrc, got, themeSrc)
		}
	}
}

// slideCount is how many <section class="slide"> the deck has.
func slideCount(body string) int {
	return strings.Count(body, `<section class="slide`)
}

// TestEverySlideHasSomethingToSay is the rule that keeps the deck and the
// speech from drifting apart: a slide with no notes is a slide the presenter
// will improvise, and improvising is where the factual claims go wrong.
func TestEverySlideHasSomethingToSay(t *testing.T) {
	body := deck(t)
	slides := slideCount(body)
	if slides < 10 {
		t.Fatalf("found %d slides in %s; the parser is looking for the wrong marker", slides, deckSrc)
	}
	if notes := strings.Count(body, `<aside class="notes">`); notes != slides {
		t.Errorf("%d slides but %d speaker notes: every slide needs a line to say", slides, notes)
	}
}

// TestSpeechCoversEverySlide pins SPEECH.md to the deck one-to-one. The
// headings are numbered, so a slide inserted in the middle without a matching
// section fails here rather than on stage.
func TestSpeechCoversEverySlide(t *testing.T) {
	slides := slideCount(deck(t))

	re := regexp.MustCompile(`(?m)^## (\d+) · `)
	matches := re.FindAllStringSubmatch(readFile(t, speechSrc), -1)

	seen := map[int]bool{}
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if seen[n] {
			t.Errorf("%s has two sections numbered %d", speechSrc, n)
		}
		seen[n] = true
	}
	for i := 1; i <= slides; i++ {
		if !seen[i] {
			t.Errorf("%s has no section for slide %d; the deck has %d slides", speechSrc, i, slides)
		}
	}
	for n := range seen {
		if n > slides {
			t.Errorf("%s has a section %d but the deck only has %d slides", speechSrc, n, slides)
		}
	}
}

// TestDeckClaimsAreStillTrue checks the load-bearing numbers on the slides
// against the code they describe. These are the sentences a judge can check,
// so they must not be allowed to rot.
func TestDeckClaimsAreStillTrue(t *testing.T) {
	body := deck(t)

	// "8 plugins" on the architecture slide.
	entries, err := os.ReadDir("../exts")
	if err != nil {
		t.Fatalf("read ../exts: %v", err)
	}
	var plugins int
	for _, e := range entries {
		if e.IsDir() {
			plugins++
		}
	}
	if !strings.Contains(body, `<div class="n am">`+strconv.Itoa(plugins)+`</div>`) {
		t.Errorf("the deck does not claim %d plugins, but ../exts has %d directories", plugins, plugins)
	}

	// Every plugin named in the ABI table is a directory that exists.
	row := regexp.MustCompile(`<td class="k[^"]*">([a-z]+)</td><td class="m">(profile|retrieval|assessor|agent)</td>`)
	named := map[string]bool{}
	for _, m := range row.FindAllStringSubmatch(body, -1) {
		named[m[1]] = true
		if _, err := os.Stat(filepath.Join("../exts", m[1])); err != nil {
			t.Errorf("the deck lists a plugin %q that has no directory in ../exts", m[1])
		}
	}
	for _, e := range entries {
		if e.IsDir() && !named[e.Name()] {
			t.Errorf("../exts/%s is a shipped plugin the deck's table does not list", e.Name())
		}
	}

	// "3 methods in the entire plugin ABI".
	iface := between(t, readFile(t, "../internal/ext/abi.go"), "type Extension interface {", "\n}")
	methods := regexp.MustCompile(`(?m)^\t([A-Z]\w*)\(`).FindAllString(iface, -1)
	if len(methods) != 3 {
		t.Errorf("ext.Extension now has %d methods; the deck says 3", len(methods))
	}

	// The screenshot the app slide points at has to be TRACKED, not merely
	// present. Render serves what is in the repository, and a .gitignore rule
	// on *.png has already silently dropped deck assets once -- a file sitting
	// on one laptop ships as a broken image on the demo slide.
	if _, err := os.Stat("../presentation/assets/app.png"); err != nil {
		t.Errorf("the deck embeds assets/app.png but it is missing: %v", err)
	} else if !tracked(t, "presentation/assets/app.png") {
		t.Error("presentation/assets/app.png exists but git is ignoring it; the deployed deck will show a broken image")
	}
}

// tracked reports whether git has the path. Skips rather than fails where git
// is unavailable, so the check cannot break a build outside a checkout.
func tracked(t *testing.T, path string) bool {
	t.Helper()
	out, err := exec.Command("git", "-C", "..", "ls-files", "--error-unmatch", path).CombinedOutput()
	if err != nil {
		if _, lookErr := exec.LookPath("git"); lookErr != nil {
			t.Skip("git is not available, cannot check what is tracked")
		}
		t.Logf("git ls-files %s: %s", path, strings.TrimSpace(string(out)))
		return false
	}
	return true
}

// numberWords lets the slide say "nineteen" while SOURCES.md counts rows.
var numberWords = map[int]string{
	10: "ten", 11: "eleven", 12: "twelve", 13: "thirteen", 14: "fourteen",
	15: "fifteen", 16: "sixteen", 17: "seventeen", 18: "eighteen", 19: "nineteen",
	20: "twenty", 21: "twenty-one", 22: "twenty-two", 23: "twenty-three",
	24: "twenty-four", 25: "twenty-five",
}

// normalise flattens case, spacing and the dash characters a headline picks up
// when it is retyped, so two spellings of one title still compare equal.
func normalise(s string) string {
	r := strings.NewReplacer("—", "-", "–", "-", "’", "'", "‘", "'", "\u00a0", " ")
	return strings.Join(strings.Fields(strings.ToLower(r.Replace(s))), " ")
}

// TestEveryHeadlineOnTheSlidesIsCited is the check that would have caught a
// real defect: the deck claimed a count and a date range that SOURCES.md did
// not support. A headline on a slide is a factual claim in front of judges, so
// it has to be traceable to a link somebody can open.
func TestEveryHeadlineOnTheSlidesIsCited(t *testing.T) {
	sources := normalise(readFile(t, sourcesSrc))

	headline := regexp.MustCompile(`<span class="t">([^<]+)</span>`)
	found := headline.FindAllStringSubmatch(deck(t), -1)
	if len(found) == 0 {
		t.Fatal("no headlines found on the slides; the parser is looking for the wrong markup")
	}
	for _, m := range found {
		// Slides truncate a long title to fit the column, so compare on the
		// opening of the title rather than the whole string.
		want := normalise(m[1])
		if len(want) > 40 {
			want = want[:40]
		}
		if !strings.Contains(sources, want) {
			t.Errorf("the slide shows %q but %s does not cite it", m[1], sourcesSrc)
		}
	}
}

// TestTheEvidenceCountMatchesTheSources pins the number the presenter says out
// loud to the number of rows anyone can count in the file.
func TestTheEvidenceCountMatchesTheSources(t *testing.T) {
	rows := regexp.MustCompile(`(?m)^\| (\d{4}-\d{2}-\d{2}) \|`).FindAllStringSubmatch(readFile(t, sourcesSrc), -1)
	n := len(rows)
	if n < 5 {
		t.Fatalf("%s lists %d dated sources; the table format changed", sourcesSrc, n)
	}

	word, ok := numberWords[n]
	if !ok {
		t.Fatalf("%s lists %d sources, which has no spelling in numberWords", sourcesSrc, n)
	}
	if want := "of " + word + " Hacker News stories"; !strings.Contains(deck(t), want) {
		t.Errorf("%s lists %d sources, so a slide should say %q", sourcesSrc, n, want)
	}
	if !strings.Contains(readFile(t, speechSrc), word) {
		t.Errorf("the deck claims %s sources but %s says a different number", word, speechSrc)
	}

	// The date range on the slide has to cover the sources actually listed.
	first, last := rows[0][1], rows[0][1]
	for _, r := range rows {
		if r[1] < first {
			first = r[1]
		}
		if r[1] > last {
			last = r[1]
		}
	}
	months := map[string]string{"01": "January", "02": "February", "03": "March", "04": "April",
		"05": "May", "06": "June", "07": "July", "08": "August", "09": "September",
		"10": "October", "11": "November", "12": "December"}
	want := "between " + months[first[5:7]] + " and " + months[last[5:7]]
	if !strings.Contains(deck(t), want) {
		t.Errorf("the sources run from %s to %s, so the slide should say %q", first, last, want)
	}
}

// TestTheDeckLinksToThisRepository keeps the address on the closing slide equal
// to the repository the deck is committed in. A wrong link there is a judge who
// cannot find the code.
func TestTheDeckLinksToThisRepository(t *testing.T) {
	out, err := exec.Command("git", "-C", "..", "remote", "get-url", "origin").Output()
	if err != nil {
		t.Skipf("no git remote to compare against: %v", err)
	}
	remote := strings.TrimSpace(string(out))
	remote = strings.TrimSuffix(remote, ".git")
	remote = strings.TrimPrefix(remote, "git@github.com:")
	remote = strings.TrimPrefix(remote, "https://github.com/")
	if remote == "" {
		t.Skip("could not parse the remote")
	}

	if !strings.Contains(deck(t), "github.com/"+remote) {
		t.Errorf("the slides do not link to github.com/%s", remote)
	}
}

// TestDeckStillLeadsWithTheBreakdown: if the slides stop making the mandatory
// limit the centrepiece, the deck is pitching a different product from the one
// in this repository.
func TestDeckStillLeadsWithTheBreakdown(t *testing.T) {
	body := deck(t)
	if n := strings.Count(body, `class="br"`); n < 4 {
		t.Errorf("only %d analogy rows on the slides carry a breakdown line; every one must", n)
	}
	if !strings.Contains(body, "does not pay down cognitive debt") {
		t.Error("the deck no longer states the one invariant: an analogy without a limit creates debt")
	}
}
