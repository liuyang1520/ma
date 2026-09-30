package render

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func BenchmarkNativeCanvas(b *testing.B) {
	for _, scale := range []float64{1, 2} {
		b.Run(fmt.Sprintf("scale%g", scale), func(b *testing.B) {
			e, err := New(context.Background())
			if err != nil {
				b.Fatal(err)
			}
			defer e.Close()
			if err = e.Resize(800, 600, scale); err != nil {
				b.Fatal(err)
			}
			if err = e.Load([]byte(strings.Repeat("## Heading\n\nA paragraph of **bold** words with `code` and enough text to measure scrolling performance.\n\n", 100)), "", "dark", 24); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err = e.Canvas(float64(i%20)*36, 1800); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkNativeLayout(b *testing.B) {
	e, err := New(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	source := []byte(strings.Repeat("## Heading\n\nA paragraph of **bold** words with `code` and enough text to measure scrolling performance.\n\n", 1000))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err = e.Load(source, "", "dark", 24); err != nil {
			b.Fatal(err)
		}
	}
}
