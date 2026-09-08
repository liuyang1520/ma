package render

import (
	"html"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/util"
)

func (e *Engine) blocks(parent ast.Node, x, y, width float64, s style, depth int) float64 {
	if depth > 64 || !e.check() {
		return y
	}
	for n := parent.FirstChild(); n != nil && e.check(); n = n.NextSibling() {
		y = e.block(n, x, y, width, s, depth)
	}
	return y
}

func (e *Engine) block(n ast.Node, x, y, width float64, s style, depth int) float64 {
	switch n := n.(type) {
	case *ast.Heading:
		if n.PreviousSibling() != nil {
			y += 8
		}
		s.size = e.fontSize * []float64{2, 1.5, 1.25, 1, .875, .85}[n.Level-1]
		s.flags |= bold
		if n.Level == 6 {
			s.color = e.palette.muted
		}
		y = e.flow(e.inline(n, s), x, y, width, s, 1.25, false, extast.AlignLeft)
		if n.Level <= 2 {
			y += s.size * .3
			e.box(rect{x, y, width, 1}, e.palette.line, 0)
			y++
		}
		return y + 16
	case *ast.Paragraph:
		return e.flow(e.inline(n, s), x, y, width, s, 1.5, false, extast.AlignLeft) + 16
	case *ast.TextBlock:
		return e.flow(e.inline(n, s), x, y, width, s, 1.5, false, extast.AlignLeft)
	case *ast.FencedCodeBlock:
		return e.codeBlock(string(n.Lines().Value(e.source)), x, y, width, s)
	case *ast.CodeBlock:
		return e.codeBlock(string(n.Lines().Value(e.source)), x, y, width, s)
	case *ast.Blockquote:
		start := y
		index := len(e.ops)
		e.box(rect{}, e.palette.line, 0)
		s.color = e.palette.muted
		y = e.blocks(n, x+s.size, y, math.Max(1, width-s.size*2), s, depth+1)
		e.ops[index].rect = rect{x, start, 4, math.Max(s.size*1.5, y-start-16)}
		return y
	case *ast.List:
		for item, i := n.FirstChild(), n.Start; item != nil && e.check(); item, i = item.NextSibling(), i+1 {
			start := y
			var task *extast.TaskCheckBox
			if p := item.FirstChild(); p != nil {
				task, _ = p.FirstChild().(*extast.TaskCheckBox)
			}
			if task != nil {
				r := rect{x + s.size*.4, y + s.size*.35, s.size * .65, s.size * .65}
				e.box(r, e.palette.line, 3)
				if task.IsChecked {
					e.ops = append(e.ops, operation{rect: r, color: e.palette.link, checkmark: true, radius: 3})
				} else {
					e.box(rect{r.x + 1, r.y + 1, r.w - 2, r.h - 2}, e.palette.bg, 2)
				}
			} else {
				marker := "•"
				if n.IsOrdered() {
					marker = strconv.Itoa(i) + "."
				}
				e.flow([]span{{text: marker, style: s}}, x, start, s.size*1.4, s, 1.5, false, extast.AlignRight)
			}
			y = e.blocks(item, x+s.size*2, start, math.Max(1, width-s.size*2), s, depth+1)
			if n.IsTight && item.NextSibling() != nil {
				y += s.size * .25
			}
		}
		if n.IsTight {
			return y + 16
		}
		return y
	case *ast.ThematicBreak:
		y += 8
		e.box(rect{x, y, width, 4}, e.palette.line, 0)
		return y + 28
	case *extast.Table:
		return e.table(n, x, y, width, s)
	case *ast.HTMLBlock:
		return y
	default:
		return e.blocks(n, x, y, width, s, depth+1)
	}
}

func (e *Engine) codeBlock(text string, x, y, width float64, s style) float64 {
	start := y
	index := len(e.ops)
	e.box(rect{}, e.palette.soft, 6)
	s.flags |= mono
	s.size *= .85
	y = e.flow([]span{{text: strings.TrimSuffix(text, "\n"), style: s}}, x+16, y+16, math.Max(1, width-32), s, 1.45, true, extast.AlignLeft) + 16
	e.ops[index].rect = rect{x, start, width, y - start}
	return y + 16
}

