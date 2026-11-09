package chorddiagram

import (
	"fmt"
	"strings"
)

// svg layout constants, in SVG user units — tuned to read clearly at the
// small size these diagrams render at in a chart's legend row (see the
// real songbook pages this matches: a compact box, name above, roughly
// 70-90px wide printed).
const (
	stringGap  = 16.0
	fretGap    = 18.0
	marginX    = 10.0
	topGap     = 20.0 // room for X/O markers and (if BaseFret==1) the nut
	bottomGap  = 10.0
	dotRadius  = 5.0
	barreWidth = 10.0
)

// RenderSVG renders d as a standalone SVG fretboard box: nut or fret
// number, string lines, fret lines, X/O markers, fretted dots, and barre
// bars. Caller supplies name as already-escaped SVG/HTML text content
// (e.g. a chordsym-rendered root letter or the plain symbol) if a label
// above the box is wanted; an empty name omits it.
func RenderSVG(d Diagram, name string) string {
	n := len(d.Strings)
	if n == 0 {
		return ""
	}
	gridW := float64(n-1) * stringGap
	gridH := float64(d.NumFrets) * fretGap
	width := gridW + 2*marginX
	labelH := 0.0
	if name != "" {
		labelH = 20.0
	}
	height := labelH + topGap + gridH + bottomGap

	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="chord-diagram" viewBox="0 0 %.1f %.1f" width="%.0f" height="%.0f" xmlns="http://www.w3.org/2000/svg">`, width, height, width, height)

	if name != "" {
		fmt.Fprintf(&b, `<text class="chord-diagram-name" x="%.1f" y="%.1f" text-anchor="middle">%s</text>`, width/2, labelH-4, name)
	}

	gx0 := marginX
	gy0 := labelH + topGap

	// nut (bold) if this is an open-position diagram, else a thin top
	// fret line plus the base-fret number printed to its left.
	if d.BaseFret <= 1 {
		fmt.Fprintf(&b, `<line class="chord-diagram-nut" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`, gx0, gy0, gx0+gridW, gy0)
	} else {
		fmt.Fprintf(&b, `<line class="chord-diagram-fretline" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`, gx0, gy0, gx0+gridW, gy0)
		fmt.Fprintf(&b, `<text class="chord-diagram-fretnum" x="%.1f" y="%.1f" text-anchor="end">%d</text>`, gx0-6, gy0+fretGap*0.5+4, d.BaseFret)
	}
	for row := 1; row <= d.NumFrets; row++ {
		y := gy0 + float64(row)*fretGap
		fmt.Fprintf(&b, `<line class="chord-diagram-fretline" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`, gx0, y, gx0+gridW, y)
	}
	for s := 0; s < n; s++ {
		x := gx0 + float64(s)*stringGap
		fmt.Fprintf(&b, `<line class="chord-diagram-string" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`, x, gy0, x, gy0+gridH)
	}

	// barres, drawn before the dots so an absorbed-into-the-barre string
	// (Row reset to 0 by ParseSpec) still sits visually under any extra
	// dot on a neighboring string at a different row.
	for _, br := range d.Barres {
		x1 := gx0 + float64(br.FromString)*stringGap
		x2 := gx0 + float64(br.ToString)*stringGap
		y := gy0 + (float64(br.Row)-0.5)*fretGap
		fmt.Fprintf(&b, `<line class="chord-diagram-barre" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke-width="%.1f" stroke-linecap="round"/>`, x1, y, x2, y, barreWidth)
	}

	for s, sp := range d.Strings {
		x := gx0 + float64(s)*stringGap
		switch sp.State {
		case Muted:
			d := 3.5
			y := labelH + topGap*0.5
			fmt.Fprintf(&b, `<g class="chord-diagram-muted" transform="translate(%.1f,%.1f)"><line x1="-%.1f" y1="-%.1f" x2="%.1f" y2="%.1f"/><line x1="-%.1f" y1="%.1f" x2="%.1f" y2="-%.1f"/></g>`, x, y, d, d, d, d, d, d, d, d)
		case Open:
			y := labelH + topGap*0.5
			fmt.Fprintf(&b, `<circle class="chord-diagram-open" cx="%.1f" cy="%.1f" r="4"/>`, x, y)
		case Fretted:
			if sp.Row > 0 {
				y := gy0 + (float64(sp.Row)-0.5)*fretGap
				fmt.Fprintf(&b, `<circle class="chord-diagram-dot" cx="%.1f" cy="%.1f" r="%.1f"/>`, x, y, dotRadius)
			}
		}
	}

	b.WriteString(`</svg>`)
	return b.String()
}
