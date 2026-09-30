package pager

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/liuyang1520/ma/internal/render"
	"github.com/liuyang1520/ma/internal/terminal"
)

func TestResolveMarkdownAndWebLinks(t *testing.T) {
	base := filepath.Join(t.TempDir(), "docs")
	current := filepath.Join(base, "readme.md")
	for _, tc := range []struct {
		destination string
		want        linkTarget
	}{
		{"#日本語", linkTarget{fragment: "日本語", anchor: true}},
		{"#", linkTarget{anchor: true}},
		{"", linkTarget{anchor: true}},
		{"readme.md#top", linkTarget{fragment: "top", anchor: true}},
		{"../guide/a%20b.MD#some%20heading", linkTarget{path: filepath.Join(filepath.Dir(base), "guide", "a b.MD"), fragment: "some heading", anchor: true}},
		{"setup.markdown", linkTarget{path: filepath.Join(base, "setup.markdown")}},
		{"https://example.com/a?q=1#part", linkTarget{external: "https://example.com/a?q=1#part"}},
		{"//example.com/path", linkTarget{external: "https://example.com/path"}},
	} {
		got, err := resolveLink(tc.destination, current, base)
		if err != nil || got != tc.want {
			t.Errorf("%q: got %+v/%v, want %+v", tc.destination, got, err, tc.want)
		}
	}
	for _, destination := range []string{"javascript:alert(1)", "data:text/plain,hello", "file:///etc/passwd", "mailto:somebody@example.com", "https:opaque", "https:///missing-host", "image.png", "guide.md?q=1", "bad%zz.md", "a%00b.md", "#%1b", "https://example.com/\n"} {
		if target, err := resolveLink(destination, current, base); err == nil {
			t.Errorf("accepted %q: %+v", destination, target)
		}
	}
	if _, err := resolveLink("guide.md", "", ""); err == nil {
		t.Fatal("relative link from stdin used the working directory")
	}
	if _, err := resolveLink("#section", "", ""); err != nil {
		t.Fatal("stdin anchor failed", err)
	}
}

func TestDisplayedLinkCoordinatesAndStaleFrames(t *testing.T) {
	e, err := render.New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.Resize(800, 600, 1.5); err != nil {
		t.Fatal(err)
	}
	if err := e.Load([]byte("[target](#target)\n\n# Target\n"), "", "dark", 24); err != nil {
		t.Fatal(err)
	}
	link, _ := e.Link(1)
	r := link.Rects[0]
	size := terminal.Size{Columns: 100, Rows: 31, PixelWidth: 2000, PixelHeight: 1550}
	v := displayedView{engine: e, geometry: e.GeometryRevision(), size: size, y: 20, width: 800, height: 600}
	m := &terminal.Mouse{X: int((r.X+r.Width/2)*2.5) + 1, Y: int((r.Y+r.Height/2-20)*2.5) + 1}
	if id, ambiguous := v.hit(e, m, true, size); id != 1 || ambiguous {
		t.Fatalf("scaled pixel hit: %d/%v", id, ambiguous)
	}
	// Cell reporting must use placement dimensions, not the device scale.
	cell := &terminal.Mouse{X: int((r.X+r.Width/2)/8) + 1, Y: int((r.Y+r.Height/2-20)/20) + 1}
	if id, ambiguous := v.hit(e, cell, false, size); id != 1 || ambiguous {
		t.Fatalf("cell hit: %d/%v", id, ambiguous)
	}
	for _, pointer := range []*terminal.Mouse{{X: 1, Y: 1501}, {X: 2001, Y: 1}, {X: 0, Y: 1}} {
		if id, _ := v.hit(e, pointer, true, size); id != 0 {
			t.Fatal("status or outside pointer activated a link")
		}
	}
	changed := size
	changed.PixelWidth++
	if id, _ := v.hit(e, m, true, changed); id != 0 {
		t.Fatal("click during resize used stale placement")
	}
	if err := e.Resize(700, 600, 1.5); err != nil {
		t.Fatal(err)
	}
	if id, _ := v.hit(e, m, true, size); id != 0 {
		t.Fatal("click during reflow used stale link regions")
	}
	if closeClick(m, &terminal.Mouse{X: m.X + 30, Y: m.Y}, true) {
		t.Fatal("drag was treated as a click")
	}
}

func TestNavigationHistoryIsBoundedAndDropsForwardBranch(t *testing.T) {
	var h history
	for i := 0; i < 70; i++ {
		h.push(visit{match: i})
	}
	if len(h.back) != 64 || h.back[0].match != 6 {
		t.Fatal("unbounded history")
	}
	last, ok := h.peek(false)
	if !ok || last.match != 69 {
		t.Fatal("wrong back destination")
	}
	h.commit(visit{match: 70}, false)
	if next, ok := h.peek(true); !ok || next.match != 70 {
		t.Fatal("forward destination lost")
	}
	h.commit(last, true)
	if last, ok := h.peek(false); !ok || last.match != 69 {
		t.Fatal("back/forward round trip failed")
	}
	h.commit(visit{}, false)
	h.push(visit{match: 100})
	if _, ok := h.peek(true); ok {
		t.Fatal("new navigation retained forward branch")
	}
	state := savedVisit(Options{Path: "/notes.md", Source: make([]byte, 1024), Watch: func() ([]byte, error) { return nil, nil }}, render.Position{}, "query", 2)
	if state.document.Source != nil || state.document.Watch != nil {
		t.Fatal("history retained local source/watch closures")
	}
	stdin := []byte("piped document")
	if state := savedVisit(Options{Source: stdin}, render.Position{}, "", -1); !bytes.Equal(state.document.Source, stdin) {
		t.Fatal("stdin history lost its only source")
	}
}

func TestFailedDocumentPreparationLeavesCurrentPixelsIntact(t *testing.T) {
	e := cacheEngine(t)
	before, _, err := e.Frame(0)
	if err != nil {
		t.Fatal(err)
	}
	if next, _, err := prepareDocument(context.Background(), e, Document{Source: make([]byte, 16<<20+1)}, "dark", 24, "", -1); err == nil || next != nil {
		t.Fatal("oversized destination was accepted")
	}
	after, _, err := e.Frame(0)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed destination replaced current pixels", err)
	}
}

func TestBrowserLaunchUsesOneLiteralArgument(t *testing.T) {
	destination := "https://example.com/?q=$(touch%20unexpected)&x=hello"
	for _, platform := range []string{"darwin", "linux"} {
		_, args := browserCommand(platform, destination)
		if !reflect.DeepEqual(args, []string{destination}) {
			t.Fatal("URL changed or split into arguments", args)
		}
	}
	dir := t.TempDir()
	program, _ := browserCommand(runtime.GOOS, destination)
	log := filepath.Join(dir, "argument")
	// A local fixture replaces the platform opener; no browser is launched.
	script := "#!/bin/sh\nprintf '%s' \"$1\" > \"$MA_TEST_URL_LOG\"\n"
	if err := os.WriteFile(filepath.Join(dir, program), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("MA_TEST_URL_LOG", log)
	if err := openURL(context.Background(), destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(log)
	if err != nil || string(got) != destination {
		t.Fatal("opener did not receive literal URL", string(got), err)
	}
	t.Setenv("PATH", filepath.Join(dir, "missing"))
	if err := openURL(context.Background(), destination); err == nil {
		t.Fatal("missing opener went unreported")
	}
}
