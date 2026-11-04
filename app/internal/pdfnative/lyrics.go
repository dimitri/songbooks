package pdfnative

import (
	"strings"

	"app/internal/song"
)

// lyricLine draws (or measures) one chorded lyric line — v2/lib/lyrics.typ's
// job: each word wraps normally within the column, and the FIRST word of
// any chord-bearing segment gets its chord symbol set directly above it,
// bold and in the theme's chord color. A chord only ever labels where its
// segment starts (not re-shown if that segment's text wraps across
// several words) — the same convention every real lead sheet uses.
func (c *canvas) lyricLine(ln song.LyricLine, size float64) {
	lineH := size * 1.95
	spaceW := c.measureWidth(" ", faceSans, size)

	c.ensureSpace(lineH)
	x := c.curX()
	lineStartX := x
	baseline := func() float64 { return c.y + lineH - size*0.55 }

	// maxX is recomputed on every call, never cached: ensureSpace/newLine
	// can switch column (or page), which moves curX() — a maxX captured
	// before that happened would belong to the OLD column and reject
	// every word on the new one, collapsing the whole rest of the
	// paragraph to one word per line (a real bug this fixed).
	maxX := func() float64 { return c.curX() + c.colW }

	newLine := func() {
		c.y += lineH
		c.ensureSpace(lineH)
		x = c.curX()
		lineStartX = x
	}

	for _, seg := range ln.Segments {
		words := strings.Fields(seg.Text)
		if len(words) == 0 {
			// A chord-bearing segment with no following word at all — the
			// routine trailing-chord/turnaround shape ("...but you [E] "),
			// common in both UG and LaTeX-songs-package imports. The
			// chord still needs to be drawn (dropping it here was a real
			// bug: strings.Fields of an empty/whitespace-only string
			// returns zero words, so the per-word loop below never ran
			// at all) — same x position a word would have started at,
			// advancing by roughly one chord-symbol's width so a run of
			// several bare trailing chords doesn't stack on top of
			// itself.
			if seg.HasChord {
				c.drawChordSymbol(x, baseline()-size*0.85, seg.Chord, size*0.95, c.th.chord)
				x += c.measureWidth(seg.Chord, faceSans, size*0.95) + spaceW
			}
			continue
		}
		for wi, w := range words {
			ww := c.measureWidth(w, faceSans, size)
			if x+ww > maxX() && x > lineStartX {
				newLine()
			}
			if wi == 0 && seg.HasChord {
				c.drawChordSymbol(x, baseline()-size*0.85, seg.Chord, size*0.95, c.th.chord)
			}
			c.textAt(x, baseline(), w, faceSans, size, c.th.ink)
			x += ww + spaceW
		}
	}
	c.y += lineH
}

// wrappedText draws (or measures) a plain, unchorded paragraph (section
// notes, artist bylines on the contents page) wrapped to the column width.
func (c *canvas) wrappedText(s string, face string, size float64, col rgb) {
	lineH := size * 1.5
	spaceW := c.measureWidth(" ", face, size)

	c.ensureSpace(lineH)
	x := c.curX()
	lineStartX := x
	maxX := func() float64 { return c.curX() + c.colW }

	words := strings.Fields(s)
	for _, w := range words {
		ww := c.measureWidth(w, face, size)
		if x+ww > maxX() && x > lineStartX {
			c.y += lineH
			c.ensureSpace(lineH)
			x = c.curX()
			lineStartX = x
		}
		c.textAt(x, c.y+lineH-size*0.3, w, face, size, col)
		x += ww + spaceW
	}
	c.y += lineH
}
