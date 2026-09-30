package render

import (
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/util"
)

// LinkRect uses logical document coordinates, independent of viewport crops.
type LinkRect struct{ X, Y, Width, Height float64 }
type Link struct {
	ID                 int
	Destination, Label string
	Rects              []LinkRect
}

// Index nodes once: table measurement and layout both traverse inline nodes.
func (e *Engine) indexLinks() error {
	e.links = nil
	e.linkIDs = make(map[ast.Node]int)
	return ast.Walk(e.root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if err := e.ctx.Err(); err != nil {
			return ast.WalkStop, err
		}
		if !entering {
			return ast.WalkContinue, nil
		}
		var destination, label string
		switch n := n.(type) {
		case *ast.Link:
			destination = html.UnescapeString(string(util.UnescapePunctuations(n.Destination)))
			var labelText strings.Builder
			for _, sp := range e.inline(n, style{}) {
				labelText.WriteString(sp.text)
			}
			label = labelText.String()
		case *ast.AutoLink:
			destination, label = string(n.URL(e.source)), string(n.Label(e.source))
			if n.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(destination), "mailto:") {
				destination = "mailto:" + destination
			}
		default:
			return ast.WalkContinue, nil
		}
		if len(e.links) >= 200_000 {
			return ast.WalkStop, fmt.Errorf("document exceeds native layout limit (200,000 links)")
		}
		id := len(e.links) + 1
		e.linkIDs[n] = id
		e.links = append(e.links, Link{ID: id, Destination: destination, Label: label})
		return ast.WalkContinue, nil
	})
}

func (e *Engine) addLinkRect(id int, r rect) {
	if id <= 0 || id > len(e.links) || r.w <= 0 || r.h <= 0 {
		return
	}
	l := &e.links[id-1]
	if len(l.Rects) > 0 {
		last := &l.Rects[len(l.Rects)-1]
		if last.Y == r.y && last.Height == r.h && math.Abs(last.X+last.Width-r.x) < .01 {
			last.Width = r.x + r.w - last.X
			return
		}
	}
	l.Rects = append(l.Rects, LinkRect{r.x, r.y, r.w, r.h})
}

func (e *Engine) Link(id int) (Link, bool) {
	if id <= 0 || id > len(e.links) {
		return Link{}, false
	}
	l := e.links[id-1]
	l.Rects = append([]LinkRect(nil), l.Rects...)
	return l, true
}

func (e *Engine) VisibleLinks(y, height float64) []int {
	var ids []int
	for _, l := range e.links {
		for _, r := range l.Rects {
			if r.Y+r.Height > y && r.Y < y+height {
				ids = append(ids, l.ID)
				break
			}
		}
	}
	return ids
}

// HitLink returns a unique link intersecting the pointer's area. Cell mouse
// reporting supplies a whole cell; ambiguous cells must not open a guessed URL.
func (e *Engine) HitLink(area LinkRect) (id int, ambiguous bool) {
	for _, l := range e.links {
		for _, r := range l.Rects {
			if r.X < area.X+area.Width && r.X+r.Width > area.X && r.Y < area.Y+area.Height && r.Y+r.Height > area.Y {
				if id != 0 && id != l.ID {
					return 0, true
				}
				id = l.ID
				break
			}
		}
	}
	return id, false
}

func (e *Engine) FocusLink(id int) {
	if id < 0 || id > len(e.links) {
		id = 0
	}
	if e.focusedLink != id {
		e.focusedLink = id
		e.revision++
	}
}

func (e *Engine) headingID(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || r == '_' || (!unicode.IsPunct(r) && !unicode.IsSymbol(r) && !unicode.IsSpace(r) && !unicode.IsControl(r)):
			b.WriteRune(r)
		}
	}
	base, id := b.String(), b.String()
	for suffix := e.anchorIDs[base]; e.anchorIDs[id] > 0; suffix++ {
		id = base + "-" + strconv.Itoa(suffix)
	}
	e.anchorIDs[base]++
	if id != base {
		e.anchorIDs[id]++
	}
	return id
}

func (e *Engine) Anchor(id string) (float64, bool) {
	if id == "" {
		return 0, true
	}
	for _, h := range e.headings {
		if h.ID == id {
			return math.Max(0, h.Y-24), true
		}
	}
	return 0, false
}
