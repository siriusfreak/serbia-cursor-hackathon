package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"

	"github.com/sirius/cogdebt/internal/ext"
)

// Remote images are fetched by the host, never carried through the ABI. See
// ext.ImageProps for why.
const (
	// imageWidth is the drawn width. The feed column is the constraint, not the
	// picture: a 1024px diagram at full size pushes the text out of view.
	imageWidth = 520
	// maxImageBytes caps a download. A generator that answers with something
	// enormous must not be able to exhaust the app's memory.
	maxImageBytes = 12 << 20
	// imageTimeout bounds the fetch. A picture is an embellishment; it may not
	// hold a turn open.
	imageTimeout = 30 * time.Second
)

// imageCache keeps decoded bytes per URL, so redrawing the feed -- which Fyne
// does on every resize -- does not refetch the picture.
var imageCache sync.Map // url -> []byte

// onUI schedules work on the Fyne goroutine. It is a variable so a test can
// run the completion path inline, without standing up a driver loop.
var onUI = post

// fetchImage is a variable so tests can exercise the render path without a
// network. It returns the raw encoded bytes.
var fetchImage = func(ctx context.Context, url string) ([]byte, error) {
	if b, ok := imageCache.Load(url); ok {
		return b.([]byte), nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("image fetch returned %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
	if err != nil {
		return nil, err
	}
	imageCache.Store(url, b)
	return b, nil
}

// renderImage draws a picture that lives behind a URL.
//
// The node renders immediately as a caption and a placeholder, and the picture
// swaps in when it arrives. Blocking the feed on a download would mean one slow
// CDN freezes the conversation.
func renderImage(v ext.ViewSpec) fyne.CanvasObject {
	var p ext.ImageProps
	_ = v.DecodeProps(&p)
	if p.URL == "" {
		return muted("(no image)")
	}

	alt := p.Alt
	if alt == "" {
		alt = "drawing the mapping…"
	}
	slot := container.NewStack(muted(alt))

	// The HBox holds the picture at its own width instead of stretching it
	// across the card, so its left edge lines up with the caption underneath.
	holder := container.NewVBox(container.NewHBox(slot))
	if p.Caption != "" {
		holder.Add(muted(p.Caption))
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), imageTimeout)
		defer cancel()

		raw, err := fetchImage(ctx, p.URL)
		if err != nil {
			onUI(func() {
				slot.Objects = []fyne.CanvasObject{muted("(could not load the diagram: " + err.Error() + ")")}
				slot.Refresh()
			})
			return
		}
		img, size, err := decodeImage(raw)
		if err != nil {
			onUI(func() {
				slot.Objects = []fyne.CanvasObject{muted("(the diagram is not an image this app can read)")}
				slot.Refresh()
			})
			return
		}
		onUI(func() {
			img.SetMinSize(size)
			slot.Objects = []fyne.CanvasObject{img}
			slot.Refresh()
		})
	}()

	return accented(currentPalette().surface, currentPalette().accent, 8, holder)
}

// decodeImage turns encoded bytes into a canvas image scaled to the feed width.
// The dimensions are read from the header rather than guessed, so a portrait
// diagram is not stretched into a letterbox.
func decodeImage(raw []byte) (*canvas.Image, fyne.Size, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, fyne.Size{}, err
	}
	h := float32(imageWidth)
	if cfg.Width > 0 {
		h = imageWidth * float32(cfg.Height) / float32(cfg.Width)
	}

	img := canvas.NewImageFromResource(fyne.NewStaticResource("diagram", raw))
	img.FillMode = canvas.ImageFillContain
	return img, fyne.NewSize(imageWidth, h), nil
}
