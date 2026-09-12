package design

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const themeSrc = "../internal/ui/theme.go"

// artboards is every .dc.html the canvas ships.
var artboards = []string{
	"Main.dc.html", "Prediction.dc.html", "Reveal.dc.html",
	"Contrast.dc.html", "Falsify.dc.html", "Ledger.dc.html",
}

// radii are the corner radii the app actually uses: 8 on cards and inputs
// (theme.SizeNameInputRadius, ui.accented), 9 on tool pills (ui.chip), 10 on
// the learner's own message (ui.appendUser), 3 on a progress track and 1.5 on
// a card's accent stripe (ui.progressRow, ui.accented).
var radii = map[string]bool{"8": true, "9": true, "10": true, "3": true, "1.5": true}

// paletteFromTheme reads the dark palette out of the running app's theme, so
// this test cannot drift from the code by restating its colours.
func paletteFromTheme(t *testing.T) map[string]string {
	t.Helper()
	src := readFile(t, themeSrc)

	block := between(t, src, "var darkPalette = palette{", "}")
	re := regexp.MustCompile(`(\w+):\s*rgb\(0x([0-9A-Fa-f]{6})\)`)
	out := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(block, -1) {
		out[strings.ToUpper(m[2])] = m[1]
	}
	if len(out) < 8 {
		t.Fatalf("parsed only %d colours from %s; the palette shape changed", len(out), themeSrc)
	}
	return out
}

// sizesFromTheme reads the type ramp out of the theme for the same reason.
func sizesFromTheme(t *testing.T) map[string]bool {
	t.Helper()
	src := readFile(t, themeSrc)

	block := between(t, src, "func (t *appTheme) Size(", "\n}")
	re := regexp.MustCompile(`case theme\.SizeName(Text|HeadingText|SubHeadingText|CaptionText):\s*\n\s*return (\d+)`)
	out := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(block, -1) {
		out[m[2]] = true
	}
	if len(out) < 4 {
		t.Fatalf("parsed only %d text sizes from %s; the ramp changed", len(out), themeSrc)
	}
	return out
}

// TestColoursComeFromTheTheme is the check that matters most: a stray hex in a
// mockup is a design that promises something the app cannot render.
func TestColoursComeFromTheTheme(t *testing.T) {
	palette := paletteFromTheme(t)
	hex := regexp.MustCompile(`#([0-9A-Fa-f]{3,8})\b`)

	for _, name := range append(artboards, "base.css") {
		body := readFile(t, name)
		for _, m := range hex.FindAllStringSubmatch(body, -1) {
			got := strings.ToUpper(m[1])
			if len(got) != 6 {
				t.Errorf("%s: colour %s is not a 6-digit hex; the theme has no shorthand or alpha colours", name, m[0])
				continue
			}
			if _, ok := palette[got]; !ok {
				t.Errorf("%s: colour #%s is not in the app palette (%s)", name, got, themeSrc)
			}
		}
	}
}

func TestTextSizesComeFromTheTheme(t *testing.T) {
	sizes := sizesFromTheme(t)
	re := regexp.MustCompile(`font-size:\s*(\d+(?:\.\d+)?)px`)

	for _, name := range append(artboards, "base.css") {
		for _, m := range re.FindAllStringSubmatch(readFile(t, name), -1) {
			if !sizes[m[1]] {
				t.Errorf("%s: font-size %spx is not in the app type ramp (%s)", name, m[1], themeSrc)
			}
		}
	}
}

func TestCornerRadiiMatchTheComponents(t *testing.T) {
	re := regexp.MustCompile(`border-radius:\s*(\d+(?:\.\d+)?)px`)
	for _, name := range append(artboards, "base.css") {
		for _, m := range re.FindAllStringSubmatch(readFile(t, name), -1) {
			if !radii[m[1]] {
				t.Errorf("%s: border-radius %spx is not a radius the app uses", name, m[1])
			}
		}
	}
}

// TestPredictionHidesTheTarget guards the whole point of the design. If the
// answer is visible on the card that asks for it, the screen is a lecture with
// a text box and the generation effect is gone.
func TestPredictionHidesTheTarget(t *testing.T) {
	body := strings.ToLower(readFile(t, "Prediction.dc.html"))

	for _, leak := range []string{"feature store", "model registry", "drift detection"} {
		if strings.Contains(body, leak) {
			t.Errorf("Prediction.dc.html names %q: the target must stay hidden until the learner commits", leak)
		}
	}
	if !strings.Contains(body, "etcd") {
		t.Error("Prediction.dc.html does not show the source concept")
	}
	if !strings.Contains(body, "class=\"hole\"") {
		t.Error("Prediction.dc.html has no placeholder standing in for the hidden target")
	}
}

