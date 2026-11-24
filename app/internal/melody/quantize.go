package melody

import (
	"math"
	"strconv"
	"strings"

	"app/internal/audioanalysis"
)

// Meter is a bar's time signature.
type Meter struct{ Num, Den int }

// ParseMeter reads "6/8"; 4/4 for anything else.
func ParseMeter(s string) Meter {
	n, d, ok := strings.Cut(strings.TrimSpace(s), "/")
	if ok {
		num, e1 := strconv.Atoi(strings.TrimSpace(n))
		den, e2 := strconv.Atoi(strings.TrimSpace(d))
		if e1 == nil && e2 == nil && num > 0 && num <= 32 && (den == 2 || den == 4 || den == 8 || den == 16) {
			return Meter{num, den}
		}
	}
	return Meter{4, 4}
}

// Ticks is the bar's length in sixteenth notes, the unit everything is
// placed on.
func (m Meter) Ticks() int {
	if m.Num <= 0 || m.Den <= 0 {
		return 16
	}
	return max(1, m.Num*16/m.Den)
}

// Event is one notated note or rest of a bar.
type Event struct {
	Pos, Len int // sixteenths from the bar's start
	Midi     int // -1: a rest
	// TieIn: held from the event before (a tie over a bar line, or a
	// long note written as two); TieOut: held into the next.
	TieIn, TieOut bool
	// Note: the placed note it writes (Sheet.Notes), -1 for a rest.
	Note int
}

// Bar is one played bar of the sheet.
type Bar struct {
	Played int // the player's bar number
	Meter  Meter
	// Fine: written in sixteenths (the notes were too close for
	// eighths).
	Fine   bool
	Events []Event
}

// Placed is a note on the sheet: the sung note it comes from (an index
// into the notes quantized), its place in ticks from bar 0's start, its
// MIDI note (the recording's key) and whether it's an unsure one.
type Placed struct {
	Src        int
	Start, End int
	Midi       int
	Low        bool
	// Bar: the played bar it starts in.
	Bar int
}

// Sheet is the notes quantized on the grid.
type Sheet struct {
	Bars  []Bar
	Notes []Placed
	// Dropped: sung notes not placed (before bar 0 or past the grid,
	// or crowded out).
	Dropped int
}

// Options bound the sheet: its first and last played bars (inclusive;
// -1: from bar 0, to the last bar with a note).
type Options struct {
	FirstBar, LastBar int
}

