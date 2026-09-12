package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
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
