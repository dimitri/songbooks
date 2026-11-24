package brand

// Uploaded logos. A logo can be an https address (Brand.LogoURL), which
// the browser loads from another site, or an image uploaded to the app:
// PrepareLogo checks it and makes the image actually kept, which the
// store saves in brand_logos (migration 0098) and the app serves at
// LogoPath, so a brand's pages never depend on, or tell, another site.
//
// The uploaded file itself is never kept or served: it is decoded,
// turned upright (a phone photo's EXIF orientation), fitted into a
// LogoMaxSide square and encoded again, which also drops its metadata.
// SVG is refused rather than sanitised: a script or an outside reference
// in it is too easy to miss, and a PNG of a logo looks the same.

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"regexp"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registers WebP with image.Decode
)

const (
	// MaxLogoUpload is the largest file accepted.
	MaxLogoUpload = 2 << 20
	// LogoMaxSide is the largest side of a kept logo; smaller images
	// keep their size.
	LogoMaxSide = 512
	// maxLogoPixels guards decoding: a small file can claim a huge image.
	maxLogoPixels = 30_000_000
)

var (
	ErrLogoTooLarge = errors.New("the image is larger than 2 MB")
	ErrLogoFormat   = errors.New("not a PNG, JPEG or WebP image")
	ErrLogoSVG      = errors.New("SVG logos aren't accepted; export it as PNG")
)

// logoImagePath is what LogoPath makes, the only LogoImage Clean keeps.
var logoImagePath = regexp.MustCompile(`^/brand-logos/(community|band)/[0-9]+/[0-9a-f]{16}\.(png|jpg)$`)

// LogoImage is an uploaded logo made ready to keep.
type LogoImage struct {
	Data          []byte
	ContentType   string // image/png or image/jpeg
	Width, Height int
	SHA256        string // hex, of Data
}

// LogoPath is where a community's or band's uploaded logo is served: its
// hash is in the address, so the image can be cached for good and a new
// logo is a new address.
func LogoPath(kind string, id int64, sha, contentType string) string {
	ext := "png"
	if contentType == "image/jpeg" {
		ext = "jpg"
	}
	if len(sha) > 16 {
		sha = sha[:16]
	}
	return fmt.Sprintf("/brand-logos/%s/%d/%s.%s", kind, id, sha, ext)
}

// PrepareLogo checks an uploaded image (PNG, JPEG or WebP, at most
// MaxLogoUpload) and makes the logo to keep: upright, fitted into a
// LogoMaxSide square, as PNG when it has transparency or when PNG is
// about as small (flat colors, lettering), as JPEG otherwise (a photo).
func PrepareLogo(data []byte) (LogoImage, error) {
	if len(data) > MaxLogoUpload {
		return LogoImage{}, ErrLogoTooLarge
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
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxLogoPixels {
		return LogoImage{}, ErrLogoTooLarge
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return LogoImage{}, ErrLogoFormat
	}
	img := fit(src, LogoMaxSide)
	if format == "jpeg" {
		img = orient(img, jpegOrientation(data))
	}

	var pngBuf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&pngBuf, img); err != nil {
		return LogoImage{}, err
	}
	out, ct := pngBuf.Bytes(), "image/png"
	if img.Opaque() {
		var jpgBuf bytes.Buffer
		if err := jpeg.Encode(&jpgBuf, img, &jpeg.Options{Quality: 90}); err != nil {
			return LogoImage{}, err
		}
		if pngBuf.Len() > jpgBuf.Len()*3/2 {
			out, ct = jpgBuf.Bytes(), "image/jpeg"
		}
	}
	sum := sha256.Sum256(out)
	b := img.Bounds()
	return LogoImage{Data: out, ContentType: ct, Width: b.Dx(), Height: b.Dy(), SHA256: hex.EncodeToString(sum[:])}, nil
}

func looksLikeSVG(data []byte) bool {
	head := data[:min(len(data), 1024)]
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	head = bytes.TrimSpace(head)
	return bytes.HasPrefix(head, []byte("<")) && strings.Contains(strings.ToLower(string(head)), "svg")
}

// fit scales src down to fit a side×side square (never up), as RGBA.
func fit(src image.Image, side int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > side || h > side {
		if w >= h {
			w, h = side, max(1, (h*side+w/2)/w)
		} else {
			w, h = max(1, (w*side+h/2)/h), side
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	if w == b.Dx() && h == b.Dy() {
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	} else {
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	}
	return dst
}

// orient turns an image the way its EXIF orientation (1–8) says to.
func orient(img *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return img
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2: // mirrored
				dx, dy = w-1-x, y
			case 3: // upside down
				dx, dy = w-1-x, h-1-y
			case 4: // mirrored upside down
				dx, dy = x, h-1-y
			case 5: // transposed
				dx, dy = y, x
			case 6: // turned a quarter right
				dx, dy = h-1-y, x
			case 7: // transversed
				dx, dy = h-1-y, w-1-x
			case 8: // turned a quarter left
				dx, dy = y, w-1-x
			}
			i, j := img.PixOffset(x, y), dst.PixOffset(dx, dy)
			copy(dst.Pix[j:j+4], img.Pix[i:i+4])
		}
	}
	return dst
}

// jpegOrientation is a JPEG's EXIF orientation, 1 (upright) when it has
// none or it can't be read.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		if marker == 0xDA || marker == 0xD9 { // image data: no EXIF before it
			return 1
		}
		n := int(binary.BigEndian.Uint16(data[i+2:]))
		if n < 2 || i+2+n > len(data) {
			return 1
		}
		seg := data[i+4 : i+2+n]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + n
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int(bo.Uint32(t[4:]))
	if off < 8 || off+2 > len(t) {
		return 1
	}
	for k := range int(bo.Uint16(t[off:])) {
		e := off + 2 + 12*k
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 { // Orientation, a SHORT
			if v := int(bo.Uint16(t[e+8:])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}
