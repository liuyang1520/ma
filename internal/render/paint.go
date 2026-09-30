package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"sort"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

func (e *Engine) box(r rect, c color.RGBA, radius float64) {
	e.ops = append(e.ops, operation{rect: r, color: c, radius: radius})
}

func (e *Engine) Frame(y float64) ([]byte, Metrics, error) {
	metrics := e.Viewport(y)
	im, err := e.Canvas(metrics.Y, float64(e.height))
	if err != nil {
		return nil, Metrics{}, err
	}
	var buf bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&buf, im); err != nil {
		return nil, Metrics{}, err
	}
	return buf.Bytes(), metrics, nil
}

// Canvas paints an arbitrary document band into native pixels. PNG encoding
// is used only by Frame/export; the interactive pager uploads these pixels.
func (e *Engine) Canvas(y, height float64) (*image.RGBA, error) {
	return e.CanvasInto(nil, y, height)
}

// CanvasInto lets a caller reuse its own scratch pixels after uploading them.
// Canvas and Frame still return independent images for callers retaining crops.
func (e *Engine) CanvasInto(im *image.RGBA, y, height float64) (*image.RGBA, error) {
	if err := e.ctx.Err(); err != nil {
		return nil, err
	}
	w, h := math.Round(float64(e.width)*e.scale), math.Round(height*e.scale)
	if math.IsNaN(y) || math.IsInf(y, 0) || y < 0 || math.IsNaN(height) || math.IsInf(height, 0) || w < 1 || h < 1 || w*h > 32_000_000 {
		return nil, fmt.Errorf("invalid canvas region or greater than 32 megapixels")
	}
	bounds := image.Rect(0, 0, int(w), int(h))
	if im == nil || im.Bounds() != bounds {
		im = image.NewRGBA(bounds)
	}
	draw.Draw(im, im.Bounds(), image.NewUniform(e.palette.bg), image.Point{}, draw.Src)
	for _, group := range e.groups {
		if group.bottom < y || group.top > y+height {
			continue
		}
		for _, op := range e.ops[group.start:group.end] {
			if op.y+op.h < y || op.y > y+height {
				continue
			}
			if err := e.ctx.Err(); err != nil {
				return nil, err
			}
			if op.checkmark {
				r := pixels(op.rect, y, e.scale)
				fill(im, r, op.color, op.radius*e.scale)
				// A filled polygon makes the check independent of font coverage.
				v := vector.NewRasterizer(r.Dx(), r.Dy())
				points := [][2]float32{{.16, .51}, {.28, .39}, {.43, .56}, {.74, .22}, {.86, .34}, {.43, .81}}
				for i, p := range points {
					px, py := p[0]*float32(r.Dx()), p[1]*float32(r.Dy())
					if i == 0 {
						v.MoveTo(px, py)
					} else {
						v.LineTo(px, py)
					}
				}
				v.ClosePath()
				v.Draw(im, r, image.NewUniform(rgb(0xffffff)), image.Point{})
			} else if op.image != nil {
				r := pixels(op.rect, y, e.scale)
				xdraw.BiLinear.Scale(im, r, op.image, op.image.Bounds(), draw.Over, nil)
			} else if op.end > op.start {
				e.paintLine(im, op, y)
			} else {
				fill(im, pixels(op.rect, y, e.scale), op.color, op.radius*e.scale)
			}
		}
	}
	if e.focusedLink > 0 && e.focusedLink <= len(e.links) {
		for _, r := range e.links[e.focusedLink-1].Rects {
			if r.Y+r.Height < y || r.Y > y+height {
				continue
			}
			for _, edge := range []rect{{r.X, r.Y, r.Width, 1}, {r.X, r.Y + r.Height - 1, r.Width, 1}, {r.X, r.Y, 1, r.Height}, {r.X + r.Width - 1, r.Y, 1, r.Height}} {
				fill(im, pixels(edge, y, e.scale), e.palette.link, 0)
			}
		}
	}
	return im, nil
}

func (e *Engine) paintLine(im *image.RGBA, op operation, y float64) {
	// Clip each line to its content box, including exceptionally narrow cells.
	im = im.SubImage(pixels(op.rect, y, e.scale)).(*image.RGBA)
	gs := e.glyphs[op.start:op.end]
	// Inline-code backgrounds are grouped to keep rounded corners at run ends.
	for i := 0; i < len(gs); {
		if gs[i].style.flags&code == 0 {
			i++
			continue
		}
		j := i + 1
		for j < len(gs) && gs[j].style.flags&code != 0 {
			j++
		}
		first, last := gs[i], gs[j-1]
		fill(im, pixels(rect{first.x - 3, op.y + 2, last.x + last.w - first.x + 6, op.h - 4}, y, e.scale), e.palette.code, 4*e.scale)
		i = j
	}
	for _, g := range gs {
		c := g.style.color
		index := sort.Search(len(e.matches), func(i int) bool { return e.matches[i].end > g.index })
		if index < len(e.matches) && e.matches[index].start <= g.index {
			bg := rgb(0x9e6a03)
			c = rgb(0xffffff)
			if index == e.active {
				bg = rgb(0xf2cc60)
				c = rgb(0x0d1117)
			}
			fill(im, pixels(rect{g.x, op.y, g.w, op.h}, y, e.scale), bg, 0)
		}
		dot := fixed.Point26_6{X: fixed.Int26_6(math.Round(g.x * e.scale * 64)), Y: fixed.Int26_6(math.Round((g.y - y) * e.scale * 64))}
		dr, mask := e.fonts.glyph(g.face, g.style.size*e.scale, dot, g.r)
		if !dr.Empty() {
			ink := e.inks[c]
			if ink == nil {
				ink = image.NewUniform(c)
				e.inks[c] = ink
			}
			draw.DrawMask(im, dr, ink, image.Point{}, mask, image.Point{}, draw.Over)
		}
		if g.style.flags&strike != 0 {
			fill(im, pixels(rect{g.x, g.y - g.style.size*.3, g.w, 1}, y, e.scale), c, 0)
		}
	}
}

func pixels(r rect, y, scale float64) image.Rectangle {
	return image.Rect(int(math.Round(r.x*scale)), int(math.Round((r.y-y)*scale)), int(math.Round((r.x+r.w)*scale)), int(math.Round((r.y+r.h-y)*scale)))
}

func fill(im *image.RGBA, r image.Rectangle, c color.RGBA, radius float64) {
	ink := image.NewUniform(c)
	if radius <= 0 {
		draw.Draw(im, r, ink, image.Point{}, draw.Src)
		return
	}
	radius = math.Min(radius, float64(min(r.Dx(), r.Dy()))/2)
	for y := max(r.Min.Y, im.Bounds().Min.Y); y < min(r.Max.Y, im.Bounds().Max.Y); y++ {
		dy := math.Max(0, math.Max(float64(r.Min.Y)+radius-(float64(y)+.5), (float64(y)+.5)-(float64(r.Max.Y)-radius)))
		inset := radius - math.Sqrt(math.Max(0, radius*radius-dy*dy))
		line := image.Rect(r.Min.X+int(math.Round(inset)), y, r.Max.X-int(math.Round(inset)), y+1)
		draw.Draw(im, line, ink, image.Point{}, draw.Src)
	}
}
