package pdfnative

import (
	"strings"

	"github.com/signintech/gopdf"
)

// canvas is a small stateful cursor over a gopdf document: it tracks the
// current column (song pages flow two-up, like the Typst pipeline it
// replaces) and Y position, and can run in two modes over the identical
// layout logic:
//
//   - draw=true:  actually emits text/rects/lines onto doc.
//   - draw=false: advances y exactly as drawing would, without emitting
//     anything — used by fitSongToPage to measure whether a candidate
//     font size fits before committing to drawing it for real.
//
// Keeping ONE layout function serving both modes (rather than a separate
// "measure" implementation) is deliberate: two implementations drift.
type canvas struct {
	doc  *gopdf.GoPdf
	draw bool
	th   theme

	cols   int // 1 or 2
	colIdx int
	colX   []float64
	colW   float64
	top    float64
	bottom float64
	y      float64

	// overflowed is set once content needed a column/page beyond what
	// cols provides — fitSongToPage reads this to decide a smaller size
	// is needed (or, at the floor size, that a second page is unavoidable).
	overflowed bool
	pages      int // pages actually used (1, or 2 if overflow was allowed)
	allowPage2 bool
}

func newCanvas(doc *gopdf.GoPdf, draw bool, th theme, cols int, allowPage2 bool) *canvas {
	c := &canvas{doc: doc, draw: draw, th: th, cols: cols, top: marginY, bottom: contentBottom(), allowPage2: allowPage2, pages: 1}
	gutter := 18.0
	full := contentWidth()
	if cols == 2 {
		c.colW = (full - gutter) / 2
		c.colX = []float64{marginX, marginX + c.colW + gutter}
	} else {
		c.colW = full
		c.colX = []float64{marginX}
	}
	c.y = c.top
	return c
}

func (c *canvas) curX() float64 { return c.colX[c.colIdx] }

// ensureSpace advances to the next column (or a new page, if allowed) when
// h more points won't fit in the current column.
func (c *canvas) ensureSpace(h float64) {
	if c.y+h <= c.bottom {
		return
	}
	if c.colIdx+1 < c.cols {
		c.colIdx++
		c.y = c.top
		return
	}
	// Out of columns on this page.
	if !c.allowPage2 {
		c.overflowed = true
		return // caller keeps drawing past the margin at measurement time;
		// at draw time this only happens once we've already committed to
		// the floor size, so a slight page-bottom overrun is preferable
		// to silently dropping content.
	}
	if c.draw {
		c.doc.AddPage()
		paintPageBackground(c.doc, c.th)
	}
	c.pages++
	c.colIdx = 0
	c.y = c.top
}

func paintPageBackground(doc *gopdf.GoPdf, th theme) {
	if th == darkTheme {
		doc.SetFillColor(th.bg.r, th.bg.g, th.bg.b)
		doc.RectFromUpperLeftWithStyle(0, 0, pageW, pageH, "F")
	}
}

func (c *canvas) setFont(face string, size float64) {
	c.doc.SetFont(face, "", size)
}

// text draws (or measures) a single line of text at the current cursor,
// left-aligned, and returns its width.
func (c *canvas) text(s string, face string, size float64, col rgb) float64 {
	c.setFont(face, size)
	w, _ := c.doc.MeasureTextWidth(s)
	if c.draw {
		c.doc.SetTextColor(col.r, col.g, col.b)
		c.doc.SetXY(c.curX(), c.y)
		c.doc.Text(s)
	}
	return w
}

// textAt draws text at an explicit x (still y = c.y), used for chord
// symbols placed above a specific word's start.
func (c *canvas) textAt(x, y float64, s, face string, size float64, col rgb) {
	if !c.draw {
		return
	}
	c.setFont(face, size)
	c.doc.SetTextColor(col.r, col.g, col.b)
	c.doc.SetXY(x, y)
	c.doc.Text(s)
}

func (c *canvas) line(x1, y1, x2, y2 float64, col rgb, width float64) {
	if !c.draw {
		return
	}
	c.doc.SetStrokeColor(col.r, col.g, col.b)
	c.doc.SetLineWidth(width)
	c.doc.Line(x1, y1, x2, y2)
}

func (c *canvas) filledRect(x, y, w, h float64, col rgb) {
	if !c.draw {
		return
	}
	c.doc.SetFillColor(col.r, col.g, col.b)
	c.doc.RectFromUpperLeftWithStyle(x, y, w, h, "F")
}

func (c *canvas) strokedRect(x, y, w, h float64, col rgb, lw float64) {
	if !c.draw {
		return
	}
	c.doc.SetStrokeColor(col.r, col.g, col.b)
	c.doc.SetLineWidth(lw)
	c.doc.RectFromUpperLeftWithStyle(x, y, w, h, "D")
}

// measureWidth measures s in face/size without moving the cursor or
// drawing — needed to decide word-wrap breaks and chord-over-word x
// offsets regardless of draw mode.
func (c *canvas) measureWidth(s, face string, size float64) float64 {
	c.setFont(face, size)
	w, _ := c.doc.MeasureTextWidth(s)
	return w
}

// sectionTag draws the grey rounded-rect "VERSE"/"CHORUS"/"INTRO" label
// (v2/lib/sheet.typ's chip) and advances y past it.
func (c *canvas) sectionTag(label string, size float64) {
	label = strings.ToUpper(label)
	c.setFont(faceSansBold, size)
	w, _ := c.doc.MeasureTextWidth(label)
	padX, h := 7.0, size+7
	c.ensureSpace(h + 6)
	c.filledRect(c.curX(), c.y, w+2*padX, h, c.th.chip)
	if c.draw {
		c.doc.SetTextColor(c.th.chipInk.r, c.th.chipInk.g, c.th.chipInk.b)
		c.doc.SetXY(c.curX()+padX, c.y+h-size*0.72)
		c.doc.Text(label)
	}
	c.y += h + 6
}
