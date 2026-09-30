package render

import (
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

const (
	bold = 1 << iota
	italic
	mono
	strike
	code
)

// Fonts are bundled so the binary works with an empty PATH and no system
// fonts. Prefer familiar system faces where available; load CJK lazily.
type fonts struct {
	parsed    []*opentype.Font
	faces     map[faceKey]font.Face
	cjk       bool
	masks     map[glyphKey]glyphMask
	maskBytes int
}
type faceKey struct {
	id   int
	size float64
}

type advanceResult struct {
	width fixed.Int26_6
	ok    bool
}

type runePair struct{ first, second rune }

// Repeated words should not repeatedly look up font tables during layout.
// Caps keep documents with many distinct characters/pairs bounded.
type measuredFace struct {
	font.Face
	advances map[rune]advanceResult
	kerns    map[runePair]fixed.Int26_6
}

func (f *measuredFace) GlyphAdvance(r rune) (fixed.Int26_6, bool) {
	if a, ok := f.advances[r]; ok {
		return a.width, a.ok
	}
	width, ok := f.Face.GlyphAdvance(r)
	if len(f.advances) < 4096 {
		f.advances[r] = advanceResult{width, ok}
	}
	return width, ok
}

func (f *measuredFace) Kern(first, second rune) fixed.Int26_6 {
	pair := runePair{first, second}
	if k, ok := f.kerns[pair]; ok {
		return k
	}
	k := f.Face.Kern(first, second)
	if len(f.kerns) < 8192 {
		f.kerns[pair] = k
	}
	return k
}

type glyphKey struct {
	faceKey
	r    rune
	x, y uint8 // Exact 1/64-pixel phase; never round typography for caching.
}

type glyphMask struct {
	rect image.Rectangle
	mask *image.Alpha
}

const glyphCacheLimit = 16 << 20

// OpenType reuses a scratch mask, so retain an owned copy. Integer translations
// share a mask; fractional positions and font sizes remain distinct.
func (f *fonts) glyph(id int, size float64, dot fixed.Point26_6, r rune) (image.Rectangle, *image.Alpha) {
	key := glyphKey{faceKey{id, size}, r, uint8(dot.X & 63), uint8(dot.Y & 63)}
	origin := image.Pt(dot.X.Floor(), dot.Y.Floor())
	if g, ok := f.masks[key]; ok {
		return g.rect.Add(origin), g.mask
	}
	dr, mask, maskp, _, _ := f.face(id, size).Glyph(dot, r)
	if dr.Empty() {
		return dr, nil
	}
	owned := image.NewAlpha(image.Rect(0, 0, dr.Dx(), dr.Dy()))
	draw.Draw(owned, owned.Bounds(), mask, maskp, draw.Src)
	cost := len(owned.Pix) + 128 // Include map/entry overhead in the budget.
	if cost <= glyphCacheLimit {
		if f.maskBytes+cost > glyphCacheLimit {
			clear(f.masks)
			f.maskBytes = 0
		}
		if f.masks == nil {
			f.masks = make(map[glyphKey]glyphMask)
		}
		f.masks[key] = glyphMask{dr.Sub(origin), owned}
		f.maskBytes += cost
	}
	return dr, owned
}

func newFonts() (*fonts, error) {
	return loadFonts(os.ReadFile)
}

func loadFonts(readFile func(string) ([]byte, error)) (*fonts, error) {
	f := &fonts{faces: make(map[faceKey]font.Face)}
	embedded := [][]byte{goregular.TTF, gobold.TTF, goitalic.TTF, gobolditalic.TTF, gomono.TTF, gomonobold.TTF, gomonoitalic.TTF, gomonobolditalic.TTF}
	arial := []string{"Arial.ttf", "Arial Bold.ttf", "Arial Italic.ttf", "Arial Bold Italic.ttf"}
	liberation := []string{"LiberationSans-Regular.ttf", "LiberationSans-Bold.ttf", "LiberationSans-Italic.ttf", "LiberationSans-BoldItalic.ttf"}
	menlo := make(map[int]*opentype.Font)
	if data, err := readFile("/System/Library/Fonts/Menlo.ttc"); err == nil {
		if collection, err := opentype.ParseCollection(data); err == nil {
			for i := 0; i < collection.NumFonts(); i++ {
				p, err := collection.Font(i)
				if err != nil {
					continue
				}
				name, _ := p.Name(nil, sfnt.NameIDSubfamily)
				name = strings.ToLower(name)
				flags := mono
				if strings.Contains(name, "bold") {
					flags |= bold
				}
				if strings.Contains(name, "italic") || strings.Contains(name, "oblique") {
					flags |= italic
				}
				menlo[flags] = p
			}
		}
	}
	for i, data := range embedded {
		var parsed *opentype.Font
		if i < 4 {
			for _, path := range []string{filepath.Join("/System/Library/Fonts/Supplemental", arial[i]), filepath.Join("/usr/share/fonts/truetype/liberation2", liberation[i]), filepath.Join("/usr/share/fonts/truetype/liberation", liberation[i])} {
				if b, e := readFile(path); e == nil {
					if p, e := opentype.Parse(b); e == nil {
						parsed = p
						break
					}
				}
			}
		} else if p := menlo[i]; p != nil {
			parsed = p
		} else {
			name := strings.Replace(liberation[i-4], "Sans", "Mono", 1)
			for _, dir := range []string{"/usr/share/fonts/truetype/liberation2", "/usr/share/fonts/truetype/liberation"} {
				if data, err := readFile(filepath.Join(dir, name)); err == nil {
					if p, err := opentype.Parse(data); err == nil {
						parsed = p
						break
					}
				}
			}
		}
		if parsed == nil {
			var err error
			parsed, err = opentype.Parse(data)
			if err != nil {
				return nil, err
			}
		}
		f.parsed = append(f.parsed, parsed)
	}
	return f, nil
}

func (f *fonts) face(id int, size float64) font.Face {
	key := faceKey{id, size}
	if face := f.faces[key]; face != nil {
		return face
	}
	face, err := opentype.NewFace(f.parsed[id], &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		panic(err)
	} // Parsed fonts and positive sizes are validated at entry.
	measured := &measuredFace{Face: face, advances: make(map[rune]advanceResult), kerns: make(map[runePair]fixed.Int26_6)}
	f.faces[key] = measured
	return measured
}

func (f *fonts) selectFont(r rune, flags int, size float64) int {
	id := flags & (bold | italic | mono)
	if r < 128 {
		return id
	}
	if _, ok := f.face(id, size).GlyphAdvance(r); ok {
		return id
	}
	if !f.cjk {
		f.cjk = true
		for _, path := range []string{"/System/Library/Fonts/Supplemental/Arial Unicode.ttf", "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc", "/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc"} {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			collection, err := opentype.ParseCollection(data)
			if err != nil {
				continue
			}
			p, err := collection.Font(0)
			if err == nil {
				f.parsed = append(f.parsed, p)
				break
			}
		}
	}
	for i := 8; i < len(f.parsed); i++ {
		if _, ok := f.face(i, size).GlyphAdvance(r); ok {
			return i
		}
	}
	return id
}

func (f *fonts) closeFaces() {
	for _, face := range f.faces {
		_ = face.Close()
	}
	f.faces = make(map[faceKey]font.Face)
	f.masks = nil
	f.maskBytes = 0
}
