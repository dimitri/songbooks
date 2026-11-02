package pdfnative

import "github.com/signintech/gopdf"

// centeredText draws s horizontally centered on the page at baseline y.
func centeredText(doc *gopdf.GoPdf, s string, face string, size float64, col rgb, y float64) {
	doc.SetFont(face, "", size)
	w, _ := doc.MeasureTextWidth(s)
	doc.SetTextColor(col.r, col.g, col.b)
	doc.SetXY((pageW-w)/2, y)
	doc.Text(s)
}

func darkPage(doc *gopdf.GoPdf) {
	doc.AddPage()
	doc.SetFillColor(plateBg.r, plateBg.g, plateBg.b)
	doc.RectFromUpperLeftWithStyle(0, 0, pageW, pageH, "F")
}

// renderCover draws a plain, literal book-cover plate: just the book/band
// name, very large, centered — the page BEFORE the title plate, deliberately
// barer than it (a cover introduces, a title page informs).
func renderCover(doc *gopdf.GoPdf, title string) {
	darkPage(doc)
	centeredText(doc, title, faceSerif, 40, plateInk, pageH*0.5)
}

// renderTitlePlate draws a plain title page: title large, an optional
// subtitle beneath it — used for a songbook export (no single event to
// name) or a setlist with no gig attached.
func renderTitlePlate(doc *gopdf.GoPdf, title, subtitle string) {
	darkPage(doc)
	y := pageH * 0.42
	centeredText(doc, title, faceSerif, 32, plateInk, y)
	if subtitle != "" {
		centeredText(doc, subtitle, faceSans, 13, plateMeta, y+34)
	}
}

// renderEventTitlePlate draws the event-aware title page: event name
// (large), band name (medium) just beneath it, then date + venue smaller
// further down — used only when a setlist is tied to a real gig (real
// date/venue), per FrontMatter's selection rule (see pdfnative.go).
func renderEventTitlePlate(doc *gopdf.GoPdf, eventTitle, bandName, date, venue string) {
	darkPage(doc)
	y := pageH * 0.40
	centeredText(doc, eventTitle, faceSerif, 34, plateInk, y)
	if bandName != "" {
		centeredText(doc, bandName, faceSans, 15, plateMeta, y+32)
	}
	dv := date
	if venue != "" {
		if dv != "" {
			dv += "  •  "
		}
		dv += venue
	}
	if dv != "" {
		centeredText(doc, dv, faceSansSpacedStyle(), 10.5, plateMeta, pageH*0.62)
	}
}

// faceSansSpacedStyle exists only so renderEventTitlePlate's date/venue
// line reads as a distinct face choice from this file's callers — gopdf
// has no letter-tracking knob, so "spaced" here is just the plain sans
// face; kept as a named indirection in case a tracked variant is worth
// adding later rather than silently falling back to faceSans everywhere.
func faceSansSpacedStyle() string { return faceSans }
