# presentation

The cogdebt pitch deck for the Cursor Community Serbia Hackathon.

**Live: https://serbia-cursor-hackathon.onrender.com**

The binaries the last slide points at are on the
[releases page](https://github.com/siriusfreak/serbia-cursor-hackathon/releases).

Plain static HTML — no build step, no dependencies, no CDN. Open
[`index.html`](index.html) in any browser.

| key | |
|---|---|
| `→` `←` `space` | next / previous slide (a presenter clicker sends these) |
| `1`–`9`, `0` | jump to a slide (`0` is slide 10) |
| `N` | speaker notes for the current slide |
| `F` | fullscreen |
| click | right half forward, left half back |

The slide number is in the URL hash, so `index.html#7` opens on slide 7. Use it
to go straight to the examples when a judge asks for one.

Ten slides, five minutes. The text uses Simple Technical English: short
sentences, one idea in each sentence, active voice, and no idioms. The audience
is international, and a projector is a bad place for a long sentence.

**Printing gives you a PDF.** Every slide becomes one landscape page. Do this
before you present: a laptop that will not talk to the projector is the most
likely thing to go wrong, and a PDF on a phone still gets you through the pitch.

## Files

```
index.html        the deck — markup, styles and navigation in one file
SPEECH.md         what to say, one numbered section per slide
SOURCES.md        a link for every headline and number the slides claim
assets/app.png    screenshot of the running app, used on the demo slide
```

## It is checked

`design/deck_test.go` runs as part of `go test ./...` and fails when the deck
drifts from the code or from its evidence: colours outside the app palette, a
plugin table that no longer matches `exts/`, a wrong method count for the plugin
ABI, a slide with no speaker notes, a `SPEECH.md` missing a section for a slide,
a headline that `SOURCES.md` does not cite, a claimed source count that does not
equal the rows in `SOURCES.md`, or a repository link that is not this
repository.

See **“The pitch deck is part of the product”** in [`AGENTS.md`](../AGENTS.md)
for what to update when the code changes.

## Deployment

Served by Render as a static site from `master`:

| setting | value |
|---|---|
| root directory | `presentation` |
| build command | *(none — there is nothing to build)* |
| publish directory | `.` |

Pushing to `master` redeploys — Render service `srv-daijib5g1s2s73fjv45g`.
Nothing here reads an environment variable, and no key is needed to serve it.

Deep-link a slide with the hash: `…onrender.com/#7` opens on the examples,
including the Daytona result that judges ask about.

## Regenerating the screenshot

```bash
go run ./cmd/cogdebt -screenshot presentation/assets/app.png -shot-after 3s
```

That fills the window with representative sample content and exits without
spending a model call. Re-shoot it whenever the UI visibly changes — a slide
showing last week's interface is the kind of detail judges notice.
