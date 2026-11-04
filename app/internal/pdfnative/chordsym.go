package pdfnative

import "app/internal/chordsym"

// chordsym.go: typographic chord-symbol rendering for the native PDF
// renderer, built directly on internal/chordsym.Split/DecomposeQuality —
// the SAME root/quality/bass split and major7/half-dim/dim/aug/minor
// classification the web Play view's chord rendering (internal/chordsym.
// Render, HTML/CSS) already uses, so a chord reads identically on both
// surfaces. This file supplies the PDF-specific DRAWING: real flat/sharp
// accidentals, a major-seventh triangle, bold root, a minor marker that
// hangs BELOW the baseline, other extensions riding ABOVE it and smaller.
//
// ♭/♯/△ are drawn as small hand-built vector glyphs, not the Unicode
// codepoints internal/chordsym emits for the web: neither embedded font
// here (Lato, EB Garamond) has U+266D/U+266F/U+25B3 in its cmap (checked
// with fontTools before writing this), so literal text would render as
// missing-glyph boxes in the shipped PDF. ø (half-diminished) and °
// (diminished) ARE present in both fonts, so those stay plain text.

// chordSym is one chord symbol, already split/classified via
// internal/chordsym, holding only what the PDF drawing code below needs.
// root/ext/bass are kept RAW (not glyph-substituted) — substitution into
// the drawn flat/sharp glyphs happens at draw time via
// drawRunWithAccidentals, not by pre-rewriting the string.
type chordSym struct {
	plain     string // non-chord token ("%", ".", "N.C.", or anything with no parseable root) — drawn as-is
	root      string
	marker    chordsym.Marker
	markerRaw string // MarkerMinor only: "m"/"min"/"mi"/"-" verbatim (never contains b/#)
	ext       string
	bass      string
}

func parseChordSym(raw string, jazz bool) chordSym {
	p := chordsym.Split(raw)
	if p.Root == "" {
		return chordSym{plain: p.Quality}
	}
	dq := chordsym.DecomposeQuality(p.Quality)
	if jazz {
		dq = chordsym.DecomposeQualityJazz(p.Quality)
	}
	return chordSym{root: p.Root, marker: dq.Marker, markerRaw: dq.MarkerRaw, ext: dq.Ext, bass: p.Bass}
}

// drawRunWithAccidentals draws (or, when c.draw is false, only measures)
// s in face/size at (x, y), substituting a hand-built flat/sharp glyph
// anywhere the token spells one as ASCII "b"/"#" — mirroring
// internal/chordsym's own glyphs() substitution rule, just emitting a
// drawn shape here instead of a Unicode codepoint. Returns the total
// width drawn.
func (c *canvas) drawRunWithAccidentals(x, y float64, s string, face string, size float64, col rgb) float64 {
	cx := x
	run := ""
	flush := func() {
		if run == "" {
			return
		}
		c.textAt(cx, y, run, face, size, col)
		cx += c.measureWidth(run, face, size)
		run = ""
	}
	for _, ch := range s {
		switch ch {
		case 'b':
			flush()
			cx += c.drawFlat(cx, y, size, col)
		case '#':
			flush()
			cx += c.drawSharp(cx, y, size, col)
		default:
			run += string(ch)
		}
	}
	flush()
	return cx - x
}

// drawFlat draws a small vector flat (♭) glyph: a vertical stem with a
// filled oval "bowl" hanging off its lower half. y is the text baseline
// it should sit on, matching drawRunWithAccidentals' surrounding text.
func (c *canvas) drawFlat(x, y, size float64, col rgb) float64 {
	w := size * 0.34
	if !c.draw {
		return w
	}
	stemX := x + w*0.22
	top, bottom := y-size*0.78, y+size*0.06
	c.line(stemX, top, stemX, bottom, col, size*0.09)
	r := size * 0.13
	cx, cy := stemX+r*0.85, bottom-r*0.7
	c.doc.SetFillColor(col.r, col.g, col.b)
	c.doc.Oval(cx-r, cy-r*0.8, cx+r, cy+r*0.8)
	return w
}

