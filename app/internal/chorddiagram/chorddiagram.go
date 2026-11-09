// Package chorddiagram parses and renders fretboard chord-box diagrams —
// the small grid-with-dots graphic printed above a chart (see any real
// songbook: a row of these under the title, one per chord used), as
// opposed to chordsym's inline "Cm"/"D9/F#" text symbols used in the
// grid/lyrics.
//
// The diagram GRID notation itself (fret number + a per-string column of
// X/O/digit, parens for a barre) is kept byte-compatible with the legacy
// LaTeX "songs" package's \gtab second argument — real examples in this
// repo's legacy corpus (Mercy/gone/gone.tex: "\gtab{Cm}{8:XX3(111)}",
// "\gtab{G}{7:XX(3121)}", "\gtab{A&}{8:X4312X}", "\gtab{E&}{8:X3(111)X}")
// are valid Spec strings here unchanged — there is no reason to invent a
// second grid encoding for the same shape. What's NOT carried over is
// \gtab's first argument: songs.sty treats a chord's name as an opaque
// string with no connection to how the rest of the chart spells chords.
// This app already has one normalized chord-name grammar (chordsym,
// shared with grid:/lyrics), so a diagram's Name is a plain chordsym
// symbol ("Cm", "Ab", "D9/F#"), not a second naming scheme.
package chorddiagram

import (
	"strconv"
	"strings"
)

// StringState is what one string of the diagram shows above/within the grid.
type StringState int

const (
	Fretted StringState = iota // a dot at Row (and/or part of a Barre)
	Muted                      // "X" above the nut
	Open                       // "O" above the nut
)

// StringPos is one string's state. Row is only meaningful when State ==
// Fretted: the 1-based row within the diagram (actual fret = BaseFret +
// Row - 1).
type StringPos struct {
	State StringState
	Row   int
}

// Barre is a finger laid across several adjacent strings at the same
// row — drawn as a bar instead of separate dots. FromString/ToString are
// 0-based indices into Diagram.Strings (inclusive), always FromString <
// ToString since a single string is never its own barre.
type Barre struct {
	Row                  int
	FromString, ToString int
}

// Diagram is one parsed chord box.
type Diagram struct {
	Name string // a chordsym symbol, e.g. "Cm", "Am/G", "D9/F#"

	// BaseFret is the real fret number row 1 of the grid represents. 1
	// means "open position" (no fret number printed to the left of the
	// box, matching \gtab with no leading "N:").
	BaseFret int

	Strings []StringPos // left to right = lowest-pitched string to highest, e.g. low E..high e for a 6-string guitar
	Barres  []Barre

	// NumFrets is how many rows the grid should draw — at least 4 (the
	// legacy package's own \minfrets default), or the highest row any
	// string actually uses, whichever is larger.
	NumFrets int
}

// ParseSpec parses one Spec string: "[BaseFret:]StringSpec", where
// StringSpec is a run of per-string tokens — "X" (muted), "O" or "0"
// (open), a digit 1-9 (a fretted dot at that row), optionally with a
// contiguous run of digits wrapped in "(" ")" marking a barre. The barre
// is drawn at the LOWEST digit inside the parens (the fret the index
// finger actually lies across); any other digit inside the parens that
// differs from that row is still drawn as its own dot layered on top —
// matching how a real songbook shows a barre shape with extra fretted
// notes above it (e.g. \gtab{G}{7:XX(3121)}: a full barre at row 1
// across 4 strings, plus separate dots at rows 3 and 2 on two of those
// same strings).
//
// This is a faithful, readable re-reading of \gtab's grid shape, not a
// re-implementation of songs.sty's own TeX macro bit-for-bit (that macro
// drives its barre placement through loop-counter side effects not
// fully recoverable by inspection alone) — every real diagram in this
// repo's corpus round-trips to the same visual shape either way.
func ParseSpec(spec string) Diagram {
	spec = strings.TrimSpace(spec)
	baseFret := 1
	body := spec
	if i := strings.IndexByte(spec, ':'); i >= 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(spec[:i])); err == nil {
			baseFret = n
		}
		body = spec[i+1:]
	}

	var d Diagram
	d.BaseFret = baseFret

	inBarre := false
	barreStart := -1
	barreRows := []int{}
	maxRow := 0

	closeBarre := func(endIdx int) {
		if !inBarre || barreStart < 0 || endIdx <= barreStart {
			inBarre = false
			barreStart = -1
			barreRows = nil
			return
		}
		row := barreRows[0]
		for _, r := range barreRows {
			if r < row {
				row = r
			}
		}
		d.Barres = append(d.Barres, Barre{Row: row, FromString: barreStart, ToString: endIdx})
		// strings exactly at the barre's own row need no separate dot —
		// the bar already marks them.
		for i := barreStart; i <= endIdx; i++ {
			if d.Strings[i].Row == row {
				d.Strings[i].Row = 0
			}
		}
		if row > maxRow {
			maxRow = row
		}
		inBarre = false
		barreStart = -1
		barreRows = nil
	}

	for _, r := range body {
		switch {
		case r == '(':
			inBarre = true
			barreStart = len(d.Strings)
			barreRows = barreRows[:0]
		case r == ')':
			closeBarre(len(d.Strings) - 1)
		case r == 'X' || r == 'x':
			d.Strings = append(d.Strings, StringPos{State: Muted})
		case r == 'O' || r == 'o' || r == '0':
			d.Strings = append(d.Strings, StringPos{State: Open})
		case r >= '1' && r <= '9':
			row := int(r - '0')
			d.Strings = append(d.Strings, StringPos{State: Fretted, Row: row})
			if row > maxRow {
				maxRow = row
			}
			if inBarre {
				barreRows = append(barreRows, row)
			}
		default:
			// whitespace or anything unrecognized — ignored rather than
			// erroring, same liberal-input policy as chordsym.
		}
	}
	if inBarre {
		closeBarre(len(d.Strings) - 1)
	}

	d.NumFrets = 4
	if maxRow > d.NumFrets {
		d.NumFrets = maxRow
	}
	return d
}
