package melody

import (
	"math"
)

// The lyrics under the notes, without listening to the words: the song's
// lines, in order, laid on the voice's phrases, in order — a dynamic
// programme over (lines × phrases) whose costs are what can be counted:
// a line's syllables against a phrase's notes, how fast that would be
// sung, and whether the phrase falls in the line's section as the sync
// places it. A line may take two or three phrases (a breath mid-line), a
// phrase may be left out (an ad-lib, a backing vocal the split kept), a
// line may find none (a phrase the voice tracker missed). Each line's
// syllables then go on its notes, one each; extra notes hold the last
// syllable (a melisma), extra syllables share the last notes.

// Line is one lyric line to place.
type Line struct {
	Key     string // "section:line", what an editor's correction is kept by
	Section int
	Text    string
	Syls    []Syl
	// From, To: its section's span in the recording (NaN: unknown).
	From, To float64
}

// Span is a note's time in the recording.
type Span struct{ Start, End float64 }

// Phrase is a run of notes sung in one breath: [First, Last] indexes
// into the notes.
type Phrase struct{ First, Last int }

// LineFit is where a line was laid.
type LineFit struct {
	Key     string
	Section int
	// Matched: it found its phrases; First..Last the notes its syllables
	// go on (after Shift, an editor's correction).
	Matched     bool
	First, Last int
	Shift       int
	// Conf 0..1: how well its syllables fit those notes; under LowConf
	// the line is drawn lighter.
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

// PhrasesFromNotes groups notes into phrases at rests of gap seconds or
// more.
func PhrasesFromNotes(notes []Span, gap float64) []Phrase {
	var out []Phrase
	for i := range notes {
		if i == 0 || notes[i].Start-notes[i-1].End >= gap {
			out = append(out, Phrase{i, i})
		} else {
			out[len(out)-1].Last = i
		}
	}
	return out
}

// PhrasesFromSpans are the analysis' voice phrases (start, end seconds)
// as runs of notes: each note goes to the phrase its start falls in
// (notes outside every phrase start one of their own); empty phrases go.
func PhrasesFromSpans(notes []Span, spans [][2]float64) []Phrase {
	var out []Phrase
	k := 0
	cur := -1 // the span the last phrase came from
	for i, n := range notes {
		for k < len(spans) && spans[k][1] <= n.Start {
			k++
		}
		in := k < len(spans) && n.Start >= spans[k][0]-0.08
		if in && cur == k && len(out) > 0 {
			out[len(out)-1].Last = i
			continue
		}
		if !in && cur == -2 && len(out) > 0 && n.Start-notes[i-1].End < 0.35 {
			out[len(out)-1].Last = i
			continue
		}
		out = append(out, Phrase{i, i})
		if in {
			cur = k
		} else {
			cur = -2
		}
	}
	return out
}

const (
	maxMerge  = 3   // phrases one line may take
	skipLine  = 3.0 // a line laid on no phrase
	mergeCost = 0.6 // per phrase more than one a line takes
)

// skipPhrase is the cost of leaving a phrase out: cheap for a short one
// (an ad-lib, an echo), dearer for a long one.
func skipPhrase(notes int) float64 { return math.Min(2.2, 0.5+0.12*float64(notes)) }

// fitCost is how badly s syllables fit n notes sung over dur seconds in
// [from, to], against a section spanning [secFrom, secTo].
func fitCost(s, n int, dur, from, to, secFrom, secTo float64) float64 {
	if s == 0 {
		return skipLine
	}
	var c float64
	fs, fn := float64(s), float64(n)
	if fn >= fs {
		c += 1.2 * math.Log(fn/fs) // a melisma, or notes the segmentation split
	} else {
		c += 2.5 * math.Log(fs/fn) // notes it merged
	}
	if dur > 0 {
		rate := fs / dur
		switch {
		case rate > 9:
			c += math.Log(rate / 9)
		case rate < 0.7:
			c += math.Log(0.7 / rate)
		}
	}
	if !math.IsNaN(secFrom) && !math.IsNaN(secTo) && to > from {
		// The share of the phrase outside its line's section (a bar's
		// slack either side is in the section's span already).
		out := math.Max(0, math.Min(to, secFrom)-from) + math.Max(0, to-math.Max(from, secTo))
		c += 4 * math.Min(1, out/(to-from))
	}
	return c
}

// AlignLines lays lines on phrases. shifts are editors' corrections, by
// line key: the notes a line's syllables start on, moved left (-) or
// right (+).
func AlignLines(lines []Line, phrases []Phrase, notes []Span, shifts map[string]int) ([]LineFit, []NoteLyric) {
	L, P := len(lines), len(phrases)
	inf := math.Inf(1)
	f := make([][]float64, L+1)
	type step struct{ di, dj int }
	from := make([][]step, L+1)
	for i := range f {
		f[i] = make([]float64, P+1)
		from[i] = make([]step, P+1)
		for j := range f[i] {
			f[i][j] = inf
		}
	}
	f[0][0] = 0
	match := func(i, j, k int) float64 {
		a, b := phrases[j].First, phrases[j+k-1].Last
		n := b - a + 1
		s, e := notes[a].Start, notes[b].End
		c := fitCost(len(lines[i].Syls), n, e-s, s, e, lines[i].From, lines[i].To) + mergeCost*float64(k-1)
		// A breath mid-line is short: a long silence between two phrases
		// says they're two lines (or a line and an ad-lib).
		for q := j + 1; q < j+k; q++ {
			if gap := notes[phrases[q].First].Start - notes[phrases[q-1].Last].End; gap > 1 {
				c += 0.8 * (gap - 1)
			}
		}
		return c
	}
	for i := 0; i <= L; i++ {
		for j := 0; j <= P; j++ {
			c := f[i][j]
			if math.IsInf(c, 1) {
				continue
			}
			if j < P {
				n := phrases[j].Last - phrases[j].First + 1
				if v := c + skipPhrase(n); v < f[i][j+1] {
					f[i][j+1], from[i][j+1] = v, step{0, 1}
				}
			}
			if i < L {
				if v := c + skipLine; v < f[i+1][j] {
					f[i+1][j], from[i+1][j] = v, step{1, 0}
				}
				for k := 1; k <= maxMerge && j+k <= P; k++ {
					if v := c + match(i, j, k); v < f[i+1][j+k] {
						f[i+1][j+k], from[i+1][j+k] = v, step{1, k}
					}
				}
			}
		}
	}
	fits := make([]LineFit, L)
	for i := range fits {
		fits[i] = LineFit{Key: lines[i].Key, Section: lines[i].Section, First: -1, Last: -1}
	}
	for i, j := L, P; i > 0 || j > 0; {
		s := from[i][j]
		if s.di == 1 && s.dj > 0 {
			ln := i - 1
			a, b := phrases[j-s.dj].First, phrases[j-1].Last
			cost := match(ln, j-s.dj, s.dj)
			fits[ln].Matched, fits[ln].First, fits[ln].Last = true, a, b
			fits[ln].Conf = math.Max(0, math.Min(1, math.Exp(-cost/2.2)))
		}
		i, j = i-s.di, j-s.dj
	}
	lyr := make([]NoteLyric, len(notes))
	for i := range lyr {
		lyr[i].Line = -1
	}
	prevEnd := -1
	for i := range fits {
		ft := &fits[i]
		if !ft.Matched {
			continue
		}
		if sh := shifts[ft.Key]; sh != 0 {
			first := min(ft.Last, max(prevEnd+1, ft.First+sh))
			ft.Shift = first - ft.First
			ft.First = first
		}
		ft.First = max(ft.First, prevEnd+1)
		if ft.First > ft.Last {
			ft.Matched = false
			continue
		}
		spread(lines[i].Syls, ft.First, ft.Last, i, lyr)
		prevEnd = ft.Last
	}
	return fits, lyr
}

// spread puts syllables on notes first..last.
func spread(syls []Syl, first, last, line int, lyr []NoteLyric) {
	n := last - first + 1
	for k := range n {
		lyr[first+k].Line = line
	}
	if len(syls) == 0 {
		return
	}
	if n >= len(syls) {
		for k, s := range syls {
			lyr[first+k].Syls = []Syl{s}
		}
		for k := len(syls); k < n; k++ {
			lyr[first+k].Hold = true
		}
		return
	}
	for k := 0; k < n-1; k++ {
		lyr[first+k].Syls = []Syl{syls[k]}
	}
	lyr[last].Syls = append([]Syl(nil), syls[n-1:]...)
}
