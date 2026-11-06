package pdfnative

import (
	"math"
	"strconv"
	"strings"

	"app/internal/pdf"
	"app/internal/song"

	"github.com/signintech/gopdf"
)

// timeSigBeats is internal/httpapi's own timeSigBeats, duplicated rather
// than imported (httpapi depends on pdfnative, not the other way around)
// — same numerator extraction, same compound-meter (6/8, 9/8, 12/8 — beat
// is the dotted quarter, so beats = numerator/3) rule, so the PDF's
// rhythm-slash count always agrees with the web chart's and the
// metronome's for the same time signature.
func timeSigBeats(t string) int {
	i := strings.IndexByte(t, '/')
	if i <= 0 {
		return 4
	}
	n, err := strconv.Atoi(strings.TrimSpace(t[:i]))
	if err != nil || n <= 0 {
		return 4
	}
	den := strings.TrimSpace(t[i+1:])
	if den == "8" && n%3 == 0 && n >= 6 {
		return n / 3
	}
	return n
}

// beatSlot is one rhythm-slash position in a bar: either a chord's
// downbeat (Label set) or a tied/sustained beat (empty Label) — mirrors
// internal/httpapi's viewerBeat/expandBeats closely enough to produce the
// identical slash count and chord placement for the same measure.
type beatSlot struct {
	label string
	tied  bool
}

// expandBeatSlots maps a bar's raw tokens onto beatsPerBar rhythm-slash
// slots, same rule as internal/httpapi's expandBeats: a single whole-bar
// chord (or any token count that divides beatsPerBar evenly, with no
// explicit "." sub-beat dot anywhere) spreads across all beatsPerBar
// slots, chord shown on the downbeat only, the rest tied — rather than
// one slash per raw token, which under-counts a plain "Em" whole-bar
// chord to a single slash instead of the real 4 (in 4/4). A token list
// that's already explicit (has a ".", or already beatsPerBar tokens long)
// maps 1:1, no spreading.
func expandBeatSlots(tokens []string, beatsPerBar int) []beatSlot {
	hasDot := false
	for _, t := range tokens {
		if t == "." {
			hasDot = true
			break
		}
	}
	if !hasDot && beatsPerBar > 0 && len(tokens) > 0 && len(tokens) < beatsPerBar && beatsPerBar%len(tokens) == 0 {
		span := beatsPerBar / len(tokens)
		out := make([]beatSlot, 0, beatsPerBar)
		for _, t := range tokens {
			out = append(out, beatSlot{label: chordLabel(t)})
			for i := 1; i < span; i++ {
				out = append(out, beatSlot{tied: true})
			}
		}
		return out
	}
	out := make([]beatSlot, 0, len(tokens))
	for _, t := range tokens {
		if t == "." {
			out = append(out, beatSlot{tied: true})
			continue
		}
		out = append(out, beatSlot{label: chordLabel(t)})
	}
	return out
}

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
//
// Reserved for ModeJazz, which deliberately stays iReal-style — see
// measureSection's own call site. ModeDefault/ModeMap now go through
// staffBarRow instead (below): a direct, careful port of the web Play
// view's own "Song" chart (faint 5-line staff + a rhythm slash per beat,
// no grid borders at all), not a repeat of the earlier rejected attempt —
// this one matches the web's exact, already-tuned proportions rather than
// a from-scratch guess.
// timeSigCellWidth is how much horizontal room drawTimeSigTempo needs,
// for a caller to reserve before computing its own bar-box widths.
func timeSigCellWidth(size float64) float64 { return size * 2.5 }

