package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"time"

	"fyne.io/fyne/v2"

	"github.com/sirius/cogdebt/internal/ext"
)

// Seed fills the feed with representative content. It drives design review and
// demo screenshots without spending a model call.
func (s *Shell) Seed() {
	s.appendUser("I know Kubernetes, Go and distributed systems. I want to learn ML pipelines.")
	s.appendToolCall("profile_upsert")
	s.streamText("Saved your profile. Mapping ML infrastructure onto what you already hold.")
	s.appendToolCall("analogy")

	s.AppendView(ext.View(ext.ViewAnalogyTable, "", ext.AnalogyTableProps{Rows: []ext.AnalogyRow{
		{
			Source: "etcd", Target: "feature store", SharedRole: "source_of_truth",
			CarryOver: "Dual writes are split-brain. Caches and \"we'll recompute it in the script\" are not truth.",
			Breakdown: "etcd is small and strongly consistent. A feature store is a materialized view over dirty events, split into an offline and an online path. Point-in-time correctness is an invariant etcd never had.",
		},
		{
			Source: "rolling update", Target: "promotion in the model registry", SharedRole: "rollback",
			CarryOver: "Never overwrite :latest. Pin by digest, keep the previous generation warm.",
			Breakdown: "Rolling back a Deployment restores a deterministic binary. Rolling back a model does NOT restore the data distribution it was trained on — v[n-1] can be just as wrong.",
		},
		{
			Source: "liveness probe", Target: "drift detection", SharedRole: "degradation_signal",
			CarryOver: "\"Process is up\" is a vanity metric. You need a signal the control loop can consume.",
			Breakdown: "A probe is cheap, binary and contemporaneous. Labels arrive late, incomplete and biased — drift can look healthy while the business metric dies.",
		},
	}}))

	s.AppendView(ext.View(ext.ViewImage, "", ext.ImageProps{
		URL:     seedImage(),
		Caption: "etcd  maps to  feature store",
	}))

	s.AppendView(ext.View(ext.ViewQuestion, "q1", ext.QuestionProps{
		Level:  "L2",
		Prompt: "You said a model registry is etcd for models. Where does that analogy stop working?",
	}))

	// A preview reads better from the top; live use stays pinned to the newest.
	s.scroll.ScrollToTop()

	s.refreshMasteryWith([]ext.MasteryItem{
		{Label: "Kubernetes", Level: 0.82},
		{Label: "Go", Level: 0.74},
		{Label: "distributed systems", Level: 0.61},
		{Label: "feature store", Level: 0.18, Debt: 0.9},
		{Label: "point-in-time correctness", Level: 0.05, Debt: 1.4},
	})
}

// seedImage puts a diagram in the image cache and returns its url, so the
// preview exercises the real fetch-and-draw path without a network call and
// without spending an image generation.
func seedImage() string {
	const url = "cogdebt://seed/diagram.png"
	imageCache.Store(url, diagramPNG())
	return url
}

// diagramPNG draws the shape fal is asked for: two boxes, a link between them,
// and a break in the link. Deliberately crude -- it stands in for a generated
// picture, it does not pretend to be one.
func diagramPNG() []byte {
	const w, h = 800, 340
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := color.RGBA{R: 0x14, G: 0x16, B: 0x1A, A: 0xff}
	amber := color.RGBA{R: 0xE8, G: 0xA3, B: 0x3D, A: 0xff}
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	box := func(x0, y0, x1, y1 int) {
		for x := x0; x <= x1; x++ {
			img.Set(x, y0, amber)
			img.Set(x, y1, amber)
		}
		for y := y0; y <= y1; y++ {
			img.Set(x0, y, amber)
			img.Set(x1, y, amber)
		}
	}
	box(60, 110, 300, 230)
	box(500, 110, 740, 230)
	// The link, with a gap in the middle: the breakdown is part of the drawing.
	for x := 300; x <= 500; x++ {
		if x > 380 && x < 420 {
			continue
		}
		img.Set(x, 170, amber)
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// refreshMasteryWith paints the sidebar from explicit items.
func (s *Shell) refreshMasteryWith(items []ext.MasteryItem) {
	s.cfg.Mastery = func(context.Context) []ext.MasteryItem { return items }
	s.refreshMastery()
}

// Ask submits text as if the learner had typed it, once the window has settled.
// It drives scripted live runs, so the real streaming and card-rendering paths
// can be exercised and screenshotted without a human at the keyboard.
func (s *Shell) Ask(text string, after time.Duration) {
	go func() {
		time.Sleep(after)
		fyne.Do(func() {
			s.input.SetText(text)
			s.submit()
		})
	}()
}

// OpenSettings shows the configuration dialog after the window settles. It
// exists so a screenshot can show the dialog without a hand on the mouse.
func (s *Shell) OpenSettings(after time.Duration) {
	go func() {
		time.Sleep(after)
		fyne.Do(s.openSettings)
	}()
}

// RunAndCapture shows the window, saves a PNG once it has settled, and exits.
// The capture runs on the UI goroutine because the canvas belongs to it.
func (s *Shell) RunAndCapture(path string, after time.Duration) error {
	var captureErr error
	go func() {
		time.Sleep(after)
		fyne.Do(func() {
			defer s.app.Quit()
			f, err := os.Create(path)
			if err != nil {
				captureErr = err
				return
			}
			defer f.Close()
			if err := png.Encode(f, s.win.Canvas().Capture()); err != nil {
				captureErr = fmt.Errorf("encode %s: %w", path, err)
			}
		})
	}()
	s.win.ShowAndRun()
	return captureErr
}
