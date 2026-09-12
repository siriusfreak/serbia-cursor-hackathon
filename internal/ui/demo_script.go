package ui

import (
	"time"

	"github.com/sirius/cogdebt/internal/ext"
)

// HappyPath is the scripted run: one skill in, the full L1-L4 climb out.
//
// It shows the thing that is hard to show live in three minutes — the ladder
// actually climbing — and it shows it honestly. The learner names ONE skill,
// because that is the only way a single concept gathers enough mastery to reach
// L4; a demo that opened with three skills would rotate between them and stall
// at L2, which is correct behaviour and a bad demo. See exts/assessor.
func HappyPath() []Step {
	// The renderer reads the cache before it reaches for the network, so the
	// embedded diagram simply is the answer at that URL.
	imageCache.Store(demoDiagramURL, demoDiagram)

	view := func(v ext.ViewSpec) *ext.ViewSpec { return &v }
	analogy := func(rows ...ext.AnalogyRow) *ext.ViewSpec {
		return view(ext.View(ext.ViewAnalogyTable, "", ext.AnalogyTableProps{Rows: rows}))
	}
	question := func(id, level, prompt string) *ext.ViewSpec {
		return view(ext.View(ext.ViewQuestion, id, ext.QuestionProps{Level: level, Prompt: prompt}))
	}
	// Mastery after each graded answer, from the real damping: 0.34, 0.56,
	// 0.71, 0.81. Frequency 1.0 on Postgres comes from the repository scan, so
	// the debt bar starts full and drains as the answers land.
	progress := func(level float64) []ext.MasteryItem {
		return []ext.MasteryItem{
			{Label: "PostgreSQL", Level: level, Debt: 1 - level},
			{Label: "Raft", Level: level * 0.6},
		}
	}

	return []Step{
		{Wait: 2 * time.Second,
			Learner: "The only thing I know really well is PostgreSQL. I want to learn Raft."},
		{Wait: 1200 * time.Millisecond, Tool: "profile_upsert", Thinking: true},
		{Wait: 1500 * time.Millisecond, Tool: "github_scan", Thinking: true},
		{Wait: 1200 * time.Millisecond, Tool: "profile_set_frequency", Thinking: true},
		{Wait: 1500 * time.Millisecond,
			Text:    "Your repositories say you live in Postgres. That is the stake: everything you owe on Raft, you owe in a language you already speak.",
			Mastery: []ext.MasteryItem{{Label: "PostgreSQL", Level: 0, Debt: 1.0}}},

		{Wait: 3500 * time.Millisecond, Tool: "analogy", Thinking: true},
		{Wait: 3 * time.Second, View: analogy(
			ext.AnalogyRow{
				Source: "the write-ahead log", Target: "the replicated log", SharedRole: "ordered_change_propagation",
				CarryOver: "Append before you apply. Order is the whole guarantee, and a follower catches up by replaying it.",
				Breakdown: "A Raft entry carries a term. The WAL has no notion of an election, so nothing in it can reject a writer who was the leader a moment ago.",
			},
			ext.AnalogyRow{
				Source: "the primary", Target: "the leader", SharedRole: "source_of_truth",
				CarryOver: "One writer. Everyone else follows and may lag.",
				Breakdown: "Postgres has one primary because something outside it says so. Raft elects by majority and stamps a term — it cannot have two, by construction. Yours can.",
			},
			ext.AnalogyRow{
				Source: "synchronous commit", Target: "commitIndex", SharedRole: "durability_barrier",
				CarryOver: "An acknowledgement means it survived somewhere other than the writer's memory.",
				Breakdown: "synchronous_standby_names counts the standbys you named. Raft counts a majority of the cluster, and a leader may not commit an entry from an earlier term by counting replicas alone.",
			})},

		{Wait: 12 * time.Second,
			Learner: "Show me that first pair as a picture — I think visually."},
		{Wait: 2 * time.Second, Tool: "fal_illustrate", Thinking: true},
		{Wait: 2500 * time.Millisecond, View: view(ext.View(ext.ViewImage, "", ext.ImageProps{
			URL:     demoDiagramURL,
			Caption: "the write-ahead log  maps to  the replicated log",
		}))},

		{Wait: 5 * time.Second, Tool: "assessor_next", Thinking: true},
		{Wait: 1200 * time.Millisecond, Tool: "assessor_ask", Thinking: true},
		{Wait: 1200 * time.Millisecond, View: question("q1", "L1",
			"In Postgres the WAL is the ordered, durable record of every write the primary accepted. What plays that role in Raft?")},

		{Wait: 9 * time.Second,
			Learner: "The replicated log. Every state change is appended to it before it's applied, and followers replay it to catch up."},
		{Wait: 1500 * time.Millisecond, Tool: "assessor_grade", Thinking: true},
		{Wait: 1200 * time.Millisecond, Tool: "assessor_next", Thinking: true},
		{Wait: 1200 * time.Millisecond, Tool: "assessor_ask", Thinking: true},
		{Wait: 1500 * time.Millisecond,
			Text:    "That is the transfer. Now the rung that does the teaching.",
			Mastery: progress(0.34)},
		{Wait: 1200 * time.Millisecond, View: question("q2", "L2",
			"You called the leader a primary. Where does that stop working?")},

		{Wait: 11 * time.Second,
			Learner: "Postgres has exactly one primary, but that's enforced from outside — failover is manual or Patroni's job, and two primaries can both take writes. Raft enforces it in the protocol: a leader needs a majority vote and carries a term, so an old leader's AppendEntries get rejected the moment it comes back. Postgres can split-brain; Raft can't."},
		{Wait: 1500 * time.Millisecond, Tool: "assessor_grade", Thinking: true},
		{Wait: 1200 * time.Millisecond, Tool: "assessor_next", Thinking: true},
		{Wait: 1200 * time.Millisecond, Tool: "assessor_ask", Thinking: true},
		{Wait: 1800 * time.Millisecond,
			Text:    "You found the seam yourself. The analogy has done its work — from here, Raft on its own terms.",
			Mastery: progress(0.56)},
		{Wait: 1200 * time.Millisecond, View: question("q3", "L3",
			"No analogy this time. When is an entry committed, and what may a leader NOT do with an entry from a previous term?")},

		{Wait: 11 * time.Second,
			Learner: "Committed once the leader has replicated it to a majority; then commitIndex advances and everything up to it is applied to the state machine. It may not commit an entry from an earlier term just by counting replicas — it has to commit one from its own term first."},
		{Wait: 1500 * time.Millisecond, Tool: "assessor_grade", Thinking: true},
		{Wait: 1200 * time.Millisecond, Tool: "assessor_next", Thinking: true},
		{Wait: 1200 * time.Millisecond, Tool: "assessor_ask", Thinking: true},
		{Wait: 1200 * time.Millisecond, Mastery: progress(0.71)},
		{Wait: 1200 * time.Millisecond, View: question("q4", "L3",
			"A follower rejects AppendEntries. What did the leader get wrong, and what does it do next?")},

		{Wait: 9 * time.Second,
			Learner: "Its prevLogIndex/prevLogTerm didn't match the follower's log. It walks nextIndex back until the two agree, then overwrites everything after that point."},
		{Wait: 1500 * time.Millisecond, Tool: "assessor_grade", Thinking: true},
		{Wait: 1200 * time.Millisecond, Tool: "assessor_next", Thinking: true},
		{Wait: 2 * time.Second,
			Text:    "Four rungs in four answers. The last one is not a question — you write it, and the tests decide.",
			Mastery: progress(0.81)},

		{Wait: 1200 * time.Millisecond, Tool: "assessor_ask", Thinking: true},
		{Wait: 1500 * time.Millisecond, View: question("q5", "L4",
			"Write commit_index(match_index, current_term, log) returning the highest index safe to commit. Three nodes, majority of two. Hidden tests include a leader trying to commit an entry from an earlier term.")},

		{Wait: 11 * time.Second,
			Learner: "def commit_index(match, term, log):\n    for n in range(len(log) - 1, -1, -1):\n        if sum(1 for m in match if m >= n) >= 2 and log[n].term == term:\n            return n\n    return -1"},
		{Wait: 2500 * time.Millisecond, Tool: "daytona_run_task", Thinking: true},
		{Wait: 3 * time.Second,
			Text: "```\npassed=3 failed=0 in 1840ms\n```\n\nIncluding the one that fails if you commit an entry from an earlier term on a majority alone. You wrote the term check without being told to — that is the borrowed intuition gone.",
			Mastery: []ext.MasteryItem{
				{Label: "PostgreSQL", Level: 0.81, Debt: 0.19},
				{Label: "Raft", Level: 0.62},
			}},
	}
}
