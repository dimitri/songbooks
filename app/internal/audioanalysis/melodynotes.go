package audioanalysis

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
)

// The lead voice's notes, from a recording's vocals part (docs/melody-sheet.md):
// the voice tracked frame by frame as for its range (YIN, pitchFrames),
// then cut into notes where it stops (a breath, a consonant), where its
// pitch moves to another note and stays there, or where its loudness dips
// between two syllables sung on one pitch. Each note is in seconds and in
// fractional MIDI; placing them in bars (the song's sync) and under the
// lyrics happens later, at request time (internal/melody), since the sync
// and the chart change without the recording changing.

// SungNote is one note the voice holds.
type SungNote struct {
	Start, End float64 // seconds
	Pitch      float64 // MIDI, fractional (the median of its frames)
	// Conf 0..1: how clear and steady its pitch was; a note under
	// LowConf is drawn lighter (a slide, a breathy or doubled voice).
	Conf float64
}

// MarshalJSON writes [start, end, pitch, conf], rounded: a song has a
// few hundred.
func (n SungNote) MarshalJSON() ([]byte, error) {
	return json.Marshal([]float64{round3(n.Start), round3(n.End), math.Round(n.Pitch*100) / 100, math.Round(n.Conf*100) / 100})
}

// UnmarshalJSON reads [start, end, pitch, conf].
func (n *SungNote) UnmarshalJSON(b []byte) error {
	var a []float64
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	if len(a) < 4 {
		return fmt.Errorf("a note is [start, end, pitch, conf]")
	}
	n.Start, n.End, n.Pitch, n.Conf = a[0], a[1], a[2], a[3]
	return nil
}

func round3(x float64) float64 { return math.Round(x*1000) / 1000 }

// MelodyNotesAnalyzer names this method, kept with each result: a new
// one finds every recording's notes again.
const MelodyNotesAnalyzer = "notes:2"

const (
	// LowConf: a note less sure than this is marked.
	LowConf = 0.5
	// A note shorter than this is a blip (a scoop into the next note,
	// a consonant's pitch), dropped.
	minNoteSeconds = 0.09
	// A gap of this many unvoiced frames ends a note (about 46 ms): less
	// is a frame YIN missed.
	gapFrames = 2
	// The pitch moves on when it stays this far (semitones) from the
	// note's own for this many frames (about 93 ms): a vibrato swings
	// back sooner.
	moveSemitones = 0.85
	moveFrames    = 4
	// Too little singing for a melody (a backing track's bleed).
	minNotes = 8
)

// MelodyNotes is what's kept for a recording.
type MelodyNotes struct {
	Notes []SungNote
	// Voiced: the seconds of singing the notes cover.
	Voiced float64
}

// AnalyzeMelodyNotes decodes a vocals part and finds its notes; ok false
// when there's too little singing to call a melody.
func AnalyzeMelodyNotes(ctx context.Context, vocals []byte) (MelodyNotes, bool, error) {
	x, err := Decode(ctx, vocals)
	if err != nil {
		return MelodyNotes{}, false, fmt.Errorf("vocals part: %w", err)
	}
	m, ok := MelodyNotesOf(x)
	return m, ok, nil
}

// MelodyNotesOf finds the notes of samples at SampleRate.
func MelodyNotesOf(x []float32) (MelodyNotes, bool) {
	notes := dropOutliers(SegmentNotes(pitchFrames(x)))
	var m MelodyNotes
	m.Notes = notes
	for _, n := range notes {
		m.Voiced += n.End - n.Start
	}
	return m, len(notes) >= minNotes
}

// SegmentNotes cuts frames into notes.
func SegmentNotes(fr Frames) []SungNote {
	p := cleanPitch(fr.Pitch)
	n := len(p)
	clar := func(i int) float64 {
		if i < len(fr.Clarity) {
			return fr.Clarity[i]
		}
		return 1
	}
	energy := func(i int) float64 {
		if i < len(fr.Energy) {
			return fr.Energy[i]
		}
		return 1
	}
	// Voiced runs (gaps shorter than gapFrames bridged).
	type run struct{ a, b int } // frames [a, b)
	var runs []run
	for i := 0; i < n; {
		if p[i] <= 0 {
			i++
			continue
		}
		j := i
		for j < n {
			if p[j] > 0 {
				j++
				continue
			}
			k := j
			for k < n && p[k] <= 0 && k-j < gapFrames {
				k++
			}
			if k < n && p[k] > 0 && k-j < gapFrames {
				j = k
				continue
			}
			break
		}
		runs = append(runs, run{i, j})
		i = j
	}
	var out []SungNote
	for _, r := range runs {
		for _, seg := range splitRun(p, r.a, r.b) {
			for _, s := range splitOnDips(p, energy, seg[0], seg[1]) {
				if note, ok := makeNote(p, clar, s[0], s[1]); ok {
					out = append(out, note)
				}
			}
		}
	}
	return out
}

