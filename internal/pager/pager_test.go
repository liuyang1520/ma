package pager

import (
	"testing"

	"github.com/liuyang1520/ma/internal/render"
)

func TestHeadingNavigation(t *testing.T) {
	headings := []render.Heading{{Text: "Title", Y: 32}, {Text: "Next", Y: 300}, {Text: "End", Y: 1000}}
	for _, test := range []struct {
		y       float64
		forward bool
		want    string
	}{
		{0, true, "Next"},
		{276, true, "End"},
		{276, false, "Title"},
		{500, false, "Next"},
		{0, false, ""},
		{976, true, ""},
	} {
		h, ok := adjacentHeading(headings, test.y, test.forward)
		if h.Text != test.want || ok != (test.want != "") {
			t.Errorf("y=%v forward=%v: got %q/%v, want %q", test.y, test.forward, h.Text, ok, test.want)
		}
	}
	if _, ok := adjacentHeading(nil, 0, true); ok {
		t.Fatal("invented a heading for an empty document")
	}
}
