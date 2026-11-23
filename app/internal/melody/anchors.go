package melody

import (
	"math"
	"strings"
	"unicode"

	"app/internal/chartalign"
)

// Anchoring the lyrics to the chart: a line's "[Chord]" markers say on
// which chord its words are sung, and the section's grid says in which
// bar (and on which beat) each chord is played, repeats included. Laying
// the one on the other gives, for each marker, a place in the section's
// played bars — the syllable after the marker is sung there. That's
// what holds each line to its bars; the voice only says which notes.

// ChordMark is a "[Chord]" in a lyric line: the chord, and the syllable
// of the line it falls on.
type ChordMark struct {
	Chord string
	Syl   int
}

// BarAnchor is a chord marker placed: its syllable, and where it is, in
// played bars from the section's first (2.5: halfway through its third).
type BarAnchor struct {
	Syl int
	Bar float64
}

// Segment is a run of a lyric line: the chord written before it ("" for
// none) and its text.
type Segment struct {
	Chord string
	Text  string
}

// MarksOf is a line's chord markers, each on the syllable its text
// starts in (a chord written mid-word, "lo[G]ve", on that syllable), and
// the line's text.
func MarksOf(segs []Segment, lang Lang) ([]ChordMark, string) {
	var raw strings.Builder
	type at struct {
		chord string
		off   int
	}
	var chords []at
	for _, s := range segs {
		if s.Chord != "" && strings.TrimSpace(s.Text) != "" {
			// Where its text's first letter is.
			off := raw.Len() + strings.IndexFunc(s.Text, unicode.IsLetter)
			if strings.IndexFunc(s.Text, unicode.IsLetter) < 0 {
				off = raw.Len()
			}
			chords = append(chords, at{s.Chord, off})
		}
		raw.WriteString(s.Text)
	}
	text := raw.String()
	syls := SplitLine(text, lang)
	// Each syllable's place in the text: found in order, by its letters.
	starts := make([]int, len(syls))
	cur := 0
	for i, sy := range syls {
		core := strings.TrimFunc(sy.Text, func(r rune) bool { return !unicode.IsLetter(r) })
		starts[i] = cur
		if core == "" {
			continue
		}
		if k := strings.Index(text[cur:], core); k >= 0 {
			starts[i] = cur + k
			cur += k + len(core)
		}
	}
	var marks []ChordMark
	for _, c := range chords {
		n := 0
		for i := range syls {
			if starts[i] <= c.off {
				n = i
			}
		}
		if len(marks) == 0 || n > marks[len(marks)-1].Syl {
			marks = append(marks, ChordMark{Chord: c.chord, Syl: n})
		}
	}
	return marks, strings.Join(strings.Fields(text), " ")
}

// chordEvent is a chord change in the section's played bars (and each
// bar's first beat, even when the chord carries on: a line may start on
// it).
type chordEvent struct {
	chord  chartalign.Chord
	ok     bool
	bar    float64
	change bool // the chord changes here (else a bar's first beat, the chord carrying on)
}

func chordEvents(bars [][]string) []chordEvent {
	var out []chordEvent
	prev := ""
	for b, beats := range bars {
		for j, c := range beats {
			if j > 0 && c == prev {
				continue
			}
			change := c != prev
			prev = c
			if c == "" || c == "N.C." {
				continue
			}
			ch, ok := chartalign.ParseSymbol(c)
			out = append(out, chordEvent{chord: ch, ok: ok, bar: float64(b) + float64(j)/float64(max(1, len(beats))), change: change})
		}
	}
	return out
}

// chordCost is how badly a written chord matches a played one: the same
// chord 0, the same root and family a little (a colour, a bass), the same
// root more; else no match.
func chordCost(a chartalign.Chord, b chordEvent) (float64, bool) {
	if !b.ok || a.NC || b.chord.NC {
		return 0, false
	}
	switch {
	case a.Root == b.chord.Root && a.PCs == b.chord.PCs && a.Bass == b.chord.Bass:
		return 0, true
	case a.Root == b.chord.Root && a.Class == b.chord.Class:
		return 0.2, true
	case a.Root == b.chord.Root:
		return 0.6, true
	}
	return 0, false
}

