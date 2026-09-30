package pager

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/liuyang1520/ma/internal/render"
	"github.com/liuyang1520/ma/internal/terminal"
)

type Document struct {
	Path, Name, BaseDir string
	Source              []byte
	Reload              func() ([]byte, error)
	Watch               func() ([]byte, error)
}

type linkTarget struct {
	path, fragment, external string
	anchor                   bool
}

func resolveLink(destination, currentPath, baseDir string) (linkTarget, error) {
	if len(destination) > 16<<10 || strings.IndexFunc(destination, unicode.IsControl) >= 0 {
		return linkTarget{}, fmt.Errorf("invalid link destination")
	}
	u, err := url.Parse(destination)
	if err != nil {
		return linkTarget{}, fmt.Errorf("invalid link: %w", err)
	}
	for _, s := range []string{u.Path, u.Fragment, u.Host} {
		if strings.IndexFunc(s, unicode.IsControl) >= 0 {
			return linkTarget{}, fmt.Errorf("invalid link destination")
		}
	}
	if u.Scheme == "" && u.Host != "" {
		u.Scheme = "https"
	}
	if u.Scheme != "" {
		if (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) && u.Hostname() != "" && u.Opaque == "" {
			return linkTarget{external: u.String()}, nil
		}
		return linkTarget{}, fmt.Errorf("unsupported link scheme: %s", u.Scheme)
	}
	if u.RawQuery != "" || u.ForceQuery {
		return linkTarget{}, fmt.Errorf("local Markdown links cannot contain a query")
	}
	target := linkTarget{fragment: u.Fragment, anchor: strings.Contains(destination, "#") || u.Path == ""}
	if u.Path == "" {
		return target, nil
	}
	if baseDir == "" && !filepath.IsAbs(u.Path) {
		return linkTarget{}, fmt.Errorf("relative file links need a Markdown filename; input is piped")
	}
	path := filepath.FromSlash(u.Path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	path = filepath.Clean(path)
	if path == currentPath {
		target.anchor = true
		return target, nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".md" && ext != ".markdown" {
		return linkTarget{}, fmt.Errorf("local links must point to .md or .markdown files")
	}
	target.path = path
	return target, nil
}

func browserCommand(platform, destination string) (string, []string) {
	if platform == "darwin" {
		return "open", []string{destination}
	}
	return "xdg-open", []string{destination}
}

func openURL(ctx context.Context, destination string) error {
	program, args := browserCommand(runtime.GOOS, destination)
	if err := exec.CommandContext(ctx, program, args...).Run(); err != nil {
		return fmt.Errorf("cannot open browser (%s): %w", program, err)
	}
	return nil
}

type visit struct {
	document Document // local visits retain only path/name/directory, not source or watchers
	position render.Position
	query    string
	match    int
}

func savedVisit(opts Options, position render.Position, query string, match int) visit {
	doc := Document{Path: opts.Path, Name: opts.Name, BaseDir: opts.BaseDir}
	if doc.Path == "" {
		doc.Source = opts.Source
	}
	return visit{document: doc, position: position, query: query, match: match}
}

type history struct{ back, forward []visit }

func appendVisit(stack []visit, v visit) []visit {
	const limit = 64
	if len(stack) == limit {
		copy(stack, stack[1:])
		stack = stack[:limit-1]
	}
	return append(stack, v)
}

func (h *history) push(v visit) {
	h.back = appendVisit(h.back, v)
	h.forward = nil
}

func (h *history) peek(forward bool) (visit, bool) {
	stack := h.back
	if forward {
		stack = h.forward
	}
	if len(stack) == 0 {
		return visit{}, false
	}
	return stack[len(stack)-1], true
}

func (h *history) commit(v visit, forward bool) {
	from, to := &h.back, &h.forward
	if forward {
		from, to = to, from
	}
	(*from)[len(*from)-1] = visit{}
	*from = (*from)[:len(*from)-1]
	*to = appendVisit(*to, v)
}

// Keep the presented viewport separate from queued scroll/reflow state.
type displayedView struct {
	engine        *render.Engine
	geometry      uint64
	size          terminal.Size
	y             float64
	width, height int
}

func (v displayedView) hit(e *render.Engine, mouse *terminal.Mouse, pixel bool, currentSize terminal.Size) (int, bool) {
	if mouse == nil || e != v.engine || e.GeometryRevision() != v.geometry || currentSize != v.size || mouse.X < 1 || mouse.Y < 1 {
		return 0, false
	}
	area := render.LinkRect{}
	if pixel {
		physicalHeight := float64(v.size.PixelHeight) * float64(v.size.Rows-1) / float64(v.size.Rows)
		if mouse.X > v.size.PixelWidth || float64(mouse.Y-1) >= physicalHeight {
			return 0, false
		}
		// Kitty scales the source crop to exactly columns × rows. Account for
		// rounding and overrides instead of assuming a 1:1 device scale.
		area.Width = float64(v.width) / float64(v.size.PixelWidth)
		area.Height = float64(v.height) / physicalHeight
		area.X = float64(mouse.X-1) * area.Width
		area.Y = v.y + float64(mouse.Y-1)*area.Height
	} else {
		if mouse.X > v.size.Columns || mouse.Y >= v.size.Rows {
			return 0, false
		}
		area.Width = float64(v.width) / float64(v.size.Columns)
		area.Height = float64(v.height) / float64(v.size.Rows-1)
		area.X = float64(mouse.X-1) * area.Width
		area.Y = v.y + float64(mouse.Y-1)*area.Height
	}
	return e.HitLink(area)
}

func prepareDocument(ctx context.Context, current *render.Engine, doc Document, theme string, fontSize int, query string, match int) (*render.Engine, int, error) {
	next, err := render.New(ctx)
	if err != nil {
		return nil, -1, err
	}
	w, h, scale := current.Dimensions()
	if err = next.Resize(w, h, scale); err == nil {
		err = next.Load(doc.Source, doc.BaseDir, theme, fontSize)
	}
	if err == nil && query != "" {
		var matches []float64
		matches, err = next.Search(query)
		match = min(max(0, match), len(matches)-1)
		if err == nil {
			err = next.SelectMatch(match)
		}
	} else {
		match = -1
	}
	if err != nil {
		next.Close()
		return nil, -1, err
	}
	return next, match, nil
}

func nextLink(ids []int, focused int, backwards bool) int {
	if len(ids) == 0 {
		return 0
	}
	for i, id := range ids {
		if id == focused {
			if backwards {
				return ids[(i-1+len(ids))%len(ids)]
			}
			return ids[(i+1)%len(ids)]
		}
	}
	if backwards {
		return ids[len(ids)-1]
	}
	return ids[0]
}

func closeClick(a, b *terminal.Mouse, pixel bool) bool {
	if a == nil || b == nil {
		return false
	}
	tolerance := 0.0
	if pixel {
		tolerance = 4
	}
	return math.Abs(float64(a.X)-float64(b.X)) <= tolerance && math.Abs(float64(a.Y)-float64(b.Y)) <= tolerance
}
