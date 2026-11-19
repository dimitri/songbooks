package brand

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand"
	"strings"
	"testing"
)

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestPrepareLogoTransparentStaysPNG(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1000, 500))
	for y := 100; y < 400; y++ {
		for x := 100; x < 900; x++ {
			img.Set(x, y, color.NRGBA{200, 30, 60, 255})
		}
	}
	l, err := PrepareLogo(encodePNG(t, img))
	if err != nil {
		t.Fatal(err)
	}
	if l.ContentType != "image/png" || l.Width != 512 || l.Height != 256 {
		t.Fatalf("got %s %dx%d, want a 512x256 PNG", l.ContentType, l.Width, l.Height)
	}
	out, err := png.Decode(bytes.NewReader(l.Data))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := out.At(0, 0).RGBA(); a != 0 {
		t.Error("the transparent corner isn't transparent any more")
	}
	if len(l.SHA256) != 64 {
		t.Errorf("sha256 %q", l.SHA256)
	}
}

func TestPrepareLogoPhotoBecomesJPEG(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	img := image.NewRGBA(image.Rect(0, 0, 700, 700))
	for i := range img.Pix {
		img.Pix[i] = byte(rng.Intn(256))
		if i%4 == 3 {
			img.Pix[i] = 255
		}
	}
	l, err := PrepareLogo(encodePNG(t, img))
	if err != nil {
		t.Fatal(err)
	}
	if l.ContentType != "image/jpeg" || l.Width != 512 || l.Height != 512 {
		t.Fatalf("got %s %dx%d, want a 512x512 JPEG", l.ContentType, l.Width, l.Height)
	}
}

func TestPrepareLogoSmallKeepsItsSize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 120, 80))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	l, err := PrepareLogo(encodePNG(t, img))
	if err != nil {
		t.Fatal(err)
	}
	if l.Width != 120 || l.Height != 80 || l.ContentType != "image/png" {
		t.Fatalf("got %s %dx%d", l.ContentType, l.Width, l.Height)
	}
}

func TestPrepareLogoRefuses(t *testing.T) {
	var g bytes.Buffer
	if err := gif.Encode(&g, image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Black, color.White}), nil); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		data []byte
		want error
	}{
		"svg":       {[]byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), ErrLogoSVG},
		"bom svg":   {[]byte("\xef\xbb\xbf  <svg onload=alert(1)></svg>"), ErrLogoSVG},
		"html":      {[]byte(`<html><body>hi</body></html>`), ErrLogoFormat},
		"gif":       {g.Bytes(), ErrLogoFormat},
		"text":      {[]byte("not an image"), ErrLogoFormat},
		"truncated": {encodePNG(t, image.NewRGBA(image.Rect(0, 0, 50, 50)))[:60], ErrLogoFormat},
		"too large": {bytes.Repeat([]byte{0}, MaxLogoUpload+1), ErrLogoTooLarge},
	} {
		if _, err := PrepareLogo(c.data); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
}

// A small PNG that says it's huge is refused before it's decoded.
func TestPrepareLogoRefusesHugeDimensions(t *testing.T) {
	data := encodePNG(t, image.NewGray(image.Rect(0, 0, 1, 1)))
	// IHDR's width and height, right after the signature and chunk header.
	binary.BigEndian.PutUint32(data[16:], 40000)
	binary.BigEndian.PutUint32(data[20:], 40000)
	if _, err := PrepareLogo(data); !errors.Is(err, ErrLogoTooLarge) && !errors.Is(err, ErrLogoFormat) {
		t.Fatalf("got %v", err)
	}
}

// withOrientation puts an EXIF APP1 segment with an orientation into a
// JPEG, right after its start marker.
func withOrientation(jpg []byte, o uint16) []byte {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08" + // big-endian, IFD at 8
		"\x00\x01" + // one entry
		"\x01\x12\x00\x03\x00\x00\x00\x01" + // Orientation, SHORT, 1 value
		"\x00\x00\x00\x00" + // its value, set below
		"\x00\x00\x00\x00") // no next IFD
	binary.BigEndian.PutUint16(tiff[18:], o)
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(app1[2:], uint16(len(seg)+2))
	out := append([]byte{}, jpg[:2]...)
	out = append(out, app1...)
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

func TestPrepareLogoTurnsPhoneJPEGUpright(t *testing.T) {
	// Red on the left, blue on the right; orientation 6 means "turn a
	// quarter right to show": red ends up on top.
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			c := color.RGBA{220, 20, 20, 255}
			if x >= 20 {
				c = color.RGBA{20, 20, 220, 255}
			}
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	data := withOrientation(b.Bytes(), 6)
	if o := jpegOrientation(data); o != 6 {
		t.Fatalf("orientation read %d", o)
	}
	l, err := PrepareLogo(data)
	if err != nil {
		t.Fatal(err)
	}
	if l.Width != 20 || l.Height != 40 {
		t.Fatalf("got %dx%d, want 20x40", l.Width, l.Height)
	}
	out, _, err := image.Decode(bytes.NewReader(l.Data))
	if err != nil {
		t.Fatal(err)
	}
	r, _, bl, _ := out.At(10, 5).RGBA()
	if r < bl {
		t.Error("the top isn't red")
	}
	r, _, bl, _ = out.At(10, 35).RGBA()
	if r > bl {
		t.Error("the bottom isn't blue")
	}
}

func TestLogoImageIsOnlyTheAppsOwn(t *testing.T) {
	for _, bad := range []string{
		"https://evil.example/x.png", "/brand-logos/community/7/../../admin", "javascript:alert(1)",
		"/brand-logos/person/7/0123456789abcdef.png", "//evil.example/brand-logos/band/1/0123456789abcdef.png",
	} {
		if b := Clean(Brand{LogoImage: bad}); b.LogoImage != "" {
			t.Errorf("kept %q", bad)
		}
	}
	good := LogoPath("band", 12, strings.Repeat("ab", 32), "image/jpeg")
	if good != "/brand-logos/band/12/abababababababab.jpg" {
		t.Fatalf("LogoPath: %q", good)
	}
	b := Clean(Brand{LogoImage: good, LogoURL: "https://example.com/logo.png", Preset: "stage"})
	if b.LogoImage != good || b.Logo() != good {
		t.Fatalf("the uploaded logo should come first: %+v", b)
	}
	if th := Resolve(b, "X"); th.LogoURL != good {
		t.Errorf("a Theme's LogoURL is the logo shown: %q", th.LogoURL)
	}
	if th := Resolve(Brand{LogoURL: "https://example.com/logo.png"}, "X"); th.LogoURL != "https://example.com/logo.png" {
		t.Errorf("without an upload, the address: %q", th.LogoURL)
	}
	if th := Resolve(Brand{LogoImage: good}, "X"); th == nil || th.LogoURL != good {
		t.Error("an uploaded logo alone is a brand")
	}
}
