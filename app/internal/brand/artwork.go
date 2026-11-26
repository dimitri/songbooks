package brand

// An album's artwork, uploaded (docs/albums-and-live-versions.md): kept
// like a logo (logo.go: decoded, turned upright, encoded again, so its
// metadata goes), but cropped to a centred square and fitted into
// ArtworkMaxSide, always as JPEG (a sleeve is a picture).

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"

	"golang.org/x/image/draw"
)

const (
	// MaxArtworkUpload is the largest artwork file accepted (Bandcamp's
	// full-size sleeves are a few MB).
	MaxArtworkUpload = 12 << 20
	// ArtworkMaxSide is the side of a kept artwork; a smaller square
	// keeps its size.
	ArtworkMaxSide = 1200
	// maxArtworkPixels guards decoding.
	maxArtworkPixels = 60_000_000
)

// ErrArtworkTooLarge is a file over MaxArtworkUpload, or an image too big
// to decode.
var ErrArtworkTooLarge = errors.New("the image is larger than 12 MB")

// PrepareArtwork checks an uploaded image (PNG, JPEG or WebP) and makes
// the artwork to keep: upright, cropped to its centred square, at most
// ArtworkMaxSide on a side, JPEG. Errors: ErrArtworkTooLarge,
// ErrLogoFormat, ErrLogoSVG.
func PrepareArtwork(data []byte) (LogoImage, error) {
	if len(data) > MaxArtworkUpload {
		return LogoImage{}, ErrArtworkTooLarge
	}
	if looksLikeSVG(data) {
		return LogoImage{}, ErrLogoSVG
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return LogoImage{}, ErrLogoFormat
	}
	switch format {
	case "png", "jpeg", "webp":
	default:
		return LogoImage{}, ErrLogoFormat
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxArtworkPixels {
		return LogoImage{}, ErrArtworkTooLarge
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return LogoImage{}, ErrLogoFormat
	}
	b := src.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
	if format == "jpeg" {
		rgba = orient(rgba, jpegOrientation(data))
	}
	img := squareFit(rgba, ArtworkMaxSide)
	// A transparent PNG's see-through parts go white, not black.
	flat := image.NewRGBA(img.Bounds())
	draw.Draw(flat, flat.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), img, image.Point{}, draw.Over)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: 88}); err != nil {
		return LogoImage{}, err
	}
	sum := sha256.Sum256(buf.Bytes())
	return LogoImage{Data: buf.Bytes(), ContentType: "image/jpeg", Width: flat.Bounds().Dx(), Height: flat.Bounds().Dy(), SHA256: hex.EncodeToString(sum[:])}, nil
}

// squareFit crops img to its centred square and scales it down to side
// (never up).
func squareFit(img *image.RGBA, side int) *image.RGBA {
	b := img.Bounds()
	n := min(b.Dx(), b.Dy())
	min0 := image.Pt(b.Min.X+(b.Dx()-n)/2, b.Min.Y+(b.Dy()-n)/2)
	crop := image.Rectangle{Min: min0, Max: min0.Add(image.Pt(n, n))}
	out := min(n, side)
	dst := image.NewRGBA(image.Rect(0, 0, out, out))
	if out == n {
		draw.Draw(dst, dst.Bounds(), img, crop.Min, draw.Src)
	} else {
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, crop, draw.Src, nil)
	}
	return dst
}
