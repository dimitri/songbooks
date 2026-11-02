package pdfnative

import (
	"fmt"

	"app/internal/song"

	"github.com/signintech/gopdf"
)

// renderSongMap draws the "song map" mode: not a smaller chart, a
// genuinely different page — just the section order (label, bar count if
// the section has a bar-chart, a "(repeat)" marker where that's all a
// section occurrence is), the roadmap a player glances at mid-song to
// remember the arrangement. No chords, no lyrics. Always one page: a
// section list is short enough that the shrink-to-fit ladder
// renderSongBody's other modes need would be overkill here, so this picks
// one comfortable size directly rather than measuring candidates.
func renderSongMap(doc *gopdf.GoPdf, s *song.Song, th theme, hideNotes bool) error {
	doc.AddPage()
	paintPageBackground(doc, th)
	c := newCanvas(doc, true, th, 1, false)
	renderSongHeader(c, s)

	const (
		rowSize = 13.0
		rowH    = rowSize * 2.0
	)
	for i, sec := range s.Sections {
		label := sec.Label
		if label == "" {
			label = fmt.Sprintf("Section %d", i+1)
		}
		line := label
		if n := len(sec.Measures); n > 0 {
			unit := "bars"
			if n == 1 {
				unit = "bar"
			}
			line += fmt.Sprintf("  —  %d %s", n, unit)
		}
		if sec.Repeat {
			line += "  (repeat)"
		}

		c.ensureSpace(rowH)
		num := fmt.Sprintf("%d.", i+1)
		c.textAt(c.curX(), c.y+rowSize*0.8, num, faceSans, rowSize, th.meta)
		c.textAt(c.curX()+28, c.y+rowSize*0.8, line, faceSansBold, rowSize, th.ink)
		c.y += rowH

		if !hideNotes {
			for _, note := range sec.Notes {
				c.ensureSpace(rowSize * 1.3)
				c.textAt(c.curX()+28, c.y+rowSize*0.65, note, faceSansIt, rowSize*0.8, th.faint)
				c.y += rowSize * 1.3
			}
		}
	}
	return nil
}
