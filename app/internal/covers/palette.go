package covers

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // covers are JPEGs or PNGs
	_ "image/png"
	"io"
	"net/http"
	"sort"
	"time"
)

// Swatch is one color of a cover and its share of the picture (0 to 1).
type Swatch struct {
	Hex   string  `json:"hex"`
	Share float64 `json:"share"`
}

// maxImage is the most bytes read of a cover: a 600 px JPEG is ~100 KB.
const maxImage = 8 << 20

// Colors fetches a cover's image and returns its palette (Palette).
func (f *Finder) Colors(ctx context.Context, c Cover) ([]Swatch, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Image, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", c.Image, resp.Status)
	}
	img, _, err := image.Decode(io.LimitReader(resp.Body, maxImage))
	if err != nil {
		return nil, err
	}
	if c.Source == "youtube" {
		// hqdefault is 4:3 with black bands above and below a 16:9 picture.
		b := img.Bounds()
		band := b.Dy() / 8
		if s, ok := img.(interface {
			SubImage(image.Rectangle) image.Image
		}); ok {
			img = s.SubImage(image.Rect(b.Min.X, b.Min.Y+band, b.Max.X, b.Max.Y-band))
		}
	}
	return Palette(img, 6), nil
}

// Palette is an image's n main colors, the largest share first, by
// median cut: the pixels (a grid of about 100×100 of them) are split in
// two at the median of their widest channel, the box with the most spread
// first, until there are n boxes; each box gives its mean color.
func Palette(img image.Image, n int) []Swatch {
	b := img.Bounds()
	step := max(1, max(b.Dx(), b.Dy())/100)
	var px [][3]uint8
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			r, g, bl, a := img.At(x, y).RGBA()
			if a < 0x8000 {
				continue
			}
			px = append(px, [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8)})
		}
	}
	if len(px) == 0 {
		return nil
	}
	boxes := [][][3]uint8{px}
	for len(boxes) < n {
		// The box to split: the most pixels times the widest spread.
		best, bestScore, bestCh := -1, 0, 0
		for i, bx := range boxes {
			if len(bx) < 2 {
				continue
			}
			ch, spread := widest(bx)
			if s := spread * len(bx); s > bestScore {
				best, bestScore, bestCh = i, s, ch
			}
		}
		if best < 0 {
			break
		}
		bx := boxes[best]
		sort.Slice(bx, func(i, j int) bool { return bx[i][bestCh] < bx[j][bestCh] })
		// Split between two values, never inside a run of one color.
		mid := len(bx) / 2
		for mid < len(bx) && bx[mid][bestCh] == bx[mid-1][bestCh] {
			mid++
		}
		if mid == len(bx) {
			mid = len(bx) / 2
			for mid > 0 && bx[mid-1][bestCh] == bx[mid][bestCh] {
				mid--
			}
		}
		boxes = append(boxes[:best], append([][][3]uint8{bx[:mid], bx[mid:]}, boxes[best+1:]...)...)
	}
	out := make([]Swatch, 0, len(boxes))
	for _, bx := range boxes {
		var r, g, bl int
		for _, p := range bx {
			r, g, bl = r+int(p[0]), g+int(p[1]), bl+int(p[2])
		}
		k := len(bx)
		out = append(out, Swatch{
			Hex:   fmt.Sprintf("#%02x%02x%02x", (r+k/2)/k, (g+k/2)/k, (bl+k/2)/k),
			Share: float64(k) / float64(len(px)),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Share > out[j].Share })
	return out
}

// widest is the channel with the largest range in a box, and that range.
func widest(px [][3]uint8) (int, int) {
	lo, hi := [3]uint8{255, 255, 255}, [3]uint8{}
	for _, p := range px {
		for c := range 3 {
			lo[c], hi[c] = min(lo[c], p[c]), max(hi[c], p[c])
		}
	}
	ch, spread := 0, 0
	for c := range 3 {
		if s := int(hi[c]) - int(lo[c]); s > spread {
			ch, spread = c, s
		}
	}
	return ch, spread
}
