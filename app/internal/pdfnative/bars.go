package pdfnative

import "app/internal/song"

// barRow draws (or measures) one line of bar-chart measures — the
// chord-grid half of v2/lib/grid.typ: bordered boxes, a diagonal slash per
// beat, repeat-barline double-strokes with dots, and the chord symbol for
// any beat that actually names one, placed above its slash.
func (c *canvas) barRow(measures []song.Measure, size float64) {
	if len(measures) == 0 {
		return
	}
	boxH := size*1.3 + 10
	chordH := size + 4
	rowH := boxH + chordH
	c.ensureSpace(rowH + 4)

	n := float64(len(measures))
	boxW := c.colW / n
	x0 := c.curX()
	boxTop := c.y + chordH

	for i, m := range measures {
		bx := x0 + float64(i)*boxW
		c.strokedRect(bx, boxTop, boxW, boxH, c.th.ink, 0.7)

		// Repeat barlines: a heavier second stroke + two dots just inside
		// the box, at whichever edge opens/closes the repeat.
		if m.Open {
			c.line(bx+3, boxTop, bx+3, boxTop+boxH, c.th.ink, 1.6)
			c.repeatDots(bx+7, boxTop, boxH)
		}
		if m.Close {
			c.line(bx+boxW-3, boxTop, bx+boxW-3, boxTop+boxH, c.th.ink, 1.6)
			c.repeatDots(bx+boxW-10, boxTop, boxH)
		}

		// Volta marker ("1.", "1,2,3.") sits just above the box, left edge.
		if m.Volta != "" {
			c.textAt(bx+2, c.y-1, m.Volta, faceSans, size*0.72, c.th.faint)
		}

		toks := m.Tokens
		if len(toks) == 1 && toks[0] == "%" {
			// Whole-bar repeat-previous: one centered "%" mark, no slashes.
			tw := c.measureWidth("%", faceSansBold, size)
			c.textAt(bx+boxW/2-tw/2, boxTop+boxH/2+size*0.35, "%", faceSansBold, size, c.th.faint)
			continue
		}
		if len(toks) == 0 {
			toks = []string{"."}
		}
		sub := boxW / float64(len(toks))
		for j, tok := range toks {
			cx := bx + float64(j)*sub
			// Diagonal slash, the universal "play a beat here" mark —
			// every beat gets one, chorded or sustained ("." / repeated).
			c.line(cx+sub*0.28, boxTop+boxH*0.82, cx+sub*0.72, boxTop+boxH*0.18, c.th.faint, 0.9)
			if tok != "." && tok != "%" && tok != "" {
				tw := c.measureWidth(tok, faceSansBold, size*0.82)
				chordX := cx + 2
				if chordX+tw > bx+boxW-2 {
					chordX = bx + boxW - 2 - tw
				}
				c.textAt(chordX, c.y+chordH-3, tok, faceSansBold, size*0.82, c.th.chord)
			}
		}
	}
	c.y = boxTop + boxH + 5
}

// repeatDots draws the two dots a repeat barline carries, vertically
// centered in the box.
func (c *canvas) repeatDots(x, top, h float64) {
	if !c.draw {
		return
	}
	r := 1.6
	c.doc.SetFillColor(c.th.ink.r, c.th.ink.g, c.th.ink.b)
	c.doc.Oval(x-r, top+h*0.33-r, x+r, top+h*0.33+r)
	c.doc.Oval(x-r, top+h*0.67-r, x+r, top+h*0.67+r)
}

// barsPerLineFor applies the section's own override (song.Section.
// BarsPerLine), defaulting to 4 — same rule v2/lib/grid.typ uses.
func barsPerLineFor(sec song.Section, lineIdx int) int {
	switch len(sec.BarsPerLine) {
	case 0:
		return 4
	case 1:
		return sec.BarsPerLine[0]
	default:
		if lineIdx < len(sec.BarsPerLine) {
			return sec.BarsPerLine[lineIdx]
		}
		return sec.BarsPerLine[len(sec.BarsPerLine)-1]
	}
}

// measureSection draws (or measures) every bar-row and lyric line a
// section carries, in source order — grid rows first mirrors how a real
// chart reads top to bottom: the groove, then the words sung over it.
func (c *canvas) measureSection(sec song.Section, bodySize float64) {
	if len(sec.Measures) > 0 {
		lineIdx := 0
		start := 0
		for start < len(sec.Measures) {
			n := barsPerLineFor(sec, lineIdx)
			end := start + n
			if end > len(sec.Measures) {
				end = len(sec.Measures)
			}
			c.barRow(sec.Measures[start:end], bodySize)
			start = end
			lineIdx++
		}
	}
	for _, ln := range sec.Lyrics {
		c.lyricLine(ln, bodySize)
	}
	for _, note := range sec.Notes {
		c.wrappedText(note, faceSansIt, bodySize*0.92, c.th.faint)
	}
}