// Quantize places notes on the grid, bar by bar: on eighths, or
// sixteenths in a bar whose notes are too close for eighths; a note
// longer than its bar tied into the next; the short breath between two
// notes (less than an eighth) given to the first, so the line reads
// legato. meterOf is each played bar's time signature.
func Quantize(notes []audioanalysis.SungNote, g Grid, meterOf func(bar int) Meter, opt Options) Sheet {
	var sh Sheet
	nb := g.Bars()
	if nb < 1 {
		sh.Dropped = len(notes)
		return sh
	}
	ticks := make([]int, nb+1) // ticks[b]: bar b's first tick
	for b := range nb {
		ticks[b+1] = ticks[b] + meterOf(b).Ticks()
	}
	// The fractional bar of each note's start and end; bars whose notes
	// collide on eighths are written in sixteenths.
	type pos struct {
		i      int
		bs, be float64
	}
	var ps []pos
	for i, n := range notes {
		bs := g.BarAt(n.Start)
		if math.IsNaN(bs) || bs < 0 || bs >= float64(nb) {
			sh.Dropped++
			continue
		}
		be := math.Min(g.BarAt(n.End), float64(nb))
		ps = append(ps, pos{i, bs, be})
	}
	fine := make([]bool, nb)
	snap := func(x float64, unit func(int) int) int {
		b := min(nb-1, max(0, int(math.Floor(x))))
		u := unit(b)
		S := ticks[b+1] - ticks[b]
		k := int(math.Round((x - float64(b)) * float64(S) / float64(u)))
		return ticks[b] + min(S, k*u)
	}
	coarse := func(int) int { return 2 }
	last := -1
	for _, p := range ps {
		t := snap(p.bs, coarse)
		if t == last {
			b := min(nb-1, int(math.Floor(p.bs)))
			fine[b] = true
			if b > 0 && math.Floor(p.bs) > p.bs-0.1 {
				fine[b-1] = true
			}
		}
		last = t
	}
	unit := func(b int) int {
		if fine[b] {
			return 1
		}
		return 2
	}
	for _, p := range ps {
		n := notes[p.i]
		s, e := snap(p.bs, unit), snap(p.be, unit)
		bar := min(nb-1, int(math.Floor(p.bs)))
		if s >= ticks[nb] {
			sh.Dropped++
			continue
		}
		e = max(e, s+unit(bar))
		if k := len(sh.Notes); k > 0 {
			prev := &sh.Notes[k-1]
			if s <= prev.Start {
				s = prev.Start + 1
				if s >= e {
					sh.Dropped++
					continue
				}
			}
			if prev.End > s || s-prev.End < 2 {
				prev.End = s
			}
		}
		e = min(e, ticks[nb])
		if e <= s {
			sh.Dropped++
			continue
		}
		sh.Notes = append(sh.Notes, Placed{Src: p.i, Start: s, End: e, Midi: int(math.Round(n.Pitch)), Low: n.Conf < audioanalysis.LowConf, Bar: barOf(ticks, s)})
	}
	first, lastBar := opt.FirstBar, opt.LastBar
	if first < 0 {
		first = 0
	}
	if lastBar < 0 {
		lastBar = 0
		if k := len(sh.Notes); k > 0 {
			lastBar = barOf(ticks, sh.Notes[k-1].End-1)
		}
	}
	lastBar = min(lastBar, nb-1)
	k := 0
	for b := first; b <= lastBar; b++ {
		bt, bt1 := ticks[b], ticks[b+1]
		bar := Bar{Played: b, Meter: meterOf(b), Fine: fine[b]}
		at := bt
		for k < len(sh.Notes) && sh.Notes[k].End <= bt {
			k++
		}
		for j := k; j < len(sh.Notes) && sh.Notes[j].Start < bt1; j++ {
			n := sh.Notes[j]
			s, e := max(n.Start, bt), min(n.End, bt1)
			if s > at {
				bar.Events = append(bar.Events, rests(at-bt, s-at, bt1-bt)...)
			}
			bar.Events = append(bar.Events, notePieces(s-bt, e-s, n.Midi, j, n.Start < bt, n.End > bt1)...)
			at = e
		}
		if at < bt1 {
			bar.Events = append(bar.Events, rests(at-bt, bt1-at, bt1-bt)...)
		}
		sh.Bars = append(sh.Bars, bar)
	}
	return sh
}

func barOf(ticks []int, t int) int {
	for b := 0; b+1 < len(ticks); b++ {
		if t < ticks[b+1] {
			return b
		}
	}
	return len(ticks) - 2
}

// notePieces writes a note of n ticks at pos as notated values, tied.
func notePieces(pos, n, midi, note int, tieIn, tieOut bool) []Event {
	var out []Event
	for n > 0 {
		d := noteValue(pos, n)
		out = append(out, Event{Pos: pos, Len: d, Midi: midi, Note: note, TieIn: tieIn || len(out) > 0, TieOut: true})
		pos += d
		n -= d
	}
	out[len(out)-1].TieOut = tieOut
	return out
}

// noteValue is the longest written value for a note at pos with n ticks
// left: a whole note on the bar, a dotted half or a half on a beat, a
// dotted quarter or a quarter on an eighth, and so on.
func noteValue(pos, n int) int {
	for _, c := range []struct{ d, align int }{{16, 16}, {12, 4}, {8, 4}, {6, 2}, {4, 2}, {3, 1}, {2, 1}, {1, 1}} {
		if c.d <= n && pos%c.align == 0 {
			return c.d
		}
	}
	return 1
}

// rests fills n ticks from pos with rests, each on its own value's
// boundary; a bar's whole length one rest when it can be written so.
func rests(pos, n, barLen int) []Event {
	var out []Event
	if pos == 0 && n == barLen && (n == 16 || n == 12 || n == 8 || n == 6 || n == 4) {
		return []Event{{Pos: 0, Len: n, Midi: -1, Note: -1}}
	}
	for n > 0 {
		d := 1
		for _, c := range []int{16, 8, 4, 2, 1} {
			if c <= n && pos%c == 0 {
				d = c
				break
			}
		}
		out = append(out, Event{Pos: pos, Len: d, Midi: -1, Note: -1})
		pos += d
		n -= d
	}
	return out
}