// AnchorLines places a section's lines' chord markers on its played bars
// (bars: a chord per beat, each played bar in order): an alignment of the
// markers, in order, with the bars' chord changes, in order — a marker on
// a change of its chord, changes with no marker skipped (instrumental
// bars, a chord not written over the words), a marker left unplaced when
// its chord isn't played where it could be. When the grid's chords come
// round again (a line's chord in several bars), what's sung decides:
// notes, when given (the onsets between two places, in bars from the
// section's first), are counted against the syllables between the two
// markers — the line sung between two places has as many notes as it has
// syllables, give or take; without them, the place the syllables' share
// of the section says. syls: each line's syllable count.
func AnchorLines(bars [][]string, lines [][]ChordMark, syls []int, notes func(from, to float64) int) [][]BarAnchor {
	type tok struct {
		line, syl int
		chord     chartalign.Chord
		ok        bool
		g         int // its syllable in the section's syllables
		expect    float64
	}
	total := 0
	for _, n := range syls {
		total += n
	}
	var toks []tok
	before := 0
	for li, ms := range lines {
		for _, m := range ms {
			c, ok := chartalign.ParseSymbol(m.Chord)
			e := 0.0
			if total > 0 {
				e = float64(len(bars)) * float64(before+m.Syl) / float64(total)
			}
			toks = append(toks, tok{li, m.Syl, c, ok, before + m.Syl, e})
		}
		if li < len(syls) {
			before += syls[li]
		}
	}
	evs := chordEvents(bars)
	T, E := len(toks), len(evs)
	out := make([][]BarAnchor, len(lines))
	if T == 0 || E == 0 {
		return out
	}
	const (
		skipEvent = 0.15 // a chord change no marker is on
		skipTok   = 1.5  // a marker left unplaced
		prior     = 0.06 // per bar from where the syllables' share puts it (no notes)
		fit       = 1.2  // per unit of |log| notes against syllables
		pickup    = 0.5  // bars before the section a pickup may start in
	)
	// span is how badly n notes fit s syllables.
	span := func(n, s int) float64 { return fit * math.Abs(math.Log(float64(n+1)/float64(s+1))) }
	match := func(i, j int) (float64, bool) {
		if !toks[i].ok {
			return 0, false
		}
		c, ok := chordCost(toks[i].chord, evs[j])
		if !ok {
			return 0, false
		}
		if !evs[j].change {
			c += 0.3 // a chord written where it changes, mostly
		}
		if notes == nil {
			c += prior * math.Abs(evs[j].bar-toks[i].expect)
		} else if notes(evs[j].bar, evs[j].bar+0.125) == 0 {
			c += 0.6 // nothing sung on its beat: its syllable isn't there
		}
		return c, true
	}
	// f[i][j]: token i on event j, the tokens before it placed or not.
	inf := math.Inf(1)
	f := make([][]float64, T)
	from := make([][][2]int, T)
	for i := range f {
		f[i] = make([]float64, E)
		from[i] = make([][2]int, E)
		for j := range f[i] {
			f[i][j] = inf
			mc, ok := match(i, j)
			if !ok {
				continue
			}
			// The first placed: every token before it unplaced, the
			// changes before it skipped, the notes before it (from a
			// pickup's room before the section) its syllables before.
			v := skipTok*float64(i) + skipEvent*float64(j) + mc
			if notes != nil {
				v += span(notes(-pickup, evs[j].bar), toks[i].g)
			}
			f[i][j], from[i][j] = v, [2]int{-1, -1}
			for i0 := 0; i0 < i; i0++ {
				for j0 := 0; j0 < j; j0++ {
					if math.IsInf(f[i0][j0], 1) {
						continue
					}
					v := f[i0][j0] + skipTok*float64(i-i0-1) + skipEvent*float64(j-j0-1) + mc
					if notes != nil {
						v += span(notes(evs[j0].bar, evs[j].bar), toks[i].g-toks[i0].g)
					}
					if v < f[i][j] {
						f[i][j], from[i][j] = v, [2]int{i0, j0}
					}
				}
			}
		}
	}
	// The last placed: the tokens after it unplaced, the notes after it
	// its syllables after.
	bi, bj, best := -1, -1, skipTok*float64(T)
	for i := range T {
		for j := range E {
			if math.IsInf(f[i][j], 1) {
				continue
			}
			v := f[i][j] + skipTok*float64(T-1-i) + skipEvent*float64(E-1-j)
			if notes != nil {
				v += span(notes(evs[j].bar, float64(len(bars))), total-toks[i].g)
			}
			if v < best {
				bi, bj, best = i, j, v
			}
		}
	}
	for i, j := bi, bj; i >= 0; {
		t := toks[i]
		out[t.line] = append([]BarAnchor{{Syl: t.syl, Bar: evs[j].bar}}, out[t.line]...)
		p := from[i][j]
		i, j = p[0], p[1]
	}
	return out
}
