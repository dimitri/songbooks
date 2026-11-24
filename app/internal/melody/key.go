package melody

import (
	"math"
	"strings"

	"app/internal/audioanalysis"
	"app/internal/chordsym"
)

// The key a song's melody and chords are in, when its chart doesn't say
// (docs/melody-sheet.md, "Key signature"): a Krumhansl–Schmuckler
// estimate from the melody's notes and the chart's chords. The chart's
// "key:" is what the sheet writes when there is one; this only fills in
// for a song without, and tells an editor when the two disagree.

// Krumhansl–Kessler profiles (as audioanalysis/key.go), from the tonic.
var (
	majorProfile = [12]float64{6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88}
	minorProfile = [12]float64{6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17}
)

// KeyGuess is a key found by GuessKey: its tonic (0 = C) and mode, its
// correlation with the evidence, and its lead over the best other key
// that writes another key signature (a relative major or minor writes the
// same one: it's no rival for the sheet).
type KeyGuess struct {
	Tonic int
	Minor bool
	Score float64
	// Lead: (r1 − r2) / (1 − r2) against the best key with another
	// signature; 0..1.
	Lead float64
	// RelativeLead: the same against its relative major or minor (how
	// sure the mode is: often not very).
	RelativeLead float64
}

// Name is the key as a chart writes it ("Em", "Bb").
func (g KeyGuess) Name(flats bool) string { return chordsym.KeyName(g.Tonic, g.Minor, flats) }

// Fifths is the key's signature: sharps (+) or flats (−).
func (g KeyGuess) Fifths() int {
	major := g.Tonic
	if g.Minor {
		major = (g.Tonic + 3) % 12
	}
	return majorFifths[major]
}

// KeyEvidence is what a key is guessed from: the melody's pitch classes
// (weighted by how long and how surely each note is sung), the chart's
// chord tones (by beats), and its first and last chords (a song mostly
// starts and ends on its tonic chord).
type KeyEvidence struct {
	Notes, Chords [12]float64
	// First, Last: the chart's first and last chords' roots (−1: none)
	// and whether they're minor.
	First, Last           int
	FirstMinor, LastMinor bool
}

// NotePitchClasses is the melody's pitch-class histogram: each note's
// length × confidence on its pitch class, the recording's tuning offset
// (the notes' mean distance from the equal-tempered pitches) taken out
// first.
func NotePitchClasses(notes []audioanalysis.SungNote) [12]float64 {
	var h [12]float64
	var sx, sy float64
	for _, n := range notes {
		a := 2 * math.Pi * (n.Pitch - math.Round(n.Pitch))
		w := n.End - n.Start
		sx += w * math.Cos(a)
		sy += w * math.Sin(a)
	}
	off := 0.0
	if sx != 0 || sy != 0 {
		off = math.Atan2(sy, sx) / (2 * math.Pi)
	}
	for _, n := range notes {
		w := (n.End - n.Start) * math.Max(0.2, n.Conf)
		if w <= 0 {
			continue
		}
		pc := ((int(math.Round(n.Pitch-off)) % 12) + 12) % 12
		h[pc] += w
	}
	return h
}

// ChordEvidence reads a chart's chords, one per beat ("" holds the chord
// before; "N.C.", "%" and unreadable ones count for nothing).
func ChordEvidence(beats []string) KeyEvidence {
	ev := KeyEvidence{First: -1, Last: -1}
	prev := ""
	for _, c := range beats {
		c = strings.TrimSpace(c)
		if c == "" {
			c = prev
		}
		prev = c
		tones, root, minor := chordTones(c)
		if root < 0 {
			continue
		}
		for pc, w := range tones {
			ev.Chords[pc] += w
		}
		if ev.First < 0 {
			ev.First, ev.FirstMinor = root, minor
		}
		ev.Last, ev.LastMinor = root, minor
	}
	return ev
}

// chordTones are a chord's pitch classes, weighted (root 1, third 0.7,
// fifth 0.6, seventh or sixth 0.4, a bass note 0.5), its root (−1: not a
// chord) and whether its third is minor.
func chordTones(sym string) (tones map[int]float64, root int, minor bool) {
	p := chordsym.Split(strings.NewReplacer("♭", "b", "♯", "#").Replace(sym))
	root = chordsym.PitchClass(p.Root)
	if root < 0 {
		return nil, -1, false
	}
	q := strings.ToLower(p.Quality)
	q = strings.NewReplacer("♭", "b", "♯", "#", "△", "maj", "Δ", "maj", "°", "dim", "ø", "m7b5").Replace(q)
	major7 := strings.HasPrefix(q, "maj") || strings.HasPrefix(p.Quality, "M")
	if major7 {
		q = strings.TrimPrefix(strings.TrimPrefix(q, "maj"), "m")
	}
	dim := strings.HasPrefix(q, "dim") || strings.Contains(q, "b5") || strings.Contains(q, "-5")
	minor = !major7 && (dim || strings.HasPrefix(q, "m") || strings.HasPrefix(q, "-"))
	tones = map[int]float64{root: 1}
	add := func(semis int, w float64) { tones[(root+semis)%12] += w }
	switch {
	case strings.Contains(q, "sus2"):
		add(2, 0.5)
	case strings.Contains(q, "sus"):
		add(5, 0.5)
	case minor:
		add(3, 0.7)
	default:
		add(4, 0.7)
	}
	switch {
	case dim:
		add(6, 0.6)
	case strings.HasPrefix(q, "aug") || strings.HasPrefix(q, "+") || strings.Contains(q, "#5"):
		add(8, 0.6)
	default:
		add(7, 0.6)
	}
	switch {
	case major7 && strings.ContainsAny(q, "79"):
		add(11, 0.4)
	case strings.HasPrefix(q, "dim7"):
		add(9, 0.4)
	case strings.ContainsAny(q, "79") || strings.Contains(q, "11") || strings.Contains(q, "13"):
		add(10, 0.4)
	case strings.Contains(q, "6"):
		add(9, 0.4)
	}
	if b := chordsym.PitchClass(p.Bass); b >= 0 && b != root {
		tones[b] += 0.5
	}
	return tones, root, minor
}

