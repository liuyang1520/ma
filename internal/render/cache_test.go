package render

import (
	"bytes"
	"image"
	"math"
	"os"
	"strings"
	"testing"

	"golang.org/x/image/math/fixed"
)

func TestGlyphCachePreservesRasterization(t *testing.T) {
	f, err := loadFonts(func(string) ([]byte, error) { return nil, os.ErrNotExist })
	if err != nil {
		t.Fatal(err)
	}
	defer f.closeFaces()
	for _, id := range []int{0, 3, 4, 7} {
		for _, size := range []float64{20, 30.6, 48} {
			for _, r := range "gÀ& 中🙂" {
				for _, phase := range []int{0, 1, 17, 32, 63} {
					dot := fixed.Point26_6{X: fixed.Int26_6(-7*64 + phase), Y: fixed.Int26_6(40*64 + phase)}
					dr, raw, mp, _, _ := f.face(id, size).Glyph(dot, r)
					if dr.Empty() {
						continue
					}
					want := image.NewAlpha(image.Rect(0, 0, dr.Dx(), dr.Dy()))
					for y := 0; y < dr.Dy(); y++ {
						for x := 0; x < dr.Dx(); x++ {
							want.Set(x, y, raw.At(x+mp.X, y+mp.Y))
						}
					}
					f.glyph(id, size, dot, r)
					// Overwrite the OpenType scratch mask before requesting the
					// same raster at a different integer position.
					f.face(id, size).Glyph(dot, 'W')
					shift := image.Pt(21, -11)
					dot.X += fixed.I(shift.X)
					dot.Y += fixed.I(shift.Y)
					gotRect, got := f.glyph(id, size, dot, r)
					if gotRect != dr.Add(shift) || !bytes.Equal(got.Pix, want.Pix) {
						t.Fatalf("cache changed face=%d size=%v rune=%q phase=%d", id, size, r, phase)
					}
				}
			}
		}
	}
}

func TestIndexedPaintingAndScratchReuse(t *testing.T) {
	e := newTestEngine(t)
	if err := e.Resize(401, 303, 1.25); err != nil {
		t.Fatal(err)
	}
	source := []byte("# Title\n\n> " + strings.Repeat("quoted words ", 200) + "\n\n```\n" + strings.Repeat("long code line\n", 200) + "```\n\n" + strings.Repeat("| Left | Right |\n|:---|---:|\n| a | A cell that wraps many times in a narrow column |\n\n", 30))
	if err := e.Load(source, "", "dark", 24); err != nil {
		t.Fatal(err)
	}
	groups := e.groups
	var scratch *image.RGBA
	for _, y := range []float64{0, 500, 1500, e.Viewport(1e9).Y} {
		indexed, err := e.Canvas(y, 303)
		if err != nil {
			t.Fatal(err)
		}
		e.groups = []operationGroup{{start: 0, end: len(e.ops), top: math.Inf(-1), bottom: math.Inf(1)}}
		scratch, err = e.CanvasInto(scratch, y, 303)
		if err != nil {
			t.Fatal(err)
		}
		e.groups = groups
		if !bytes.Equal(indexed.Pix, scratch.Pix) {
			t.Fatalf("indexed/reused pixels differ from full painting at y=%v", y)
		}
	}
}

func TestHeadingsAndReadingPositionSurviveReflow(t *testing.T) {
	e := newTestEngine(t)
	source := []byte("# A **formatted** title\n\n" + strings.Repeat("A paragraph with enough words to wrap differently after a resize.\n\n", 20) + "## Next `section`\n\n" + strings.Repeat("More text to leave room below the anchor.\n\n", 20))
	if err := e.Load(source, "", "dark", 24); err != nil {
		t.Fatal(err)
	}
	h := e.Headings()
	if len(h) != 2 || h[0].Text != "A formatted title" || h[1].Text != "Next section" || h[1].Level != 2 {
		t.Fatal("incorrect heading outline", h)
	}
	y := h[1].Y - 24
	position, top := e.Position(y), e.Position(0)
	if err := e.Resize(350, 600, 1); err != nil {
		t.Fatal(err)
	}
	if err := e.Load(source, "", "dark", 28); err != nil {
		t.Fatal(err)
	}
	restored := e.Restore(position)
	if restored <= y || math.Abs(restored-(e.Headings()[1].Y-24)) > 10 {
		t.Fatal("reading position lost on resize/zoom", y, restored, e.Headings()[1].Y)
	}
	if e.Restore(top) != 0 {
		t.Fatal("zoom scrolled away from the top")
	}
}
