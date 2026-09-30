package render

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLinkRegionsAcrossFormattingWrappingAndTables(t *testing.T) {
	e := newTestEngine(t)
	if err := e.Resize(300, 180, 1); err != nil {
		t.Fatal(err)
	}
	source := []byte("Outside [a **bold** `code` " + strings.Repeat("wrapped ", 12) + "](docs/a%20b.md#part) tail.\n\nhttps://example.com\n\n[reference][r]\n\n| left | right |\n|---|---|\n| [table](#part) | ordinary |\n\n[r]: https://example.com/?a=1&amp;b=2\n")
	if err := e.Load(source, "", "dark", 24); err != nil {
		t.Fatal(err)
	}
	if len(e.links) != 4 {
		t.Fatalf("link index duplicated measurement nodes: %d", len(e.links))
	}
	link, ok := e.Link(1)
	if !ok || link.Destination != "docs/a%20b.md#part" || len(link.Rects) < 3 {
		t.Fatalf("wrapped link missing: %+v", link)
	}
	for _, r := range link.Rects {
		if r.X < 20 || r.X+r.Width > 280.01 {
			t.Fatalf("link escaped content box: %+v", r)
		}
		if id, ambiguous := e.HitLink(LinkRect{r.X + r.Width/2, r.Y + r.Height/2, .01, .01}); id != 1 || ambiguous {
			t.Fatalf("hit %d/%v for %+v", id, ambiguous, r)
		}
	}
	for _, g := range e.glyphs {
		if g.link == 0 && g.r != ' ' {
			if id, _ := e.HitLink(LinkRect{g.x + g.w/2, g.y - 1, .01, .01}); id != 0 {
				t.Fatal("plain text became a link", g.r, id)
			}
		}
	}
	ref, _ := e.Link(3)
	if ref.Destination != "https://example.com/?a=1&b=2" {
		t.Fatal(ref.Destination)
	}
	before := e.GeometryRevision()
	if err := e.Resize(450, 180, 1.5); err != nil {
		t.Fatal(err)
	}
	if e.GeometryRevision() == before {
		t.Fatal("reflow retained stale geometry")
	}
	after, _ := e.Link(1)
	if after.Destination != link.Destination || len(after.Rects) >= len(link.Rects) {
		t.Fatal("reflow lost link identity or retained old rectangles")
	}
	if _, ok := e.Link(0); ok {
		t.Fatal("zero is not a link")
	}
}

func TestLinkedImageAndFocusPixels(t *testing.T) {
	dir := t.TempDir()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 60, 40))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "image.png"), data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	e := newTestEngine(t)
	if err := e.Load([]byte("[![picture](image.png)](next.md)\n\n[![fallback](missing.png)](#top)\n"), dir, "light", 24); err != nil {
		t.Fatal(err)
	}
	link, _ := e.Link(1)
	if len(link.Rects) != 1 || link.Rects[0].Width != 60 || link.Rects[0].Height != 40 {
		t.Fatalf("image link: %+v", link)
	}
	id, _ := e.HitLink(LinkRect{link.Rects[0].X + 30, link.Rects[0].Y + 20, 1, 1})
	if id != 1 {
		t.Fatal("image not clickable")
	}
	fallback, _ := e.Link(2)
	if len(fallback.Rects) == 0 {
		t.Fatal("image fallback lost enclosing link")
	}
	before, _, err := e.Frame(0)
	if err != nil {
		t.Fatal(err)
	}
	revision := e.Revision()
	e.FocusLink(1)
	focused, _, err := e.Frame(0)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, focused) || e.Revision() == revision {
		t.Fatal("focus did not paint or invalidate pixels")
	}
	e.FocusLink(0)
	cleared, _, err := e.Frame(0)
	if err != nil || !bytes.Equal(before, cleared) {
		t.Fatal("clearing focus changed document pixels", err)
	}
}

func TestHeadingAnchorsAndAmbiguousCells(t *testing.T) {
	e := newTestEngine(t)
	if err := e.Load([]byte("# A **B** `Code`!\n\n# A B Code\n\n# a-b-code-1\n\n# 日本語\n\n[a](#a-b-code)[b](#日本語)\n"), "", "dark", 24); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, h := range e.Headings() {
		ids = append(ids, h.ID)
	}
	if !reflect.DeepEqual(ids, []string{"a-b-code", "a-b-code-1", "a-b-code-1-1", "日本語"}) {
		t.Fatal(ids)
	}
	if y, ok := e.Anchor("日本語"); !ok || y <= 0 {
		t.Fatal("Unicode anchor missing")
	}
	if _, ok := e.Anchor("missing"); ok {
		t.Fatal("unknown heading resolved")
	}
	if y, ok := e.Anchor(""); !ok || y != 0 {
		t.Fatal("empty anchor must go to top")
	}
	a, _ := e.Link(1)
	b, _ := e.Link(2)
	area := LinkRect{a.Rects[0].X, a.Rects[0].Y, b.Rects[0].X + b.Rects[0].Width - a.Rects[0].X, a.Rects[0].Height}
	if id, ambiguous := e.HitLink(area); id != 0 || !ambiguous {
		t.Fatal("ambiguous cell guessed a destination", id, ambiguous)
	}
}
