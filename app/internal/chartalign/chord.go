package chartalign

import (
	"math"
	"strings"

	"app/internal/chordsym"
)

// Class is a chord's family, what two spellings of "the same chord"
// share: the colour (7, 9, add9, 6) and the bass note don't change it.
type Class int

const (
	ClassNone  Class = iota // N.C., silence
	ClassMajor              // C, C7, Cmaj7, C6, Cadd9, C9
	ClassMinor              // Cm, Cm7, Cm9, Cm6
	ClassSus                // Csus4, Csus2, C7sus4
	ClassDim                // Cdim, Cdim7, Cm7b5
	ClassAug                // C+, Caug
	ClassPower              // C5
)

// Chord is a chord symbol reduced to what can be heard: its root, its
// bass, its pitch classes (bit i set: pitch class i, C = 0) and family.
// A zero Chord with NC set is "no chord".
type Chord struct {
	NC    bool
	Root  int
	Bass  int
	PCs   uint16
	Class Class
}

// NoChord is N.C.
var NoChord = Chord{NC: true, Root: -1, Bass: -1}

// ParseSymbol reads a chart's chord symbol ("Em/B", "D7sus4", "F#m7",
// "N.C."); ok is false for what isn't a chord (an empty token, "%", a
// rest "R" is N.C.).
func ParseSymbol(sym string) (Chord, bool) {
	s := strings.TrimSpace(sym)
	switch strings.ToUpper(s) {
	case "N.C.", "NC", "N.C", "R", "X":
		return NoChord, true
	}
	p := chordsym.Split(s)
	if p.Root == "" {
		return Chord{}, false
	}
	root := chordsym.PitchClass(p.Root)
	if root < 0 {
		return Chord{}, false
	}
	ivs, class := qualityIntervals(p.Quality)
	c := Chord{Root: root, Bass: root, Class: class}
	for _, iv := range ivs {
		c.PCs |= 1 << uint((root+iv)%12)
	}
	if p.Bass != "" {
		if b := chordsym.PitchClass(p.Bass); b >= 0 {
			c.Bass = b
			c.PCs |= 1 << uint(b)
		}
	}
	return c, true
}

// qualityIntervals turns a chart quality ("m7", "7sus4", "maj7", "add9",
// "m7b5", "6") into semitones above the root and a family.
func qualityIntervals(q string) ([]int, Class) {
	q = strings.TrimSpace(q)
	qp := chordsym.DecomposeQuality(q)
	ext := strings.ToLower(qp.Ext)
	var ivs []int
	class := ClassMajor
	switch qp.Marker {
	case chordsym.MarkerMajor7:
		// "maj" alone is a major triad, "maj7"/"maj9" carry the major
		// seventh (the extension switch below adds it).
		ivs, class = []int{0, 4, 7}, ClassMajor
	case chordsym.MarkerHalfDim:
		ivs, class = []int{0, 3, 6, 10}, ClassDim
	case chordsym.MarkerDim:
		ivs, class = []int{0, 3, 6}, ClassDim
		if strings.HasPrefix(ext, "7") {
			ivs = append(ivs, 9)
		}
	case chordsym.MarkerAug:
		ivs, class = []int{0, 4, 8}, ClassAug
	case chordsym.MarkerMinor:
		ivs, class = []int{0, 3, 7}, ClassMinor
		if strings.HasPrefix(ext, "maj") || strings.HasPrefix(ext, "Δ") || strings.HasPrefix(ext, "M7") {
			ivs = append(ivs, 11) // minor-major seventh
			ext = strings.TrimLeft(strings.TrimPrefix(strings.TrimPrefix(ext, "maj"), "Δ"), "M")
			ext = strings.TrimPrefix(ext, "7")
		}
	default:
		ivs = []int{0, 4, 7}
		if strings.HasPrefix(q, "+") {
			ivs, class = []int{0, 4, 8}, ClassAug
		}
		if q == "5" {
			return []int{0, 7}, ClassPower
		}
	}
	has := func(s string) bool { return strings.Contains(ext, s) }
	set := map[int]bool{}
	for _, i := range ivs {
		set[i] = true
	}
	if has("sus2") {
		delete(set, 3)
		delete(set, 4)
		set[2] = true
		class = ClassSus
	} else if has("sus") {
		delete(set, 3)
		delete(set, 4)
		set[5] = true
		class = ClassSus
	}
	seventh := func() {
		if qp.Marker == chordsym.MarkerMajor7 || set[11] {
			set[11] = true
		} else if qp.Marker == chordsym.MarkerDim {
			set[9] = true
		} else {
			set[10] = true
		}
	}
	plain := strings.NewReplacer("sus4", "", "sus2", "", "sus", "", "add9", "", "add2", "", "add11", "", "add4", "").Replace(ext)
	switch {
	case strings.HasPrefix(plain, "7"):
		seventh()
	case strings.HasPrefix(plain, "9"):
		seventh()
		set[2] = true
	case strings.HasPrefix(plain, "11"):
		seventh()
		set[5] = true
	case strings.HasPrefix(plain, "13"):
		seventh()
		set[9] = true
	case strings.HasPrefix(plain, "69"), strings.HasPrefix(plain, "6/9"):
		set[9], set[2] = true, true
	case strings.HasPrefix(plain, "6"):
		set[9] = true
	}
	if has("add9") || has("add2") {
		set[2] = true
	}
	if has("add11") || has("add4") {
		set[5] = true
	}
	if has("b5") || has("-5") {
		delete(set, 7)
		set[6] = true
	}
	if has("#5") || (has("+5")) {
		delete(set, 7)
		set[8] = true
	}
	out := make([]int, 0, len(set))
	for i := 0; i < 12; i++ {
		if set[i] {
			out = append(out, i)
		}
	}
	return out, class
}