func (e *Engine) inline(parent ast.Node, s style) []span {
	var spans []span
	var walk func(ast.Node, style, int)
	walk = func(n ast.Node, s style, depth int) {
		if depth > 64 {
			return
		}
		switch n := n.(type) {
		case *ast.Text:
			value := string(n.Segment.Value(e.source))
			if !n.IsRaw() {
				value = html.UnescapeString(string(util.UnescapePunctuations([]byte(value))))
			}
			spans = append(spans, span{text: value, style: s})
			if n.HardLineBreak() {
				spans = append(spans, span{text: "\n", style: s})
			} else if n.SoftLineBreak() {
				spans = append(spans, span{text: " ", style: s})
			}
			return
		case *ast.String:
			spans = append(spans, span{text: string(n.Value), style: s})
			return
		case *ast.Emphasis:
			if n.Level == 2 {
				s.flags |= bold
			} else {
				s.flags |= italic
			}
		case *ast.CodeSpan:
			var text strings.Builder
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					text.Write(t.Segment.Value(e.source))
					if t.SoftLineBreak() {
						text.WriteByte(' ')
					}
				}
			}
			s.size *= .85
			s.flags |= mono | code
			spans = append(spans, span{text: strings.ReplaceAll(text.String(), "\n", " "), style: s})
			return
		case *ast.Link, *ast.AutoLink:
			s.color = e.palette.link
			if link, ok := n.(*ast.AutoLink); ok {
				spans = append(spans, span{text: string(link.Label(e.source)), style: s})
				return
			}
		case *extast.Strikethrough:
			s.flags |= strike
		case *ast.Image:
			spans = append(spans, span{picture: string(n.Destination), text: string(n.Text(e.source)), style: s})
			return
		case *extast.TaskCheckBox, *ast.RawHTML:
			return
		}
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			walk(child, s, depth+1)
		}
	}
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		walk(n, s, 0)
	}
	return spans
}

type character struct {
	r           rune
	style       style
	face, index int
	width       float64
	left        float64
}

