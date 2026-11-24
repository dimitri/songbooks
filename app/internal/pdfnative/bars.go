package pdfnative

import (
	"math"
	"strconv"
	"strings"

	"app/internal/song"
	"app/internal/tab"

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
		noteSize := size * 0.55
		label := " = " + tempo
		labelW := c.measureWidth(label, faceSans, size*0.62)
		noteW := c.quarterNoteWidth(noteSize, dotted)
		baselineY := barTop - size*0.35
		startX := cx - (noteW+labelW)/2
		adv := c.drawQuarterNote(startX, baselineY, noteSize, dotted, c.th.faint)
		c.textAt(startX+adv, baselineY, label, faceSans, size*0.62, c.th.faint)
	}
	if timeNum != "" && timeDen != "" {
		// Each numeral is sized and baselined to fill most of its own half
		// of the staff height (barTop..midY and midY..barBottom), not a
		// fixed offset from the exact middle — a fixed offset (the
		// previous approach) didn't scale with the half-height available,
		// which is what let both numerals print partly above/below the
		// staff itself (the reported "doesn't fit within the staff" bug).
		// 0.8 approximates a bold sans digit's ascent as a fraction of its
		// own font size (no descender to account for) — same ascent
		// convention renderSongHeader already uses for its title baseline.
		half := (barBottom - barTop) / 2
		fs := half * 0.95
		const ascent = 0.8
		midY := barTop + half
		numBaseline := barTop + (half+ascent*fs)/2
		denBaseline := midY + (half+ascent*fs)/2
		numW := c.measureWidth(timeNum, faceSansBold, fs)
		denW := c.measureWidth(timeDen, faceSansBold, fs)
		c.textAt(cx-numW/2, numBaseline, timeNum, faceSansBold, fs, c.th.ink)
		c.textAt(cx-denW/2, denBaseline, timeDen, faceSansBold, fs, c.th.ink)
	}
}

// quarterNoteWidth returns drawQuarterNote's footprint without drawing —
// used to center the glyph+"= 94" assembly as one block.
func (c *canvas) quarterNoteWidth(size float64, dotted bool) float64 {
	r := size * 0.17
	w := r*2 + size*0.05
	if dotted {
		w += size * 0.22
	}
	return w
}

// drawQuarterNote draws a quarter-note glyph (filled oval notehead + stem,
// plus an augmentation dot for the dotted/compound-meter case) as vector
// shapes rather than the Unicode "♩"/"♩." characters the tempo mark used
// to print — the embedded Lato font carries no musical-symbol glyphs, so
// that character silently drew nothing (the reported "tempo is missing
// the note" bug: the mark read as a bare "= 94"). x is the glyph's left
// edge, baselineY is where the notehead's bottom sits (same convention
// textAt's y uses for text). Returns the glyph's total width.
func (c *canvas) drawQuarterNote(x, baselineY, size float64, dotted bool, col rgb) float64 {
	r := size * 0.17
	cx := x + r
	cy := baselineY - r
	c.filledCircle(cx, cy, r, col)
	stemX := cx + r*0.9
	c.line(stemX, cy, stemX, cy-size*0.75, col, size*0.08)
	w := r*2 + size*0.05
	if dotted {
		dotR := size * 0.05
		dotCx := cx + r + size*0.17
		c.filledCircle(dotCx, cy, dotR, col)
		w = (dotCx + dotR) - x
	}
	return w
}

