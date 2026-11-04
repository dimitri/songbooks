package pdfnative

import (
	"fmt"
	"strings"

	"app/internal/pdf"
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

	// The header (title/meta/rule) is drawn once, in column 0, but it
	// visually spans the full page width above BOTH columns. c.top started
	// as the raw page margin for every column; left there, a later column
	// switch (ensureSpace) would reset column 2's cursor to that same raw
	// margin — above where the header rule sits — so overflow text in
	// column 2 would start flush with the TITLE's own row instead of below
	// the header, reading as if it overlapped it. Raising c.top to here
	// makes every column (including ones not yet visited) start below the
	// shared header, same as column 0 already does.
	c.top = c.y
}

// renderSongBody draws every section in source order at the given body
// font size — shared verbatim between the measurement pass (fitSongToPage)
// and the real draw pass, so a size that "measures as fitting" always
// draws identically. mode/hideNotes select which parts of each section
// measureSection actually draws (see its own doc comment); ModeMap never
// reaches here at all — it has its own renderSongMap, since "just the
// section order" isn't a smaller version of the chart/lyrics layout, it's
// a genuinely different one.
func renderSongBody(c *canvas, s *song.Song, bodySize float64, mode pdf.Mode, hideNotes bool) {
	renderSongHeader(c, s)
	marks := map[string]string{}
	next := 'A'
	for _, sec := range s.Sections {
		if sec.Label != "" {
			mark, ok := marks[sec.Label]
			if !ok {
				mark = string(next)
				marks[sec.Label] = mark
				next++
			}
			c.sectionTag(sec.Label, mark, bodySize*0.78)
		}
		if sec.Repeat {
			continue // mirrors v2/lib/song.typ: a (repeat) occurrence prints
			// only its label, no measures/lyrics of its own.
		}
		c.measureSection(sec, bodySize, mode, hideNotes)
	}
}

// fitSongToPage picks the largest candidateSizes entry that lays the whole
// song out in a two-column single page (measured via a draw=false pass for
// each candidate — cheap, no PDF content is actually emitted until the
// winning size is known), falling back to the smallest size with a second
// page allowed if nothing fits even at the floor.
func fitSongToPage(doc *gopdf.GoPdf, s *song.Song, th theme, mode pdf.Mode, hideNotes bool, cols int) (size float64, allowPage2 bool) {
	for _, sz := range candidateSizes {
		c := newCanvas(doc, false, th, cols, false)
		renderSongBody(c, s, sz, mode, hideNotes)
		if !c.overflowed {
			return sz, false
		}
	}
	return candidateSizes[len(candidateSizes)-1], true
}

// renderSong draws one full song, starting a fresh page, at whatever size
// fitSongToPage determines it needs (one page if at all possible, two as a
// last resort for a genuinely long chart). ModeMap is handled separately
// (renderSongMap) — it's not a smaller chart, it's a different page. cols
// is normally 2; a setlist/gig export passes 1 (see pdf.Options.Columns)
// so the printed chart is legible from a few feet away.
func renderSong(doc *gopdf.GoPdf, s *song.Song, th theme, mode pdf.Mode, hideNotes bool, cols int) error {
	if mode == pdf.ModeMap {
		return renderSongMap(doc, s, th, hideNotes)
	}
	size, allowPage2 := fitSongToPage(doc, s, th, mode, hideNotes, cols)
	doc.AddPage()
	paintPageBackground(doc, th)
	c := newCanvas(doc, true, th, cols, allowPage2)
	renderSongBody(c, s, size, mode, hideNotes)
	if c.pages > 2 {
		return fmt.Errorf("song %q still overflows at the floor size across %d pages", s.Get("title"), c.pages)
	}
	return nil
}
