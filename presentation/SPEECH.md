# Speech — cogdebt

Six minutes, twenty-two slides. Written to be said out loud, not read off the
screen: the deck carries the evidence, you carry the argument.

The same notes are in the deck itself — press **N** while presenting.

**Rules for the room.** Never read a slide aloud; the audience reads faster than
you talk. Slide 3 is a wall on purpose — point, do not recite. If you are losing
time, cut slides 18 and 20 and go straight from the demo to the team. Never cut
slide 10; it is the one idea nobody else in the room will have said.

---

## 1 · Title

> "Every system we build has a weakest link. Ours is not the database, and it is
> not the model. It is the person who is supposed to understand what we shipped.
>
> We built cogdebt for that person."

Two sentences, then move. Do not explain the name yet — slide 5 does that, and
it lands harder once they have seen the problem.

## 2 · The problem

> "We have spent two years making the machine half of engineering faster. The
> half that *understands* the system runs at exactly the speed it always did —
> and it is now being asked to keep up with code it never wrote.
>
> That is not our thesis. Engineers have been saying it out loud all year."

## 3 · The evidence

Point, do not read. Three fingers:

> "Nature, June — so this is measured now, not felt.
>
> Ford, also June — they rehired three hundred and fifty engineers, because the
> AI could not hold the institutional knowledge and the juniors had nobody to
> learn from. That one already has a price tag.
>
> And the last one — *No AI Fridays* — is the creator of HTMX making his team
> switch the assistants off one day a week. That tells you how it feels from
> inside a good team."

Then:

> "Twelve headlines, five months, one archive. My side project scrapes Hacker
> News — eight thousand eight hundred stories — and this is what the industry
> has been talking itself into all year."

## 4 · It is measured

> "Two independent studies. Polish endoscopists in the Lancet. Programmers, by
> Anthropic. Different professions, same result: once you habituate to the
> assistant, you are worse at the task without it.
>
> This is the first measurable deskilling effect of this generation of tools."

Say the last sentence slowly. This is the slide that turns a sceptical room.

## 5 · The name

> "It has a name already. Cognitive debt — Professor Margaret-Anne Storey. The
> gap between a system that keeps evolving and the understanding the people
> responsible for it actually hold.
>
> Debt is the right word. It compounds quietly, it is invisible until the day
> you need the money — and it can be paid down. That last part is what nobody
> is building."

## 6 · What is on offer today

> "Here is everything on the market. Turn the AI off one day a week. Forbid the
> agent from writing files and retype every line by hand. Flashcards.
>
> These are real products and real practices, and they are honest attempts. But
> look at the shape they share: every one of them takes something away. Your
> velocity, your tooling, or your evening.
>
> Nobody is paying down the debt. They are just refusing to borrow more."

## 7 · Our bet

> "So do not take the tool away. Pay the debt with what the engineer already
> owns.
>
> A senior engineer is not empty. They are carrying a deep, load-bearing model
> of *some* field. The fastest way into a new one is not a tutorial — it is a
> correspondence to the one already in their head.
>
> Two days ago, Hofstadter on analogy as the core of cognition hit the front
> page of the same archive. The community that named the disease pointed at the
> cure in the same week."

## 8 · Debt is a number

> "We made the debt a number. What you lean on, times how much rests on it,
> times how little of it you hold.
>
> The first term is the honest one. We do not ask you what you are good at —
> everyone flatters themselves. We scan your public repositories and read it off
> your actual work. That turns *I feel rusty* into a ranked list."

## 9 · The core mechanic

> "The mapping is matched on structural role, never on names. Not 'both have a
> controller'. 'Both are the single source of truth everything else reconciles
> against.'
>
> That is what gives you etcd to a feature store, and a liveness probe to drift
> detection."

Point at the **orange** line, not the top line.

> "And the orange line is the actual product."

## 10 · The one invariant

Slow down. This is the idea.

