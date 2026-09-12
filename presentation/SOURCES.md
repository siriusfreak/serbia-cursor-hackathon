# Sources

Every claim on slide 2 comes from this list. Open any link and check it.

## Method

We searched one Hacker News archive for these terms:

```
cognitive debt · deskilling · skill atrophy · skill rot · atrophy
forgetting how to code · AI reliance · losing skills · juniors
expertise · savviness · illusion of competence · critical thinking
```

The search returned 39 stories. We read each one and kept the stories that are
about engineers losing technical skill, about cognitive debt, or about the
training of junior engineers. Nineteen stories remained. They were published
between 26 April and 30 August 2026.

We removed twenty stories that matched a search term but have another subject.
Examples: a story about spinal muscular atrophy, a weather forecast, and a story
about font design.

This is a count from a keyword search, not a complete census of Hacker News. A
different set of search terms will return a different number.

## The six stories on slide 2

| date | story | Hacker News |
|---|---|---|
| 2026-06-19 | Is AI ruining our skills? Early results are in — and they're not good (*Nature*) | [48601286](https://news.ycombinator.com/item?id=48601286) |
| 2026-06-25 | Ford rehires 350 engineers after AI fails to preserve expertise or train juniors (*Bloomberg*) | [48674446](https://news.ycombinator.com/item?id=48674446) |
| 2026-08-13 | Understanding is the new bottleneck | [49290299](https://news.ycombinator.com/item?id=49290299) |
| 2026-08-24 | Coding expertise is going to collapse from AI reliance | [49421554](https://news.ycombinator.com/item?id=49421554) |
| 2026-08-29 | LLMs are making me lose my savviness | [49492184](https://news.ycombinator.com/item?id=49492184) |
| 2026-08-30 | No AI Fridays | [49498095](https://news.ycombinator.com/item?id=49498095) |

## The other thirteen

| date | story | Hacker News |
|---|---|---|
| 2026-04-26 | The West Forgot How to Make Things. Now It's Forgetting How to Code | [47907879](https://news.ycombinator.com/item?id=47907879) |
| 2026-04-26 | A.I. is creating engineers who can't think without it | [47913650](https://news.ycombinator.com/item?id=47913650) |
| 2026-04-26 | If You Stop Hiring Juniors, Your Senior Engineers Own You | [47913641](https://news.ycombinator.com/item?id=47913641) |
| 2026-05-04 | Agentic Coding Is a Trap | [48002442](https://news.ycombinator.com/item?id=48002442) |
| 2026-05-05 | What I'm Hearing About Cognitive Debt (So Far) | [48017298](https://news.ycombinator.com/item?id=48017298) |
| 2026-05-14 | God Damn AI is making me dumb | [48139148](https://news.ycombinator.com/item?id=48139148) |
| 2026-05-29 | Is AI causing a repeat of Front end's Lost Decade? | [48321631](https://news.ycombinator.com/item?id=48321631) |
| 2026-05-29 | Expertise in the Age of AI | [48322929](https://news.ycombinator.com/item?id=48322929) |
| 2026-06-15 | Show HN: Fata — Spaced repetition to fight skill rot from AI coding | [48489163](https://news.ycombinator.com/item?id=48489163) |
| 2026-06-28 | Reflections on Software Engineering in the Age of AI | [48708721](https://news.ycombinator.com/item?id=48708721) |
| 2026-07-04 | AI has torched the market for junior programmers | [48788361](https://news.ycombinator.com/item?id=48788361) |
| 2026-08-03 | Prevent cognitive debt by manually retyping LLM-generated code | [49153374](https://news.ycombinator.com/item?id=49153374) |
| 2026-08-26 | Beyond Recall and the Illusion of Competence | [49446442](https://news.ycombinator.com/item?id=49446442) |

## Claims on other slides

**Slide 3 — "Professor Margaret-Anne Storey defined this term."**
Her post *What I'm Hearing About Cognitive Debt (So Far)* is in the list above.
The post responds to reactions to her earlier definition of the term.

**Slide 4 — the three available solutions.**
All three are in the list above, or are the subject of a story in it:

- *No AI Fridays* — the team of the HTMX author disables AI assistants one day
  each week.
- *Prevent cognitive debt by manually retyping LLM-generated code*.
- *Show HN: Fata — Spaced repetition to fight skill rot from AI coding*.

**Slide 5 — matching by structural role.**
The method is structure mapping, from Dedre Gentner's work on analogy. The
repository implements it in `exts/analogy`.

**Slide 7 — the three examples.**
These are the output of live runs of this application against `grok-4.6`. We did
not edit the sentences. The Daytona test result is the output of a real sandbox
run.

**The 120 s to 34 s measurement** is no longer on a slide, but it is real:
measured with the tracing in `internal/obs`, on one turn of the same
conversation, before and after we changed the model of the analogy agent.
`AGENTS.md` records the numbers and the reason. Use it if a judge asks how the
agents are tuned.