// TestRevealCannotDeadEnd is the subtle one. Reveal shows a mapping before its
// limit, which is exactly the state this product exists to prevent -- so it is
// only safe while its single control leads forward into the breakdown. A
// second control, or a dismissal, would let a learner leave holding an
// unlimited analogy.
func TestRevealCannotDeadEnd(t *testing.T) {
	body := readFile(t, "Reveal.dc.html")

	if n := strings.Count(body, `class="btn`); n != 1 {
		t.Fatalf("Reveal.dc.html has %d controls, want exactly 1: any exit other than the breakdown leaves the analogy unlimited", n)
	}
	if !strings.Contains(strings.ToLower(body), "diverged") {
		t.Error("Reveal.dc.html's control does not lead to the contrast step")
	}
	// Showing the mapping one beat before its limit is only safe while the
	// screen says a limit exists. Without that line a learner who stops here
	// leaves holding an analogy that looks complete.
	if !strings.Contains(strings.ToLower(body), "has a limit") {
		t.Error("Reveal.dc.html does not say the mapping has a limit; it reads as the whole answer")
	}
}

// TestBreakdownIsPresentAndAmber: the mandatory limit line, in the accent
// colour the app reserves for it.
func TestBreakdownIsPresentAndAmber(t *testing.T) {
	palette := paletteFromTheme(t)
	var accent string
	for hex, name := range palette {
		if name == "accent" {
			accent = hex
		}
	}
	if accent == "" {
		t.Fatal("no accent colour in the palette")
	}

	body := readFile(t, "Contrast.dc.html")
	if !strings.Contains(strings.ToLower(body), "point-in-time") {
		t.Error("Contrast.dc.html carries no breakdown line")
	}
	if !strings.Contains(readFile(t, "base.css"), "#"+accent) {
		t.Errorf("the breakdown style does not use the accent colour #%s", accent)
	}
}

// TestLedgerNamesTheSource: a misconception is only actionable if it says what
// it was borrowed from -- that is what makes it debt the system issued rather
// than a generic gap.
func TestLedgerNamesTheSource(t *testing.T) {
	body := readFile(t, "Ledger.dc.html")
	if n := strings.Count(body, "borrowed from"); n < 4 {
		t.Errorf("Ledger.dc.html names a source on %d entries; every entry needs one", n)
	}
	for _, want := range []string{"OPEN", "RETIRED"} {
		if !strings.Contains(body, want) {
			t.Errorf("Ledger.dc.html has no %s section", want)
		}
	}
}

// TestIconsAreDrawnNotTyped: emoji and dingbats do not scale or recolour, and
// render differently on every machine.
func TestIconsAreDrawnNotTyped(t *testing.T) {
	banned := []string{"⚠", "⚙", "✓", "✗", "→", "←", "🔴", "🟢"}
	for _, name := range artboards {
		body := readFile(t, name)
		for _, glyph := range banned {
			if strings.Contains(body, glyph) {
				t.Errorf("%s: uses the glyph %q as an icon; draw it as inline SVG", name, glyph)
			}
		}
	}
}

func TestCanvasLayout(t *testing.T) {
	var raw struct {
		Artboards   []map[string]any `json:"artboards"`
		Annotations []struct {
			ID string `json:"id"`
		} `json:"annotations"`
		Launch map[string]string `json:"launch"`
	}
	if err := json.Unmarshal([]byte(readFile(t, "canvas.json")), &raw); err != nil {
		t.Fatalf("canvas.json: %v", err)
	}

	type box struct {
		file       string
		x, y, w, h float64
	}
	var boxes []box
	listed := map[string]bool{}

	for _, a := range raw.Artboards {
		file, _ := a["file"].(string)
		if _, err := os.Stat(file); err != nil {
			t.Errorf("canvas.json lists %q, which does not exist", file)
			continue
		}
		listed[file] = true
		boxes = append(boxes, box{file, num(a["x"]), num(a["y"]), num(a["w"]), num(a["h"])})
	}
	for _, name := range artboards {
		if !listed[name] {
			t.Errorf("%s is not placed on the canvas; every artboard needs a deliberate spot", name)
		}
	}

	// Frames must not collide: the name strip and tweak chips sit above each
	// one, so a touching pair is already unreadable.
	for i := range boxes {
		for j := i + 1; j < len(boxes); j++ {
			a, b := boxes[i], boxes[j]
			if a.x < b.x+b.w+80 && b.x < a.x+a.w+80 && a.y < b.y+b.h+120 && b.y < a.y+a.h+120 {
				t.Errorf("frames %s and %s are too close", a.file, b.file)
			}
		}
	}

	seen := map[string]bool{}
	for _, n := range raw.Annotations {
		if n.ID == "" || seen[n.ID] {
			t.Errorf("annotation id %q is empty or repeated", n.ID)
		}
		seen[n.ID] = true
	}
	if raw.Launch["view"] != "canvas" {
		t.Errorf("launch view is %q, want canvas: the loop only reads as a sequence", raw.Launch["view"])
	}
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func between(t *testing.T, src, start, end string) string {
	t.Helper()
	i := strings.Index(src, start)
	if i < 0 {
		t.Fatalf("could not find %q in the theme source", start)
	}
	rest := src[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("could not find the end of %q", start)
	}
	return rest[:j]
}
