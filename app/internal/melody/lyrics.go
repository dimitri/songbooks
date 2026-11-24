package melody

import (
	"math"
)

// The lyrics under the notes, without listening to the words. Each line
// is held to the bars the chart gives it (AnchorLines: its "[Chord]"
// markers on its section's played bars; a line without chords between
// its anchored neighbours, or spread over its section's bars when none
// has one): the voice only decides which notes, near there. Each chord's
// syllable goes on the first note sung at its beat (an eighth early is
// still on it: singers push); the syllables before a line's first chord
// (a pickup, "When the [G]night…") on the notes just before it; between
// two chords, one syllable a note. A line's last chord takes the notes of
// its phrase (phrases end at a rest of an eighth or more): extra notes
// hold the last syllable (a melisma), extra syllables share the last
// note, and a line never takes a note from the next line's place.

// Anchor is a syllable of a line placed in time by the chart.
type Anchor struct {
	Syl  int
	Time float64
}

// Line is one lyric line to place.
type Line struct {
	Key     string // "section:line", what an editor's correction is kept by
	Section int
	Text    string
	Syls    []Syl
	// Anchors: its chord markers placed on the bars (strong), in order.
	// Start: when the line is expected to start — its first anchor's
	// time, else interpolated between its neighbours' (weak).
	Anchors []Anchor
	Start   float64
}

// Span is a note's time in the recording.
type Span struct{ Start, End float64 }

// PlaceOptions are the song's time: an eighth note's and a bar's length
// at a time in the recording (they follow the sync); Strict: the sync is
// only an estimate, the chart's places are held to more tightly.
type PlaceOptions struct {
	Eighth, Bar func(t float64) float64
	Strict      bool
}

// LineFit is where a line was laid.
type LineFit struct {
	Key     string
	Section int
	// Matched: it found its notes, First..Last (after Shift, an
	// editor's correction); Anchored: by its chords on the bars (else by
	// where it was expected, between its neighbours); Anchors, Placed:
	// its chord anchors, and how many found their note.
	Matched         bool
	First, Last     int
	Shift           int
	Anchored        bool
	Anchors, Placed int
	// AnchorNote: the note its first chord's syllable is on (-1: none).
	AnchorNote int
	// Conf 0..1: how well its syllables fit those notes; under
	// LowLineConf the line is drawn lighter.
	Conf float64
}

// LowLineConf: a line placed less surely than this is marked.
const LowLineConf = 0.45

// NoteLyric is what a note sings: the syllables starting on it (more
// than one when the line has more syllables than notes), or Hold: it
// carries on the syllable before (a melisma). Line: the line's index, -1
// for none.
type NoteLyric struct {
	Syls []Syl
	Hold bool
	Line int
}

// Phrases groups notes into phrases at rests of an eighth or more:
// phrase[i] is note i's.
func Phrases(notes []Span, eighth func(float64) float64) []int {
	out := make([]int, len(notes))
	p := 0
	for i := range notes {
		if i > 0 && notes[i].Start-notes[i-1].End >= 0.9*eighth(notes[i-1].End) {
			p++
		}
		out[i] = p
	}
	return out
}

type placed struct{ syl, note int }

