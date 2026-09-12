# Speech — cogdebt

Ten slides. Five minutes.

The text below is written in Simple Technical English, in short sentences. Say
the sentences as they are written. The deck carries the evidence. You carry the
argument.

The same notes are inside the deck. Press **N** during the presentation.

**Rules for the room.**
- Do not read a slide aloud. The audience reads faster than you speak.
- Slide 2 has a list on the right. Point at two items. Do not read the list.
- If you lose time, shorten slide 9 and slide 10.
- Do not shorten slide 5. It contains the one idea that nobody else will state.

---

## 1 · Title

> "We are team BinSearch.
>
> Every technical system has a weakest part. In our systems the weakest part is
> not the database, and it is not the model. It is the person who must understand
> what we built.
>
> We built cogdebt for that person."

Then go to slide 2. Do not explain the product name here.

## 2 · The problem

> "Engineers lose technical skills when they use AI tools.
>
> This is measured. In June, Nature reported two studies. One study tested
> doctors. One study tested programmers. Both groups used an AI assistant for a
> period. After that period, both groups worked worse without the assistant.
>
> The problem also has a cost. Ford hired 350 engineers again, because the AI
> system did not keep the expert knowledge, and because junior engineers had
> nobody to learn from.
>
> And engineers write about this every month. We found nineteen articles about it
> on Hacker News between April and August. Every one is listed with its link in
> the repository."

Point at the Nature line and the Ford line. Do not read the other four.

## 3 · The name, and the number

> "The problem has a name. Cognitive debt. Professor Margaret-Anne Storey
> defined the term. It is the difference between the system you operate and the
> knowledge you hold.
>
> We made it a number. How often you use a concept, multiplied by how many other
> concepts need it, multiplied by how little of it you hold.
>
> The first value is the honest one. We do not ask you what you know, because
> people rate themselves too high. We read your public repositories.
>
> Debt is a good model here. Debt grows slowly. Debt is not visible. And you can
> pay debt back. That last part is the part nobody builds."

## 4 · Available solutions

> "This is the current market. Disable the AI assistant one day each week. Or
> type the generated code again by hand, line by line. Or use flashcards.
>
> These are three real techniques, and people use them today. They are honest
> attempts. But look at the shape they share. Every one of them removes
> something: your speed, your tools, or your evening.
>
> Each one stops new debt. None of them pays back the debt you already have."

## 5 · Our method

Slow down here.

> "So we do not remove the tool. We use the knowledge the engineer already has.
>
> A senior engineer is not empty. That engineer holds a deep model of some
> subject. The fastest way into a new subject is not a tutorial. It is a
> connection to the subject already in their head.
>
> We match the two subjects by structural role, not by name. Not 'both have a
> controller'. 'Both are the single source of truth that other components
> compare themselves against.' That match gives you etcd and a feature store.
>
> Now look at the orange line, not the first line. The first line is what any
> chat assistant gives you. The orange line is our product.
>
> An analogy without a stated limit does not pay down cognitive debt. It creates
> more debt. The engineer keeps using the analogy after the point where it is
> correct, and the engineer feels confident.
>
> So in our domain model, a mapping without a limit is invalid. Not a warning.
> Invalid. It is a required field on the type, a required field in the plugin
> schema, and a test checks that the agent still asks for it."

## 6 · The question ladder

> "Then we stop explaining, and we start asking. Four levels.
>
> Level one: find the match. Level two: find the limit. Level two does the
> teaching, so the system does not tell you the limit. You must find it. Level
> three: answer in the new vocabulary, without the analogy. Level four: solve a
> small problem that needs three new concepts together.
>
> Here is the design decision I will defend. A deterministic function selects the
> concept and the level. It uses the debt value and the measured mastery. The
> language model only writes the text of the question.
>
> A model that selects the difficulty and also grades the answer moves towards
> the topic it explained last. So we do not let it do both."

## 7 · Three examples

> "Three learners, three subjects. This output comes from live runs. We did not
> write these sentences.
>
> An infrastructure engineer learns ML pipelines. A rolling update maps to model
> promotion. Both have the rollback role. And the limit: a Deployment rollback
> restores one exact binary, but a model rollback does not restore the training
> data. Version n minus one can be equally wrong.
>
> A backend engineer learns thermodynamics. Rate limiting maps to entropy. Both
> are a quota of allowed states. And the limit: you can increase a rate limit,
> because it is a policy. You cannot increase entropy.
>
> The third learner is the interesting one. A data engineer learns dynamic
> programming, and believes that `lru_cache` on any recursion is dynamic
> programming.
>
> A model that grades text would accept that answer. So we do not grade text. We
> run the code in a Daytona sandbox against tests the tutor wrote. One test
> fails. Read the test name: 'is point-in-time correct — used a value from the
> future.' That is the wrong belief, already written down.
>
> This is the only grade in the system that is not an opinion."

