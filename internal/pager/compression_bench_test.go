package pager

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"testing"
)

func BenchmarkBandCompression(b *testing.B) {
	e := cacheEngine(b)
	if err := e.Resize(800, 600, 2); err != nil {
		b.Fatal(err)
	}
	canvas, err := e.Canvas(0, 1800)
	if err != nil {
		b.Fatal(err)
	}
	for _, level := range []int{zlib.BestSpeed, 2, 3, 4, zlib.DefaultCompression, zlib.HuffmanOnly} {
		b.Run(fmt.Sprintf("level%d", level), func(b *testing.B) {
			var buf bytes.Buffer
			z, err := zlib.NewWriterLevel(&buf, level)
			if err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buf.Reset()
				z.Reset(&buf)
				if _, err := z.Write(canvas.Pix); err != nil {
					b.Fatal(err)
				}
				if err := z.Close(); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(buf.Len()), "compressed-B/op")
		})
	}
}
