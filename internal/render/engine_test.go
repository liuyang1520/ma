package render

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e
}

func TestNativeViewport(t *testing.T) {
	e := newTestEngine(t)
	for _, theme := range []string{"dark", "light"} {
		source := []byte("# Title\n\n## Subtitle\n\n### Third\n\nNeedle text.\n\n" + strings.Repeat("Paragraph with enough words to wrap over a few lines on a narrow terminal.\n\n", 40) + "Needle at the end.\n")
		if err := e.Resize(800, 600, 1); err != nil {
			t.Fatal(err)
		}
		if err := e.Load(source, "", theme, 20); err != nil {
			t.Fatal(err)
		}
		for text, size := range map[string]float64{"Title": 40, "Subtitle": 30, "Third": 25, "Needle": 20} {
			off := strings.Index(string(e.text), text)
			found := false
			for _, g := range e.glyphs {
				if g.index == off {
					if g.style.size != size {
						t.Fatalf("%s font %v, want %v", text, g.style.size, size)
					}
					found = true
					break
				}
			}
			if !found {
				t.Fatal("missing heading", text)
			}
		}
		data, initial, err := e.Frame(0)
		if err != nil {
			t.Fatal(err)
		}
		im, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if im.Bounds() != image.Rect(0, 0, 800, 600) {
			t.Fatal("wrong viewport", im.Bounds())
		}
		if im.At(0, 0) != e.palette.bg {
			t.Fatal("incorrect theme background", im.At(0, 0), e.palette.bg)
		}
		_, scrolled, err := e.Frame(500)
		if err != nil || scrolled.Y != 500 {
			t.Fatal("scroll", scrolled, err)
		}
		_, end, err := e.Frame(1e9)
		if err != nil || end.Y != end.Height-600 {
			t.Fatal("end clamp", end, err)
		}
		matches, err := e.Search("needle")
		if err != nil || len(matches) != 2 || matches[1] <= matches[0] {
			t.Fatal("search", matches, err)
		}
		if err = e.SelectMatch(1); err != nil {
			t.Fatal(err)
		}
		selected, _, err := e.Frame(matches[1] - 30)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e.Search(""); err != nil {
			t.Fatal(err)
		}
		cleared, _, err := e.Frame(matches[1] - 30)
		if err != nil || bytes.Equal(selected, cleared) {
			t.Fatal("search highlights did not change pixels")
		}
		if err = e.Resize(400, 600, 1); err != nil {
			t.Fatal(err)
		}
		_, narrow, err := e.Frame(0)
		if err != nil || narrow.Height <= initial.Height {
			t.Fatal("resize did not reflow", err)
		}
		if err = e.Resize(400, 300, 2); err != nil {
			t.Fatal(err)
		}
		data, _, err = e.Frame(0)
		if err != nil {
			t.Fatal(err)
		}
		im, err = png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if im.Bounds() != image.Rect(0, 0, 800, 600) {
			t.Fatal("wrong retina size", im.Bounds())
		}
	}
}

func TestGFMLayoutAndLiteralText(t *testing.T) {
	e := newTestEngine(t)
	source := []byte("# A &amp; B\n\n3. First\n   - Nested\n4. Second\n\n- [x] Done\n- [ ] Later\n\n| Left | Right |\n|:---|---:|\n| hello | 123 |\n\n> Quote\n\n**Bold** *italic* ~~gone~~ [link](https://example.com)\n\n```go\n  x := `&amp;`\n\n  y := 2\n```\n\n<script>alert(1)</script>\n")
	if err := e.Load(source, "", "dark", 20); err != nil {
		t.Fatal(err)
	}
	text := string(e.text)
	for _, want := range []string{"A & B", "3.", "4.", "Nested", "Done", "Later", "hello", "123", "Quote", "  x := `&amp;`\n\n  y := 2"} {
		if !strings.Contains(text, want) {
			t.Error("missing", want, "in", text)
		}
	}
	if strings.Contains(text, "alert(1)") {
		t.Fatal("raw HTML content rendered")
	}
	for label, flag := range map[string]int{"Bold": bold, "italic": italic, "gone": strike} {
		start := len([]rune(text[:strings.Index(text, label)]))
		found := false
		for _, g := range e.glyphs {
			if g.index == start && g.style.flags&flag != 0 {
				found = true
			}
		}
		if !found {
			t.Error("lost inline style", label)
		}
	}
}