## 8 · The application

Twenty seconds. Let the audience look at the window.

> "This is real software. Ten thousand lines of Go, and a native desktop window.
>
> On the left, the analogies. Every row shows its limit. On the right, cognitive
> debt above mastery, in two separate groups, because they are two different
> scales.
>
> The application runs on your computer. This is a proof of concept, so we did
> not build accounts or sessions. There is also a second reason: a list of the
> subjects you do not know is private information."

## 9 · Architecture

> "Under the window: Fyne for the interface, Google's Agent Development Kit for
> the agents, and one seam between them.
>
> Our plugin registry implements the ADK toolset interface. ADK requests the tool
> list on every turn. So a plugin can start or stop without a restart. You add an
> API key in Settings, and the model has a new tool on the next message.
>
> The plugin interface has three methods, and only bytes cross it. No pointers,
> no channels, no database handle. So the same plugin runs inside our process or
> in a separate process, and the code does not change.
>
> The bottom layer imports nothing from this project. The debt formula and the
> ladder are testable without a key, without a network and without a GUI."

If the judges are not technical, say only the first two paragraphs.

## 10 · Partner technologies, and what is next

> "Seven of the ten partner technologies, and each one has a real job.
>
> x.ai runs the tutor. Daytona runs the learner's code. fal draws the analogy.
> Exa finds a source and Firecrawl reads it. Render serves these slides, from the
> same repository. And we built all of it in Grok Bot.
>
> We did not use three of them, and I will say so. Convex stores state on a
> server, and we keep state on the learner's disk. Wonder and Wispr Flow do not
> fit a Go desktop application.
>
> One engineering result. A turn took two minutes. Now it takes thirty-four
> seconds. We changed one field: the model of each agent. An agent that follows
> written steps does not need a reasoning model. An agent that decides the next
> action does need one. We found this with tracing, not by guessing.
>
> What is next: ask the learner to predict the match before we show it, and keep
> a list of every wrong belief.
>
> The source code is on GitHub, and there is a macOS build in the release. Both
> links are on this slide.
>
> And then a shared debt list for a team. Because cognitive debt is not only an
> individual problem. It is also what happens when the one engineer who
> understood a service leaves the company.
>
> Thank you."

Stop. Do not add a sentence after "thank you".

---

## If they ask

**"Is this only ChatGPT with a prompt?"**
> "Ask a chat assistant for an analogy. It gives you one and stops. Three things
> here are code, not a prompt. The limit is a required field. The difficulty is a
> function of measured mastery. And the level four grade comes from tests that
> ran. The model writes sentences. The model does not decide if you passed."

**"How do you know the analogy is correct?"**
> "We do not trust it. We test it. Level two asks the learner where the analogy
> breaks, and the grade moves the mastery value. A bad mapping produces debt that
> does not go down. We also ship a simple analogy plugin next to the real one, so
> you can compare them in one session."

**"What if the learner reports the wrong skills?"**
> "That is why we scan GitHub. The self-report sets the start point. The
> repositories set the frequency value. The answers set the mastery value. Two of
> the three are outside the learner's control."

**"Why Go and not Python?"**
> "One binary, no runtime to install, and the plugin model. Our ABI is three
> methods over bytes, so a plugin runs in our process or in its own process with
> the same code. Google's ADK has a Go SDK, so we lost nothing on the agent side."

**"Where is the proof for the slide 2 numbers?"**
> "`presentation/SOURCES.md` in the repository. It lists all nineteen stories with
> a link to each Hacker News item, and it states the search terms we used. It also
> says what the number is not: a keyword search, not a complete census. A
> different set of terms returns a different number."

**"Can we run it?"**
> "The source is on GitHub, and the releases page has builds for macOS, Linux and
> Windows, each on x86-64, ARM64 and 32-bit x86. No binary is signed, so macOS and
> Windows both warn on the first run. The release notes give the exact command for
> each system."

**"Who pays for this?"**
> "An individual pays because of fear. That is the Hacker News evidence. A company
> pays because of bus factor. One engineer leaves and takes the only model of a
> service with them. That costs more than a licence."

---

## Timing

| slides | topic | budget |
|---|---|---|
| 1–2 | the problem, and the evidence | 1:00 |
| 3–4 | the name, the number, and why current answers fail | 1:00 |
| 5–6 | the method and the ladder | 1:15 |
| 7–8 | three examples and the application | 1:15 |
| 9–10 | architecture, partners, and the close | 0:45 |

You will lose time on slide 2 if you read the list, and on slide 9 if you explain
too much. Watch those two slides.
