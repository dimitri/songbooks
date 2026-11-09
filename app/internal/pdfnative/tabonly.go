package pdfnative

import (
	"strconv"

	"app/internal/song"
	"app/internal/tab"

	"github.com/signintech/gopdf"
)

// Engraving proportions below are taken from two public, non-code
// sources — not copied from either project's source text, since
// LilyPond is GPL (copying its C++/Scheme into this GPL-incompatible
// codebase isn't an option) and alphaTab's own MPL-2.0 file-level
// copyleft would require keeping any literally-copied file under MPL:
//
//   - Stem length ≈ 3.5 staff-spaces for an unbeamed stem, ~3.3-3.6 for
//     a beamed one (LilyPond's own default grob property values,
//     scm/define-grobs.scm's `lengths`/`beamed-lengths` — a numeric
//     constant, not copyrightable expression).
//   - Beam thickness ≈ 0.48-0.5 staff-space, inter-beam gap ≈ 0.25
//     staff-space — confirmed in BOTH LilyPond's own beam-thickness grob
//     property AND alphaTab's SMuFL engravingDefaults
//     (beamThickness/beamSpacing) — this is the shared, standard SMuFL
//     engraving-defaults convention, not either project's own invention.
//   - Horizontal note spacing follows Gourlay's logarithmic formula
//     (see internal/tab.SpacingWeights's own doc comment) instead of
//     plain linear duration weighting — confirmed as LilyPond's actual
//     spacing algorithm by reading lily/spacing-options.cc, re-derived
//     here from the published formula/paper reference it itself cites,
//     not from that GPL file's code.
//
// "Staff space" here is stringGap (the tab staff's own string-line
// spacing) — tab notation has no 5-line staff to anchor to, so the
// string spacing plays that role, same as the web chart's CSS already
// does (1.1em string gap, rhythm marks sized relative to it).
const (
	tabStringGapEm  = 0.55
	tabStemLenEm    = 3.3 // in stringGap units, not size — see doc comment above
	tabBeamThickEm  = 0.5 // in stringGap units
	tabBeamGapEm    = 0.75
	tabFlagWEm      = 1.1 // in stringGap units
	tabChordLabelEm = 1.4
	tabRestEm       = 0.4
)