// staffBarRow draws one line of bar-chart measures as an open 5-line
// staff — the print analogue of the web Play view's "Song" chart
// (internal/webassets/static/css/app.css's --staff-5lines + .slash), not
// a boxed iReal-style table. No cell
// borders anywhere: a faint 5-line staff spans the row, thin barlines
// mark measure boundaries (real engraving convention, not a table grid),
// and a rotated slash fills every beat-slot — muted for a tied/sustained
// beat, full weight where a chord actually starts, with the chord symbol
// itself sitting above the staff. Proportions (staff height, slash angle
// and size, line weight) are carried over from the web CSS's own tuned
// values, not re-guessed — matching an already-approved design beats a
// fresh attempt at the same idea.
func (c *canvas) staffBarRow(measures []song.Measure, slots int, pickup int, riffBars []*tab.Bar, tuning []int, size float64, beatsPerBar int, firstStaff bool, tempo, timeNum, timeDen string) {
	if len(measures) == 0 {
		return
	}
	// A row the section's riff covers whole is drawn as tab (tabOnlyRow);
	// measureSection cuts the rows at the riff's ends (splitRiffRows), so
	// a row is all riff or none.
	allTab := len(riffBars) > 0
	for _, b := range riffBars {
		if b == nil {
			allTab = false
		}
	}
	if allTab {
		timeNumI, _ := strconv.Atoi(timeNum)
		if timeNumI <= 0 {
			timeNumI = beatsPerBar
		}
		timeDenI, _ := strconv.Atoi(timeDen)
		if timeDenI <= 0 {
			timeDenI = 4
		}
		c.tabOnlyRow(measures, riffBars, tuning, size, beatsPerBar, firstStaff, tempo, timeNum, timeDen, timeNumI, timeDenI)
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
	// slots: how many bars wide the row's regular bars are drawn — its
	// own count, or more for a short last row, whose bars stay as wide as
	// a full row's and whose staff ends at its last bar (rowEnd); a pickup
	// takes half a bar's width beside them (barGeometry).
	xs, ws := barGeometryN(len(measures), slots, pickup, c.colW-tsW)
	rowEnd := barsX0 + xs[len(xs)-1] + ws[len(ws)-1]
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
		c.line(x0, ly, rowEnd, ly, c.th.faint, 0.4)
	}
	// Thin barlines bound the row's own ends — real engraving draws a
	// barline at the start and end of every system, not just between
	// measures (an open/close repeat sign below overrides either end).
	c.line(barsX0, staffTop, barsX0, staffBottom, c.th.line, 0.7)
	c.line(rowEnd, staffTop, rowEnd, staffBottom, c.th.line, 0.7)

	pendingRepeatCount := 0
	for i, m := range measures {
		bx, boxW := barsX0+xs[i], ws[i]
		c.leadTag(measures, i, bx, staffBottom, size)

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
		if m.Lead && tab.SilentBar(toks) {
			// A chordless lead bar: a whole-bar rest hanging from the
			// staff's second line, as the web draws it.
			c.filledRect(bx+boxW/2-staffH*0.3, staffTop+staffH/4, staffH*0.6, staffH/8, c.th.ink)
			continue
		}
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

// pickupWidth is a pickup bar's width, in regular bars: it holds a beat or
// two, or a count-in's rest (the web's .bar-pickup is narrow too).
const pickupWidth = 0.5

// barGeometry places a row's n bars across width: each one's offset and
// width, its regular bars slots to the width (more than n for a short
// row), a pickup (the first bar, when pickup) pickupWidth of one.
func barGeometry(n, slots int, pickup bool, width float64) (xs, ws []float64) {
	pk := 0
	if pickup {
		pk = 1
	}
	return barGeometryN(n, slots, pk, width)
}

// barGeometryN is barGeometry for a row opening with pickups pickups (a
// "lead:" line's bars, song.LeadMeasures), each pickupWidth wide.
func barGeometryN(n, slots, pickups int, width float64) (xs, ws []float64) {
	pk := 0.0
	if pickups > 0 && n > pickups {
		pk = float64(pickups)
	}
	regular := float64(n) - pk
	unit := width / (math.Max(regular, float64(slots)) + pk*pickupWidth)
	x := 0.0
	for i := 0; i < n; i++ {
		w := unit
		if float64(i) < pk {
			w = unit * pickupWidth
		}
		xs, ws = append(xs, x), append(ws, w)
		x += w
	}
	return xs, ws
}

// leadTag prints a small "lead" under the first of a run of "lead:" bars
// (song.LeadMeasures): the song's bars before bar 1, in no section —
// the web chart's .lead-tag.
func (c *canvas) leadTag(measures []song.Measure, i int, bx, below, size float64) {
	if !measures[i].Lead || (i > 0 && measures[i-1].Lead) {
		return
	}
	c.textAt(bx+1.5, below+size*0.55, "lead", faceSans, size*0.48, c.th.faint)
}

// phraseBars is the print chart's default row: 4 bars, the lead-sheet
// phrase (distinct from the web Jazz view's intentionally denser
// 8-bars/row default, set in internal/httpapi/handlers_pages.go's
// groupMeasures to match the approved iReal Pro mockup). An 8-bar form
// wraps as 2 rows, a 12-bar blues as 3 — real sheet music convention —
// rather than one long uneven row.
const phraseBars = 4

// measureSection draws (or measures) every bar-row and lyric line a
// section carries, in source order — grid rows first mirrors how a real
// chart reads top to bottom: the groove, then the words sung over it.
// mode trims which parts actually draw: ModeLyrics skips the bar-chart rows entirely (lyrics + the section tags
// above are the structure indication, per the user's own "lyrics only +
// struct indications" spec — no separate structure element needed).
// ModeDefault draws both (ModeMap and ModeJazz never reach this
// function: renderSongMap, and jazz.go's renderJazz, the web's own Jazz
// chart).
// hideNotes mutes the Notes loop regardless of mode. firstStaff/tempo/
// timeNum/timeDen: when firstStaff is true, the row's own FIRST printed
// line gets the tempo+time-signature cell before its opening barline —
// see renderSongBody's one-shot firstStaffDrawn tracking.
func (c *canvas) measureSection(sec song.Section, riff *tab.Riff, bodySize float64, mode Mode, hideNotes bool, globalBeats int, firstStaff, pickup bool, tempo, timeNum, timeDen string) {
	// The song's first grid: its pickup bar, or the song's "lead:" bars
	// drawn ahead of it (withLeadDrawn), as many as it has — on the first
	// row, on top of its "bars:" count.
	leads, pickups := 0, 0
	if pickup {
		leads = song.LeadBars(sec.Measures)
		pickups = max(1, leads)
	}
	if mode != ModeLyrics && len(sec.Measures) > 0 {
		beatsPerBar := globalBeats
		if sec.Time != "" {
			beatsPerBar = timeSigBeats(sec.Time)
		}
		// riffBars is sec.Measures-length, nil everywhere except the bar
		// indices riff actually has data for — "optionally we have
		// scores content for some of the bars", same convention the web
		// chart's attachTabRiffs uses.
		var riffBars []*tab.Bar
		var tuning []int
		if riff != nil {
			tuning = riff.Tuning
			riffBars = make([]*tab.Bar, len(sec.Measures))
			// The score starts on the section's first written bar, after
			// its "lead:" bars.
			lead := song.LeadBars(sec.Measures)
			for i := range riff.Bars {
				if i+lead < len(riffBars) {
					riffBars[i+lead] = &riff.Bars[i]
				}
			}
		}
		// Each row's bar count, and how many bars wide its regular bars
		// are drawn: the web Song view's rows (sectionBarRows, the Go twin
		// of bar-rows.js) — 4-bar phrases or the author's "bars:" rows, a
		// pickup riding on the first row, a short last row keeping a full
		// row's bar width.
		start := 0
		rows := sectionBarRowsN(sec.BarsPerLine, len(sec.Measures), pickup && leads == 0, leads)
		// In a half-width column (the two-column Song/Lyrics layout) a
		// staff row holds at most 4 bars, scaled to the column — the web
		// Song view's own cap (song.js reflowStaffLines, modeCap 4) — so a
		// section authored "bars: 8" for a full-width page doesn't cram 8
		// bars into half the width.
		// A riff over part of the section (a 4-bar intro riff over a
		// 12-bar blues): rows cut at the riff's ends, all tab or all grid.
		if riffBars != nil {
			rows = splitRiffRows(rows, riffBars, len(sec.BarsPerLine) > 0, pickups)
		}
		if c.colW < contentWidth()-1 {
			rows = capBarRowsN(rows, 4, pickups)
		}
		for lineIdx, row := range rows {
			end := min(start+row.Bars, len(sec.Measures))
			showTS := firstStaff && lineIdx == 0
			rowPickup := 0
			if lineIdx == 0 {
				rowPickup = pickups
			}
			var rowRiffBars []*tab.Bar
			if riffBars != nil {
				rowRiffBars = riffBars[start:end]
			}
			c.staffBarRow(sec.Measures[start:end], row.Cap, rowPickup, rowRiffBars, tuning, bodySize, beatsPerBar, showTS, tempo, timeNum, timeDen)
			start = end
		}
		if len(sec.Lyrics) > 0 {
			// A beat more air between the grid's own closing baseline
			// and the lyric block right under it — without this they
			// visually fused into one run-on block (the grid's last
			// rule sat only 6pt above the first lyric line's own
			// baseline), blurring where the chart ends and the words
			// begin.
			c.y += bodySize * 0.35
		}
	}
	for _, ln := range sec.Lyrics {
		c.lyricLine(ln, bodySize)
	}
	if !hideNotes {
		for _, note := range sec.Notes {
			c.wrappedText(note, faceSansIt, bodySize*0.92, c.th.faint)
		}
	}
}
