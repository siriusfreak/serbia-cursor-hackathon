package e2e_test

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/sirius/cogdebt/e2e"
	"github.com/sirius/cogdebt/internal/app"
)

func init() { app.LoadDotEnv("../.env") }

// Four fields, four different reasons to reach for a plugin.
//
// The point of spreading the suite across domains is not coverage for its own
// sake. Each field asks a different thing of the system: an algorithm can be
// checked by running it, biology has to be grounded in a source because the
// model's recall of it is not trustworthy, physics is where a picture does more
// than a paragraph, and distributed systems is where the learner's own repos
// say what they actually lean on. A tutor that can only do one of these is not
// a tutor, it is a chat window.

// TestAlgorithms exercises the sandbox: the grade comes from tests that ran.
func TestAlgorithms(t *testing.T) {
	r := e2e.Run(t, e2e.Scenario{
		Name: "algorithms",
		Learner: e2e.Learner{
			Knows:    []string{"Python", "pandas", "оконные функции в SQL"},
			Wants:    "динамическое программирование",
			Language: "Russian",
			Grasp:    0.4,
			Misconception: "Динамическое программирование — это просто рекурсия с кэшем. " +
				"Навесил @lru_cache на любую рекурсивную функцию — и это уже ДП.",
		},
		Opening: []string{
			"Я знаю Python, pandas и оконные функции в SQL. Хочу разобраться в динамическом программировании.",
			"Дай мне маленькую задачу на код по этой теме и проверь моё решение настоящими тестами, а не на глаз.",
		},
		Replies:  3,
		Needs:    []string{"DAYTONA_API_KEY"},
		MustCall: []string{"profile_upsert", "analogy", "assessor_next", "assessor_ask", "daytona_run_task"},
		Check: func(t *testing.T, r *e2e.Result) {
			// The whole argument for the sandbox is that a grade can be earned
			// rather than asserted. If the code never ran, the grade is an
			// opinion about prose and the plugin bought nothing.
			if r.Called("daytona_run_task") == 0 {
				t.Error("no code was executed, so every grade in this run is an opinion")
			}
		},
	})
	t.Logf("rungs asked: %v", r.Levels())
}

// TestBiology exercises retrieval: search by meaning, then read the page.
func TestBiology(t *testing.T) {
	e2e.Run(t, e2e.Scenario{
		Name: "biology",
		Learner: e2e.Learner{
			Knows:    []string{"микросервисы", "очереди сообщений", "ретраи и идемпотентность"},
			Wants:    "передача сигнала в клетке, каскад MAPK",
			Language: "Russian",
			Grasp:    0.5,
			Misconception: "Рецептор на мембране — это HTTP-эндпоинт: один сигнал даёт один ответ, " +
				"а если ответа нет, надо просто повторить запрос.",
		},
		Opening: []string{
			"Я знаю микросервисы, очереди сообщений, ретраи и идемпотентность. Хочу разобраться в передаче сигнала в клетке.",
			"Найди приличный источник про каскад MAPK, прочитай его и опирайся на него, а не на память.",
		},
		Replies:  3,
		Needs:    []string{"EXA_API_KEY", "FIRECRAWL_API_KEY"},
		MustCall: []string{"analogy", "exa_search", "assessor_ask"},
		Check: func(t *testing.T, r *e2e.Result) {
			// Search alone is a list of titles. Grounding means something was
			// actually read, which is the second call.
			if r.Called("firecrawl_fetch") == 0 && r.Called("exa_search") > 0 {
				t.Log("searched but never opened a page: the answer is grounded in snippets only")
			}
		},
	})
}

// TestPhysics exercises generation: the mapping comes back as a picture.
func TestPhysics(t *testing.T) {
	e2e.Run(t, e2e.Scenario{
		Name: "physics",
		Learner: e2e.Learner{
			Knows:    []string{"rate limiting", "backpressure", "queueing theory"},
			Wants:    "entropy and the second law of thermodynamics",
			Language: "English",
			Grasp:    0.5,
			Misconception: "Entropy is just disorder. A tidy room has low entropy, so entropy is " +
				"basically a measure of how messy something is.",
		},
		Opening: []string{
			"I know rate limiting, backpressure and queueing theory. I want to understand entropy and the second law.",
			"I think in pictures — draw me the mapping so I can see it.",
		},
		Replies:  3,
		Needs:    []string{"FAL_KEY"},
		MustCall: []string{"analogy", "fal_illustrate", "assessor_ask"},
		Check: func(t *testing.T, r *e2e.Result) {
			if len(r.Images) == 0 {
				t.Fatal("no image url came back")
			}
			// A url is not a picture. The renderer downloads it, so the
			// scenario checks the same thing the renderer depends on.
			resp, err := http.Head(r.Images[0])
			if err != nil {
				t.Fatalf("the generated image is not reachable: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 300 {
				t.Errorf("image url answered %s", resp.Status)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
				t.Errorf("image url serves %q, which the renderer cannot draw", ct)
			}
		},
	})
}

// TestDistributedSystems exercises measurement: cognitive debt from real repos.
func TestDistributedSystems(t *testing.T) {
	e2e.Run(t, e2e.Scenario{
		Name: "distributed-systems",
		Learner: e2e.Learner{
			Knows:    []string{"Go", "PostgreSQL", "HTTP backends"},
			Wants:    "consensus and Raft",
			Language: "English",
			Grasp:    0.6,
			Misconception: "A Raft cluster is really just a primary with replicas. As long as writes go " +
				"to one node, the rest is an implementation detail of replication.",
		},
		Opening: []string{
			// A github login is what turns self-report into measurement: the
			// scan says what this person actually leans on, which is the input
			// the debt formula has been missing.
			"I work in Go and Postgres, mostly HTTP backends. My GitHub is golang. I want to learn consensus and Raft.",
		},
		Replies:  4,
		MustCall: []string{"github_scan", "analogy", "assessor_ask"},
		Check: func(t *testing.T, r *e2e.Result) {
			if r.Called("profile_set_frequency") == 0 {
				t.Error("the scan ran but its frequencies were never stored, so cognitive debt stays at zero " +
					"and the sidebar is still showing guesses")
			}
			var withDebt int
			for _, m := range r.Mastery {
				if m.Debt > 0 {
					withDebt++
				}
			}
			if withDebt == 0 {
				t.Error("nothing carries debt after a repo scan; the formula is running on frequency 0")
			}
		},
	})
}

// TestMain writes the cross-scenario index once every scenario has run.
func TestMain(m *testing.M) {
	code := m.Run()
	e2e.WriteIndex()
	os.Exit(code)
}
