package songimport

import (
	"strconv"
	"strings"

	"app/internal/chordsym"
	"app/internal/song"
)

// Keys are named the way chord charts name them: a root ("F#", "Bb")
// and an "m" for minor.
type key struct {
	pc    int
	minor bool
}

func parseKey(s string) (key, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return key{}, false
	}
	p := chordsym.Split(s)
	pc := chordsym.PitchClass(p.Root)
	if pc < 0 {
		return key{}, false
	}
	q := strings.ToLower(p.Quality)
	minor := q == "m" || q == "min" || q == "-" || strings.HasPrefix(q, "minor")
	return key{pc, minor}, true
}

// flatKeys are the major keys written with flats; a minor key uses its
// relative major's spelling.
var flatKeys = map[int]bool{5: true, 10: true, 3: true, 8: true, 1: true, 6: true}

func (k key) String() string {
	major := k.pc
	if k.minor {
		major = (k.pc + 3) % 12
	}
	name := chordsym.SpellPitchClass(k.pc, flatKeys[major])
	if k.minor {
		return name + "m"
	}
	return name
}

// relative reports whether a and b share a key signature (C and Am).
func relative(a, b key) bool {
	if a.minor == b.minor {
		return a.pc == b.pc
	}
	if a.minor {
		a, b = b, a
	}
	return (a.pc+9)%12 == b.pc
}

// ChordKey estimates the key a chart's chords are written in: each
// candidate key scores the chords that belong to it (a diatonic triad
// counts fully, a diatonic root with another quality, a secondary
// dominant say, partly), and the tonic chord's weight and the first and
// last chords settle a major key against its relative minor.
func ChordKey(chords []string) (string, float64) {
	if len(chords) == 0 {
		return "", 0
	}
	type ch struct {
		pc    int
		minor bool
	}
	var list []ch
	for _, c := range chords {
		p := chordsym.Split(c)
		pc := chordsym.PitchClass(p.Root)
		if pc < 0 {
			continue
		}
		list = append(list, ch{pc, chordsym.DecomposeQuality(p.Quality).Marker == chordsym.MarkerMinor})
	}
	if len(list) == 0 {
		return "", 0
	}
	// Degrees (semitones above the tonic) and their triad being minor,
	// for a major key and a (natural, with a major V) minor key.
	majorDeg := map[int]bool{0: false, 2: true, 4: true, 5: false, 7: false, 9: true, 11: true}
	minorDeg := map[int]bool{0: true, 2: true, 3: false, 5: true, 7: false, 8: false, 10: false}
	best, second := -1.0, -1.0
	var bestKey key
	for tonic := 0; tonic < 12; tonic++ {
		for _, minor := range []bool{false, true} {
			deg := majorDeg
			if minor {
				deg = minorDeg
			}
			s := 0.0
			for i, c := range list {
				d := (c.pc - tonic + 12) % 12
				isMinor, ok := deg[d]
				switch {
				case ok && isMinor == c.minor:
					s += 1
				case ok:
					s += 0.3
				}
				if d == 0 && c.minor == minor {
					s += 0.5
					if i == 0 || i == len(list)-1 {
						s += 1
					}
				}
			}
			if s > best {
				second, best, bestKey = best, s, key{tonic, minor}
			} else if s > second {
				second = s
			}
		}
	}
	conf := 0.0
	if best > 0 {
		conf = (best - second) / best
	}
	return bestKey.String(), conf
}

// KeyReport compares what each source says the song's key is: the chart
// (its "key:" header, or its chords), iReal Pro's chart, and the
// recording itself. Sounding is the chart's key with its capo applied,
// the one the recording should be in.
type KeyReport struct {
	Chart        string   `json:"chart"`
	ChartFrom    string   `json:"chartFrom"` // "header" or "chords"
	Capo         int      `json:"capo"`
	Sounding     string   `json:"sounding"`
	IReal        string   `json:"ireal,omitempty"`
	Audio        string   `json:"audio,omitempty"`
	AudioConf    float64  `json:"audioConfidence,omitempty"`
	AudioAgrees  bool     `json:"audioAgrees"`
	IRealAgrees  bool     `json:"irealAgrees"`
	ShiftToAudio int      `json:"shiftToAudio"` // semitones to transpose the chart by to match the recording, 0 when it does
	Notes        []string `json:"notes,omitempty"`
}

// CheckKey builds a KeyReport for body against an iReal key and an
// audio key (either may be "").
func CheckKey(body, irealKey, audioKey string, audioConf float64) KeyReport {
	sg := song.Parse(body)
	r := KeyReport{IReal: irealKey, Audio: audioKey, AudioConf: audioConf}
	if h := sg.Get("key"); h != "" {
		r.Chart, r.ChartFrom = h, "header"
	} else if k, _ := ChordKey(song.UsedChords(sg)); k != "" {
		r.Chart, r.ChartFrom = k, "chords"
	}
	r.Capo, _ = strconv.Atoi(strings.TrimSpace(sg.Get("capo")))
	ck, ok := parseKey(r.Chart)
	if !ok {
		r.Notes = append(r.Notes, "the chart has no chords to tell its key from")
		return r
	}
	sounding := key{(ck.pc + r.Capo) % 12, ck.minor}
	r.Sounding = sounding.String()
	if ik, ok := parseKey(irealKey); ok {
		r.IRealAgrees = relative(ik, ck) || relative(ik, sounding)
		if !r.IRealAgrees {
			r.Notes = append(r.Notes, "iReal Pro's chart is in "+irealKey+", the chart in "+r.Chart)
		}
	}
	if ak, ok := parseKey(audioKey); ok {
		r.AudioAgrees = relative(ak, sounding)
		if !r.AudioAgrees {
			// The recording's tonic in the chart's own mode.
			target := ak
			if ak.minor != sounding.minor {
				if ak.minor {
					target = key{(ak.pc + 3) % 12, false}
				} else {
					target = key{(ak.pc + 9) % 12, true}
				}
			}
			d := (target.pc - sounding.pc + 12) % 12
			if d > 6 {
				d -= 12
			}
			r.ShiftToAudio = d
			r.Notes = append(r.Notes, "the recording sounds in "+audioKey+", the chart (with its capo) in "+r.Sounding)
		}
		if audioConf < 0.15 {
			r.Notes = append(r.Notes, "the recording's key is uncertain: check by ear")
		}
	}
	return r
}