// drawTimeSigTempo draws the tempo mark above the time signature, both
// sitting in the space before the staff's opening barline — same
// engraving convention (and the same reasoning: the time signature is
// part of the staff, the tempo mark is the one thing that genuinely
// belongs above it) the web Play view's own .staff-timesig-cell uses,
// now carried over to print instead of staying a text-line fact ("120
// bpm · 4/4") with no relation to the staff it governs. x0 is the cell's
// left edge; barTop/barBottom bound the staff/row height to center the
// time-signature numerals in. Centered dotted-quarter tempo mark, to
// match the web's own compound-meter (6/8, 9/8, 12/8) special case.
func (c *canvas) drawTimeSigTempo(x0, barTop, barBottom, size float64, tempo, timeNum, timeDen string) {
	if !c.draw {
		return
	}
	cellW := timeSigCellWidth(size)
	cx := x0 + cellW/2
	if tempo != "" {
		dotted := timeDen == "8" && (timeNum == "6" || timeNum == "9" || timeNum == "12")
		note := "♩"
		if dotted {
			note += "."
		}
		mark := note + " = " + tempo
		w := c.measureWidth(mark, faceSans, size*0.62)
		c.textAt(cx-w/2, barTop-size*0.35, mark, faceSans, size*0.62, c.th.faint)
	}
	if timeNum != "" && timeDen != "" {
		midY := (barTop + barBottom) / 2
		numW := c.measureWidth(timeNum, faceSansBold, size*0.95)
		denW := c.measureWidth(timeDen, faceSansBold, size*0.95)
		c.textAt(cx-numW/2, midY-size*0.08, timeNum, faceSansBold, size*0.95, c.th.ink)
		c.textAt(cx-denW/2, midY+size*0.78, timeDen, faceSansBold, size*0.95, c.th.ink)
	}
}

func (c *canvas) barRow(measures []song.Measure, size float64, jazz bool, firstStaff bool, tempo, timeNum, timeDen string) {
	if len(measures) == 0 {
		return
	}
	chordH := size * 1.4
	ruleGap := size * 0.65
	rowH := chordH + ruleGap
	// barRow's own barTop sits very close to the row's own top edge (the
	// boxed/jazz layout, unlike staffBarRow, reserves almost nothing
	// above it) — the tempo mark drawn above barTop needs its own extra
	// headroom here, or it collides with whatever was drawn just before
	// (the section tag) instead of sitting inside this row's own space.
	topPad := 0.0
	if firstStaff && tempo != "" {
		topPad = size * 0.65
	}
	c.ensureSpace(rowH + topPad + 6)
	c.y += topPad

	x0 := c.curX()
	// When this is the first staff (see measureSection), a time-sig/
	// tempo cell sits before the opening barline — same "part of the
	// staff, not a separate heading" convention the web chart's own
	// .staff-timesig-cell uses (see drawTimeSigTempo's own doc comment).
	// The staff's cap/base lines still span the full row width (the
	// staff itself runs continuously under the cell); only the OPENING
	// barline moves right, past the cell.
	tsW := 0.0
	if firstStaff {
		tsW = timeSigCellWidth(size)
	}
	barsX0 := x0 + tsW
	n := float64(len(measures))
	boxW := (c.colW - tsW) / n
	baseline := c.y + chordH
	ruleY := baseline + ruleGap*0.5
	barTop := baseline - chordH*0.92

	if firstStaff {
		c.drawTimeSigTempo(x0, barTop, ruleY, size, tempo, timeNum, timeDen)
	}

	// A stated repeat count ("|:4 ...") carries from the Open bar across
	// to the Close bar, same as buildSections does for the web chart —
	// printed at the END of the repeated span (see the Close branch
	// below for why), not the start. Only tracked within this one
	// printed row/call: the common case (a repeat bracket's bars all fit
	// on one line) prints correctly; a bracket that happens to wrap onto
	// a second printed row silently prints nothing rather than guessing
	// — an acceptable, rare edge case for a print layout, not worth the
	// extra plumbing a cross-row canvas-level variable would need.
	// A thin cap line above the row and a bolder baseline below it frame
	// the row as an actual staff strip — the earlier version hung chords
	// above one faint 0.7pt rule with no top edge and no closing barline
	// at either end, which read as a loose row of ticks rather than a
	// measure grid a musician's eye can lock onto while playing.
	c.line(x0, barTop, x0+c.colW, barTop, c.th.line, 0.6)
	c.line(barsX0, barTop, barsX0, ruleY, c.th.line, 1.1)
	c.line(x0+c.colW, barTop, x0+c.colW, ruleY, c.th.line, 1.1)

	pendingRepeatCount := 0
	for i, m := range measures {
		bx := barsX0 + float64(i)*boxW

		// A divider between bars — not a box, just the one edge that
		// actually separates two measures (the row's own left/right
		// margins already bound the row itself via the framing barlines
		// just above, so the first bar needs no line on its own left and
		// the last none on its right). Weighted a notch heavier than the
		// original 0.5pt hairline — visible as a real measure boundary at
		// a glance, not just on close inspection.
		if i > 0 {
			c.line(bx, barTop, bx, ruleY, c.th.line, 0.7)
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
			c.drawChordSymbolMode(startX+float64(si)*slotW, baseline, label, size*0.95, c.th.chord, jazz)
		}
	}
	c.line(x0, ruleY, x0+c.colW, ruleY, c.th.line, 1.3)
	c.y = ruleY + 6
}