// drawSharp draws a small vector sharp (♯) glyph: two vertical strokes
// crossed by two short horizontal strokes, the classic tic-tac-toe shape.
func (c *canvas) drawSharp(x, y, size float64, col rgb) float64 {
	w := size * 0.5
	if !c.draw {
		return w
	}
	lw := size * 0.07
	x1, x2 := x+w*0.28, x+w*0.74
	top, bottom := y-size*0.8, y+size*0.08
	c.line(x1, top, x1, bottom, col, lw)
	c.line(x2, top, x2, bottom, col, lw)
	c.line(x, y-size*0.5, x+w, y-size*0.56, col, lw)
	c.line(x, y-size*0.16, x+w, y-size*0.22, col, lw)
	return w
}

const triangleGlyphW = 0.42 // width of the maj7 triangle, as a fraction of size

// drawTriangle draws the major-seventh triangle (△) as three strokes
// rather than a font glyph: EB Garamond has U+25B3 but Lato (the bold
// sans every other chord-symbol stroke uses) doesn't, and switching font
// family just for this one mark reads as visually inconsistent — drawing
// it keeps it in the same bold-sans weight as the rest of the symbol.
func (c *canvas) drawTriangle(x, y, size float64, col rgb) float64 {
	w := size * triangleGlyphW
	if !c.draw {
		return w
	}
	lw := size * 0.07
	topX, topY := x+w/2, y-size*0.62
	blX, blY := x, y-size*0.02
	brX, brY := x+w, y-size*0.02
	c.line(topX, topY, blX, blY, col, lw)
	c.line(blX, blY, brX, brY, col, lw)
	c.line(brX, brY, topX, topY, col, lw)
	return w
}

// drawChordSymbol draws (or, when c.draw is false, only measures) one
// chord symbol at (x, baselineY), per the typographic rules above.
// Returns the symbol's total width.
func (c *canvas) drawChordSymbol(x, baselineY float64, raw string, size float64, col rgb) float64 {
	return c.drawChordSymbolMode(x, baselineY, raw, size, col, false)
}

// drawChordSymbolMode is drawChordSymbol with jazz-mode selectable: jazz
// uses chordsym.DecomposeQualityJazz's "-" minor marker, drawn on the
// baseline at normal weight (matching the web Jazz chart's .csym-minor,
// never the engraved modes' subscript-below-baseline "m") — see
// RenderJazz's own doc comment in internal/chordsym for why jazz gets a
// different marker at all.
func (c *canvas) drawChordSymbolMode(x, baselineY float64, raw string, size float64, col rgb, jazz bool) float64 {
	cs := parseChordSym(raw, jazz)
	if cs.plain != "" {
		c.textAt(x, baselineY, cs.plain, faceSansBold, size, col)
		return c.measureWidth(cs.plain, faceSansBold, size)
	}

	extSize := size * 0.62
	cx := x
	cx += c.drawRunWithAccidentals(cx, baselineY, cs.root, faceSansBold, size, col)

	switch cs.marker {
	case chordsym.MarkerMajor7:
		cx += 1 + c.drawTriangle(cx+1, baselineY-size*0.34, extSize, col)
	case chordsym.MarkerHalfDim:
		cx += 1 + c.drawRunWithAccidentals(cx+1, baselineY-size*0.34, "ø", faceSansBold, extSize, col)
	case chordsym.MarkerDim:
		cx += 1 + c.drawRunWithAccidentals(cx+1, baselineY-size*0.34, "°", faceSansBold, extSize, col)
	case chordsym.MarkerAug:
		cx += 1 + c.drawRunWithAccidentals(cx+1, baselineY-size*0.34, "+", faceSansBold, extSize, col)
	case chordsym.MarkerMinor:
		if jazz {
			cx += 1 + c.drawRunWithAccidentals(cx+1, baselineY, cs.markerRaw, faceSansBold, size, col)
			break
		}
		mSize := size * 0.6
		c.textAt(cx+1, baselineY+size*0.14, cs.markerRaw, faceSansBold, mSize, col)
		cx += 1 + c.measureWidth(cs.markerRaw, faceSansBold, mSize)
	}

	if cs.ext != "" {
		cx += 1 + c.drawRunWithAccidentals(cx+1, baselineY-size*0.34, cs.ext, faceSansBold, extSize, col)
	}

	if cs.bass != "" {
		bSize := size * 0.9
		c.textAt(cx+2, baselineY, "/", faceSans, bSize, col)
		slashW := c.measureWidth("/", faceSans, bSize)
		cx += 2 + slashW + c.drawRunWithAccidentals(cx+2+slashW, baselineY, cs.bass, faceSans, bSize, col)
	}

	return cx - x
}
