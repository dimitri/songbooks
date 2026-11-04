package pdfnative

import (
	"math"
	"strconv"

	"app/internal/pdf"
	"app/internal/song"

	"github.com/signintech/gopdf"
)

// chordLabel strips a token's ":duration" tag down to the bare chord/rest
// symbol — song.TokenChord, shared with the web Play view's own beat-slot
// rendering (internal/httpapi's expandBeats) so both surfaces apply the
// exact same rule.
func chordLabel(tok string) string {
	return song.TokenChord(tok)
}

// barRow draws (or measures) one line of bar-chart measures, iReal Pro
// style: thin hairline barlines (not a bordered box per bar), one bold
// chord symbol per beat that actually names one (nothing drawn for a tied/
// sustained beat — real lead sheets don't re-mark every beat, only where a
// chord starts), a single centered glyph for a whole-bar repeat, and a
// thin+thick double-rule for a repeat-barline open/close instead of a box
// edge to attach it to. This replaced an earlier bordered-box-per-bar
// version with a diagonal slash on every beat — correct but, per direct
// user feedback, "looking like done by a kid without talent"; iReal Pro's
// actual chart style (reviewed live via its App Store screenshots) is
// exactly this sparser, more typographic look.
func (c *canvas) barRow(measures []song.Measure, size float64) {
	if len(measures) == 0 {
		return
	}
	chordH := size * 1.3
	ruleGap := size * 0.55
	rowH := chordH + ruleGap
	c.ensureSpace(rowH + 6)

	n := float64(len(measures))
	boxW := c.colW / n
	x0 := c.curX()
	baseline := c.y + chordH
	ruleY := baseline + ruleGap*0.5
	barTop := baseline - chordH*0.92

	// A stated repeat count ("|:4 ...") carries from the Open bar across
	// to the Close bar, same as buildSections does for the web chart —
	// printed at the END of the repeated span (see the Close branch
	// below for why), not the start. Only tracked within this one
	// printed row/call: the common case (a repeat bracket's bars all fit
	// on one line) prints correctly; a bracket that happens to wrap onto
	// a second printed row silently prints nothing rather than guessing
	// — an acceptable, rare edge case for a print layout, not worth the
	// extra plumbing a cross-row canvas-level variable would need.
	pendingRepeatCount := 0
	for i, m := range measures {
		bx := x0 + float64(i)*boxW

		// A hairline divider between bars — not a box, just the one edge
		// that actually separates two measures (the row's own left/right
		// margins already bound the row itself, so the first bar needs no
		// line on its own left and the last none on its right).
		if i > 0 {
			c.line(bx, barTop, bx, ruleY, c.th.line, 0.5)
		}

		// Repeat barline: a thin stroke next to the music, a heavier one
		// just outside it, two dots between — LilyPond/real-chart
		// convention, same thin:thick ratio the web chart's own CSS
		// already uses, just without a box edge to hang it off of here.
		if m.Open {
			c.line(bx+2, barTop, bx+2, ruleY, c.th.ink, 1.8)
			c.repeatDots(bx+7, barTop, ruleY-barTop)
			if m.RepeatCount > 0 {
				pendingRepeatCount = m.RepeatCount
			}
		}
		if m.Close {
			c.line(bx+boxW-2, barTop, bx+boxW-2, ruleY, c.th.ink, 1.8)
			c.repeatDots(bx+boxW-9, barTop, ruleY-barTop)
			// A STATED repeat count ("|:4 ..." — song.Measure.RepeatCount's
			// doc comment) prints next to the CLOSING repeat sign, same as
			// the web chart's own .bracket-repeat-count — real engraving
			// places it at the end of the repeated span, not the start. A
			// plain count-less bracket (the implicit "x2" every repeat
			// sign already means) prints nothing, same as before this
			// existed. Y = c.y-1, same anchor the Volta marker just below
			// already uses for its own above-the-staff label — the
			// previous "barTop+size*0.2" baseline sat INSIDE the staff
			// box, so the glyph's own ascent shot up well past the row's
			// own top margin (c.y) into whatever sits above it, reading
			// as "floating high above the bar" rather than sitting on it
			// the way Guitar Pro prints an explicit repeat count.
			if pendingRepeatCount > 0 {
				c.textAt(bx+boxW-24, c.y-1, "x"+strconv.Itoa(pendingRepeatCount), faceSansBold, size*0.5, c.th.faint)
				pendingRepeatCount = 0
			}
		}

		// Volta marker ("1.", "1,2,3.") sits just above the bar, left edge.
		if m.Volta != "" {
			c.textAt(bx+2, c.y-1, m.Volta, faceSans, size*0.62, c.th.faint)
		}

		// "%" repeats the previous bar, "%%" the previous TWO — the same
		// placeholder convention v2/lib/staff.typ's PLACEHOLDER list and
		// is-simile check use; both render as one centered repeat glyph,
		// "%%" additionally getting a small repeat-count badge (matching
		// the web chart's own .simile-count, which shows "2" there).
		toks := m.Tokens
		if len(toks) == 1 && (toks[0] == "%" || toks[0] == "%%") {
			c.drawBarRepeatGlyph(bx+boxW/2, baseline, chordH*0.7, c.th.faint)
			if toks[0] == "%%" {
				c.textAt(bx+boxW/2-3, barTop+size*0.2, "2", faceSansBold, size*0.5, c.th.faint)
			}
			continue
		}

		// Chords positioned by their OWN beat-slot index, not packed
		// left-to-right in drawn order — a tied/sustained beat ("." or an
		// empty slot) draws nothing but still occupies its slot's width,
		// same as the web chart's own equal-width .beat-slot flex cells.
		// Packing by drawn order (the previous version) put every chord
		// immediately after the one before it regardless of how many ties
		// separated them, so "A . D ." (chord on beat 1, chord on beat 3)
		// rendered D hard against A as if they were beats 1 and 2 — a real
		// reported bug, caught by comparing the PDF against this exact
		// bar's own web chart rendering. Start further right when this
		// bar's own repeat-open dots (above) are drawn in the same corner.
		startX := bx + 3
		if m.Open {
			startX = bx + 14
		}
		slotW := (boxW - (startX - bx) - 3) / float64(len(toks))
		for si, tok := range toks {
			label := chordLabel(tok)
			if label == "." || label == "%" || label == "%%" || label == "" {
				continue
			}
			c.drawChordSymbol(startX+float64(si)*slotW, baseline, label, size*0.95, c.th.chord)
		}
	}
	c.line(x0, ruleY, x0+c.colW, ruleY, c.th.line, 0.7)
	c.y = ruleY + 6
}

