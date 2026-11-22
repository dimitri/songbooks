package pdfnative

import (
	"strings"

	"github.com/signintech/gopdf"
)

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

// frontPages is how many pages the front matter (renderCover) takes —
// the contents page follows it, then the songs. buildEntries numbers the
// contents table's PAGE column from this.
const frontPages = 1

// Cover title sizing: the largest size the title is set at, the smallest
// it may shrink to, and the most lines it may wrap onto before shrinking
// further. A short book name stays big on one line; a long set list name
// ("<Band> — full set (from …)") wraps onto two or three lines and only
// shrinks when it would need more.
const (
	coverTitleMax      = 40.0
	coverTitleMin      = 18.0
	coverTitleMaxLines = 3
	coverMarginX       = 56.0
)

func coverTextWidth() float64 { return pageW - 2*coverMarginX }

// wrapToWidth breaks s into lines no wider than maxW in face/size,
// breaking at spaces; a single word wider than maxW on its own is split
// by characters (never left to run off the page). A " — " stays with
// the words after it, so a title breaks before its dash rather than
// leaving the dash dangling at a line end.
func wrapToWidth(doc *gopdf.GoPdf, s, face string, size, maxW float64) []string {
	doc.SetFont(face, "", size)
	width := func(t string) float64 { w, _ := doc.MeasureTextWidth(t); return w }
	words := strings.Fields(s)
	// Glue a lone dash onto the word that follows it.
	for i := 0; i < len(words)-1; i++ {
		if words[i] == "—" || words[i] == "–" || words[i] == "-" {
			words[i+1] = words[i] + " " + words[i+1]
			words = append(words[:i], words[i+1:]...)
		}
	}
	var lines []string
	cur := ""
	for _, w := range words {
		// Hard-split a word that can't fit on any line by itself.
		for width(w) > maxW {
			rs := []rune(w)
			n := len(rs) - 1
			for n > 1 && width(string(rs[:n])) > maxW {
				n--
			}
			if cur != "" {
				lines = append(lines, cur)
				cur = ""
			}
			lines = append(lines, string(rs[:n]))
			w = string(rs[n:])
		}
		switch {
		case cur == "":
			cur = w
		case width(cur+" "+w) <= maxW:
			cur += " " + w
		default:
			lines = append(lines, cur)
			cur = w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// coverTitleLayout picks the cover title's size and line breaks: the
// largest size (coverTitleMax down to coverTitleMin, 2pt steps) at which
// the title wraps onto at most coverTitleMaxLines lines within the cover's
// text width; at the floor it takes however many lines it needs. Every
// returned line fits within coverTextWidth().
func coverTitleLayout(doc *gopdf.GoPdf, title string) (lines []string, size float64) {
	for size = coverTitleMax; size >= coverTitleMin; size -= 2 {
		lines = wrapToWidth(doc, title, faceSerif, size, coverTextWidth())
		if len(lines) <= coverTitleMaxLines {
			return balanceLines(doc, title, faceSerif, size, lines), size
		}
	}
	size = coverTitleMin
	lines = wrapToWidth(doc, title, faceSerif, size, coverTextWidth())
	return balanceLines(doc, title, faceSerif, size, lines), size
}

// balanceLines re-wraps s at the narrowest width that still gives the
// same number of lines as greedy (a centered title reads better as
// lines of similar length than as two full lines and a one-word orphan).
func balanceLines(doc *gopdf.GoPdf, s, face string, size float64, greedy []string) []string {
	if len(greedy) < 2 {
		return greedy
	}
	lo, hi := coverTextWidth()/float64(len(greedy)), coverTextWidth()
	best := greedy
	for i := 0; i < 20; i++ {
		mid := (lo + hi) / 2
		if ls := wrapToWidth(doc, s, face, size, mid); len(ls) <= len(greedy) {
			best, hi = ls, mid
		} else {
			lo = mid
		}
	}
	return best
}

// renderCover draws the export's one front page: a dark full-bleed plate
// with the title large and centered (wrapped/shrunk to stay within the
// margins — see coverTitleLayout), then, when the set list is tied to a
// band and/or a gig, the band name and the date • venue line beneath it.
// It replaces the earlier cover + title-plate pair, which printed the same
// title twice on two consecutive dark pages.
func renderCover(doc *gopdf.GoPdf, title, bandName, date, venue string) {
	darkPage(doc)
	lines, size := coverTitleLayout(doc, title)
	lineH := size * 1.22

	var meta []struct {
		text, face string
		size       float64
	}
	// The band name is worth a line only when the title doesn't already
	// say it (a set list is often named "<Band> — …").
	if bandName != "" && !strings.Contains(strings.ToLower(title), strings.ToLower(bandName)) {
		meta = append(meta, struct {
			text, face string
			size       float64
		}{bandName, faceSans, 15})
	}
	dv := date
	if venue != "" {
		if dv != "" {
			dv += "  •  "
		}
		dv += venue
	}
	if dv != "" {
		meta = append(meta, struct {
			text, face string
			size       float64
		}{dv, faceSans, 10.5})
	}

	// Center the whole block (title lines + meta lines) a little above
	// the page's middle, the way the old single-line cover sat.
	blockH := float64(len(lines)) * lineH
	for _, m := range meta {
		blockH += m.size * 2
	}
	y := pageH*0.45 - blockH/2 + size*0.8
	for _, ln := range lines {
		centeredText(doc, ln, faceSerif, size, plateInk, y)
		y += lineH
	}
	y += 6
	for _, m := range meta {
		for _, ln := range wrapToWidth(doc, m.text, m.face, m.size, coverTextWidth()) {
			centeredText(doc, ln, m.face, m.size, plateMeta, y)
			y += m.size * 1.5
		}
		y += m.size * 0.5
	}
}
