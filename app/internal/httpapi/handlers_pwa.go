package httpapi

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io/fs"
	"net/http"

	"app/internal/webassets"
)

// handleManifest and handleServiceWorker serve their files straight out of
// the embedded static FS, but NOT under the /static/ prefix: a service
// worker's scope defaults to the directory it's served from, so sw.js has
// to be served from the root to ever see navigation requests for /b/...,
// /setlists/... etc. — serving it from /static/sw.js would only ever let
// it intercept other /static/ requests.
func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	serveEmbedded(w, r, "manifest.json", "application/manifest+json")
}

func (s *Server) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	// text/javascript, not application/javascript: the modern spec-correct
	// MIME type (HTML/Fetch specs as of 2023), and WebKit/Safari has had
	// service-worker registrations fail outright on the older MIME type —
	// reproduced in this session as "An unknown error occurred when
	// fetching the script" from navigator.serviceWorker.register() on a
	// WebKit-based browser. no-cache so a new build's sw.js is never served
	// stale from an intermediate cache, which would otherwise mask this
	// same class of registration failure after a fix.
	w.Header().Set("Cache-Control", "no-cache")
	serveEmbedded(w, r, "sw.js", "text/javascript")
}

func serveEmbedded(w http.ResponseWriter, r *http.Request, name, contentType string) {
	data, err := fs.ReadFile(webassets.StaticFS(), name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Write(data)
}

// handleAppIcon draws the PWA/home-screen icon on the fly — a plain
// image.RGBA with the same dot-grid mark as icon.svg/icon-logo (and the
// public marketing pages' own header logo), rasterized with the stdlib
// only (no font/vector library, no build step, no binary asset to keep
// in sync by hand). iOS's Add to Home Screen needs a real PNG; SVG
// manifest icons cover everything else.
func (s *Server) handleAppIcon(w http.ResponseWriter, r *http.Request) {
	const size = 512
	scale := size / 100.0 // the logo's own coordinate space is 0-100
	bg := color.RGBA{0x24, 0x1e, 0x18, 0xff}
	accent := color.RGBA{0xaf, 0x4e, 0x39, 0xff}

	img := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	fillCircle := func(cx, cy, rad int) {
		for y := cy - rad; y <= cy+rad; y++ {
			for x := cx - rad; x <= cx+rad; x++ {
				dx, dy := x-cx, y-cy
				if dx*dx+dy*dy <= rad*rad {
					img.Set(x, y, accent)
				}
			}
		}
	}

	// The 11-dot grid, same coordinates as icon.svg/icon-logo.
	dots := [][2]int{
		{33, 18}, {50, 18}, {67, 18},
		{33, 34},
		{33, 50}, {50, 50}, {67, 50},
		{67, 66},
		{33, 82}, {50, 82}, {67, 82},
	}
	for _, d := range dots {
		fillCircle(int(float64(d[0])*scale), int(float64(d[1])*scale), int(7*scale))
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=604800")
	png.Encode(w, img)
}