// cleanPitch is the track with lone octave slips folded back (a frame an
// octave off from the voiced frames around it) and single-frame glitches
// smoothed by a median of three.
func cleanPitch(in []float64) []float64 {
	p := slices.Clone(in)
	n := len(p)
	near := make([]float64, 0, 16)
	for i := range n {
		if p[i] <= 0 {
			continue
		}
		near = near[:0]
		for j := max(0, i-7); j < min(n, i+8); j++ {
			if j != i && in[j] > 0 {
				near = append(near, in[j])
			}
		}
		if len(near) < 4 {
			continue
		}
		med := median(near)
		for _, k := range []float64{-12, 12, -24, 24} {
			if math.Abs(p[i]+k-med) < 1.5 && math.Abs(p[i]-med) > 6 {
				p[i] += k
				break
			}
		}
	}
	out := slices.Clone(p)
	for i := 1; i+1 < n; i++ {
		if p[i-1] > 0 && p[i] > 0 && p[i+1] > 0 {
			out[i] = median([]float64{p[i-1], p[i], p[i+1]})
		}
	}
	return out
}

// splitRun cuts a voiced run where the pitch leaves the note it was on
// and stays away: the note's pitch is the median of its frames so far.
func splitRun(p []float64, a, b int) [][2]int {
	var out [][2]int
	start := a
	away, side := 0, 0
	for i := a; i < b; i++ {
		if p[i] <= 0 {
			continue
		}
		ref := median(voiced(p[start:i]))
		if i-start < 2 {
			continue
		}
		d := p[i] - ref
		s := 0
		if d > moveSemitones {
			s = 1
		} else if d < -moveSemitones {
			s = -1
		}
		if s != 0 && (s == side || away == 0) {
			away++
			side = s
		} else {
			away, side = 0, 0
			if s != 0 {
				away, side = 1, s
			}
		}
		if away >= moveFrames {
			cut := i - away + 1
			out = append(out, [2]int{start, cut})
			start = cut
			away, side = 0, 0
		}
	}
	return append(out, [2]int{start, b})
}

// splitOnDips cuts a note where its loudness dips well below what's
// around it, both sides long enough to be notes: two syllables sung on
// one pitch with a soft consonant between them.
func splitOnDips(p []float64, energy func(int) float64, a, b int) [][2]int {
	minLen := int(math.Ceil(0.12 / FrameSeconds))
	if b-a < 2*minLen {
		return [][2]int{{a, b}}
	}
	best, bestRatio := -1, 1.0
	for i := a + minLen; i < b-minLen; i++ {
		e := energy(i)
		if e > energy(i-1) || e > energy(i+1) {
			continue
		}
		var left, right float64
		for j := max(a, i-minLen); j < i; j++ {
			left = max(left, energy(j))
		}
		for j := i + 1; j < min(b, i+minLen+1); j++ {
			right = max(right, energy(j))
		}
		peak := min(left, right)
		if peak <= 0 {
			continue
		}
		if r := e / peak; r < bestRatio {
			best, bestRatio = i, r
		}
	}
	if best < 0 || bestRatio > 0.35 {
		return [][2]int{{a, b}}
	}
	return append(splitOnDips(p, energy, a, best), splitOnDips(p, energy, best, b)...)
}

// makeNote is frames [a, b) as a note: its pitch the median of its
// middle (a scoop into it, a fall off it left out); false for a blip.
func makeNote(p []float64, clar func(int) float64, a, b int) (SungNote, bool) {
	start, end := float64(a)*FrameSeconds, float64(b)*FrameSeconds
	if end-start < minNoteSeconds {
		return SungNote{}, false
	}
	trim := (b - a) / 6
	mid := voiced(p[a+trim : b-trim])
	if len(mid) == 0 {
		return SungNote{}, false
	}
	pitch := median(mid)
	if pitch < 36 || pitch > 96 {
		return SungNote{}, false
	}
	// Steadiness: the frames' spread around the note (a vibrato of half a
	// semitone is steady; a slide is not).
	var dev, c float64
	for _, v := range mid {
		dev += math.Abs(v - pitch)
	}
	dev /= float64(len(mid))
	k := 0
	for i := a; i < b; i++ {
		if p[i] > 0 {
			c += clar(i)
			k++
		}
	}
	c /= float64(max(1, k))
	steady := math.Max(0, 1-math.Max(0, dev-0.3)/0.7)
	// Short notes are less sure.
	short := math.Min(1, (end-start)/0.18)
	return SungNote{Start: start, End: end, Pitch: pitch, Conf: math.Max(0, math.Min(1, c*steady*(0.6+0.4*short)))}, true
}

func voiced(p []float64) []float64 {
	out := make([]float64, 0, len(p))
	for _, v := range p {
		if v > 0 {
			out = append(out, v)
		}
	}
	return out
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := slices.Clone(v)
	slices.Sort(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// outlier: a note this far (semitones) from the voice's median is no
// note of the tune (a breath's pitch, a tracking error two octaves off).
const outlier = 16

// dropOutliers leaves out the notes far from the voice's median.
func dropOutliers(ns []SungNote) []SungNote {
	if len(ns) < minNotes {
		return ns
	}
	ps := make([]float64, len(ns))
	for i, n := range ns {
		ps[i] = n.Pitch
	}
	med := median(ps)
	out := ns[:0:0]
	for _, n := range ns {
		if math.Abs(n.Pitch-med) <= outlier {
			out = append(out, n)
		}
	}
	return out
}