// hartePC reads a Harte root ("C", "F#", "Bb").
func hartePC(s string) int { return chordsym.PitchClass(s) }

// harteDegree is a Harte bass degree ("3", "b7", "5") in semitones.
var harteDegree = map[string]int{"1": 0, "b2": 1, "2": 2, "#2": 3, "b3": 3, "3": 4, "4": 5, "#4": 6, "b5": 6, "5": 7, "#5": 8, "b6": 8, "6": 9, "bb7": 9, "b7": 10, "7": 11, "9": 2, "b9": 1}

// harteQuality: lv-chordia's vocabulary (its submission list) and the
// common Harte shorthands.
var harteQuality = map[string]struct {
	ivs   []int
	class Class
}{
	"maj": {[]int{0, 4, 7}, ClassMajor}, "min": {[]int{0, 3, 7}, ClassMinor},
	"7": {[]int{0, 4, 7, 10}, ClassMajor}, "maj7": {[]int{0, 4, 7, 11}, ClassMajor}, "min7": {[]int{0, 3, 7, 10}, ClassMinor},
	"9": {[]int{0, 4, 7, 10, 2}, ClassMajor}, "maj9": {[]int{0, 4, 7, 11, 2}, ClassMajor}, "min9": {[]int{0, 3, 7, 10, 2}, ClassMinor},
	"11": {[]int{0, 4, 7, 10, 2, 5}, ClassMajor}, "13": {[]int{0, 4, 7, 10, 2, 9}, ClassMajor},
	"maj6": {[]int{0, 4, 7, 9}, ClassMajor}, "min6": {[]int{0, 3, 7, 9}, ClassMinor},
	"sus4": {[]int{0, 5, 7}, ClassSus}, "sus2": {[]int{0, 2, 7}, ClassSus}, "sus4(b7)": {[]int{0, 5, 7, 10}, ClassSus},
	"dim": {[]int{0, 3, 6}, ClassDim}, "dim7": {[]int{0, 3, 6, 9}, ClassDim}, "hdim7": {[]int{0, 3, 6, 10}, ClassDim},
	"aug": {[]int{0, 4, 8}, ClassAug}, "minmaj7": {[]int{0, 3, 7, 11}, ClassMinor}, "5": {[]int{0, 7}, ClassPower},
}