// KeyWeights are how much the melody and the chords count, and the bonus
// a key gets when the chart ends (half of it: starts) on its tonic chord.
type KeyWeights struct{ Notes, Chords, Cadence float64 }

// DefaultKeyWeights were measured on the demo songs (docs/melody-sheet.md).
var DefaultKeyWeights = KeyWeights{0.3, 0.7, 0.06}

// GuessKey is the likeliest key for the evidence; ok false when there's
// too little of it.
func GuessKey(ev KeyEvidence) (KeyGuess, bool) { return GuessKeyWith(ev, DefaultKeyWeights) }

// GuessKeyWith guesses with other weights (measuring them).
func GuessKeyWith(ev KeyEvidence, keyWeights KeyWeights) (KeyGuess, bool) {
	h, ok := combine(ev, keyWeights.Notes, keyWeights.Chords)
	if !ok {
		return KeyGuess{}, false
	}
	var scores [24]float64
	for k := range 24 {
		tonic, minor := k%12, k >= 12
		prof := majorProfile
		if minor {
			prof = minorProfile
		}
		var rot [12]float64
		for i := range 12 {
			rot[(tonic+i)%12] = prof[i]
		}
		s := pearson(h, rot)
		if ev.Last == tonic && ev.LastMinor == minor {
			s += keyWeights.Cadence
		}
		if ev.First == tonic && ev.FirstMinor == minor {
			s += keyWeights.Cadence / 2
		}
		scores[k] = s
	}
	best := 0
	for k := range 24 {
		if scores[k] > scores[best] {
			best = k
		}
	}
	g := KeyGuess{Tonic: best % 12, Minor: best >= 12, Score: scores[best]}
	// Its relative minor (major) key: the same signature.
	rel := (g.Tonic+9)%12 + 12
	if g.Minor {
		rel = (g.Tonic + 3) % 12
	}
	other := math.Inf(-1)
	for k := range 24 {
		if k == best || k == rel {
			continue
		}
		other = math.Max(other, scores[k])
	}
	lead := func(r2 float64) float64 {
		if r2 >= 1 || g.Score <= r2 {
			return 0
		}
		return math.Min(1, (g.Score-r2)/(1-r2))
	}
	g.Lead, g.RelativeLead = lead(other), lead(scores[rel])
	return g, true
}

// combine is the evidence as one distribution, each source normalized
// first; false when there's none.
func combine(ev KeyEvidence, wn, wc float64) ([12]float64, bool) {
	var h [12]float64
	sn, sc := sum12(ev.Notes), sum12(ev.Chords)
	if sn == 0 {
		wn = 0
	}
	if sc == 0 {
		wc = 0
	}
	if wn+wc == 0 {
		return h, false
	}
	for i := range 12 {
		if wn > 0 {
			h[i] += wn * ev.Notes[i] / sn
		}
		if wc > 0 {
			h[i] += wc * ev.Chords[i] / sc
		}
	}
	return h, true
}

func sum12(a [12]float64) float64 {
	s := 0.0
	for _, v := range a {
		s += v
	}
	return s
}

func pearson(a, b [12]float64) float64 {
	ma, mb := sum12(a)/12, sum12(b)/12
	var num, da, db float64
	for i := range 12 {
		x, y := a[i]-ma, b[i]-mb
		num += x * y
		da += x * x
		db += y * y
	}
	if da == 0 || db == 0 {
		return 0
	}
	return num / math.Sqrt(da*db)
}

// FromGuess is the sheet's Key for a guess, moved by transpose semitones.
func FromGuess(g KeyGuess, transpose int, flats bool) Key {
	k, _ := KeyOf(g.Name(flats), transpose, flats)
	return k
}

// KeyName names a key signature's key as a chart writes it ("Em", "Bb"):
// sharps or flats as the signature has them.
func KeyName(k Key, flats bool) string {
	major := ((k.Fifths*7)%12 + 12) % 12
	if k.Minor {
		return chordsym.KeyName((major+9)%12, true, k.Fifths < 0 || flats)
	}
	return chordsym.KeyName(major, false, k.Fifths < 0 || flats && k.Fifths == 6)
}

// SameSignature is whether two signatures write the same notes (six
// sharps and six flats do).
func SameSignature(a, b int) bool { return ((a-b)%12+12)%12 == 0 }
