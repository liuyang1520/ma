package pager

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/liuyang1520/ma/internal/kitty"
	"github.com/liuyang1520/ma/internal/render"
)

func cacheEngine(t testing.TB) *render.Engine {
	t.Helper()
	e, err := render.New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	if err = e.Load([]byte(strings.Repeat("## Heading\n\nA paragraph of **bold** words with `code` and enough text to measure scrolling performance.\n\n", 100)), "", "dark", 20); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestScrollReusesTerminalPixels(t *testing.T) {
	e := cacheEngine(t)
	cache := newImageCache()
	var wire bytes.Buffer
	if _, err := cache.Present(&wire, e, 0, 80, 24); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wire.String(), "a=t,t=d,f=32,o=z") {
		t.Fatal("missing raw-pixel upload")
	}
	for _, y := range []float64{30, 60, 300, 600, 0} {
		wire.Reset()
		if _, err := cache.Present(&wire, e, y, 80, 24); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(wire.String(), "a=t") || wire.Len() > 150 {
			t.Fatalf("scroll resent pixels: y=%v, %d bytes", y, wire.Len())
		}
		if !strings.Contains(wire.String(), fmt.Sprintf("y=%d,", int(y))) {
			t.Fatal("incorrect source crop", wire.String())
		}
	}
	// A distant page uses the other image. Going back reuses the first one.
	wire.Reset()
	if _, err := cache.Present(&wire, e, 5000, 80, 24); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wire.String(), fmt.Sprintf("i=%d", kitty.SecondImageID)) {
		t.Fatal("did not use the hidden image")
	}
	wire.Reset()
	if _, err := cache.Present(&wire, e, 30, 80, 24); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(wire.String(), "a=t") {
		t.Fatal("lost previous cached band")
	}
}

func TestCacheInvalidation(t *testing.T) {
	e := cacheEngine(t)
	cache := newImageCache()
	var wire bytes.Buffer
	if _, err := cache.Present(&wire, e, 0, 80, 24); err != nil {
		t.Fatal(err)
	}
	changes := []func() error{
		func() error { _, err := e.Search("bold"); return err },
		func() error { return e.SelectMatch(0) },
		func() error { return e.Resize(800, 600, 2) },
		func() error { return e.Resize(400, 600, 2) },
		func() error { return e.Load([]byte("# Reloaded\n\nNew text"), "", "light", 22) },
	}
	for i, change := range changes {
		if err := change(); err != nil {
			t.Fatal(err)
		}
		wire.Reset()
		if _, err := cache.Present(&wire, e, 0, 80, 24); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(wire.String(), "a=t") {
			t.Fatalf("change %d left stale pixels", i)
		}
	}
	retry, err := cache.HandleReply(fmt.Sprintf("Gi=%d,p=1;ENOENT: missing image", kitty.ImageID))
	if err != nil || !retry {
		t.Fatal("missing-image recovery", retry, err)
	}
	wire.Reset()
	if _, err = cache.Present(&wire, e, 0, 80, 24); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wire.String(), "a=t") {
		t.Fatal("recovery reused missing pixels")
	}
}

type countWriter struct{ n int64 }

func TestFractionalScaleEndCrop(t *testing.T) {
	e := cacheEngine(t)
	for _, scale := range []float64{1.25, 1.5, 1.75} {
		if err := e.Resize(401, 303, scale); err != nil {
			t.Fatal(err)
		}
		cache := newImageCache()
		if _, err := cache.Present(io.Discard, e, 1e9, 40, 15); err != nil {
			t.Fatal(err)
		}
		var wire bytes.Buffer
		if _, err := cache.Present(&wire, e, 1e9, 40, 15); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(wire.String(), "a=t") {
			t.Fatalf("bottom crop escaped cache at scale %v", scale)
		}
	}
}

func (w *countWriter) Write(p []byte) (int, error) { w.n += int64(len(p)); return len(p), nil }

func BenchmarkScrollTransport(b *testing.B) {
	for _, scale := range []float64{1, 2} {
		b.Run(fmt.Sprintf("scale%g", scale), func(b *testing.B) {
			b.Run("refill_rgba", func(b *testing.B) {
				e := cacheEngine(b)
				if err := e.Resize(800, 600, scale); err != nil {
					b.Fatal(err)
				}
				cache := newImageCache()
				w := &countWriter{}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					cache.Invalidate()
					if _, err := cache.Present(w, e, float64(i%20)*30, 80, 24); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(w.n)/float64(b.N), "wire-B/op")
			})
			b.Run("full_png", func(b *testing.B) {
				e := cacheEngine(b)
				if err := e.Resize(800, 600, scale); err != nil {
					b.Fatal(err)
				}
				w := &countWriter{}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					png, _, err := e.Frame(float64(i%20) * 30)
					if err != nil {
						b.Fatal(err)
					}
					if err = kitty.Draw(w, png, 80, 24); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(w.n)/float64(b.N), "wire-B/op")
			})
			b.Run("cached_rgba", func(b *testing.B) {
				e := cacheEngine(b)
				if err := e.Resize(800, 600, scale); err != nil {
					b.Fatal(err)
				}
				cache := newImageCache()
				if _, err := cache.Present(io.Discard, e, 0, 80, 24); err != nil {
					b.Fatal(err)
				}
				w := &countWriter{}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := cache.Present(w, e, float64(i%20)*30, 80, 24); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(w.n)/float64(b.N), "wire-B/op")
			})
		})
	}
}
