package pager

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/liuyang1520/ma/internal/render"
	"github.com/liuyang1520/ma/internal/terminal"
)

type Options struct {
	Path, Name, BaseDir, Theme string
	Source                     []byte
	FontSize                   int
	Scale                      float64
	ForceGraphics              bool
	Reload                     func() ([]byte, error)
	Watch                      func() ([]byte, error) // nil bytes means unchanged
	OpenDocument               func(string) (Document, error)
	OpenURL                    func(context.Context, string) error
}

func Run(ctx context.Context, opts Options) error {
	t, err := terminal.Open(opts.ForceGraphics)
	if err != nil {
		return err
	}
	defer t.Close()
	if err = t.Status(" ma | starting renderer...", opts.Theme == "light"); err != nil {
		return err
	}
	b, err := render.New(ctx)
	if err != nil {
		return err
	}
	defer func() { b.Close() }()
	cache := newImageCache()
	var size terminal.Size
	var width, height int
	var y, total float64
	var query, draft, message string
	var matches []float64
	var headings []render.Heading
	match := -1
	searching, help := false, false
	focused := 0
	var shown displayedView
	var pressed *terminal.Mouse
	pressedLink := 0
	var pressedGeometry uint64
	load := func() error {
		e := b.Load(opts.Source, opts.BaseDir, opts.Theme, opts.FontSize)
		if e != nil {
			return e
		}
		if query != "" {
			matches = b.MatchOffsets()
			match = min(max(0, match), len(matches)-1)
			e = b.SelectMatch(match)
		}
		b.FocusLink(focused)
		headings = b.Headings()
		total = b.Viewport(y).Height
		return e
	}
	resize := func() error {
		position := b.Position(y)
		size = t.Size()
		scale := opts.Scale
		if scale == 0 {
			scale = math.Max(1, float64(size.PixelHeight)/float64(size.Rows)/24)
		}
		width = max(1, int(float64(size.PixelWidth)/scale))
		height = max(1, int(float64(size.PixelHeight)*float64(size.Rows-1)/float64(size.Rows)/scale))
		if e := b.Resize(width, height, scale); e != nil {
			return e
		}
		y = b.Restore(position)
		total = b.Viewport(y).Height
		headings = b.Headings()
		if query != "" {
			matches = b.MatchOffsets()
			return b.SelectMatch(match)
		}
		return nil
	}
	if err = resize(); err != nil {
		return err
	}
	// Set the actual terminal width before the first layout.
	if err = load(); err != nil {
		return err
	}
	status := func() error {
		text := fmt.Sprintf(" ma | %s | %.0f%% | Tab links  h/l history  / search  q quit  ? help", opts.Name, percent(y, total, float64(height)))
		if opts.Watch != nil {
			text = strings.Replace(text, " | ", " | watch | ", 1)
		}
		if query != "" {
			text = fmt.Sprintf(" ma | %s | match %d/%d | n/N next/previous | q quit", query, match+1, len(matches))
		}
		if link, ok := b.Link(focused); ok {
			text = " ma | Enter open | " + link.Destination
		}
		if message != "" {
			text = " ma | " + message
		}
		if help {
			text = " Tab/Enter links | h/l history | j/k scroll | / search | +/- zoom | t theme | r reload | q quit"
		}
		if searching {
			text = " /" + draft + "_"
		}
		return t.Status(text, opts.Theme == "light")
	}
	draw := func() error {
		metrics, e := cache.Present(t.Out, b, math.Max(0, y), size.Columns, size.Rows-1)
		if e != nil {
			return e
		}
		y, total = metrics.Y, metrics.Height
		shown = displayedView{engine: b, geometry: b.GeometryRevision(), size: size, y: y, width: width, height: height}
		return status()
	}
	if err = draw(); err != nil {
		return err
	}
	frameTimer := time.NewTimer(time.Hour)
	frameTimer.Stop()
	defer frameTimer.Stop()
	var paint <-chan time.Time
	lastPaint := time.Now()
	checkSize := time.NewTicker(200 * time.Millisecond)
	defer checkSize.Stop()
	dirty := false
	schedule := func() {
		dirty = true
		if paint == nil {
			frameTimer.Reset(max(0, 16*time.Millisecond-time.Since(lastPaint)))
			paint = frameTimer.C
		}
	}
	var watch <-chan time.Time
	var watchError string
	if opts.Watch != nil {
		watchTimer := time.NewTicker(500 * time.Millisecond)
		defer watchTimer.Stop()
		watch = watchTimer.C
	}
	var visits history
	currentVisit := func() visit { return savedVisit(opts, b.Position(y), query, match) }
	installDocument := func(doc Document, next *render.Engine, state visit, selected int) {
		b.Close()
		b = next
		opts.Path, opts.Name, opts.BaseDir, opts.Source = doc.Path, doc.Name, doc.BaseDir, doc.Source
		opts.Reload, opts.Watch = doc.Reload, doc.Watch
		query, match = state.query, selected
		matches, headings = b.MatchOffsets(), b.Headings()
		y = b.Restore(state.position)
		total = b.Viewport(y).Height
		focused, searching, help = 0, false, false
		pressed, watchError, message = nil, "", ""
		cache.Invalidate()
		dirty = true
	}
	travel := func(forward bool) error {
		state, ok := visits.peek(forward)
		if !ok {
			return fmt.Errorf("No navigation history")
		}
		previous := currentVisit()
		if state.document.Path == opts.Path {
			if query != state.query {
				var e error
				matches, e = b.Search(state.query)
				if e != nil {
					return e
				}
			}
			query, match = state.query, min(max(0, state.match), len(matches)-1)
			if e := b.SelectMatch(match); e != nil {
				return e
			}
			y = b.Restore(state.position)
			focused = 0
			b.FocusLink(0)
			pressed = nil
			dirty = true
		} else {
			doc := state.document
			if doc.Path != "" {
				if opts.OpenDocument == nil {
					return fmt.Errorf("file navigation is unavailable")
				}
				var e error
				doc, e = opts.OpenDocument(doc.Path)
				if e != nil {
					return e
				}
			}
			next, selected, e := prepareDocument(ctx, b, doc, opts.Theme, opts.FontSize, state.query, state.match)
			if e != nil {
				return e
			}
			installDocument(doc, next, state, selected)
		}
		visits.commit(previous, forward)
		return nil
	}
	type openedURL struct {
		destination string
		err         error
	}
	opened := make(chan openedURL, 1)
	opening := false
	activate := func(id int) error {
		link, ok := b.Link(id)
		if !ok {
			return nil
		}
		target, e := resolveLink(link.Destination, opts.Path, opts.BaseDir)
		if e != nil {
			return e
		}
		if target.external != "" {
			if opening {
				return fmt.Errorf("Browser launch is already in progress")
			}
			opening = true
			message = "Opening " + target.external
			opener := opts.OpenURL
			if opener == nil {
				opener = openURL
			}
			go func() {
				result := openedURL{destination: target.external, err: opener(ctx, target.external)}
				select {
				case opened <- result:
				case <-ctx.Done():
				}
			}()
			return nil
		}
		previous := currentVisit()
		if target.path == "" {
			offset, found := b.Anchor(target.fragment)
			if !found {
				return fmt.Errorf("Heading not found: #%s", target.fragment)
			}
			y = offset
			focused = 0
			b.FocusLink(0)
			dirty = true
		} else {
			if opts.OpenDocument == nil {
				return fmt.Errorf("file navigation is unavailable")
			}
			doc, e := opts.OpenDocument(target.path)
			if e != nil {
				return e
			}
			next, selected, e := prepareDocument(ctx, b, doc, opts.Theme, opts.FontSize, "", -1)
			if e != nil {
				return e
			}
			offset := 0.0
			if target.anchor {
				var found bool
				offset, found = next.Anchor(target.fragment)
				if !found {
					next.Close()
					return fmt.Errorf("Heading not found: #%s", target.fragment)
				}
			}
			installDocument(doc, next, visit{position: next.Position(offset)}, selected)
		}
		visits.push(previous)
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case result := <-opened:
			opening = false
			message = "Opened " + result.destination
			if result.err != nil {
				message = result.err.Error()
			}
			if err = status(); err != nil {
				return err
			}
		case <-checkSize.C:
			if t.Size() != size {
				if err = resize(); err != nil {
					return err
				}
				schedule()
			}
		case <-paint:
			paint = nil
			if dirty {
				if err = draw(); err != nil {
					return err
				}
				dirty = false
				lastPaint = time.Now()
			}
		case <-watch:
			if opts.Watch == nil {
				continue
			}
			source, e := opts.Watch()
			if e != nil {
				if watchError != e.Error() {
					watchError = e.Error()
					message = watchError
					if err = status(); err != nil {
						return err
					}
				}
				continue
			}
			if watchError != "" {
				if message == watchError {
					message = ""
					if err = status(); err != nil {
						return err
					}
				}
				watchError = ""
			}
			if source != nil {
				position := b.Position(y)
				focused = 0
				opts.Source = source
				if err = load(); err != nil {
					return err
				}
				message = ""
				y = b.Restore(position)
				pressed = nil
				schedule()
			}
		case e, ok := <-t.Events:
			if !ok {
				return nil
			}
			if e.Key == "graphics" {
				retry, err := cache.HandleReply(e.Text)
				if err != nil {
					return err
				}
				if retry {
					schedule()
				}
				continue
			}
			if e.Key == "mousemode" {
				pressed = nil
				if err = t.UpdateMouseMode(e); err != nil {
					return err
				}
				continue
			}
			if strings.HasPrefix(e.Key, "size") {
				t.UpdateCellSize(e)
				if t.Size() != size {
					if err = resize(); err != nil {
						return err
					}
					schedule()
				}
				continue
			}
			if e.Key == "quit" {
				return nil
			}
			if e.Key == "mouse" {
				m := e.Mouse
				if searching || m == nil || m.Button != 0 || m.Modifiers != 0 || m.Motion {
					pressed = nil
					continue
				}
				id, ambiguous := shown.hit(b, m, t.PixelMouse, t.Size())
				if ambiguous {
					pressed = nil
					message = "Several links share this cell; use Tab and Enter to choose"
				} else if !m.Released {
					pressed, pressedLink, pressedGeometry = m, id, b.GeometryRevision()
				} else {
					if id != 0 && id == pressedLink && pressedGeometry == b.GeometryRevision() && closeClick(pressed, m, t.PixelMouse) {
						message = ""
						if e := activate(id); e != nil {
							message = e.Error()
						}
					}
					pressed = nil
				}
				if dirty {
					schedule()
				} else if err = status(); err != nil {
					return err
				}
				continue
			}
			if searching {
				switch e.Key {
				case "escape":
					searching = false
					draft = query
				case "enter":
					searching = false
					query = draft
					matches, err = b.Search(query)
					if err != nil {
						return err
					}
					match = -1
					for i, offset := range matches {
						if offset >= y {
							match = i
							break
						}
					}
					if match < 0 && len(matches) > 0 {
						match = 0
					}
					if match >= 0 {
						y = math.Max(0, matches[match]-24)
						if err = b.SelectMatch(match); err != nil {
							return err
						}
					}
					if query != "" && len(matches) == 0 {
						message = "No matches for " + query
					}
					schedule()
				case "backspace":
					r := []rune(draft)
					if len(r) > 0 {
						draft = string(r[:len(r)-1])
					}
				default:
					if len(draft) < 512 && e.Text != "" {
						r := []rune(e.Text)
						if len(r) == 1 && !unicode.IsControl(r[0]) {
							draft += e.Text
						}
					}
				}
				if err = status(); err != nil {
					return err
				}
				continue
			}
			message = ""
			pressed = nil
			oldY := y
			switch e.Key {
			case "q":
				return nil
			case "enter":
				if focused != 0 {
					if e := activate(focused); e != nil {
						message = e.Error()
					}
				} else {
					y += float64(opts.FontSize) * 1.5
				}
			case "tab", "backtab":
				focused = nextLink(b.VisibleLinks(y, float64(height)), focused, e.Key == "backtab")
				b.FocusLink(focused)
				dirty = true
			case "h", "back", "l", "forward":
				if e := travel(e.Key == "l" || e.Key == "forward"); e != nil {
					message = e.Error()
				}
			case "j", "down":
				y += float64(opts.FontSize) * 1.5
			case "k", "up":
				y -= float64(opts.FontSize) * 1.5
			case "wheeldown":
				y += float64(opts.FontSize) * 4.5
			case "wheelup":
				y -= float64(opts.FontSize) * 4.5
			case " ", "f", "pagedown":
				y += float64(height) * .9
			case "b", "pageup":
				y -= float64(height) * .9
			case "d", "halfdown":
				y += float64(height) * .5
			case "u", "halfup":
				y -= float64(height) * .5
			case "g", "home":
				y = 0
			case "G", "end":
				y = math.Max(0, total-float64(height))
			case "[", "]":
				if heading, ok := adjacentHeading(headings, y, e.Key == "]"); ok {
					y = math.Max(0, heading.Y-24)
					message = heading.Text
				}
			case "/":
				focused = 0
				b.FocusLink(0)
				dirty = true
				searching = true
				draft = ""
				help = false
			case "escape":
				focused = 0
				b.FocusLink(0)
				query = ""
				matches = nil
				_, err = b.Search("")
				if err != nil {
					return err
				}
				dirty = true
				help = false
			case "n", "N":
				if len(matches) > 0 {
					if e.Key == "n" {
						match = (match + 1) % len(matches)
					} else {
						match = (match - 1 + len(matches)) % len(matches)
					}
					y = math.Max(0, matches[match]-24)
					if err = b.SelectMatch(match); err != nil {
						return err
					}
					dirty = true
				}
			case "+", "=", "-", "t", "r":
				position := b.Position(y)
				if e.Key == "+" || e.Key == "=" {
					opts.FontSize = min(32, opts.FontSize+1)
				}
				if e.Key == "-" {
					opts.FontSize = max(10, opts.FontSize-1)
				}
				if e.Key == "t" {
					if opts.Theme == "light" {
						opts.Theme = "dark"
					} else {
						opts.Theme = "light"
					}
				}
				if e.Key == "r" && opts.Reload != nil {
					source, e := opts.Reload()
					if e != nil {
						message = e.Error()
						break
					}
					opts.Source = source
					focused = 0
				}
				if err = load(); err != nil {
					return err
				}
				y = b.Restore(position)
				total = b.Viewport(y).Height
				dirty = true
			case "?":
				help = !help
			case "redraw":
				cache.Invalidate()
				dirty = true
			}
			y = math.Max(0, math.Min(y, math.Max(0, total-float64(height))))
			if y != oldY {
				dirty = true
				focused = 0
				b.FocusLink(0)
			}
			if !dirty {
				if err = status(); err != nil {
					return err
				}
			} else {
				schedule()
			}
		}
	}
}

func adjacentHeading(headings []render.Heading, y float64, forward bool) (render.Heading, bool) {
	if forward {
		for _, h := range headings {
			if h.Y > y+33 {
				return h, true
			}
		}
	} else {
		for i := len(headings) - 1; i >= 0; i-- {
			if headings[i].Y < y+23 {
				return headings[i], true
			}
		}
	}
	return render.Heading{}, false
}

func percent(y, total, height float64) float64 {
	if total <= height {
		return 100
	}
	return math.Min(100, math.Max(0, y/(total-height)*100))
}
