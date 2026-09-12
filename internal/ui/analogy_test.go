package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/sirius/cogdebt/internal/ext"
)

func oneAnalogy() ext.ViewSpec {
	return ext.View(ext.ViewAnalogyTable, "", ext.AnalogyTableProps{Rows: []ext.AnalogyRow{{
		Source: "etcd", Target: "feature store", SharedRole: "source_of_truth",
		Breakdown: "point-in-time correctness is an invariant etcd never had",
	}}})
}

func buttons(obj fyne.CanvasObject) map[string]*widget.Button {
	found := map[string]*widget.Button{}
	walk(obj, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok {
			found[b.Text] = b
		}
	})
	return found
}

// TestEveryAnalogyIsADoor: a pair the learner cares about has to be able to
// become the next turn, or the only way forward is whatever the assessor
// happens to pick.
func TestEveryAnalogyIsADoor(t *testing.T) {
	test.NewApp()
	var got []ext.ViewEvent
	obj := Render(oneAnalogy(), func(ev ext.ViewEvent) { got = append(got, ev) })

	found := buttons(obj)
	dig, ok := found["Dig deeper"]
	if !ok {
		t.Fatalf("no dig button on the card; buttons were %v", keysOf(found))
	}
	ask, ok := found["Ask me"]
	if !ok {
		t.Fatalf("no ask button on the card; buttons were %v", keysOf(found))
	}

	dig.OnTapped()
	ask.OnTapped()
	if len(got) != 2 {
		t.Fatalf("got %d events from two taps", len(got))
	}
	if got[0].Event != ext.EventDigDeeper || got[1].Event != ext.EventAskMe {
		t.Fatalf("events were %q and %q", got[0].Event, got[1].Event)
	}
	var p ext.PairPayload
	if json.Unmarshal(got[0].Payload, &p) != nil || p.Source != "etcd" || p.Target != "feature store" {
		t.Errorf("the event does not say which pair it is about: %s", got[0].Payload)
	}
}

// A preview or a screenshot passes no emit; the card must stay a card.
func TestAReadOnlyAnalogyHasNoButtons(t *testing.T) {
	test.NewApp()
	if found := buttons(Render(oneAnalogy(), nil)); len(found) != 0 {
		t.Errorf("a read-only analogy rendered buttons: %v", keysOf(found))
	}
}

// TestADoorSpeaksTheLearnersLanguage guards the thing that would quietly ruin
// a Russian demo: an English button emitting English prose makes the tutor
// answer in English from then on, because it replies in the language it is
// addressed in.
func TestADoorSpeaksTheLearnersLanguage(t *testing.T) {
	// Latin concept names, so the only Cyrillic that can appear is the
	// sentence the door writes around them.
	pair := ext.PairPayload{Source: "goroutines", Target: "wave function"}

	ru := phrase(ext.EventAskMe, pair, true)
	if !strings.ContainsAny(ru, "абвгдежзийклмнопрстуфхцчшщэюя") {
		t.Errorf("a learner writing Russian got %q", ru)
	}
	en := phrase(ext.EventAskMe, pair, false)
	if strings.ContainsAny(en, "абвгдежзийклмнопрстуфхцчшщэюя") {
		t.Errorf("a learner writing English got %q", en)
	}
	// Concept names are quoted verbatim in either language: a technical term
	// translated into the sentence is a different term.
	mixed := ext.PairPayload{Source: "горутины", Target: "wave function"}
	for _, got := range []string{phrase(ext.EventAskMe, mixed, true), phrase(ext.EventAskMe, mixed, false)} {
		if !strings.Contains(got, "горутины") || !strings.Contains(got, "wave function") {
			t.Errorf("the message does not name the pair: %q", got)
		}
	}
	// The two doors must ask for different things.
	if phrase(ext.EventAskMe, pair, true) == phrase(ext.EventDigDeeper, pair, true) {
		t.Error("dig deeper and ask me produce the same message")
	}
}

func TestScriptIsRememberedFromWhatTheLearnerTypes(t *testing.T) {
	s := &Shell{}
	if s.cyrillic {
		t.Fatal("the default is not Latin")
	}
	s.noteScript("I know Go and Postgres")
	if s.cyrillic {
		t.Error("an English message was read as Cyrillic")
	}
	s.noteScript("Я знаю Go и Postgres")
	if !s.cyrillic {
		t.Error("a Russian message was not noticed, so the doors will answer in English")
	}
}

