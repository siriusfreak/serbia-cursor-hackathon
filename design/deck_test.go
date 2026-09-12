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
	deckSrc   = "../presentation/index.html"
	speechSrc = "../presentation/SPEECH.md"
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