// ParseHarte reads a chord recogniser's label ("D:maj", "A:maj/3",
// "B:min7", "N").
func ParseHarte(label string) (Chord, bool) {
	if label == "N" || label == "X" || label == "" {
		return NoChord, true
	}
	root, rest, _ := strings.Cut(label, ":")
	r := hartePC(root)
	if r < 0 {
		return Chord{}, false
	}
	if rest == "" {
		rest = "maj"
	}
	q, bass, _ := strings.Cut(rest, "/")
	hq, ok := harteQuality[q]
	if !ok {
		hq = harteQuality["maj"]
		if strings.HasPrefix(q, "min") {
			hq = harteQuality["min"]
		}
	}
	c := Chord{Root: r, Bass: r, Class: hq.class}
	for _, iv := range hq.ivs {
		c.PCs |= 1 << uint((r+iv)%12)
	}
	if bass != "" {
		if d, ok := harteDegree[bass]; ok {
			c.Bass = (r + d) % 12
			c.PCs |= 1 << uint(c.Bass)
		}
	}
	return c, true
}

// vector is the chord as the alignment compares it: its pitch classes
// with the bass weighted up, N.C. a 13th dimension of its own; unit length.
func (c Chord) vector() [13]float64 {
	var v [13]float64
	if c.NC {
		v[12] = 1
		return v
	}
	for i := 0; i < 12; i++ {
		if c.PCs&(1<<uint(i)) != 0 {
			v[i] = 1
		}
	}
	if c.Bass >= 0 {
		v[c.Bass] += 0.5
	}
	n := 0.0
	for _, x := range v {
		n += x * x
	}
	n = math.Sqrt(n)
	for i := range v {
		v[i] /= n
	}
	return v
}

func dot(a, b [13]float64) float64 {
	s := 0.0
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

func bits(x uint16) int {
	n := 0
	for ; x != 0; x &= x - 1 {
		n++
	}
	return n
}

// triadPCs is the chord's core: root, third (or sus note) and fifth.
func (c Chord) triadPCs() uint16 {
	if c.NC {
		return 0
	}
	var ivs []int
	switch c.Class {
	case ClassMinor:
		ivs = []int{0, 3, 7}
	case ClassDim:
		ivs = []int{0, 3, 6}
	case ClassAug:
		ivs = []int{0, 4, 8}
	case ClassPower:
		ivs = []int{0, 7}
	case ClassSus:
		// whichever sus note the chord has
		if c.PCs&(1<<uint((c.Root+2)%12)) != 0 && c.PCs&(1<<uint((c.Root+5)%12)) == 0 {
			ivs = []int{0, 2, 7}
		} else {
			ivs = []int{0, 5, 7}
		}
	default:
		ivs = []int{0, 4, 7}
	}
	var m uint16
	for _, iv := range ivs {
		m |= 1 << uint((c.Root+iv)%12)
	}
	return m
}

// Compatible: the chart's chord and the heard one are the same chord as
// far as a chart goes. The bass note doesn't matter (Em/B is Em), nor
// the colour (A7, Aadd9 and A are A), nor sus against its triad (Asus4
// resolving to A within the beat is A); a chord whose notes contain the
// other's triad is it too (F#m7 holds A, C6 holds Am: relative
// spellings), and enharmonics are the same pitch classes.
func Compatible(chart, heard Chord) bool {
	if chart.NC || heard.NC {
		return chart.NC == heard.NC
	}
	if chart.Root == heard.Root {
		switch {
		case chart.Class == heard.Class:
			return true
		case chart.Class == ClassSus && (heard.Class == ClassMajor || heard.Class == ClassMinor),
			heard.Class == ClassSus && (chart.Class == ClassMajor || chart.Class == ClassMinor),
			chart.Class == ClassPower || heard.Class == ClassPower:
			return true
		}
	}
	ct, ht := chart.triadPCs(), heard.triadPCs()
	if ct&heard.PCs == ct || ht&chart.PCs == ht {
		return true
	}
	// A slash chord over the heard chord's root, or the other way round
	// (G/A is heard as an A with a sus colour: Asus4, A7sus4).
	if chart.Bass == heard.Root && bits(chart.PCs&heard.PCs) >= 2 {
		return true
	}
	if heard.Bass == chart.Root && bits(chart.PCs&heard.PCs) >= 2 {
		return true
	}
	return false
}
