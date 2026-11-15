// Package gpfacts reads what a Guitar Pro file says about its song — its
// "facts", extracted from the file by static/js/gp-facts.js (in the
// browser at upload, or in Node for stored files) and stored next to it —
// and the "gp[Track]: bars" lines a song uses to point into that file.
// See docs/gp-file-model.md.
//
// Guitar Pro is only parsed in JavaScript (alphaTab); this package never
// reads a .gp file itself.
package gpfacts

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Facts mirrors gp-facts.js's extract(). Bars are numbered from 1 as
// Guitar Pro shows them; strings run from the highest.
type Facts struct {
	V        int          `json:"v"`
	Title    string       `json:"title"`
	Artist   string       `json:"artist"`
	Tempo    int          `json:"tempo"`
	Swing    string       `json:"swing"`
	Bars     int          `json:"bars"`
	Played   []int        `json:"played"`
	Time     []TimeSig    `json:"time"`
	Tempos   []TempoMark  `json:"tempos"`
	Keys     []KeySig     `json:"keys"`
	Markers  []Marker     `json:"markers"`
	Repeats  []Repeat     `json:"repeats"`
	Tracks   []Track      `json:"tracks"`
	Chords   [][]BarChord `json:"chords"` // per bar, from the first track that has chord names
	Diagrams []Diagram    `json:"diagrams"`
}

type TimeSig struct {
	Bar int `json:"bar"`
	Num int `json:"num"`
	Den int `json:"den"`
}

type TempoMark struct {
	Bar int `json:"bar"`
	BPM int `json:"bpm"`
}

// KeySig is a key signature: Sig counts sharps (> 0) or flats (< 0).
type KeySig struct {
	Bar   int  `json:"bar"`
	Sig   int  `json:"sig"`
	Minor bool `json:"minor"`
}

type Marker struct {
	Bar  int    `json:"bar"`
	Text string `json:"text"`
}

type Repeat struct {
	Bar     int  `json:"bar"`
	Start   bool `json:"start"`
	Count   int  `json:"count"`
	Endings int  `json:"endings"`
}

type Track struct {
	N       int    `json:"n"`
	Name    string `json:"name"`
	Short   string `json:"short"`
	Program int    `json:"program"`
	Drums   bool   `json:"drums"`
	Tuning  []int  `json:"tuning"`
	Capo    int    `json:"capo"`
	Tab     bool   `json:"tab"`
	Staff   bool   `json:"staff"`
}

// BarChord is a chord name over a bar, Pos the fraction of the bar where
// it starts.
type BarChord struct {
	Pos  float64 `json:"pos"`
	Name string  `json:"name"`
}

// Diagram is one of the file's chord diagrams: Frets per string from the
// highest, -1 for a muted string, 0 open; Base is Guitar Pro's first fret.
type Diagram struct {
	Name   string `json:"name"`
	Track  int    `json:"track"`
	Base   int    `json:"base"`
	Frets  []int  `json:"frets"`
	Barres []int  `json:"barres"`
}