// filledDot draws a solid circle of radius r centered on (cx, cy) — gopdf's
// own Oval always STROKES its path (cache_content_oval.go ends every oval
// with the "S" operator, never "f", regardless of SetFillColor), so calling
// it for a "dot" draws a hollow ring instead — the reported "donut, not a
// dot" look on every repeat-barline/simile mark in the PDF. Approximated as
// a 16-gon via Polygon's "F" (fill) style, the one primitive this library
// actually fills; invisible at dot size (a few points across), same as
// v2/lib/staff.typ's own simile() dots already do in the Typst pipeline.
func (c *canvas) filledDot(cx, cy, r float64) {
	const segments = 16
	pts := make([]gopdf.Point, segments)
	for i := range pts {
		a := 2 * math.Pi * float64(i) / segments
		pts[i] = gopdf.Point{X: cx + r*math.Cos(a), Y: cy + r*math.Sin(a)}
	}
	c.doc.Polygon(pts, "F")
}

// filledCircle draws a solid circle of radius r centered on (cx, cy) —
// the sectionTag badge's "stage"-style round variant. Same fill-via-
// polygon approach as filledDot (see its own doc comment for why gopdf's
// own Oval can't be used for a filled shape), just at badge size instead
// of a tiny repeat-mark dot.
func (c *canvas) filledCircle(cx, cy, r float64, col rgb) {
	if !c.draw {
		return
	}
	const segments = 32
	pts := make([]gopdf.Point, segments)
	for i := range pts {
		a := 2 * math.Pi * float64(i) / segments
		pts[i] = gopdf.Point{X: cx + r*math.Cos(a), Y: cy + r*math.Sin(a)}
	}
	c.doc.SetFillColor(col.r, col.g, col.b)
	c.doc.Polygon(pts, "F")
}

