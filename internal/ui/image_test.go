package ui

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/sirius/cogdebt/internal/ext"
)

// inline runs the completion path synchronously, so a test can look at the
// widget the fetch produced instead of racing the goroutine.
func inline(t *testing.T) *sync.WaitGroup {
	t.Helper()
	var wg sync.WaitGroup
	wg.Add(1)
	old := onUI
	var once sync.Once
	onUI = func(fn func()) {
		fn()
		once.Do(wg.Done)
	}
	t.Cleanup(func() { onUI = old })
	return &wg
}

// stubFetch swaps the downloader for the duration of one test and puts the
// real one back, so a later test is not left with a dead fetcher.
func stubFetch(t *testing.T, fn func(context.Context, string) ([]byte, error)) {
	t.Helper()
	old := fetchImage
	fetchImage = fn
	t.Cleanup(func() { fetchImage = old })
}

func samplePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 0xE8, G: 0xA3, B: 0x3D, A: 0xFF})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestDecodeImageKeepsAspect guards the one thing a diagram cannot survive:
// being stretched. The height comes from the file's own header, not a guess.
func TestDecodeImageKeepsAspect(t *testing.T) {
	for _, tc := range []struct {
		name       string
		w, h       int
		wantHeight float32
	}{
		{"landscape", 1024, 576, imageWidth * 576.0 / 1024.0},
		{"portrait", 600, 900, imageWidth * 900.0 / 600.0},
		{"square", 512, 512, imageWidth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, size, err := decodeImage(samplePNG(t, tc.w, tc.h))
			if err != nil {
				t.Fatal(err)
			}
			if size.Width != imageWidth {
				t.Errorf("width = %v, want %v", size.Width, imageWidth)
			}
			if diff := size.Height - tc.wantHeight; diff > 0.5 || diff < -0.5 {
				t.Errorf("height = %v, want %v", size.Height, tc.wantHeight)
			}
		})
	}
}

func TestRenderImageDrawsWhatArrives(t *testing.T) {
	test.NewApp()
	wg := inline(t)

	raw := samplePNG(t, 800, 400)
	stubFetch(t, func(context.Context, string) ([]byte, error) { return raw, nil })

	obj := Render(ext.View(ext.ViewImage, "", ext.ImageProps{
		URL: "https://example.test/diagram.png", Caption: "thermostat maps to congestion window",
	}), nil)
	wg.Wait()

	if count := countType[*canvas.Image](obj); count != 1 {
		t.Fatalf("found %d canvas images in the rendered node, want 1", count)
	}
	if !strings.Contains(allText(obj), "congestion window") {
		t.Error("the caption is missing; the picture alone cannot say which side is which")
	}
}

// TestRenderImageSurvivesAFailedFetch is the case that matters in the room:
// a CDN that is slow or gone must degrade to a line of text, not take the feed
// down with it.
func TestRenderImageSurvivesAFailedFetch(t *testing.T) {
	test.NewApp()
	wg := inline(t)

	stubFetch(t, func(context.Context, string) ([]byte, error) { return nil, errors.New("dns is having a day") })

	obj := Render(ext.View(ext.ViewImage, "", ext.ImageProps{URL: "https://example.test/gone.png"}), nil)
	wg.Wait()

	if countType[*canvas.Image](obj) != 0 {
		t.Error("drew an image despite the fetch failing")
	}
	if got := allText(obj); !strings.Contains(got, "dns is having a day") {
		t.Errorf("the failure is not visible to the learner; text was %q", got)
	}
}

func TestRenderImageWithoutURL(t *testing.T) {
	obj := Render(ext.View(ext.ViewImage, "", ext.ImageProps{}), nil)
	if !strings.Contains(allText(obj), "no image") {
		t.Error("an image node with no url should say so rather than render an empty card")
	}
}

// countType walks the object tree counting nodes of one type.
func countType[T fyne.CanvasObject](obj fyne.CanvasObject) int {
	n := 0
	walk(obj, func(o fyne.CanvasObject) {
		if _, ok := o.(T); ok {
			n++
		}
	})
	return n
}

func allText(obj fyne.CanvasObject) string {
	var b strings.Builder
	walk(obj, func(o fyne.CanvasObject) {
		switch v := o.(type) {
		case *widget.RichText:
			for _, seg := range v.Segments {
				if ts, ok := seg.(*widget.TextSegment); ok {
					b.WriteString(ts.Text + " ")
				}
			}
		case *canvas.Text:
			b.WriteString(v.Text + " ")
		}
	})
	return b.String()
}

func walk(obj fyne.CanvasObject, fn func(fyne.CanvasObject)) {
	if obj == nil {
		return
	}
	fn(obj)
	if c, ok := obj.(interface{ Objects() []fyne.CanvasObject }); ok {
		for _, kid := range c.Objects() {
			walk(kid, fn)
		}
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, kid := range c.Objects {
			walk(kid, fn)
		}
	}
}