// Parse decodes stored facts; nil, nil for none.
func Parse(data []byte) (*Facts, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var f Facts
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// Ref is a parsed "gp[Track]:" line.
type Ref struct {
	Track    string   // as written; "" for every track
	Ranges   [][2]int // inclusive bar ranges, in order
	Notation string   // "tab", "staff", "tab+staff", "slash", or "" for the reader's own choice
}

var (
	notationRE = regexp.MustCompile(`(?i)(?:^|\s+)(tab\+staff|staff\+tab|tab|staff|slash)$`)
	rangeRE    = regexp.MustCompile(`^(\d+)\s*(?:[-–]\s*(\d+))?$`)
)

// ParseRef reads a "gp[...]:" line's value: "9-12 staff", "1-4, 9-12",
// "bars 9". Kept in step with gp-facts.js's parseRef.
func ParseRef(track, spec string) (Ref, error) {
	r := Ref{Track: strings.TrimSpace(track)}
	v := strings.TrimSpace(spec)
	if m := notationRE.FindStringSubmatchIndex(v); m != nil {
		r.Notation = strings.ToLower(v[m[2]:m[3]])
		if r.Notation == "staff+tab" {
			r.Notation = "tab+staff"
		}
		v = strings.TrimSpace(v[:m[0]])
	}
	low := strings.ToLower(v)
	for _, p := range []string{"bars ", "bar "} {
		if strings.HasPrefix(low, p) {
			v = strings.TrimSpace(v[len(p):])
			break
		}
	}
	if v == "" {
		return r, fmt.Errorf("no bars: write them as 9-12")
	}
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		m := rangeRE.FindStringSubmatch(part)
		if m == nil {
			return r, fmt.Errorf("bars %q should look like 9-12 or 1-4, 9-12", part)
		}
		a, _ := strconv.Atoi(m[1])
		b := a
		if m[2] != "" {
			b, _ = strconv.Atoi(m[2])
		}
		if a < 1 || b < a {
			return r, fmt.Errorf("bars %s are not a range", part)
		}
		r.Ranges = append(r.Ranges, [2]int{a, b})
	}
	return r, nil
}

// String writes the ref's value back, normalized ("5-8 staff").
func (r Ref) String() string {
	parts := make([]string, len(r.Ranges))
	for i, rg := range r.Ranges {
		if rg[0] == rg[1] {
			parts[i] = strconv.Itoa(rg[0])
		} else {
			parts[i] = fmt.Sprintf("%d-%d", rg[0], rg[1])
		}
	}
	s := strings.Join(parts, ", ")
	if r.Notation != "" {
		s += " " + r.Notation
	}
	return s
}

// Fold lowers and strips accents, for matching names typed by hand.
func Fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.TrimSpace(s)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// FindTrack returns the index into f.Tracks the label names ("#2" by
// number, otherwise by name or short name, ignoring case and accents), or
// -1. Kept in step with gp-facts.js's findTrack.
func (f *Facts) FindTrack(label string) int {
	label = strings.TrimSpace(label)
	if strings.HasPrefix(label, "#") {
		if n, err := strconv.Atoi(label[1:]); err == nil && n >= 1 && n <= len(f.Tracks) {
			return n - 1
		}
		return -1
	}
	want := Fold(label)
	for i, t := range f.Tracks {
		if Fold(t.Name) == want {
			return i
		}
	}
	for i, t := range f.Tracks {
		if Fold(t.Short) == want {
			return i
		}
	}
	return -1
}

// PlayedBars lists, in playing order, the bars of r's ranges as the file
// plays them (repeats inside a range played as written).
func (f *Facts) PlayedBars(r Ref) []int {
	var out []int
	for _, rg := range r.Ranges {
		// Played order restricted to this range: a range inside a repeat
		// is played as many times as the repeat plays it, so take every
		// pass over it, in order.
		for _, b := range f.Played {
			if b >= rg[0] && b <= rg[1] {
				out = append(out, b)
			}
		}
	}
	return out
}

// TimeAt is the time signature in effect at bar.
func (f *Facts) TimeAt(bar int) (num, den int) {
	num, den = 4, 4
	for _, t := range f.Time {
		if t.Bar <= bar {
			num, den = t.Num, t.Den
		}
	}
	return
}

// KeyAt is the key signature in effect at bar.
func (f *Facts) KeyAt(bar int) (KeySig, bool) {
	var k KeySig
	ok := false
	for _, ks := range f.Keys {
		if ks.Bar <= bar {
			k, ok = ks, true
		}
	}
	return k, ok
}

// MarkerAt is the section marker on bar, if any.
func (f *Facts) MarkerAt(bar int) string {
	for _, m := range f.Markers {
		if m.Bar == bar {
			return m.Text
		}
	}
	return ""
}

