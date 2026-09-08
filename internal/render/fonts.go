package render

import (
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
	parsed []*opentype.Font
	faces  map[faceKey]font.Face
	cjk    bool
}
type faceKey struct {
	id   int
	size float64
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
	f.faces[key] = face
	return face
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
}
