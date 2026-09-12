package ui

import (
	"fmt"
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

// refreshMasteryWith paints the sidebar from explicit items.
func (s *Shell) refreshMasteryWith(items []ext.MasteryItem) {
	rows := make([]fyne.CanvasObject, 0, len(items))
	for _, it := range items {
		rows = append(rows, progressRow(s.pal, it.Label, it.Level, it.Debt))
	}
	s.side.Objects = rows
	s.side.Refresh()
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