// PlaceLines lays lines on notes. shifts are editors' corrections, by
// line key: the notes a line's syllables start on, moved left (-) or
// right (+).
func PlaceLines(lines []Line, notes []Span, opt PlaceOptions, shifts map[string]int) ([]LineFit, []NoteLyric) {
	phrase := Phrases(notes, opt.Eighth)
	firstAt := func(t float64) int { // the first note starting at t or later
		lo, hi := 0, len(notes)
		for lo < hi {
			m := (lo + hi) / 2
			if notes[m].Start < t {
				lo = m + 1
			} else {
				hi = m
			}
		}
		return lo
	}
	fits := make([]LineFit, len(lines))
	// Each line's anchors resolved to notes, before the lines meet.
	res := make([][]placed, len(lines))
	for i, l := range lines {
		fits[i] = LineFit{Key: l.Key, Section: l.Section, First: -1, Last: -1, Anchors: len(l.Anchors), AnchorNote: -1}
		last := -1
		for _, a := range l.Anchors {
			e := opt.Eighth(a.Time)
			after := 4 * e // up to half a 4/4 bar late (a held note before it)
			if opt.Strict {
				after = 2 * e
			}
			// The note sung nearest its beat, from an eighth early (a
			// pushed syllable) to half a bar late.
			n := -1
			for k := firstAt(a.Time - e); k < len(notes) && notes[k].Start < a.Time+after; k++ {
				if n < 0 || math.Abs(notes[k].Start-a.Time) < math.Abs(notes[n].Start-a.Time) {
					n = k
				}
			}
			if n >= 0 && n > last && a.Syl < len(l.Syls) {
				res[i] = append(res[i], placed{a.Syl, n})
				last = n
			}
		}
		fits[i].Placed = len(res[i])
		fits[i].Anchored = len(res[i]) > 0
	}
	// A line's earliest note (its first anchor less the pickup notes it
	// may take): where the line before it must stop.
	earliest := func(i, after int) int {
		if len(res[i]) == 0 {
			return -1
		}
		a := res[i][0]
		first := a.note
		t := notes[a.note].Start - opt.Bar(notes[a.note].Start)
		for k := 0; k < a.syl && first-1 > after && notes[first-1].Start >= t; k++ {
			first--
		}
		return first
	}
	// Lines placed by their chords first; then the others, each on the
	// phrase starting nearest where it's expected — between the anchored
	// lines around it, never on a note they claim (a pickup included).
	for i, l := range lines {
		if fits[i].Anchored || math.IsNaN(l.Start) || len(l.Syls) == 0 {
			continue
		}
		lo, hi := -1, len(notes)
		for j := i - 1; j >= 0; j-- {
			if fits[j].Anchored {
				lo = res[j][len(res[j])-1].note
				break
			}
		}
		for j := i + 1; j < len(lines); j++ {
			if fits[j].Anchored {
				hi = earliest(j, lo)
				break
			}
		}
		slack := opt.Bar(l.Start)
		if opt.Strict {
			slack /= 2
		}
		best, bestD := -1, math.Inf(1)
		for n := max(lo+1, firstAt(l.Start-slack)); n < hi && notes[n].Start <= l.Start+slack; n++ {
			if n > 0 && phrase[n] == phrase[n-1] {
				continue
			}
			// Room for it: half its syllables' notes at least, short of the
			// next anchored line.
			room := 0
			for k := n; k < hi && room < len(l.Syls); k++ {
				room++
			}
			if 2*room < len(l.Syls) {
				continue
			}
			if d := math.Abs(notes[n].Start - l.Start); d < bestD {
				best, bestD = n, d
			}
		}
		if best >= 0 {
			res[i] = []placed{{0, best}}
		}
	}
	lyr := make([]NoteLyric, len(notes))
	for i := range lyr {
		lyr[i].Line = -1
	}
	prevEnd := -1
	for i, l := range lines {
		ps := res[i][:0:0]
		for _, p := range res[i] {
			if p.note > prevEnd {
				ps = append(ps, p)
			}
		}
		if len(ps) == 0 {
			continue
		}
		res[i] = ps
		first := earliest(i, prevEnd)
		// The next placed line's earliest note bounds this one.
		limit := len(notes)
		for j := i + 1; j < len(lines); j++ {
			if e := earliest(j, first); e > first {
				limit = e
				break
			}
		}
		// The last chord's syllables: the notes of its phrase, and the
		// next phrases while syllables are left, short of the limit.
		lastP := ps[len(ps)-1]
		need := len(l.Syls) - lastP.syl
		end := lastP.note
		next := math.NaN() // the next line's expected start
		for j := i + 1; j < len(lines) && math.IsNaN(next); j++ {
			next = lines[j].Start
		}
		for k := lastP.note + 1; k < limit; k++ {
			if phrase[k] != phrase[k-1] {
				// Another phrase: only for syllables still to sing, and
				// not into the next line's place or past a long silence.
				if k-lastP.note >= need || notes[k].Start-notes[k-1].End > 2*opt.Bar(notes[k-1].End) ||
					(!math.IsNaN(next) && notes[k].Start > next-opt.Eighth(next)) {
					break
				}
			}
			end = k
		}
		if fits[i].Anchored {
			fits[i].AnchorNote = ps[0].note
		}
		if sh := shifts[l.Key]; sh != 0 {
			// An editor's correction: the whole line from another note.
			f := min(end, max(prevEnd+1, first+sh))
			fits[i].Shift = f - first
			first = f
			ps = []placed{{0, first}}
		}
		fits[i].Matched, fits[i].First, fits[i].Last = true, first, end
		// Chunks: the pickup, then from each anchor to the next.
		var fit, chunks float64
		if ps[0].syl > 0 && ps[0].note > first {
			fit += spread(l.Syls[:ps[0].syl], first, ps[0].note-1, i, lyr)
			chunks++
		}
		for k, p := range ps {
			to, sylTo := end, len(l.Syls)
			if k+1 < len(ps) {
				to, sylTo = ps[k+1].note-1, ps[k+1].syl
			}
			s0 := p.syl
			if k == 0 && first == p.note {
				s0 = 0 // no note for the pickup: it shares the first one
			}
			fit += spread(l.Syls[s0:sylTo], p.note, to, i, lyr)
			chunks++
		}
		fit /= math.Max(1, chunks)
		c := fit
		if fits[i].Anchored {
			c *= 0.6 + 0.4*float64(fits[i].Placed)/float64(max(1, fits[i].Anchors))
		} else {
			c *= 0.65
		}
		if opt.Strict {
			c *= 0.85
		}
		fits[i].Conf = math.Round(c*100) / 100
		prevEnd = end
	}
	return fits, lyr
}