// tabOnlyRow draws a display row whose bars ALL have "score:" riff data
// (the PDF counterpart of the web chart's viewerLine.TabOnly — see
// attachTabRiffs, internal/httpapi/handlers_pages.go) as a real tab
// staff REPLACING the normal 5-line staff/slash row, instead of
// drawTabBar's older fret-numbers-only overlay (still used for a row
// only PARTLY covered — see staffBarRow's own dispatch). Real stems,
// flags, and beam bars (tab.GroupBeams — the metric-pulse-aware grouping
// verified against the vendored alphaTab library this session), real
// engraving proportions and logarithmic spacing (see the const block
// above), and bar-by-bar chord-grid labels positioned independently of
// the riff's own beat timing (this can place every mid-bar chord change
// correctly, unlike the alphaTeX path the web chart hands to alphaTab,
// which only labels a bar's first beat).
func (c *canvas) tabOnlyRow(measures []song.Measure, riffBars []*tab.Bar, tuning []int, size float64, beatsPerBar int, firstStaff bool, tempo, timeNum, timeDen string, timeNumI, timeDenI int) {
	if len(measures) == 0 {
		return
	}
	n := len(tuning)
	stringGap := size * tabStringGapEm
	stemLen := stringGap * tabStemLenEm
	beamThick := stringGap * tabBeamThickEm
	beamGap := stringGap * tabBeamGapEm
	flagW := stringGap * tabFlagWEm

	hasHammerPull := false
	hasBend := false
	hasPalmMute := false
	for _, bar := range riffBars {
		if bar == nil {
			continue
		}
		for _, beat := range bar.Beats {
			if beat.HammerPull {
				hasHammerPull = true
			}
			if beat.BendSemitones > 0 {
				hasBend = true
			}
			if beat.PalmMute {
				hasPalmMute = true
			}
		}
	}
	hpH := 0.0
	if hasHammerPull || hasBend {
		hpH = size * 0.95
	}
	pmH := 0.0
	if hasPalmMute {
		pmH = size * 0.7
	}

	chordH := size * tabChordLabelEm
	gridH := float64(n) * stringGap
	rhythmH := stemLen + pmH
	ruleGap := size * 0.5
	rowH := chordH + hpH + gridH + rhythmH + ruleGap
	c.ensureSpace(rowH + 6)

	x0 := c.curX()
	tsW := 0.0
	if firstStaff {
		tsW = timeSigCellWidth(size)
	}
	barsX0 := x0 + tsW
	gridTop := c.y + chordH + hpH
	gridBottom := gridTop + gridH
	// stemBottom is the fixed depth every stem/flag/beam anchors to,
	// regardless of whether this row also reserves pmH below it for a
	// palm-mute dashed line — rhythmBottom (the row's FULL reserved
	// depth, stemBottom + pmH) must stay a strictly deeper, separate
	// band, or the beams and the "P.M." line end up drawn on top of
	// each other (a real bug found and fixed this session: both were
	// anchored to the same rhythmBottom, which grew by pmH and dragged
	// the beams down into the P.M. line's own space).
	stemBottom := gridBottom + stemLen
	rhythmBottom := stemBottom + pmH

	if firstStaff {
		c.drawTimeSigTempo(x0, gridTop, gridBottom, size, tempo, timeNum, timeDen)
	}

	for s := 1; s <= n; s++ {
		ly := gridTop + float64(s)*stringGap - stringGap
		c.line(x0, ly, x0+c.colW, ly, c.th.faint, 0.4)
	}
	c.line(barsX0, gridTop, barsX0, gridBottom, c.th.line, 0.7)
	c.line(x0+c.colW, gridTop, x0+c.colW, gridBottom, c.th.line, 0.7)

	nBars := float64(len(measures))
	boxW := (c.colW - tsW) / nBars
	pendingRepeatCount := 0

	for i, m := range measures {
		bx := barsX0 + float64(i)*boxW
		if i > 0 {
			c.line(bx, gridTop, bx, gridBottom, c.th.line, 0.7)
		}
		if m.Open {
			c.line(bx+2, gridTop, bx+2, gridBottom, c.th.ink, 1.8)
			c.repeatDots(bx+7, gridTop, gridH)
			if m.RepeatCount > 0 {
				pendingRepeatCount = m.RepeatCount
			}
		}
		if m.Close {
			c.line(bx+boxW-2, gridTop, bx+boxW-2, gridBottom, c.th.ink, 1.8)
			c.repeatDots(bx+boxW-9, gridTop, gridH)
			if pendingRepeatCount > 0 {
				c.textAt(bx+boxW-24, c.y+chordH-1, "x"+strconv.Itoa(pendingRepeatCount), faceSansBold, size*0.5, c.th.faint)
				pendingRepeatCount = 0
			}
		}

		// Chord-grid labels, positioned by the chord grid's OWN beat
		// slots (independent of the riff's own note timing) — same
		// per-slot placement staffBarRow's normal row uses.
		startX := bx + 3
		if m.Open {
			startX = bx + 14
		}
		slots := expandBeatSlots(m.Tokens, beatsPerBar)
		if len(slots) > 0 {
			slotW := (boxW - (startX - bx) - 3) / float64(len(slots))
			for si, sl := range slots {
				if !sl.tied {
					c.drawChordSymbolMode(startX+float64(si)*slotW, c.y+chordH, sl.label, size*0.95, c.th.chord, false)
				}
			}
		}

		if i >= len(riffBars) || riffBars[i] == nil {
			continue
		}
		bar := riffBars[i]

		weights := tab.SpacingWeights(bar.Beats)
		total := 0.0
		for _, w := range weights {
			total += w
		}
		if total <= 0 {
			total = 1
		}
		// Side margins reserve room for a flag hanging off the first or
		// last note's stem — without this, a flagged note placed right
		// at the bar's own edge draws its flag/beam-stub straddling the
		// barline, visually clipped by it (a real bug found and fixed
		// this session: a trailing sixteenth's flag was cut off by the
		// closing barline before this margin existed).
		margin := flagW * 0.75
		innerX0 := bx + margin
		innerW := boxW - 2*margin
		colX := make([]float64, len(bar.Beats)+1)
		acc := 0.0
		for bi, w := range weights {
			colX[bi] = innerX0 + innerW*acc/total
			acc += w
		}
		colX[len(bar.Beats)] = innerX0 + innerW

		beamOf := make([]int, len(bar.Beats)) // 1-based beam-group id, 0 = none
		beamGroups := tab.GroupBeams(bar.Beats, timeNumI, timeDenI)
		for gi, bg := range beamGroups {
			for k := bg.StartIdx; k <= bg.EndIdx; k++ {
				beamOf[k] = gi + 1
			}
		}

		for bi, beat := range bar.Beats {
			cx := (colX[bi] + colX[bi+1]) / 2
			if beat.Rest {
				c.circleOutline(cx, gridBottom+stringGap*0.9, stringGap*tabRestEm, 1.2, c.th.faint)
				continue
			}
			for _, note := range beat.Notes {
				if note.String < 1 || note.String > n {
					continue
				}
				ly := gridTop + float64(note.String)*stringGap
				txt := strconv.Itoa(note.Fret)
				tw := c.measureWidth(txt, faceSans, size*0.6)
				c.filledRect(cx-tw/2-1, ly-size*0.28, tw+2, size*0.56, c.th.bg)
				c.textAt(cx-tw/2, ly+size*0.2, txt, faceSans, size*0.6, c.th.ink)
			}
			// Slide-in (tab.Beat.HasSlideFrom — a real LilyPond
			// "\grace X( \glissando Y)" in the source, e.g. Mercy/
			// whiskey-beer-and-wine/intro.ly): a small fret number at
			// the origin fret, on the SAME string line as the main note
			// (a slide never changes string — only the fret does, which
			// in tab notation means no vertical movement at all, just a
			// horizontal glide along one string line), connected by a
			// short diagonal tick — the standard tab-chart convention
			// for this, confirmed against alphaTab's own "sl"/"gr" note
			// rendering this session.
			if beat.HasSlideFrom && len(beat.Notes) == 1 {
				note := beat.Notes[0]
				if note.String >= 1 && note.String <= n {
					ly := gridTop + float64(note.String)*stringGap
					fromTxt := strconv.Itoa(beat.SlideFromFret)
					fromSize := size * 0.42
					ftw := c.measureWidth(fromTxt, faceSans, fromSize)
					mainTw := c.measureWidth(strconv.Itoa(note.Fret), faceSans, size*0.6)
					fx := cx - flagW*0.9
					c.filledRect(fx-ftw/2-1, ly-size*0.22, ftw+2, size*0.44, c.th.bg)
					c.textAt(fx-ftw/2, ly+size*0.14, fromTxt, faceSans, fromSize, c.th.faint)
					c.line(fx+ftw/2+1, ly+size*0.08, cx-mainTw/2-2, ly-size*0.08, c.th.faint, 0.9)
				}
			}
			// Bend (tab.Beat.BendSemitones — this subset's own syntax
			// extension, see Beat.BendSemitones's own doc comment; no
			// real corpus example, added ahead of a planned Guitar Pro
			// importer): a small curved arrow rising from the fret
			// number with the bend amount labeled above it ("full",
			// "1/2", ...) — the standard tab-chart convention (matches
			// the bend arrows in real published tab, e.g. the reference
			// screenshot reviewed earlier this session).
			if beat.BendSemitones > 0 && len(beat.Notes) == 1 {
				note := beat.Notes[0]
				if note.String >= 1 && note.String <= n {
					ly := gridTop + float64(note.String)*stringGap
					label := tab.BendLabel(beat.BendSemitones, beat.BendRelease)
					lsize := size * 0.4
					lw := c.measureWidth(label, faceSansIt, lsize)
					c.textAt(cx-lw/2, ly-size*0.95, label, faceSansIt, lsize, c.th.faint)
					arrowTopY := ly - size*0.42
					c.line(cx, ly-size*0.3, cx, arrowTopY, c.th.faint, 0.9)
					c.line(cx-size*0.08, arrowTopY+size*0.12, cx, arrowTopY, c.th.faint, 0.9)
					c.line(cx+size*0.08, arrowTopY+size*0.12, cx, arrowTopY, c.th.faint, 0.9)
				}
			}
			flags, dotted := tab.DurationGlyph(beat.Duration)
			if flags == 0 {
				continue // quarter note or longer: real engraving draws no stem in tab notation at all once there's no flag to carry
			}
			c.line(cx, gridBottom, cx, stemBottom, c.th.faint, 1.0)
			if dotted {
				c.filledCircle(cx+flagW*0.18, stemBottom-stringGap*0.3, stringGap*0.09, c.th.faint)
			}
			if beamOf[bi] == 0 {
				// Unbeamed: individual hooked flag(s), one per halving —
				// a filled wedge curving away from the stem, hooking
				// back toward the staff. Real engraving (and alphaTab's
				// own flag glyphs) stacks multiple flags tightly
				// together right at the stem's tip, not spread along
				// its length — same anchor depth beamGroups below uses.
				for lvl := 0; lvl < flags; lvl++ {
					fy := stemBottom - beamThick*0.5 - float64(lvl)*beamGap
					c.filledFlag(cx, fy, flagW, c.th.faint)
				}
			}
		}

		// Real per-level beam lines (tab.ComputeBeamSegments), not
		// GroupBeams.MinFlags' single uniform beam count for the whole
		// group — a mixed-duration group (an eighth beamed with a
		// sixteenth) needs the primary beam across both notes but the
		// secondary beam only as a short partial stub on the sixteenth's
		// side, standard notation practice this app's own hand-rolled
		// web CSS renderer doesn't do either (flagged as a follow-up
		// there — see that template's own Levels-wide-rect comment).
		// Representative single-column width for a partial-beam stub's
		// fallback length (colX spacing varies per beat under log
		// spacing — the bar's own first gap is close enough at this
		// notation's small physical scale).
		partialLen := colX[1] - colX[0]
		for _, bg := range beamGroups {
			for _, seg := range tab.ComputeBeamSegments(bar.Beats, bg) {
				by := stemBottom - beamThick*0.5 - float64(seg.Level)*beamGap
				if seg.StartIdx != seg.EndIdx {
					x1 := (colX[seg.StartIdx] + colX[seg.StartIdx+1]) / 2
					x2 := (colX[seg.EndIdx] + colX[seg.EndIdx+1]) / 2
					c.line(x1, by, x2, by, c.th.faint, beamThick)
					continue
				}
				cx := (colX[seg.StartIdx] + colX[seg.StartIdx+1]) / 2
				stub := (colX[seg.StartIdx+1] - colX[seg.StartIdx]) * 0.35
				if stub <= 0 {
					stub = partialLen * 0.35
				}
				if seg.PartialToward < 0 {
					c.line(cx-stub, by, cx, by, c.th.faint, beamThick)
				} else {
					c.line(cx, by, cx+stub, by, c.th.faint, beamThick)
				}
			}
		}

		// Hammer-on/pull-off slurs (tab.Beat.HammerPull — a LilyPond
		// slur on a guitar TabStaff, e.g. Mercy/always-free/verse.ly's
		// "g,( a,)"): a shallow arc from this beat to the next sounding
		// one, labeled "H" (ascending pitch) or "P" (descending),
		// matching real tab-chart convention and the vendored alphaTab
		// library's own rendering of the same alphaTeX {h} property
		// (see internal/tab.ToAlphaTex) — alphaTab draws it the same
		// way: an arc above the tab staff with an H/P letter, direction
		// derived from comparing the two notes' pitch, not stored
		// separately.
		if hasHammerPull {
			for bi, beat := range bar.Beats {
				if !beat.HammerPull || len(beat.Notes) == 0 {
					continue
				}
				nj := -1
				for j := bi + 1; j < len(bar.Beats); j++ {
					if !bar.Beats[j].Rest && len(bar.Beats[j].Notes) > 0 {
						nj = j
						break
					}
				}
				if nj < 0 {
					continue
				}
				x1 := (colX[bi] + colX[bi+1]) / 2
				x2 := (colX[nj] + colX[nj+1]) / 2
				label := "H"
				if bar.Beats[nj].Notes[0].MIDI < beat.Notes[0].MIDI {
					label = "P"
				}
				c.hammerPullArc(x1, x2, gridTop-hpH, hpH, label, size)
			}
		}

		// Palm mute (tab.Beat.PalmMute — this subset's own syntax
		// extension, no real corpus example, added ahead of a planned
		// Guitar Pro importer): "P.M." plus a dashed line under each
		// contiguous run of muted beats, the standard tab-chart
		// convention — one label per run, not one per note, since a
		// real chart never repeats "P.M." note by note.
		i2 := 0
		for i2 < len(bar.Beats) {
			if !bar.Beats[i2].PalmMute {
				i2++
				continue
			}
			j2 := i2
			for j2+1 < len(bar.Beats) && bar.Beats[j2+1].PalmMute {
				j2++
			}
			x1 := colX[i2]
			x2 := colX[j2+1]
			y := rhythmBottom - pmH*0.35
			label := "P.M."
			lsize := size * 0.42
			c.textAt(x1, y+lsize*0.3, label, faceSansIt, lsize, c.th.faint)
			lineStartX := x1 + c.measureWidth(label, faceSansIt, lsize) + stringGap*0.3
			if lineStartX < x2 {
				c.dashedLine(lineStartX, x2, y, c.th.faint, 0.8)
			}
			i2 = j2 + 1
		}
	}
	c.y = rhythmBottom + ruleGap
}

