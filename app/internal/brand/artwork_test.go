package brand

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// An album's artwork: cropped to its centred square, at most
// ArtworkMaxSide, JPEG; an SVG or a file too big refused.
func TestPrepareArtwork(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1800, 1300))
	for x := 0; x < 1800; x++ {
		c := color.RGBA{0, 0, 255, 255} // the sides, cut off
		if x >= 250 && x < 1550 {
			c = color.RGBA{255, 0, 0, 255}
		}
		for y := 0; y < 1300; y++ {
			src.Set(x, y, c)
		}
	}
	data := encodePNG(t, src)
	img, err := PrepareArtwork(data)
	if err != nil {
		t.Fatal(err)
	}
	if img.ContentType != "image/jpeg" || img.Width != ArtworkMaxSide || img.Height != ArtworkMaxSide {
		t.Fatalf("%s %dx%d", img.ContentType, img.Width, img.Height)
	}
	out, err := jpeg.Decode(bytes.NewReader(img.Data))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []image.Point{{2, 600}, {1197, 600}} {
		r, _, b, _ := out.At(p.X, p.Y).RGBA()
		if r>>8 < 200 || b>>8 > 60 {
			t.Fatalf("at %v the sides weren't cropped: %v", p, out.At(p.X, p.Y))
		}
	}
	small, err := PrepareArtwork(encodePNG(t, image.NewGray(image.Rect(0, 0, 300, 500))))
	if err != nil || small.Width != 300 || small.Height != 300 {
		t.Fatalf("small: %dx%d %v", small.Width, small.Height, err)
	}
	if _, err := PrepareArtwork([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)); !errors.Is(err, ErrLogoSVG) {
		t.Fatalf("svg: %v", err)
	}
	if _, err := PrepareArtwork(make([]byte, MaxArtworkUpload+1)); !errors.Is(err, ErrArtworkTooLarge) {
		t.Fatalf("too large: %v", err)
	}
}