// staffBarRow draws one line of bar-chart measures as an open 5-line
// staff — the print analogue of the web Play view's "Song" chart
// (internal/webassets/static/css/app.css's --staff-5lines + .slash), not
// the boxed iReal-style table barRow draws for jazz mode. No cell
// borders anywhere: a faint 5-line staff spans the row, thin barlines
// mark measure boundaries (real engraving convention, not a table grid),
// and a rotated slash fills every beat-slot — muted for a tied/sustained
// beat, full weight where a chord actually starts, with the chord symbol
// itself sitting above the staff. Proportions (staff height, slash angle
// and size, line weight) are carried over from the web CSS's own tuned
// values, not re-guessed — matching an already-approved design beats a
// fresh attempt at the same idea.
func (c *canvas) staffBarRow(measures []song.Measure, size float64, beatsPerBar int, firstStaff bool, tempo, timeNum, timeDen string) {
	if len(measures) == 0 {
		return
	}
	chordH := size * 1.4
	staffH := size * 1.05
	ruleGap := size * 0.5
	rowH := chordH + staffH + ruleGap
	c.ensureSpace(rowH + 6)

	x0 := c.curX()
	tsW := 0.0
	if firstStaff {
		tsW = timeSigCellWidth(size)
	}
	barsX0 := x0 + tsW
	n := float64(len(measures))
	boxW := (c.colW - tsW) / n
	baseline := c.y + chordH
	staffTop := baseline + size*0.18
	staffBottom := staffTop + staffH

	if firstStaff {
		c.drawTimeSigTempo(x0, staffTop, staffBottom, size, tempo, timeNum, timeDen)
	}

	// The 5-line staff itself: evenly spaced, faint — background texture
	// for the slashes/barlines to sit on, never competing with them. Runs
	// the full row width, continuous under the time-sig cell too (see
	// barRow's own comment on why only the OPENING barline, not the
	// staff lines, moves past the cell).
	for i := 0; i < 5; i++ {
		ly := staffTop + staffH*float64(i)/4
		c.line(x0, ly, x0+c.colW, ly, c.th.faint, 0.4)
	}
	// Thin barlines bound the row's own ends — real engraving draws a
	// barline at the start and end of every system, not just between
	// measures (an open/close repeat sign below overrides either end).
	c.line(barsX0, staffTop, barsX0, staffBottom, c.th.line, 0.7)
	c.line(x0+c.colW, staffTop, x0+c.colW, staffBottom, c.th.line, 0.7)

	pendingRepeatCount := 0
	for i, m := range measures {
		bx := barsX0 + float64(i)*boxW

		if i > 0 {
			c.line(bx, staffTop, bx, staffBottom, c.th.line, 0.7)
		}

		if m.Open {
			c.line(bx+2, staffTop, bx+2, staffBottom, c.th.ink, 1.8)
			c.repeatDots(bx+7, staffTop, staffH)
			if m.RepeatCount > 0 {
				pendingRepeatCount = m.RepeatCount
			}
		}
		if m.Close {
			c.line(bx+boxW-2, staffTop, bx+boxW-2, staffBottom, c.th.ink, 1.8)
			c.repeatDots(bx+boxW-9, staffTop, staffH)
			if pendingRepeatCount > 0 {
				c.textAt(bx+boxW-24, c.y-1, "x"+strconv.Itoa(pendingRepeatCount), faceSansBold, size*0.5, c.th.faint)
				pendingRepeatCount = 0
			}
		}

		if m.Volta != "" {
			c.textAt(bx+2, c.y-1, m.Volta, faceSans, size*0.62, c.th.faint)
		}

		toks := m.Tokens
		if len(toks) == 1 && (toks[0] == "%" || toks[0] == "%%") {
			c.drawBarRepeatGlyph(bx+boxW/2, (staffTop+staffBottom)/2+size*0.2, chordH*0.7, c.th.faint)
			if toks[0] == "%%" {
				c.textAt(bx+boxW/2-3, staffTop-size*0.3, "2", faceSansBold, size*0.5, c.th.faint)
			}
			continue
		}

		startX := bx + 3
		if m.Open {
			startX = bx + 14
		}
		slots := expandBeatSlots(toks, beatsPerBar)
		slotW := (boxW - (startX - bx) - 3) / float64(len(slots))
		for si, sl := range slots {
			sx := startX + float64(si)*slotW + slotW/2
			col := c.th.ink
			if sl.tied {
				col = c.th.faint
			}
			c.drawSlash(sx, (staffTop+staffBottom)/2, staffH*0.8, col)
			if !sl.tied {
				c.drawChordSymbolMode(startX+float64(si)*slotW, baseline, sl.label, size*0.95, c.th.chord, false)
			}
		}
	}
	c.y = staffBottom + ruleGap
}

