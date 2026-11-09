package pdfnative

import (
	"strconv"

	"app/internal/chorddiagram"
	"app/internal/song"
)

// chordLegendMetrics mirrors internal/chorddiagram's web SVG layout
// constants (see RenderSVG) — a point-based, not pixel-based, version of
// the same box: string lines, fret lines, X/O markers, dots, barre bars,
// a name above and (if not open position) a fret number to the left.
// Kept visually equivalent to the web legend row rather than pixel-
// identical — gopdf has no SVG import, so this is a direct re-drawing
// using this package's own canvas primitives.
const (
	clStringGap = 8.0
	clFretGap   = 9.0
	clTopGap    = 10.0 // room for X/O markers above the nut
	clGap       = 18.0 // horizontal gap between diagrams in the row
	clRowGap    = 10.0 // vertical gap between wrapped rows
)

// drawChordLegend draws refs as a wrapping row of fretboard diagrams
// spanning the full page width (same pre-column placement as the title/
// rule above it — see renderSongHeader), advancing c.y past it. A no-op
// when refs is empty.
func (c *canvas) drawChordLegend(refs []song.ChordRef, nameSize float64) {
	if len(refs) == 0 {
		return
	}
	availW := pageW - 2*marginX
	rowH := 0.0
	x := marginX
	first := true
	for _, ref := range refs {
		d := chorddiagram.ParseSpec(ref.Spec)
		n := len(d.Strings)
		if n == 0 {
			continue
		}
		gridW := float64(n-1) * clStringGap
		gridH := float64(d.NumFrets) * clFretGap
		boxW := gridW + 10 // small side margin so fret numbers/labels don't clip into the next diagram
		labelH := nameSize * 1.3
		boxH := labelH + clTopGap + gridH + 6

		if !first && x+boxW > marginX+availW {
			x = marginX
			c.y += rowH + clRowGap
			rowH = 0
		}
		first = false
		if boxH > rowH {
			rowH = boxH
		}
		c.ensureSpace(rowH + clRowGap)
		c.drawOneChordDiagram(d, ref.Name, x, c.y, nameSize)
		x += boxW + clGap
	}
	c.y += rowH + clRowGap
}

// drawOneChordDiagram draws one Diagram at (x, top) — top is the box's
// own top edge (above the name label), same convention as the rest of
// this package's draw calls taking an explicit top.
func (c *canvas) drawOneChordDiagram(d chorddiagram.Diagram, name string, x, top, nameSize float64) {
	n := len(d.Strings)
	gridW := float64(n-1) * clStringGap
	gridH := float64(d.NumFrets) * clFretGap
	labelH := nameSize * 1.3

	nameW := c.measureWidth(name, faceSansBold, nameSize)
	c.textAt(x+(gridW-nameW)/2, top+nameSize*0.9, name, faceSansBold, nameSize, c.th.ink)

	gx0 := x
	gy0 := top + labelH + clTopGap

	if d.BaseFret <= 1 {
		c.line(gx0, gy0, gx0+gridW, gy0, c.th.ink, 1.6)
	} else {
		c.line(gx0, gy0, gx0+gridW, gy0, c.th.faint, 0.6)
		fretTxt := strconv.Itoa(d.BaseFret)
		fw := c.measureWidth(fretTxt, faceSans, nameSize*0.65)
		c.textAt(gx0-fw-3, gy0+clFretGap*0.5+nameSize*0.22, fretTxt, faceSans, nameSize*0.65, c.th.faint)
	}
	for row := 1; row <= d.NumFrets; row++ {
		y := gy0 + float64(row)*clFretGap
		c.line(gx0, y, gx0+gridW, y, c.th.faint, 0.6)
	}
	for s := 0; s < n; s++ {
		sx := gx0 + float64(s)*clStringGap
		c.line(sx, gy0, sx, gy0+gridH, c.th.faint, 0.6)
	}

	for _, br := range d.Barres {
		x1 := gx0 + float64(br.FromString)*clStringGap
		x2 := gx0 + float64(br.ToString)*clStringGap
		y := gy0 + (float64(br.Row)-0.5)*clFretGap
		c.line(x1, y, x2, y, c.th.ink, 4.5)
	}

	for s, sp := range d.Strings {
		sx := gx0 + float64(s)*clStringGap
		switch sp.State {
		case chorddiagram.Muted:
			r := 2.2
			cy := top + labelH + clTopGap*0.5
			c.line(sx-r, cy-r, sx+r, cy+r, c.th.ink, 0.8)
			c.line(sx-r, cy+r, sx+r, cy-r, c.th.ink, 0.8)
		case chorddiagram.Open:
			cy := top + labelH + clTopGap*0.5
			c.circleOutline(sx, cy, 2.6, 0.8, c.th.ink)
		case chorddiagram.Fretted:
			if sp.Row > 0 {
				y := gy0 + (float64(sp.Row)-0.5)*clFretGap
				c.filledCircle(sx, y, 2.8, c.th.ink)
			}
		}
	}
}
