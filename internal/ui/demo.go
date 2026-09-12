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
	s.appendUser("Я знаю Kubernetes, Go и распределённые системы. Хочу изучить ML-пайплайны.")
	s.appendToolCall("profile_upsert")
	s.streamText("Сохранил твой профиль. Разбираю ML-инфраструктуру через то, что ты уже держишь в голове.")
	s.appendToolCall("analogy")

	s.AppendView(ext.View(ext.ViewAnalogyTable, "", ext.AnalogyTableProps{Rows: []ext.AnalogyRow{
		{
			Source: "etcd", Target: "feature store", SharedRole: "source_of_truth",
			CarryOver: "Двойная запись — это split-brain. Кеши и «пересчитаем в скрипте» истиной не являются.",
			Breakdown: "etcd мал и строго консистентен. Feature store — материализованная витрина над грязными событиями, с раздельным офлайн- и онлайн-путём. Point-in-time корректности у etcd никогда не было.",
		},
		{
			Source: "rolling update", Target: "promotion в model registry", SharedRole: "rollback",
			CarryOver: "Никогда не перезаписывай :latest. Пинуй по digest, держи прошлое поколение тёплым.",
			Breakdown: "Откат Deployment возвращает детерминированный бинарь. Откат модели НЕ возвращает распределение данных, на котором она обучалась — v[n-1] может быть так же неправа.",
		},
		{
			Source: "liveness probe", Target: "drift detection", SharedRole: "degradation_signal",
			CarryOver: "«Процесс жив» — метрика тщеславия. Нужен сигнал, который потребляет контур управления.",
			Breakdown: "Проба дешёвая, бинарная и мгновенная. Метки приходят поздно, неполно и смещённо — дрейф выглядит здоровым, пока бизнес-метрика умирает.",
		},
	}}))

	s.AppendView(ext.View(ext.ViewQuestion, "q1", ext.QuestionProps{
		Level:  "L2",
		Prompt: "Ты сказал, что model registry — это etcd для моделей. Где эта аналогия перестаёт работать?",
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