// TestAnEventBecomesAMessage covers the seam: a tap has to turn into something
// the learner can see they said.
func TestAnEventBecomesAMessage(t *testing.T) {
	s := &Shell{}
	pair, _ := json.Marshal(ext.PairPayload{Source: "etcd", Target: "feature store"})

	for _, ev := range []string{ext.EventDigDeeper, ext.EventAskMe} {
		if got := s.messageFor(ext.ViewEvent{Event: ev, Payload: pair}); got == "" {
			t.Errorf("%s produced no message", ev)
		}
	}
	// A malformed or unknown event must be dropped, not submitted as noise.
	if got := s.messageFor(ext.ViewEvent{Event: ext.EventDigDeeper}); got != "" {
		t.Errorf("an event with no pair produced %q", got)
	}
	if got := s.messageFor(ext.ViewEvent{Event: "who_knows", Payload: pair}); got != "" {
		t.Errorf("an unknown event produced %q", got)
	}
	// A typed answer still works.
	answer, _ := json.Marshal(map[string]string{"answer": "the replicated log"})
	if got := s.messageFor(ext.ViewEvent{Event: ext.EventSubmit, Payload: answer}); got != "the replicated log" {
		t.Errorf("a typed answer came through as %q", got)
	}
}

func keysOf(m map[string]*widget.Button) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func aQuestion(id, level string) ext.ViewSpec {
	return ext.View(ext.ViewQuestion, id, ext.QuestionProps{Level: level, Prompt: "what plays that role?"})
}

func entries(obj fyne.CanvasObject) int {
	n := 0
	walk(obj, func(o fyne.CanvasObject) {
		switch o.(type) {
		case *widget.Entry, *widget.RadioGroup:
			n++
		}
	})
	return n
}

// TestAnAnsweredQuestionStopsBeingAForm: a card left live collects an empty
// input under every question in the history, and can be answered a second time
// — which the assessor would grade as a second attempt at a question it asked
// once.
func TestAnAnsweredQuestionStopsBeingAForm(t *testing.T) {
	test.NewApp()
	var got []ext.ViewEvent
	card, _ := renderQuestionCard(aQuestion("q1", "L2"), func(ev ext.ViewEvent) { got = append(got, ev) })

	answer := buttons(card)["Answer"]
	if answer == nil {
		t.Fatal("no Answer button on a fresh question")
	}
	if entries(card) != 1 {
		t.Fatal("a fresh question has no input")
	}

	// Fyne's Entry cannot be typed into headlessly, so drive the control the
	// way the button does and check the card is spent either way.
	walk(card, func(o fyne.CanvasObject) {
		if e, ok := o.(*widget.Entry); ok {
			e.SetText("the replicated log")
		}
	})
	answer.OnTapped()

	if len(got) != 1 || got[0].NodeID != "q1" {
		t.Fatalf("the answer did not reach the host: %v", got)
	}
	if entries(card) != 0 {
		t.Error("the input is still there after answering")
	}
	if b := buttons(card); len(b) != 0 {
		t.Errorf("the card can still be answered again: %v", keysOf(b))
	}
	if !strings.Contains(allText(card), "what plays that role?") {
		t.Error("the question itself was removed; the history should keep what was asked")
	}
	if !strings.Contains(allText(card), "L2") {
		t.Error("the rung was removed from the record")
	}
}

// An empty answer must not spend the card.
func TestAnEmptyAnswerLeavesTheCardOpen(t *testing.T) {
	test.NewApp()
	var got []ext.ViewEvent
	card, _ := renderQuestionCard(aQuestion("q1", "L1"), func(ev ext.ViewEvent) { got = append(got, ev) })

	buttons(card)["Answer"].OnTapped()
	if len(got) != 0 {
		t.Error("an empty answer was submitted")
	}
	if entries(card) != 1 {
		t.Error("an empty answer retired the card")
	}
}

// TestANewQuestionRetiresTheLast: the assessor parks one question at a time, so
// at most one card on screen can be answerable.
//
// The shell is assembled by hand rather than through New, which builds a real
// window and blocks without a driver to run it.
func TestANewQuestionRetiresTheLast(t *testing.T) {
	test.NewApp()
	feed := container.NewVBox()
	s := &Shell{feed: feed, scroll: container.NewVScroll(feed), streaming: &strings.Builder{}, streamAt: -1}

	s.AppendView(aQuestion("q1", "L1"))
	s.AppendView(aQuestion("q2", "L2"))
	if open := openCards(feed); open != 1 {
		t.Errorf("%d answerable cards after a second question arrived, want 1", open)
	}

	// Answering in the message box below retires whatever is still open.
	s.retireQuestions()
	if open := openCards(feed); open != 0 {
		t.Errorf("%d answerable cards after the learner answered elsewhere, want 0", open)
	}
}

func openCards(feed *fyne.Container) int {
	n := 0
	for _, obj := range feed.Objects {
		n += entries(obj)
	}
	return n
}
