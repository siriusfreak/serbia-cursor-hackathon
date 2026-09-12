# presentation

The cogdebt pitch deck for the Cursor Community Serbia Hackathon.

**Live: https://serbia-cursor-hackathon.onrender.com**

Plain static HTML — no build step, no dependencies, no CDN. Open
[`index.html`](index.html) in any browser.

| key | |
|---|---|
| `→` `←` `space` | next / previous slide (a presenter clicker sends these) |
| `1`–`9` | jump to a slide |
| `N` | speaker notes for the current slide |
| `F` | fullscreen |
| click | right half forward, left half back |

The slide number is in the URL hash, so `index.html#14` opens on slide 14 — use
it to jump straight to a happy path when a judge asks.

**Printing gives you a PDF.** Every slide becomes one landscape page. Do this
before you present: a laptop that will not talk to the projector is the most
likely thing to go wrong, and a PDF on a phone still gets you through the pitch.

## Files

```
index.html        the deck — markup, styles and navigation in one file
SPEECH.md         what to say, one numbered section per slide
assets/app.png    screenshot of the running app, used on the demo slide
assets/           drop sirius.jpg and danila.jpg here for the team slide;
                  until they exist the slide falls back to monograms
```

## It is checked

`design/deck_test.go` runs as part of `go test ./...` and fails when the deck
drifts from the code: colours outside the app palette, a plugin table that no
longer matches `exts/`, a wrong method count for the plugin ABI, a slide with no
speaker notes, or a `SPEECH.md` missing a section for a slide.

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

Deep-link a slide with the hash: `…onrender.com/#14` opens on the Daytona
happy path, which is the one judges ask about.

## Regenerating the screenshot

```bash
go run ./cmd/cogdebt -screenshot presentation/assets/app.png -shot-after 3s
```

That fills the window with representative sample content and exits without
spending a model call. Re-shoot it whenever the UI visibly changes — a slide
showing last week's interface is the kind of detail judges notice.
