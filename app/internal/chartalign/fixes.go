package chartalign

import (
	"math"
	"strings"

	"app/internal/chordsym"
)

// harteSuffix is how a chart writes a Harte quality.
var harteSuffix = map[string]string{
	"maj": "", "min": "m", "7": "7", "maj7": "maj7", "min7": "m7", "9": "9", "maj9": "maj9", "min9": "m9",
	"11": "11", "13": "13", "maj6": "6", "min6": "m6", "sus4": "sus4", "sus2": "sus2", "sus4(b7)": "7sus4",
	"dim": "dim", "dim7": "dim7", "hdim7": "m7b5", "aug": "+", "minmaj7": "m(maj7)", "5": "5",
}

var letters = "CDEFGAB"
var letterPC = [7]int{0, 2, 4, 5, 7, 9, 11}

// degreeSteps: a Harte bass degree's distance in letters.
var degreeSteps = map[string]int{"1": 0, "b2": 1, "2": 1, "9": 1, "b9": 1, "#2": 1, "b3": 2, "3": 2, "4": 3, "#4": 3, "b5": 4, "5": 4, "#5": 4, "b6": 5, "6": 5, "bb7": 6, "b7": 6, "7": 6}

// Spell writes a Harte label ("B:maj/3") as a chart writes it ("B/D#"),
// shifted by shift semitones, with flats when flats is set. The bass is
// spelled from the root (B's third is D#, not Eb).
func Spell(label string, shift int, flats bool) string {
	c, ok := ParseHarte(label)
	if !ok || c.NC {
		return "N.C."
	}
	rootName, rest, _ := strings.Cut(label, ":")
	if rest == "" {
		rest = "maj"
	}
	q, bass, _ := strings.Cut(rest, "/")
	rootPC := (chordsym.PitchClass(rootName) + shift + 120) % 12
	root := chordsym.SpellPitchClass(rootPC, flats)
	suffix, ok := harteSuffix[q]
	if !ok {
		suffix = ""
		if strings.HasPrefix(q, "min") {
			suffix = "m"
		}
	}
	out := root + suffix
	if bass != "" {
		if d, ok := harteDegree[bass]; ok && d != 0 {
			out += "/" + spellAbove(root, (rootPC+d)%12, degreeSteps[bass])
		}
	}
	return out
}

// spellAbove names pitch class pc as the note steps letters above root.
func spellAbove(root string, pc, steps int) string {
	li := strings.IndexByte(letters, root[0])
	if li < 0 {
		return chordsym.SpellPitchClass(pc, false)
	}
	l := (li + steps) % 7
	diff := ((pc-letterPC[l])%12 + 12) % 12
	switch diff {
	case 0:
		return string(letters[l])
	case 1:
		return string(letters[l]) + "#"
	case 11:
		return string(letters[l]) + "b"
	}
	return chordsym.SpellPitchClass(pc, false)
}

// usesFlats: the chart spells its chords with flats more than sharps.
func usesFlats(secs []Section) bool {
	f, s := 0, 0
	for _, sec := range secs {
		for _, b := range sec.Bars {
			for _, c := range b.Beats {
				p := chordsym.Split(c)
				for _, n := range []string{p.Root, p.Bass} {
					if len(n) > 1 {
						switch n[1] {
						case 'b':
							f++
						case '#':
							s++
						}
					}
				}
			}
		}
	}
	return f > s
}

type fixKey struct{ def, measure, slot int }

