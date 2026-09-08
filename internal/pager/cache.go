package pager

import (
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/liuyang1520/ma/internal/kitty"
	"github.com/liuyang1520/ma/internal/render"
)

type cachedBand struct {
	id, top, height int // source geometry in physical pixels
	revision        uint64
	valid           bool
}

// imageCache retains two overscanned canvases in terminal memory. Upload to
// the hidden image first, then switch placements: the old view stays visible
// during the expensive work. No full-document raster or disk cache is used.
type imageCache struct {
	bands  [2]cachedBand
	active int
	errors int
}

func newImageCache() *imageCache {
	return &imageCache{active: -1, bands: [2]cachedBand{{id: kitty.ImageID}, {id: kitty.SecondImageID}}}
}

func (c *imageCache) Invalidate() {
	for i := range c.bands {
		c.bands[i].valid = false
	}
}

func (c *imageCache) Present(w io.Writer, e *render.Engine, y float64, columns, rows int) (render.Metrics, error) {
	metrics := e.Viewport(y)
	width, height, scale := e.Dimensions()
	pixelWidth := int(math.Round(float64(width) * scale))
	pixelHeight := int(math.Round(float64(height) * scale))
	docHeight := int(math.Round(metrics.Height * scale))
	// Fractional scales can otherwise put the last crop one pixel outside
	// the image when rounding each term separately.
	start := max(0, min(int(math.Round(metrics.Y*scale)), docHeight-pixelHeight))
	metrics.Y = float64(start) / scale
	chosen := -1
	for i, band := range c.bands {
		if band.valid && band.revision == e.Revision() && start >= band.top && start+pixelHeight <= band.top+band.height {
			chosen = i
			break
		}
	}
	if chosen < 0 {
		chosen = 0
		if c.active == 0 {
			chosen = 1
		}
		// At most 16 MP per cached band, except when the viewport itself is
		// larger (then no overscan). The renderer's 32 MP limit still applies.
		bandHeight := min(3*pixelHeight, max(pixelHeight, 16_000_000/pixelWidth))
		bandHeight = min(bandHeight, docHeight)
		top := max(0, min(start-pixelHeight/2, docHeight-bandHeight))
		canvas, err := e.Canvas(float64(top)/scale, float64(bandHeight)/scale)
		if err != nil {
			return metrics, err
		}
		if err = kitty.Upload(w, c.bands[chosen].id, canvas); err != nil {
			return metrics, err
		}
		c.bands[chosen] = cachedBand{id: c.bands[chosen].id, top: top, height: bandHeight, revision: e.Revision(), valid: true}
	}
	band := c.bands[chosen]
	if err := kitty.Place(w, band.id, start-band.top, pixelWidth, pixelHeight, columns, rows); err != nil {
		return metrics, err
	}
	if c.active >= 0 && c.active != chosen {
		if err := kitty.Hide(w, c.bands[c.active].id); err != nil {
			return metrics, err
		}
	}
	c.active = chosen
	return metrics, nil
}

// A terminal may evict image data. Retry a missing placement with fresh pixels
// rather than leaving an empty screen; bound retries for incompatible peers.
func (c *imageCache) HandleReply(reply string) (bool, error) {
	owned := false
	for _, b := range c.bands {
		if strings.Contains(reply, fmt.Sprintf("i=%d", b.id)) {
			owned = true
		}
	}
	if !owned || strings.HasSuffix(reply, ";OK") {
		return false, nil
	}
	if strings.Contains(reply, ";ENOENT") && c.errors < 2 {
		c.errors++
		c.Invalidate()
		return true, nil
	}
	return false, fmt.Errorf("terminal rejected cached graphics: %s", reply)
}
