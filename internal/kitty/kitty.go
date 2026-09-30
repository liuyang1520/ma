// Package kitty implements direct pixel transfer, requiring neither shared files
// nor Unicode placeholders. Direct transfer also works across SSH.
package kitty

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"image"
	"io"
	"sync"
)

const ImageID = 1073741821
const QueryID = 1073741820
const SecondImageID = ImageID + 1

type compressor struct {
	buffer bytes.Buffer
	writer *zlib.Writer
}

var compressors = sync.Pool{New: func() any {
	c := &compressor{}
	// Level 2 measured faster than BestSpeed for large native text canvases,
	// while also reducing transfer size. Keep the tradeoff reproducible.
	c.writer, _ = zlib.NewWriterLevel(&c.buffer, 2)
	return c
}}

// Upload stores an opaque native canvas in the terminal without displaying
// it. Compression is lossless zlib over RGBA bytes, with no PNG filtering.
func Upload(w io.Writer, id int, canvas *image.RGBA) error {
	c := compressors.Get().(*compressor)
	c.buffer.Reset()
	c.writer.Reset(&c.buffer)
	defer func() {
		// Do not retain unusually large compressed images in the pool.
		if c.buffer.Cap() <= 8<<20 {
			compressors.Put(c)
		}
	}()
	width, height := canvas.Bounds().Dx(), canvas.Bounds().Dy()
	if canvas.Stride == width*4 {
		if _, err := c.writer.Write(canvas.Pix[:height*canvas.Stride]); err != nil {
			return err
		}
	} else {
		for y := 0; y < height; y++ {
			if _, err := c.writer.Write(canvas.Pix[y*canvas.Stride : y*canvas.Stride+width*4]); err != nil {
				return err
			}
		}
	}
	if err := c.writer.Close(); err != nil {
		return err
	}
	return transmit(w, fmt.Sprintf("a=t,t=d,f=32,o=z,i=%d,s=%d,v=%d,q=2", id, width, height), c.buffer.Bytes())
}

// Place replaces an existing placement with a new crop. Cached scrolls send
// only this small control sequence; the terminal reuses its stored pixels.
func Place(w io.Writer, id, y, width, height, columns, rows int) error {
	_, err := fmt.Fprintf(w, "\x1b[H\x1b_Ga=p,i=%d,p=1,x=0,y=%d,w=%d,h=%d,c=%d,r=%d,C=1,q=1;\x1b\\", id, y, width, height, columns, rows)
	return err
}

func Hide(w io.Writer, id int) error {
	_, err := fmt.Fprintf(w, "\x1b_Ga=d,d=i,i=%d,p=1,q=2;\x1b\\", id)
	return err
}

func transmit(w io.Writer, header string, data []byte) error {
	var encoded [4096]byte
	for start := 0; start < len(data); start += 3072 {
		end := min(start+3072, len(data))
		payload := encoded[:base64.StdEncoding.EncodedLen(end-start)]
		base64.StdEncoding.Encode(payload, data[start:end])
		more := 0
		if end < len(data) {
			more = 1
		}
		params := fmt.Sprintf("m=%d,q=2", more)
		if start == 0 {
			params = fmt.Sprintf("%s,m=%d", header, more)
		}
		if _, err := fmt.Fprintf(w, "\x1b_G%s;", params); err != nil {
			return err
		}
		if _, err := w.Write(payload); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\x1b\\"); err != nil {
			return err
		}
	}
	return nil
}

func Query(w io.Writer) error {
	_, err := fmt.Fprintf(w, "\x1b_Ga=q,i=%d,t=d,f=24,s=1,v=1;AAAA\x1b\\", QueryID)
	return err
}

// Draw replaces one owned image. Each base64 chunk is at most 4096 bytes as
// required by the protocol. c/r map device pixels to the exact terminal grid.
func Draw(w io.Writer, png []byte, columns, rows int) error {
	encoded := base64.StdEncoding.EncodeToString(png)
	if _, err := io.WriteString(w, "\x1b[H"); err != nil {
		return err
	}
	for start := 0; start < len(encoded); start += 4096 {
		end := min(start+4096, len(encoded))
		more := 0
		if end < len(encoded) {
			more = 1
		}
		header := fmt.Sprintf("m=%d,q=2", more)
		if start == 0 {
			header = fmt.Sprintf("a=T,f=100,t=d,i=%d,p=1,c=%d,r=%d,C=1,q=2,m=%d", ImageID, columns, rows, more)
		}
		if _, err := fmt.Fprintf(w, "\x1b_G%s;%s\x1b\\", header, encoded[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func Delete(w io.Writer) error {
	for _, id := range []int{ImageID, SecondImageID} {
		if _, err := fmt.Fprintf(w, "\x1b_Ga=d,d=I,i=%d,q=2;\x1b\\", id); err != nil {
			return err
		}
	}
	return nil
}