// KeyName names a key signature: 2 sharps major is "D", 3 flats minor
// "Cm". A signature written with flats is named with flats.
func KeyName(k KeySig) string {
	// Circle of fifths from C (major) / A (minor), in pitch classes.
	pc := ((k.Sig*7)%12 + 12) % 12
	if k.Minor {
		pc = (pc + 9) % 12
	}
	sharps := [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
	flats := [12]string{"C", "Db", "D", "Eb", "E", "F", "Gb", "G", "Ab", "A", "Bb", "B"}
	name := sharps[pc]
	if k.Sig < 0 {
		name = flats[pc]
	}
	if k.Minor {
		name += "m"
	}
	return name
}

var (
	majSevenRE = regexp.MustCompile(`7M|M7|Maj7|MAJ7`)
	tensionRE  = regexp.MustCompile(`/?\(?([#+b-])?(5|9|11|13)([#+b-])?\)?`)
)

// ChordName turns a Guitar Pro chord name into this app's chord notation:
// Guitar Pro writes major sevenths "7M" and altered tensions "/9#" or
// "/9+", which would read here as a slash chord. Same rules as the GP
// import's normalizeChordName.
func ChordName(name string) string {
	n := strings.Join(strings.Fields(name), "")
	if loc := majSevenRE.FindStringIndex(n); loc != nil {
		n = n[:loc[0]] + "maj7" + n[loc[1]:]
	}
	return tensionRE.ReplaceAllStringFunc(n, func(m string) string {
		sub := tensionRE.FindStringSubmatch(m)
		alt := sub[1]
		if alt == "" {
			alt = sub[3]
		}
		if alt == "" {
			return m
		}
		if alt == "+" || alt == "#" {
			return "#" + sub[2]
		}
		return "b" + sub[2]
	})
}

// Spec writes d in this app's chord diagram grammar (internal/chorddiagram:
// "[BaseFret:]" then one token per string from the lowest, X muted, O
// open, a digit for the row, a barre in parentheses). ok is false for a
// shape that doesn't fit a diagram (more than 9 rows, or no fretted or
// open string).
func (d Diagram) Spec() (string, bool) {
	n := len(d.Frets)
	if n == 0 {
		return "", false
	}
	low := make([]int, n) // lowest string first
	for i, f := range d.Frets {
		low[n-1-i] = f
	}
	minF, maxF, any := 0, 0, false
	for _, f := range low {
		if f >= 0 {
			any = true
		}
		if f > 0 {
			if minF == 0 || f < minF {
				minF = f
			}
			if f > maxF {
				maxF = f
			}
		}
	}
	if !any {
		return "", false
	}
	base := 1
	if maxF > 4 {
		base = minF
	}
	if maxF-base+1 > 9 {
		return "", false
	}
	tok := make([]string, n)
	for i, f := range low {
		switch {
		case f < 0:
			tok[i] = "X"
		case f == 0:
			tok[i] = "O"
		default:
			tok[i] = strconv.Itoa(f - base + 1)
		}
	}
	// A barre starts at the lowest string fretted at its fret and runs
	// over every following string held at that fret or higher.
	open := make([]bool, n)
	closeAt := make([]bool, n)
	for _, bf := range d.Barres {
		first := -1
		for i, f := range low {
			if f == bf {
				first = i
				break
			}
		}
		if first < 0 {
			continue
		}
		last := first
		for last+1 < n && low[last+1] >= bf {
			last++
		}
		if last > first {
			open[first], closeAt[last] = true, true
		}
	}
	var b strings.Builder
	if base > 1 {
		b.WriteString(strconv.Itoa(base) + ":")
	}
	for i, t := range tok {
		if open[i] {
			b.WriteByte('(')
		}
		b.WriteString(t)
		if closeAt[i] {
			b.WriteByte(')')
		}
	}
	return b.String(), true
}
