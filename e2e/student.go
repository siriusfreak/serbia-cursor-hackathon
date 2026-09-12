package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"github.com/sirius/cogdebt/internal/app"
)

// StudentModel answers as the learner. It is non-reasoning on purpose: the
// student is playing a part, not solving the problem, and a reasoning model
// both costs more and quietly outperforms the persona it was asked to play.
var StudentModel = envOr("COGDEBT_STUDENT_MODEL", "grok-4.20-0309-non-reasoning")

// Learner is the persona on the other side of the conversation.
type Learner struct {
	// Knows is the field they already hold, in their own words.
	Knows []string
	// Wants is the topic they came to learn.
	Wants string
	// Misconception is the borrowed intuition they are carrying: the thing
	// their own field taught them that does NOT transfer.
	//
	// This is the load-bearing field. A student who is simply right about
	// everything grades 1.0 on every rung and the ladder never has to work; a
	// student holding a specific wrong belief is what makes L2 -- "where does
	// the analogy break" -- do anything at all.
	Misconception string
	// Language is what they write in, e.g. "Russian" or "English".
	Language string
	// Grasp is 0..1: how readily they update when corrected. Low means the
	// misconception survives several turns, which is the realistic case.
	Grasp float64
}

// Student plays the learner against the tutor.
type Student struct {
	runner  *runner.Runner
	session string
}

// NewStudent builds the simulated learner.
func NewStudent(ctx context.Context, l Learner) (*Student, error) {
	model, err := app.BuildNamedModel(ctx, StudentModel)
	if err != nil {
		return nil, fmt.Errorf("build student model: %w", err)
	}
	ag, err := llmagent.New(llmagent.Config{
		Name:        "student",
		Description: "A simulated learner.",
		Instruction: l.instruction(),
		Model:       model,
	})
	if err != nil {
		return nil, fmt.Errorf("build student agent: %w", err)
	}
	r, err := runner.New(runner.Config{
		AppName:           "cogdebt-student",
		Agent:             ag,
		SessionService:    session.InMemoryService(),
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, fmt.Errorf("build student runner: %w", err)
	}
	return &Student{runner: r, session: "student"}, nil
}

// Answer replies to whatever the tutor last said.
func (s *Student) Answer(ctx context.Context, tutorSaid string) (string, error) {
	if strings.TrimSpace(tutorSaid) == "" {
		tutorSaid = "(the tutor said nothing; ask them to continue)"
	}
	var b strings.Builder
	msg := genai.NewContentFromText(tutorSaid, genai.RoleUser)
	for ev, err := range s.runner.Run(ctx, "student", s.session, msg, agent.RunConfig{}) {
		if err != nil {
			return "", err
		}
		if ev == nil || ev.Content == nil {
			continue
		}
		for _, part := range ev.Content.Parts {
			if part.Thought || part.Text == "" {
				continue
			}
			b.WriteString(part.Text)
		}
	}
	answer := strings.TrimSpace(b.String())
	if answer == "" {
		return "", fmt.Errorf("the student produced nothing")
	}
	return answer, nil
}

// instruction turns the persona into a brief.
//
// The hard part is not making the student sound human, it is stopping the
// model from being a good student. Left alone it answers every question
// correctly and in full, the assessor grades 1.0 throughout, and the run proves
// only that the happy path is happy. The negative instructions below are what
// buy a run with something in it.
func (l Learner) instruction() string {
	lang := l.Language
	if lang == "" {
		lang = "English"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You are a working engineer being tutored. Write in %s, in the first person.\n\n", lang)
	fmt.Fprintf(&b, "What you already know well: %s.\n", strings.Join(l.Knows, ", "))
	fmt.Fprintf(&b, "What you came to learn: %s. You are a beginner at it.\n\n", l.Wants)

	if l.Misconception != "" {
		fmt.Fprintf(&b, "You hold this belief, carried over from your own field, and you believe it firmly:\n  %q\n", l.Misconception)
		b.WriteString("Defend it when it comes up. Give it up only if the tutor shows you a concrete case where it fails -- ")
		b.WriteString("not merely because they assert it is wrong.\n\n")
	}

	b.WriteString("How to answer:\n")
	b.WriteString("- Two or three sentences. You are typing into a chat box, not writing an essay.\n")
	b.WriteString("- Answer with what you actually think, including when it is wrong or half-formed.\n")
	b.WriteString("- Reach for the field you know when you do not know the answer: that is exactly how you think.\n")
	b.WriteString("- If you do not understand a question, say what confuses you instead of guessing cleanly.\n")
	if l.Grasp < 0.5 {
		b.WriteString("- You pick things up slowly. A correction rarely lands the first time.\n")
	} else {
		b.WriteString("- You pick things up quickly once you see a concrete case.\n")
	}
	b.WriteString("- If asked to write code, write it, and write it the way you would on a first attempt.\n\n")

	b.WriteString("Never break character: no mention of being a model, a simulation or an assistant. ")
	b.WriteString("Never offer to help the tutor. Never ask what they would like to cover next -- you are the one being taught.")
	return b.String()
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