func TestWrappingAndSearchAcrossStyles(t *testing.T) {
	e := newTestEngine(t)
	if err := e.Resize(320, 240, 1); err != nil {
		t.Fatal(err)
	}
	source := []byte("A **needle** across *styles* and café.\n\n" + strings.Repeat("abcdefgh", 100) + "\n\nLine one\nline two.\n\nHard break  \nnext line.")
	if err := e.Load(source, "", "light", 20); err != nil {
		t.Fatal(err)
	}
	matches, err := e.Search("NEEDLE across styles")
	if err != nil || len(matches) != 1 {
		t.Fatal("search across formatting", matches, err)
	}
	matches, err = e.Search("CAFÉ")
	if err != nil || len(matches) != 1 {
		t.Fatal("Unicode case-insensitive search", matches, err)
	}
	if !strings.Contains(string(e.text), "Line one line two.") || !strings.Contains(string(e.text), "Hard break\nnext line.") {
		t.Fatal("line break semantics", string(e.text))
	}
	for _, g := range e.glyphs {
		if g.x < 19 || g.x+g.w > 301 {
			t.Fatalf("glyph escaped text bounds: %+v", g)
		}
	}
}

func TestLocalImages(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "docs")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "outside.png"), filepath.Join(dir, "inside.png")} {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 20, 10)))
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "outside.png"), filepath.Join(dir, "link.png")); err != nil {
		t.Fatal(err)
	}
	budget := int64(128 << 20)
	if localImage("inside.png", dir, &budget) == nil {
		t.Fatal("valid local image missing")
	}
	for _, path := range []string{"../outside.png", "%2e%2e/outside.png", "link.png", "file:///etc/passwd", "https://example.com/a.png"} {
		if localImage(path, dir, &budget) != nil {
			t.Error("escaped boundary", path)
		}
	}
	budget = 1
	if localImage("inside.png", dir, &budget) != nil {
		t.Fatal("decoded image budget ignored")
	}
	e := newTestEngine(t)
	if err := e.Load([]byte("![local](inside.png)\n\n![remote](https://example.com/a.png)"), dir, "dark", 20); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, op := range e.ops {
		if op.image != nil {
			count++
		}
	}
	if count != 1 || !strings.Contains(string(e.text), "[image: remote]") {
		t.Fatal("image layout", count, string(e.text))
	}
}

func TestInputBoundsAndCancellation(t *testing.T) {
	e := newTestEngine(t)
	if err := e.Resize(4096, 4096, 4); err == nil {
		t.Fatal("unbounded raster allocation allowed")
	}
	if err := e.Load([]byte("text"), "", "dark", 0); err == nil {
		t.Fatal("invalid font size allowed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.ctx = ctx
	if err := e.Load([]byte("hello"), "", "dark", 20); err != context.Canceled {
		t.Fatal("cancellation ignored", err)
	}
}

func TestHeightOnlyResizeAndCodeSpacing(t *testing.T) {
	e := newTestEngine(t)
	if err := e.Load([]byte("Before `code` after."), "", "dark", 20); err != nil {
		t.Fatal(err)
	}
	initialHeight := e.documentHeight
	if err := e.Resize(800, 200, 1); err != nil {
		t.Fatal(err)
	}
	_, metrics, err := e.Frame(1000)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Y != 0 || metrics.Height != 200 || e.documentHeight != initialHeight {
		t.Fatal("short document gained blank scrolling space", metrics)
	}
	matches, err := e.Search("Before code after")
	if err != nil || len(matches) != 1 {
		t.Fatal("code padding changed search text", matches, err)
	}
}

func TestBundledFontsWithoutSystemFiles(t *testing.T) {
	f, err := loadFonts(func(string) ([]byte, error) { return nil, os.ErrNotExist })
	if err != nil {
		t.Fatal(err)
	}
	defer f.closeFaces()
	for id := 0; id < 8; id++ {
		for _, r := range "Markdown 123 café" {
			if width, ok := f.face(id, 20).GlyphAdvance(r); !ok || width <= 0 {
				t.Fatalf("bundled face %d cannot render %q", id, r)
			}
		}
	}
}

func BenchmarkNativeFrame(b *testing.B) {
	e, err := New(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	if err = e.Load([]byte(strings.Repeat("## Heading\n\nA paragraph of **bold** words with `code` and enough text to measure scrolling performance.\n\n", 100)), "", "dark", 20); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err = e.Frame(float64(i%20) * 30); err != nil {
			b.Fatal(err)
		}
	}
}

func TestCachedCanvasCropMatchesViewport(t *testing.T) {
	e := newTestEngine(t)
	if err := e.Resize(400, 300, 2); err != nil {
		t.Fatal(err)
	}
	if err := e.Load([]byte(strings.Repeat("# Heading\n\nSome **text** and `code`.\n\n", 20)), "", "dark", 20); err != nil {
		t.Fatal(err)
	}
	band, err := e.Canvas(0, 900)
	if err != nil {
		t.Fatal(err)
	}
	pngData, _, err := e.Frame(60)
	if err != nil {
		t.Fatal(err)
	}
	viewport, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 600; y++ {
		for x := 0; x < 800; x++ {
			a := band.RGBAAt(x, y+120)
			r, g, b, alpha := viewport.At(x, y).RGBA()
			if a.R != uint8(r>>8) || a.G != uint8(g>>8) || a.B != uint8(b>>8) || a.A != uint8(alpha>>8) {
				t.Fatalf("crop changed rendering at %d,%d", x, y)
			}
		}
	}
}
