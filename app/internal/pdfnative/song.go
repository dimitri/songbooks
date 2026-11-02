package pdfnative

import (
	"fmt"
	"strings"

	"app/internal/song"

	"github.com/signintech/gopdf"
)

// candidateSizes is the shrink-to-fit ladder fitSongToPage walks — same
// spirit as v2/tools/compile-sheet.sh's bisection loop, just a short fixed
// ladder instead of a search: cheap, and "the song is editorially too long
// for one page at any reasonable size" is a real, useful signal to stop
// shrinking and allow a second page rather than silently chase a 1pt font.
var candidateSizes = []float64{10.5, 10, 9.5, 9, 8.5, 8, 7.5, 7}

// renderSongHeader draws (or measures, under c.draw=false) the title/
// artist/key/tempo/time-signature line and the rule beneath it — fixed
// size regardless of the body's fit-to-page size, so every song in a
// songbook has the same header weight even if their charts print at
// different sizes.
func renderSongHeader(c *canvas, s *song.Song) {
	title := s.Get("title")
	if title == "" {
		title = "Untitled"
	}
	// c.y is treated as the TOP of each line's box throughout this
	// function, baseline computed as top + size*0.8 (ascent) — every call
	// below is explicit about that rather than mixing it with the plain
	// c.text() helper's "y IS the baseline" convention, which is what
	// caused the title and meta line to collide: c.y advanced by the
	// title's SIZE, not its full line height (ascent+descent), landing
	// the meta baseline inside the title's own glyph box.
	titleSize := 20.0
	titleLineH := titleSize * 1.3
	c.ensureSpace(titleLineH)
	c.textAt(c.curX(), c.y+titleSize*0.8, title, faceSerif, titleSize, c.th.ink)
	c.y += titleLineH

	var meta []string
	if a := s.Get("artist"); a != "" {
		meta = append(meta, a)
	}
	if k := s.Get("key"); k != "" {
		meta = append(meta, "key "+k)
	}
	if t := s.Get("tempo"); t != "" {
		meta = append(meta, t+" bpm")
	}
	if tm := s.Get("time"); tm != "" {
		meta = append(meta, tm)
	}
	if len(meta) > 0 {
		metaSize := 10.5
		metaLineH := metaSize * 1.4
		c.ensureSpace(metaLineH)
		c.textAt(c.curX(), c.y+metaSize*0.8, strings.Join(meta, "  ·  "), faceSans, metaSize, c.th.meta)
		c.y += metaLineH
	}
	c.y += 4
	c.line(marginX, c.y, pageW-marginX, c.y, c.th.ink, 1.1)
	c.y += 10
}

// renderSongBody draws every section in source order at the given body
// font size — shared verbatim between the measurement pass (fitSongToPage)
// and the real draw pass, so a size that "measures as fitting" always
// draws identically.
func renderSongBody(c *canvas, s *song.Song, bodySize float64) {
	renderSongHeader(c, s)
	for _, sec := range s.Sections {
		if sec.Label != "" {
			c.sectionTag(sec.Label, bodySize*0.78)
		}
		if sec.Repeat {
			continue // mirrors v2/lib/song.typ: a (repeat) occurrence prints
			// only its label, no measures/lyrics of its own.
		}
		c.measureSection(sec, bodySize)
	}
}

// fitSongToPage picks the largest candidateSizes entry that lays the whole
// song out in a two-column single page (measured via a draw=false pass for
// each candidate — cheap, no PDF content is actually emitted until the
// winning size is known), falling back to the smallest size with a second
// page allowed if nothing fits even at the floor.
func fitSongToPage(doc *gopdf.GoPdf, s *song.Song, th theme) (size float64, allowPage2 bool) {
	for _, sz := range candidateSizes {
		c := newCanvas(doc, false, th, 2, false)
		renderSongBody(c, s, sz)
		if !c.overflowed {
			return sz, false
		}
	}
	return candidateSizes[len(candidateSizes)-1], true
}

// renderSong draws one full song, starting a fresh page, at whatever size
// fitSongToPage determines it needs (one page if at all possible, two as a
// last resort for a genuinely long chart).
func renderSong(doc *gopdf.GoPdf, s *song.Song, th theme) error {
	size, allowPage2 := fitSongToPage(doc, s, th)
	doc.AddPage()
	paintPageBackground(doc, th)
	c := newCanvas(doc, true, th, 2, allowPage2)
	renderSongBody(c, s, size)
	if c.pages > 2 {
		return fmt.Errorf("song %q still overflows at the floor size across %d pages", s.Get("title"), c.pages)
	}
	return nil
}
