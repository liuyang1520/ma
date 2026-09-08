// Package render lays out Markdown and paints only the visible viewport.
// All layout, search geometry, and drawing are native Go; no HTML or browser.
package render

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

type Metrics struct{ Height, Y float64 }
type palette struct{ bg, fg, muted, line, soft, code, link color.RGBA }

func rgb(v uint32) color.RGBA { return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255} }
func colors(theme string) palette {
	if theme == "light" {
		return palette{rgb(0xffffff), rgb(0x1f2328), rgb(0x59636e), rgb(0xd1d9e0), rgb(0xf6f8fa), rgb(0xeff1f3), rgb(0x0969da)}
	}
	return palette{rgb(0x0d1117), rgb(0xe6edf3), rgb(0x9198a1), rgb(0x3d444d), rgb(0x151b23), rgb(0x232830), rgb(0x4493f8)}
}

type style struct {
	size  float64
	flags int
	color color.RGBA
}
type span struct {
	text    string
	style   style
	picture string
}
type glyph struct {
	r           rune
	x, y, w     float64
	style       style
	face, index int
}
type rect struct{ x, y, w, h float64 }
type operation struct {
	rect
	color      color.RGBA
	radius     float64
	image      image.Image
	checkmark  bool
	start, end int // glyph range, or empty for rectangles/images
}
type matchRange struct {
	start, end int
	y          float64
}

type Engine struct {
	ctx            context.Context
	fonts          *fonts
	source         []byte
	root           ast.Node
	baseDir        string
	fontSize       float64
	width, height  int
	scale          float64
	palette        palette
	ops            []operation
	glyphs         []glyph
	text           []rune
	documentHeight float64
	query          string
	matches        []matchRange
	active         int
	images         map[string]image.Image
	imageBudget    int64
	err            error
	revision       uint64
}

func New(ctx context.Context) (*Engine, error) {
	f, err := newFonts()
	if err != nil {
		return nil, err
	}
	return &Engine{ctx: ctx, fonts: f, width: 800, height: 600, scale: 1, active: -1}, nil
}
func (e *Engine) Close()                          { e.fonts.closeFaces() }
func (e *Engine) Revision() uint64                { return e.revision }
func (e *Engine) Dimensions() (int, int, float64) { return e.width, e.height, e.scale }

// Viewport describes scrolling without allocating or painting any pixels.
func (e *Engine) Viewport(y float64) Metrics {
	if math.IsNaN(y) || math.IsInf(y, 0) {
		y = 0
	}
	return Metrics{Height: math.Max(e.documentHeight, float64(e.height)), Y: math.Max(0, math.Min(y, math.Max(0, e.documentHeight-float64(e.height))))}
}

func (e *Engine) Load(source []byte, baseDir, theme string, fontSize int) error {
	if len(source) > 16<<20 {
		return fmt.Errorf("Markdown exceeds 16 MiB input limit")
	}
	if fontSize < 10 || fontSize > 32 {
		return fmt.Errorf("font size must be between 10 and 32")
	}
	if theme != "light" && theme != "dark" {
		return fmt.Errorf("theme must be light or dark")
	}
	if e.root == nil || !bytes.Equal(e.source, source) || baseDir != e.baseDir {
		e.source = bytes.Clone(source)
		e.root = goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader(e.source))
	}
	e.images = make(map[string]image.Image)
	e.imageBudget = 128 << 20
	e.baseDir = baseDir
	e.fontSize = float64(fontSize)
	e.palette = colors(theme)
	return e.layout()
}

func (e *Engine) Resize(width, height int, scale float64) error {
	if width < 1 || height < 1 || width > 16384 || height > 16384 || math.IsNaN(scale) || math.IsInf(scale, 0) || scale < .5 || scale > 4 || float64(width)*float64(height)*scale*scale > 32_000_000 {
		return fmt.Errorf("viewport must fit within 32 megapixels; scale must be between 0.5 and 4")
	}
	reflow := e.width != width
	if e.width != width || e.height != height || e.scale != scale {
		e.revision++
	}
	e.width = width
	e.height = height
	e.scale = scale
	if reflow && e.root != nil {
		return e.layout()
	}
	return nil
}

func (e *Engine) layout() error {
	e.fonts.closeFaces()
	e.ops = nil
	e.glyphs = nil
	e.text = nil
	e.err = nil
	padding := 32.0
	if e.width < 600 {
		padding = 20
	}
	width := math.Min(float64(e.width), 1012)
	x := (float64(e.width)-width)/2 + padding
	y := e.blocks(e.root, x, padding, math.Max(1, width-2*padding), style{size: e.fontSize, color: e.palette.fg}, 0)
	e.documentHeight = y + padding
	if e.err != nil {
		return e.err
	}
	_, err := e.Search(e.query)
	return err
}

// The search index uses one Unicode rune per character, so case folding never
// changes offsets. Formatting boundaries and soft wraps remain searchable.
func (e *Engine) Search(query string) ([]float64, error) {
	if err := e.ctx.Err(); err != nil {
		return nil, err
	}
	e.query = query
	e.revision++
	e.matches = nil
	e.active = -1
	needle := []rune(query)
	if len(needle) == 0 {
		return nil, nil
	}
	for i := range needle {
		needle[i] = unicode.ToLower(needle[i])
	}
	// KMP bounds repeated-character searches to O(document + query).
	prefix := make([]int, len(needle))
	for i, j := 1, 0; i < len(needle); i++ {
		for j > 0 && needle[i] != needle[j] {
			j = prefix[j-1]
		}
		if needle[i] == needle[j] {
			j++
		}
		prefix[i] = j
	}
	for i, j := 0, 0; i < len(e.text); i++ {
		r := unicode.ToLower(e.text[i])
		for j > 0 && r != needle[j] {
			j = prefix[j-1]
		}
		if r == needle[j] {
			j++
		}
		if j == len(needle) {
			e.matches = append(e.matches, matchRange{start: i - j + 1, end: i + 1})
			j = 0
		}
	}
	var offsets []float64
	g := 0
	for i := range e.matches {
		for g < len(e.glyphs) && e.glyphs[g].index < e.matches[i].start {
			g++
		}
		if g < len(e.glyphs) {
			e.matches[i].y = e.glyphs[g].y - e.glyphs[g].style.size
		}
		offsets = append(offsets, math.Max(0, e.matches[i].y))
	}
	return offsets, nil
}
func (e *Engine) SelectMatch(index int) error {
	if e.active != index {
		e.revision++
	}
	e.active = index
	return nil
}

func (e *Engine) check() bool {
	if e.err != nil {
		return false
	}
	if len(e.text) > 1_000_000 || len(e.ops) > 200_000 {
		e.err = fmt.Errorf("document exceeds native layout limit (1 million characters or 200,000 drawing operations)")
		return false
	}
	e.err = e.ctx.Err()
	return e.err == nil
}
