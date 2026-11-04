package song

import (
	"strconv"
	"strings"
)

// Write renders a Song back to .song source text. It regenerates canonical
// formatting (one field per line, grid before lyrics) rather than trying to
// reproduce a human author's exact whitespace — semantically equivalent to
// the input when the input was itself produced by Parse, which is what the
// round-trip tests check.
func Write(s *Song) string {
	var b strings.Builder

	for _, f := range s.Meta {
		b.WriteString(f.Key)
		b.WriteString(": ")
		b.WriteString(f.Value)
		b.WriteString("\n")
	}

	for _, sec := range s.Sections {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[")
		b.WriteString(sec.Label)
		b.WriteString("]")
		if sec.Repeat {
			b.WriteString(" (repeat)")
		}
		b.WriteString("\n")

		if sec.Time != "" {
			b.WriteString("time: " + sec.Time + "\n")
		}
		if sec.Score != "" {
			b.WriteString("score: " + sec.Score + "\n")
		}
		if len(sec.BarsPerLine) > 0 {
			b.WriteString("bars: " + writeBars(sec.BarsPerLine) + "\n")
		}
		if sec.Times > 0 {
			b.WriteString("times: " + strconv.Itoa(sec.Times) + "\n")
		}
		if len(sec.Measures) > 0 {
			b.WriteString("grid: " + writeGrid(sec.Measures) + "\n")
		}
		for _, n := range sec.Notes {
			b.WriteString("note: " + n + "\n")
		}
		for _, l := range sec.Lyrics {
			b.WriteString(writeLyric(l) + "\n")
		}
	}

	return b.String()
}

func writeBars(bars []int) string {
	parts := make([]string, len(bars))
	for i, n := range bars {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

func writeGrid(measures []Measure) string {
	parts := make([]string, len(measures))
	closes := make([]bool, len(measures))
	opens := make([]bool, len(measures))
	for i, m := range measures {
		var tok strings.Builder
		if m.Meter != "" {
			tok.WriteString("{" + m.Meter + "} ")
		}
		if m.Volta != "" {
			tok.WriteString("(" + m.Volta + ") ")
		}
		tok.WriteString(strings.Join(m.Tokens, " "))
		s := strings.TrimSpace(tok.String())
		if m.Open {
			if m.RepeatCount > 0 {
				s = ":" + strconv.Itoa(m.RepeatCount) + " " + s
			} else {
				s = ": " + s
			}
			opens[i] = true
		}
		if m.Close {
			s = s + " :"
			closes[i] = true
		}
		parts[i] = s
	}
	var b strings.Builder
	b.WriteByte('|')
	for i, p := range parts {
		if i > 0 {
			switch {
			// A closing bracket glues straight to the "|" that follows it
			// ("E :|"), and an opening one glues straight to the "|"
			// before it ("% |:3 Cadd9") — matching how a hand-written
			// chart (and the existing parser) writes a repeat bracket;
			// everywhere else measures get the usual " | " separator.
			case closes[i-1] && opens[i]:
				b.WriteString("|")
			case closes[i-1]:
				b.WriteString("| ")
			case opens[i]:
				b.WriteString(" |")
			default:
				b.WriteString(" | ")
			}
		}
		b.WriteString(p)
	}
	b.WriteByte('|')
	return b.String()
}

func writeLyric(l LyricLine) string {
	var b strings.Builder
	for _, seg := range l.Segments {
		if seg.HasChord {
			b.WriteString("[" + seg.Chord + "]")
		}
		b.WriteString(seg.Text)
	}
	return b.String()
}
