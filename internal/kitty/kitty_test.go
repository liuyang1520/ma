package kitty

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"image"
	"image/color"
	"io"
	"strings"
	"testing"
)

func TestChunkedTransfer(t *testing.T) {
	payload := bytes.Repeat([]byte{0, 1, 255}, 5000)
	var out bytes.Buffer
	if err := Draw(&out, payload, 80, 23); err != nil {
		t.Fatal(err)
	}
	var encoded strings.Builder
	parts := strings.Split(out.String(), "\x1b_G")[1:]
	for i, part := range parts {
		header, data, ok := strings.Cut(strings.TrimSuffix(part, "\x1b\\"), ";")
		if !ok {
			t.Fatal("missing payload")
		}
		if len(data) > 4096 {
			t.Fatal("oversize chunk")
		}
		encoded.WriteString(data)
		if i == 0 && !strings.Contains(header, "c=80,r=23,C=1") {
			t.Fatal("placement missing")
		}
		if i == len(parts)-1 && !strings.Contains(header, "m=0") {
			t.Fatal("missing final chunk")
		}
		if i < len(parts)-1 && !strings.Contains(header, "m=1") {
			t.Fatal("missing continuation")
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded.String())
	if err != nil || !bytes.Equal(decoded, payload) {
		t.Fatal("corrupt transfer", err)
	}
}

func TestRawCanvasUploadAndCrop(t *testing.T) {
	// A subimage has padded rows: only the requested pixels may be sent.
	parent := image.NewRGBA(image.Rect(0, 0, 100, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 100; x++ {
			parent.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 120, 255})
		}
	}
	canvas := parent.SubImage(image.Rect(10, 10, 90, 70)).(*image.RGBA)
	var wire bytes.Buffer
	if err := Upload(&wire, ImageID, canvas); err != nil {
		t.Fatal(err)
	}
	var encoded strings.Builder
	for i, part := range strings.Split(wire.String(), "\x1b_G")[1:] {
		header, data, ok := strings.Cut(strings.TrimSuffix(part, "\x1b\\"), ";")
		if !ok || len(data) > 4096 {
			t.Fatal("invalid chunk")
		}
		if i == 0 && !strings.Contains(header, "f=32,o=z") {
			t.Fatal("wrong pixel format")
		}
		encoded.WriteString(data)
	}
	compressed, err := base64.StdEncoding.DecodeString(encoded.String())
	if err != nil {
		t.Fatal(err)
	}
	z, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	pixels, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	if len(pixels) != 80*60*4 {
		t.Fatal("incorrect pixel count", len(pixels))
	}
	for y := 0; y < 60; y++ {
		if !bytes.Equal(pixels[y*80*4:(y+1)*80*4], canvas.Pix[y*canvas.Stride:y*canvas.Stride+80*4]) {
			t.Fatal("pixel corruption", y)
		}
	}
	wire.Reset()
	if err = Place(&wire, ImageID, 12, 80, 40, 80, 24); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wire.String(), "y=12,w=80,h=40,c=80,r=24,C=1") {
		t.Fatal("bad crop command", wire.String())
	}
}