> "An analogy with no stated limit does not pay down cognitive debt. It creates
> more.
>
> Everyone here has been handed a bad analogy and then over-applied it — and
> felt *confident* the whole time. That is the failure mode of every 'X is just
> Y' explanation ever given.
>
> So in our domain model, a mapping without a breakdown is invalid. Not a
> warning. Invalid. It is a required field on the type, a required field in the
> plugin schema, and there is a test that fails if the agent's instruction stops
> demanding it."

## 11 · The ladder

> "Then we stop explaining and start extracting. Four rungs.
>
> L1, what maps to what. L2 — the one that does the teaching — where does this
> analogy break? We make you find the seam. We do not name it for you, because
> being told is not the same as noticing. L3, answer in the new field's own
> vocabulary with nothing left to lean on. L4, a real problem needing three ideas
> at once.
>
> And here is the design decision I would defend hardest: which concept, and
> which rung, is deterministic code. Not the model. The model only writes the
> sentence. A model asked to pick the difficulty *and* grade the answer will
> drift straight toward whatever it just explained."

## 12 · Happy path A

> "Concretely. An infrastructure engineer moving to ML platform work.
>
> They say what they know. We scan their GitHub, so frequency is real. The
> analogy sub-agent builds the mapping, and the assessor picks the probe.
>
> Rolling update maps to promotion in a model registry — same rollback role. And
> then the limit: rolling back a Deployment restores a deterministic binary.
> Rolling back a model does not restore the data distribution it was trained on.
> The previous version can be exactly as wrong.
>
> That is the sentence that saves them a postmortem."

## 13 · Happy path B

> "A backend engineer learning thermodynamics. This output is verbatim from a
> live run — I did not write these sentences.
>
> Rate limiting maps to entropy: both are a quota on how many configurations are
> still allowed. And the break: a rate limit is a policy you can raise. Entropy
> is a count of accessible microstates — you cannot raise it without changing
> the physics.
>
> They said they think in pictures, so the tutor generated one. Notice the
> diagram is drawn around the *gap*, not around the match."

## 14 · Happy path C

> "And the case we are proudest of. A data engineer learning dynamic
> programming, who believed that putting `lru_cache` on any recursion makes it
> DP.
>
> A model grading that answer in prose would have waved it through. So we do not
> grade it in prose — we run it. Their code goes into a Daytona sandbox against
> tests the tutor wrote, and one fails.
>
> Read the test name: *is point-in-time correct — used a value from the future.*
> That is the misconception, already worded better than the tutor could have
> worded it. It is the one grade in this system that is not an opinion."

## 15 · The app

Twenty seconds. Let them look.

> "It is real software. Seven and a half thousand lines of Go, a native desktop
> app.
>
> Left, the analogies — every row carrying its limit. Right, cognitive debt
> above mastery, as two separate scales, because conflating them was a genuine
> bug we shipped and caught."

## 16 · Architecture

> "Under it: Fyne for the window, Google's Agent Development Kit for the agents,
> and one seam between them.
>
> Our plugin registry *is* ADK's toolset interface. ADK asks it for the tool
> list on every single turn — so plugins appear and disappear with no restart.
> Add an API key in Settings and the model has a new tool on its next message.
> We got hot reload for free out of a decision we made for a different reason.
>
> And the bottom layer imports nothing from this project. No ADK, no Fyne, no
> plugins. The debt formula and the ladder are testable with no key, no network
> and no GUI."

## 17 · The plugin ABI

Only if the judges look technical. Otherwise skip to 18.

> "Three methods. Only bytes cross the boundary — no pointers, no channels, no
> database handle — which is why the same plugin runs in-process or in its own
> OS process with no code change. Drop a binary into the plugins folder and it
> silently replaces its built-in twin.
>
> The interesting one is `analogy`. It does not implement an agent. It
> *describes* one — a manifest, no Go. Swap the instruction and you have swapped
> teaching strategy without a recompile."

## 18 · Why local

Pre-empt the question.