// drawSlash draws one rhythm-slash mark (the LilyPond/Real Book "play a
// beat of this chord" convention), centered on (cx, cy), rotated to match
// the web chart's own .slash (10°) — a short thick diagonal stroke, not a
// character glyph (no face has a slash at the right weight/angle).
func (c *canvas) drawSlash(cx, cy, h float64, col rgb) {
	if !c.draw {
		return
	}
	const deg = 10.0 * math.Pi / 180
	dx, dy := math.Sin(deg)*h/2, math.Cos(deg)*h/2
	c.line(cx-dx, cy+dy, cx+dx, cy-dy, col, h*0.16)
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
// hideNotes mutes the Notes loop regardless of mode. firstStaff/tempo/
// timeNum/timeDen: when firstStaff is true, the row's own FIRST printed
// line gets the tempo+time-signature cell before its opening barline —
// see renderSongBody's one-shot firstStaffDrawn tracking.
func (c *canvas) measureSection(sec song.Section, bodySize float64, mode pdf.Mode, hideNotes bool, globalBeats int, firstStaff bool, tempo, timeNum, timeDen string) {
	if mode != pdf.ModeLyrics && len(sec.Measures) > 0 {
		beatsPerBar := globalBeats
		if sec.Time != "" {
			beatsPerBar = timeSigBeats(sec.Time)
		}
		lineIdx := 0
		start := 0
		for start < len(sec.Measures) {
			n := barsPerLineFor(sec, lineIdx)
			end := start + n
			if end > len(sec.Measures) {
				end = len(sec.Measures)
			}
			showTS := firstStaff && lineIdx == 0
			if mode == pdf.ModeJazz {
				c.barRow(sec.Measures[start:end], bodySize, true, showTS, tempo, timeNum, timeDen)
			} else {
				c.staffBarRow(sec.Measures[start:end], bodySize, beatsPerBar, showTS, tempo, timeNum, timeDen)
			}
			start = end
			lineIdx++
		}
		if mode != pdf.ModeJazz && len(sec.Lyrics) > 0 {
			// A beat more air between the grid's own closing baseline
			// and the lyric block right under it — without this they
			// visually fused into one run-on block (the grid's last
			// rule sat only 6pt above the first lyric line's own
			// baseline), blurring where the chart ends and the words
			// begin.
			c.y += bodySize * 0.35
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