// flow wraps words across styling boundaries. A long word is split by glyph
// so a URL or unbroken code token cannot draw outside the content box.
func (e *Engine) flow(spans []span, x, y, width float64, base style, leading float64, pre bool, alignment extast.Alignment) float64 {
	var line []character
	lineWidth := 0.0
	flush := func(force bool) {
		if len(line) == 0 && !force {
			return
		}
		if !pre {
			for len(line) > 0 && line[len(line)-1].r == ' ' {
				lineWidth -= line[len(line)-1].width
				line = line[:len(line)-1]
			}
		}
		lineHeight := base.size * leading
		ascent, descent := 0.0, 0.0
		for _, c := range line {
			m := e.fonts.face(c.face, c.style.size).Metrics()
			ascent = math.Max(ascent, float64(m.Ascent)/64)
			descent = math.Max(descent, float64(m.Descent)/64)
			lineHeight = math.Max(lineHeight, c.style.size*leading)
		}
		lineHeight = math.Max(lineHeight, ascent+descent)
		baseline := y + (lineHeight-ascent-descent)/2 + ascent
		xpos := x
		if alignment == extast.AlignRight {
			xpos += math.Max(0, width-lineWidth)
		} else if alignment == extast.AlignCenter {
			xpos += math.Max(0, (width-lineWidth)/2)
		}
		start := len(e.glyphs)
		for _, c := range line {
			e.glyphs = append(e.glyphs, glyph{r: c.r, x: xpos + c.left, y: baseline, w: c.width - c.left, style: c.style, face: c.face, index: c.index})
			xpos += c.width
		}
		if len(e.glyphs) > start {
			e.ops = append(e.ops, operation{rect: rect{x, y, width, lineHeight}, start: start, end: len(e.glyphs)})
		}
		y += lineHeight
		line = nil
		lineWidth = 0
	}
	var word []character
	wordWidth := 0.0
	appendChar := func(c character) {
		if c.r == '\n' {
			flush(true)
			return
		}
		if !pre && c.r == ' ' && len(line) == 0 {
			return
		}
		if lineWidth+c.width > width && len(line) > 0 {
			flush(false)
			if !pre && c.r == ' ' {
				return
			}
		}
		line = append(line, c)
		lineWidth += c.width
	}
	flushWord := func() {
		if len(word) == 0 {
			return
		}
		if lineWidth+wordWidth > width && len(line) > 0 {
			flush(false)
		}
		for _, c := range word {
			appendChar(c)
		}
		word = nil
		wordWidth = 0
	}
	lastSpace := false
	for _, sp := range spans {
		if !e.check() {
			break
		}
		if sp.picture != "" {
			flushWord()
			flush(false)
			if im := e.picture(sp.picture); im != nil {
				w := math.Min(width, float64(im.Bounds().Dx()))
				h := w * float64(im.Bounds().Dy()) / float64(im.Bounds().Dx())
				e.ops = append(e.ops, operation{rect: rect{x, y, w, h}, image: im})
				y += h + 8
				continue
			}
			sp.text = "[image: " + sp.text + "]"
			sp.style.color = e.palette.muted
		}
		first := true
		for offset, r := range sp.text {
			if len(e.text)%4096 == 0 && !e.check() {
				break
			}
			if unicode.IsControl(r) && r != '\n' && r != '\t' {
				continue
			}
			if !pre && r == '\t' {
				r = ' '
			}
			if !pre && r == ' ' && lastSpace {
				continue
			}
			lastSpace = r == ' '
			count := 1
			if r == '\t' {
				count = 4
				r = ' '
			}
			for range count {
				index := len(e.text)
				e.text = append(e.text, r)
				id := e.fonts.selectFont(r, sp.style.flags, sp.style.size)
				advance, _ := e.fonts.face(id, sp.style.size).GlyphAdvance(r)
				w := float64(advance) / 64
				c := character{r: r, style: sp.style, face: id, index: index, width: w}
				if sp.style.flags&code != 0 {
					if first {
						c.left = 3
						c.width += 3
						first = false
					}
					if offset+len(string(r)) == len(sp.text) {
						c.width += 3
					}
				}
				if r == ' ' || r == '\n' {
					flushWord()
					appendChar(c)
				} else {
					if len(word) > 0 {
						prev := &word[len(word)-1]
						if prev.face == c.face && prev.style == c.style && c.left == 0 {
							kern := float64(e.fonts.face(id, sp.style.size).Kern(prev.r, r)) / 64
							prev.width += kern
							wordWidth += kern
						}
					}
					word = append(word, c)
					wordWidth += c.width
					if r == '-' || r == '/' || isCJK(r) {
						flushWord()
					}
				}
			}
		}
	}
	flushWord()
	flush(false)
	e.text = append(e.text, '\n')
	return y
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

func (e *Engine) table(n *extast.Table, x, y, width float64, s style) float64 {
	cols := len(n.Alignments)
	if cols == 0 {
		return y
	}
	widths := make([]float64, cols)
	for row := n.FirstChild(); row != nil; row = row.NextSibling() {
		for cell, i := row.FirstChild(), 0; cell != nil && i < cols; cell, i = cell.NextSibling(), i+1 {
			cs := s
			if _, ok := row.(*extast.TableHeader); ok {
				cs.flags |= bold
			}
			w := 0.0
			for _, sp := range e.inline(cell, cs) {
				if sp.style.flags&code != 0 {
					w += 6
				}
				for _, r := range sp.text {
					id := e.fonts.selectFont(r, sp.style.flags, sp.style.size)
					adv, _ := e.fonts.face(id, sp.style.size).GlyphAdvance(r)
					w += float64(adv) / 64
				}
			}
			widths[i] = math.Max(widths[i], math.Min(width*.6, w+26))
		}
	}
	total := 0.0
	for i, w := range widths {
		widths[i] = math.Max(40, w)
		total += widths[i]
	}
	if total > width {
		for i := range widths {
			widths[i] *= width / total
		}
		total = width
	}
	for row, rowIndex := n.FirstChild(), 0; row != nil && e.check(); row, rowIndex = row.NextSibling(), rowIndex+1 {
		start := y
		bgIndex := len(e.ops)
		bg := e.palette.bg
		if rowIndex%2 == 1 {
			bg = e.palette.soft
		}
		e.box(rect{}, bg, 0)
		right := x
		maxY := y
		for cell, i := row.FirstChild(), 0; cell != nil && i < cols; cell, i = cell.NextSibling(), i+1 {
			cs := s
			if rowIndex == 0 {
				cs.flags |= bold
			}
			padding := math.Min(13, widths[i]*.15)
			bottom := e.flow(e.inline(cell, cs), right+padding, y+6, math.Max(1, widths[i]-2*padding), cs, 1.5, false, n.Alignments[i]) + 6
			maxY = math.Max(maxY, bottom)
			right += widths[i]
		}
		y = math.Max(maxY, start+s.size*1.5+12)
		e.ops[bgIndex].rect = rect{x, start, total, y - start}
		e.box(rect{x, start, total, 1}, e.palette.line, 0)
		e.box(rect{x, y - 1, total, 1}, e.palette.line, 0)
		right = x
		e.box(rect{right, start, 1, y - start}, e.palette.line, 0)
		for _, w := range widths {
			right += w
			e.box(rect{right - 1, start, 1, y - start}, e.palette.line, 0)
		}
	}
	return y + 16
}