// circleOutline draws a stroked (unfilled) ring of radius r centered on
// (cx, cy) — gopdf's Oval always strokes, never fills (see filledDot's
// doc comment), which is exactly the shape this needs: the "classic"
// style's plain rehearsal-mark ring, restrained next to iReal Pro's
// filled chip.
func (c *canvas) circleOutline(cx, cy, r, width float64, col rgb) {
	if !c.draw {
		return
	}
	c.doc.SetStrokeColor(col.r, col.g, col.b)
	c.doc.SetLineWidth(width)
	c.doc.Oval(cx-r, cy-r, cx+r, cy+r)
}

// drawBarRepeatGlyph draws a "play the previous bar again" mark: a
// diagonal slash flanked by two dots, the real-chart convention (and
// v2/lib/staff.typ's own simile() glyph), centered on (cx, baselineY).
func (c *canvas) drawBarRepeatGlyph(cx, baselineY, h float64, col rgb) {
	if !c.draw {
		return
	}
	c.line(cx-h*0.32, baselineY+h*0.25, cx+h*0.32, baselineY-h*0.65, col, h*0.14)
	r := h * 0.08
	c.doc.SetFillColor(col.r, col.g, col.b)
	c.filledDot(cx-h*0.32, baselineY-h*0.1, r)
	c.filledDot(cx+h*0.32, baselineY-h*0.5, r)
}

// repeatDots draws the two dots a repeat barline carries, vertically
// centered in the box.
func (c *canvas) repeatDots(x, top, h float64) {
	if !c.draw {
		return
	}
	r := 1.1
	c.doc.SetFillColor(c.th.ink.r, c.th.ink.g, c.th.ink.b)
	c.filledDot(x, top+h*0.33, r)
	c.filledDot(x, top+h*0.67, r)
}

// barsPerLineFor applies the section's own override (song.Section.
// BarsPerLine), defaulting to 4 — the print chart's own default (distinct
// from the web Jazz view's intentionally denser 8-bars/row default, set in
// internal/httpapi/handlers_pages.go's groupMeasures to match the approved
// iReal Pro mockup). 4/row is what makes standard forms read cleanly on
// paper: an 8-bar form wraps as 2 rows, a 12-bar blues as 3 — real sheet
// music convention — rather than one long uneven row.
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
// mode trims which parts actually draw: ModeJazz skips the lyric lines
// (chords-only, the print analogue of the web Jazz chart view — with no
// lyrics to fit, fitSongToPage naturally lands on a larger, denser-reading
// size instead of needing a second, separate "tight spacing" code path);
// ModeLyrics skips the bar-chart rows entirely (lyrics + the section tags
// above are the structure indication, per the user's own "lyrics only +
// struct indications" spec — no separate structure element needed).
// ModeDefault/ModeMap (ModeMap never reaches this function) draw both.
// hideNotes mutes the Notes loop regardless of mode.
func (c *canvas) measureSection(sec song.Section, bodySize float64, mode pdf.Mode, hideNotes bool) {
	if mode != pdf.ModeLyrics && len(sec.Measures) > 0 {
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
	if mode != pdf.ModeJazz {
		for _, ln := range sec.Lyrics {
			c.lyricLine(ln, bodySize)
		}
	}
	if !hideNotes {
		for _, note := range sec.Notes {
			c.wrappedText(note, faceSansIt, bodySize*0.92, c.th.faint)
		}
	}
}