> "You will ask why it is not a web app. Because this is a proof of concept and
> auth is not the interesting part. No accounts, no tenancy, no cloud bill —
> your profile is one SQLite file on your own disk.
>
> There is a real reason too: a ranked list of what you do not know is not a
> thing you want sitting on somebody else's server.
>
> Sync and teams are a transport change behind the same ABI. We spent the
> hackathon on the teaching loop, because login screens are solved and that is
> not."

## 19 · Partner stack

> "Six of the ten partner technologies, each doing a real job.
>
> x.ai runs the tutor. Daytona runs the learner's code. fal draws the mapping.
> Exa finds the source and Firecrawl reads it, for the topics where a model's
> own recall should not be trusted. Render is serving this deck right now, from
> the same repository.
>
> Three we did not use, and I would rather say so than pretend: Convex, because
> the entire point was keeping state on the learner's own disk. Wonder and Wispr
> Flow, because a Go desktop app has no React handoff and we were typing, not
> talking."

## 20 · What we measured

> "One engineering note, because it is the most transferable thing we learned.
>
> A turn took two minutes. It now takes thirty-four seconds. The whole
> difference was one field: which model each agent runs.
>
> The analogy agent was burning four thousand tokens over seventy-four seconds
> — all of it reasoning, none of it visible — on an instruction that asked for
> three short pairs. An agent that *follows* a written procedure does not want a
> reasoning model. An agent that *decides what to do next* does.
>
> We only found it because every seam is traced with timings."

## 21 · The team

> "Two of us. I lead a team at Nebius; Danila is a senior Go developer at
> Group-IB and owns the plugin layer.
>
> We picked this problem because it is ours. We have both watched good engineers
> get faster and less certain at the same time."

## 22 · Where it goes

> "Next is the part we have designed and not yet built: stop explaining, start
> extracting. Make them predict the mapping before we reveal it — the guess is
> the measurement. Show their answer beside the real one. Keep a ledger of every
> misconception and bring it back later.
>
> And then the version a company actually pays for. Because cognitive debt is
> not an individual problem. It is what happens when the only person who
> understood the service leaves.
>
> Thank you."

Stop. Do not add anything after "thank you".

---

## If they ask

**"Isn't this just ChatGPT with a prompt?"**
> "Ask ChatGPT for an analogy and it will give you one and stop. Three things
> here are code, not prompt: the breakdown is a required field, the difficulty
> is chosen by a deterministic function of measured mastery, and the L4 grade
> comes from tests that actually ran. The model writes sentences. It does not
> decide whether you passed."

**"How do you know the analogy is any good?"**
> "We do not trust it — we test it. L2 asks the learner where it breaks, and the
> grade moves mastery. A mapping that produces wrong answers shows up as debt
> that will not go down. And we ship a deliberately naive analogy plugin
> alongside the real one, so you can watch the difference in the same session."

**"What if the learner lies about what they know?"**
> "That is why the GitHub scan exists. Self-report sets the starting point; the
> repositories set the frequency term; the answers set the mastery term. Two of
> the three are outside their control."

**"Why Go and not Python?"**
> "Single binary, no runtime to install, and the plugin story. Our ABI is three
> methods over bytes, so a plugin can run in-process for speed or in its own
> process for isolation — same code either way. Google's ADK has a Go SDK, so we
> gave up nothing on the agent side."

**"Who pays for this?"**
> "The individual buys it out of fear — that is the Hacker News evidence. The
> company buys it out of bus factor: one leaver taking the only mental model of
> a service with them costs far more than a seat."

---

## Timing

| slides | topic | budget |
|---|---|---|
| 1–3 | the problem, and that it is not ours alone | 1:15 |
| 4–6 | measured, named, and unsolved | 1:15 |
| 7–11 | the idea and the mechanic | 1:30 |
| 12–15 | three happy paths and the running app | 1:20 |
| 16–20 | architecture, stack, engineering | 0:50 |
| 21–22 | team and close | 0:30 |

Overruns come from slide 3 (reciting) and slide 16 (over-explaining). Watch those two.