// hammerPullArc draws one hammer-on/pull-off marking: label ("H" or "P")
// centered above the arc, a shallow Bezier arc connecting x1 to x2,
// sitting within the hpH-tall band directly above the tab staff
// (top..top+hpH).
func (c *canvas) hammerPullArc(x1, x2, top, hpH float64, label string, size float64) {
	if !c.draw {
		return
	}
	labelSize := size * 0.68
	lw := c.measureWidth(label, faceSansBold, labelSize)
	cx := (x1 + x2) / 2
	c.textAt(cx-lw/2, top+labelSize*0.85, label, faceSansBold, labelSize, c.th.faint)
	y := top + hpH*0.78
	ctrlY := y - hpH*0.4
	c.doc.SetStrokeColor(c.th.faint.r, c.th.faint.g, c.th.faint.b)
	c.doc.SetLineWidth(0.8)
	c.doc.Curve(x1, y, x1+(x2-x1)*0.25, ctrlY, x1+(x2-x1)*0.75, ctrlY, x2, y, "D")
}

// filledFlag draws one eighth-note-style flag as a filled pennant
// hanging off a stem at (stemX, y) — y is where this flag's hook starts
// on the stem, curving down-right over a w-wide, w*0.9-tall wedge.
// Approximated as a 4-point filled polygon (gopdf has no bezier fill
// primitive — see filledDot's own comment on the same constraint),
// close enough at chart print size to read as a real flag rather than a
// bare line stroke.
func (c *canvas) filledFlag(stemX, y, w float64, col rgb) {
	if !c.draw {
		return
	}
	h := w * 0.9
	pts := []gopdf.Point{
		{X: stemX, Y: y},
		{X: stemX + w*0.6, Y: y + h*0.2},
		{X: stemX + w*0.85, Y: y + h},
		{X: stemX, Y: y + h*0.55},
	}
	c.doc.Polygon(pts, "F")
}

// dashedLine draws a horizontal dashed line from x1 to x2 at height y —
// the real tab-chart convention for a palm-mute span ("P.M. - - - - -").
// gopdf has no native dash pattern, so this draws the dashes as
// individual short segments.
func (c *canvas) dashedLine(x1, y, x2 float64, col rgb, width float64) {
	const dash, gap = 3.0, 2.5
	for x := x1; x < x2; x += dash + gap {
		end := x + dash
		if end > x2 {
			end = x2
		}
		c.line(x, y, end, y, col, width)
	}
}
