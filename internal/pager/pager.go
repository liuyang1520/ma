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
	Name, BaseDir, Theme string
	Source               []byte
	FontSize             int
	Scale                float64
	ForceGraphics        bool
	Reload               func() ([]byte, error)
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
	defer b.Close()
	cache := newImageCache()
	var size terminal.Size
	var width, height int
	var y, total float64
	var query, draft, message string
	var matches []float64
	match := -1
	searching, help := false, false
	load := func() error {
		e := b.Load(opts.Source, opts.BaseDir, opts.Theme, opts.FontSize)
		if e != nil {
			return e
		}
		if query != "" {
			matches, e = b.Search(query)
			match = -1
		}
		return e
	}
	if err = load(); err != nil {
		return err
	}
	resize := func() error {
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
		if query != "" {
			var e error
			matches, e = b.Search(query)
			if e != nil {
				return e
			}
			return b.SelectMatch(match)
		}
		return nil
	}
	if err = resize(); err != nil {
		return err
	}
	status := func() error {
		text := fmt.Sprintf(" ma | %s | %.0f%% | q quit  / search  ? help", opts.Name, percent(y, total, float64(height)))
		if query != "" {
			text = fmt.Sprintf(" ma | %s | match %d/%d | n/N next/previous | q quit", query, match+1, len(matches))
		}
		if message != "" {
			text = " ma | " + message
		}
		if help {
			text = " j/k scroll | space/b page | d/u half | g/G ends | / search | +/- zoom | t theme | r reload | q quit"
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
		return status()
	}
	if err = draw(); err != nil {
		return err
	}
	paint := time.NewTicker(16 * time.Millisecond)
	defer paint.Stop()
	checkSize := time.NewTicker(200 * time.Millisecond)
	defer checkSize.Stop()
	dirty := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-checkSize.C:
			if t.Size() != size {
				if err = resize(); err != nil {
					return err
				}
				dirty = true
			}
		case <-paint.C:
			if dirty {
				if err = draw(); err != nil {
					return err
				}
				dirty = false
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
				dirty = dirty || retry
				continue
			}
			if strings.HasPrefix(e.Key, "size") {
				continue
			}
			if e.Key == "quit" {
				return nil
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
					dirty = true
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
			oldY := y
			switch e.Key {
			case "q":
				return nil
			case "j", "down", "enter":
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
			case "/":
				searching = true
				draft = ""
				help = false
			case "escape":
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
				}
				if err = load(); err != nil {
					return err
				}
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
			}
			if !dirty {
				if err = status(); err != nil {
					return err
				}
			}
		}
	}
}

func percent(y, total, height float64) float64 {
	if total <= height {
		return 100
	}
	return math.Min(100, math.Max(0, y/(total-height)*100))
}