// spread puts syllables on notes first..last, one a note: extra notes
// hold the last syllable, extra syllables share the last note. It
// returns how well they fit (1 one a note; a melisma costs little).
func spread(syls []Syl, first, last, line int, lyr []NoteLyric) float64 {
	n := last - first + 1
	if n <= 0 {
		return 0
	}
	for k := range n {
		lyr[first+k].Line = line
	}
	if len(syls) == 0 {
		for k := range n {
			lyr[first+k].Hold = true
		}
		return 0.7
	}
	if n >= len(syls) {
		for k, s := range syls {
			lyr[first+k].Syls = []Syl{s}
		}
		for k := len(syls); k < n; k++ {
			lyr[first+k].Hold = true
		}
		return 1 - 0.3*math.Min(1, float64(n-len(syls))/float64(len(syls)+2))
	}
	for k := 0; k < n-1; k++ {
		lyr[first+k].Syls = []Syl{syls[k]}
	}
	lyr[last].Syls = append([]Syl(nil), syls[n-1:]...)
	return float64(n) / float64(len(syls))
}

// Interpolate fills the expected start of lines with none (NaN) between
// the nearest lines with one, by their syllables; before the first and
// after the last known, from the bounds given (a section's start and
// end).
func Interpolate(starts []float64, syls []int, from, to float64) []float64 {
	out := append([]float64(nil), starts...)
	n := len(out)
	known := func(i int) bool { return i >= 0 && i < n && !math.IsNaN(out[i]) }
	for i := 0; i < n; {
		if known(i) {
			i++
			continue
		}
		j := i
		for j < n && !known(j) {
			j++
		}
		// Lines i..j-1 between a (the line before i's start, or from) and
		// b (line j's start, or to), by syllables: line i-1's own take
		// its share first.
		a, b := from, to
		total := 0
		if i > 0 {
			a = out[i-1]
			total += syls[i-1]
		}
		if j < n {
			b = out[j]
		}
		for k := i; k < j; k++ {
			total += syls[k]
		}
		acc := 0
		if i > 0 {
			acc = syls[i-1]
		}
		for k := i; k < j; k++ {
			out[k] = a + (b-a)*float64(acc)/float64(max(1, total))
			acc += syls[k]
		}
		i = j
	}
	return out
}
