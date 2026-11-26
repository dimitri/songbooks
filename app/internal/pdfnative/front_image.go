package pdfnative

import (
	"github.com/signintech/gopdf"
)

// renderImageCover draws a cover page with a picture (an album's artwork)
// centred in its upper part, the title and the meta lines under it. It
// reports false, having drawn nothing, when the picture can't be read:
// the plain cover is drawn instead.
func renderImageCover(doc *gopdf.GoPdf, title string, front FrontMatter) bool {
	holder, err := gopdf.ImageHolderByBytes(front.Image)
	if err != nil {
		return false
	}
	side := pageW - 2*coverMarginX - 60
	if side > pageH*0.52 {
		side = pageH * 0.52
	}
	x, y := (pageW-side)/2, pageH*0.09
	darkPage(doc)
	if err := doc.ImageByHolder(holder, x, y, &gopdf.Rect{W: side, H: side}); err != nil {
		return true // the page is there; the title still goes on it
	}
	lines, size := coverTitleLayout(doc, title)
	if size > 30 {
		size = 30
		lines = wrapToWidth(doc, title, faceSerif, size, coverTextWidth())
	}
	ty := y + side + 48
	for _, ln := range lines {
		centeredText(doc, ln, faceSerif, size, plateInk, ty)
		ty += size * 1.22
	}
	ty += 8
	for _, m := range []struct {
		text string
		size float64
	}{{front.BandName, 15}, {joinDot(front.Date, front.Venue), 10.5}} {
		if m.text == "" {
			continue
		}
		for _, ln := range wrapToWidth(doc, m.text, faceSans, m.size, coverTextWidth()) {
			centeredText(doc, ln, faceSans, m.size, plateMeta, ty)
			ty += m.size * 1.5
		}
		ty += m.size * 0.5
	}
	return true
}

func joinDot(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "  •  " + b
}