// chordFixes: the chart's chords the recording contradicts, a whole
// chord at a time (all its beats, at least two), when the recogniser is
// sure of another chord and gives the chart's almost nothing. A measure
// played several times (a repeat, a bare section) is proposed only when
// every time it's played says the same.
func chordFixes(r *Recording, g grid, secs []Section, all []flatBar, place []placement, heard []Chord, sp []secPlace, M int) []ChordFix {
	flats := usesFlats(secs)
	type cand struct {
		fix ChordFix
		ok  bool // false: this time it's played, the chart's chord is fine (or unclear)
	}
	byKey := map[fixKey][]cand{}
	var order []fixKey
	for i, p := range place {
		if p.d == 0 {
			continue
		}
		f := all[i]
		nb := len(f.bar.Beats)
		if nb == 0 {
			continue
		}
		// The beats of each written token.
		type span struct {
			slot  int
			sym   string
			beats []int
		}
		var spans []span
		for q := 0; q < p.d; q++ {
			k := (q * nb) / p.d
			slot := -1
			if k < len(f.bar.Slots) {
				slot = f.bar.Slots[k]
			}
			sym := f.bar.Beats[k]
			if len(spans) > 0 && spans[len(spans)-1].slot == slot && spans[len(spans)-1].sym == sym {
				spans[len(spans)-1].beats = append(spans[len(spans)-1].beats, p.j+q)
				continue
			}
			spans = append(spans, span{slot, sym, []int{p.j + q}})
		}
		for _, s := range spans {
			if s.slot < 0 {
				continue
			}
			chart, ok := ParseSymbol(s.sym)
			if !ok || chart.NC {
				continue
			}
			key := fixKey{f.bar.DefSection, f.bar.Measure, s.slot}
			if _, seen := byKey[key]; !seen {
				order = append(order, key)
			}
			votes := map[string]int{}
			var pH, pC float64
			n := 0
			for _, j := range s.beats {
				if j >= M {
					continue
				}
				h := heard[j]
				if h.NC || Compatible(chart, h) {
					continue
				}
				// Over a pedal: a slash chord's bass under the heard
				// chord's root (Em/B heard as Bm) is a voicing question,
				// not a wrong chord.
				if chart.Bass != chart.Root && chart.Bass == h.Root {
					continue
				}
				ph, pc := 0.6, 0.0
				if bc := r.beatChordsAt((g.beats[j] + g.beats[j+1]) / 2); len(bc) > 0 {
					ph, pc = 0, 0
					for _, gss := range bc {
						c, ok := ParseHarte(gss.C)
						if !ok {
							continue
						}
						if Compatible(chart, c) {
							pc += gss.P
						}
						if Compatible(h, c) && !Compatible(chart, c) {
							ph += gss.P
						}
					}
				}
				if pc >= 0.15 || ph < 0.4 {
					continue
				}
				votes[r.chordAt((g.beats[j]+g.beats[j+1])/2)]++
				pH += ph
				pC += pc
				n++
			}
			need := max(2, int(math.Ceil(0.6*float64(len(s.beats)))))
			best, bc := "", 0
			for l, c := range votes {
				if c > bc || (c == bc && l < best) {
					best, bc = l, c
				}
			}
			if n < need || bc*10 < 6*n {
				byKey[key] = append(byKey[key], cand{ok: false})
				continue
			}
			j0 := s.beats[0]
			fix := ChordFix{
				Section: secs[f.sec].Index, Bar: secs[f.sec].StartBar + f.k, SecBar: f.k + 1, Time: round3(g.beats[j0]),
				Chart: s.sym, Heard: Spell(best, 0, flats), Beats: n,
				Confidence: round2((pH / float64(n)) * (1 - pC/float64(n))),
				DefSection: f.bar.DefSection, Measure: f.bar.Measure, Slot: s.slot,
			}
			if j0+1 < len(g.beats) {
				for _, gss := range r.beatChordsAt((g.beats[j0] + g.beats[j0+1]) / 2) {
					sp := Spell(gss.C, 0, flats)
					if sp != fix.Heard && sp != "N.C." && len(fix.Alternatives) < 2 {
						fix.Alternatives = append(fix.Alternatives, sp)
					}
				}
			}
			byKey[key] = append(byKey[key], cand{fix: fix, ok: true})
		}
	}
	out := []ChordFix{}
	for _, k := range order {
		cs := byKey[k]
		agree := len(cs) > 0
		for _, c := range cs {
			if !c.ok || c.fix.Heard != cs[0].fix.Heard {
				agree = false
				break
			}
		}
		if !agree {
			continue
		}
		f := cs[0].fix
		for _, c := range cs[1:] {
			f.Confidence = math.Min(f.Confidence, c.fix.Confidence)
		}
		out = append(out, f)
	}
	return out
}
