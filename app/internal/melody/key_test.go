package melody

import (
	"testing"

	"app/internal/audioanalysis"
)

// TestGuessKey: the key from the melody's notes and the chart's chords:
// a plain major and minor song, the relative minor told apart by its
// chords and its last chord, a recording tuned a little flat, and a song
// with no chords (the melody alone).
func TestGuessKey(t *testing.T) {
	sing := func(pitches ...float64) []audioanalysis.SungNote {
		var n []audioanalysis.SungNote
		for i, p := range pitches {
			n = append(n, audioanalysis.SungNote{Start: float64(i), End: float64(i) + 0.8, Pitch: p, Conf: 0.9})
		}
		return n
	}
	cases := []struct {
		name   string
		notes  []audioanalysis.SungNote
		chords []string
		want   string
	}{
		{"C major", sing(60, 62, 64, 65, 67, 69, 67, 64, 62, 60), []string{"C", "", "", "", "F", "", "G7", "", "C", ""}, "C"},
		{"A minor", sing(69, 72, 71, 69, 67, 64, 65, 64, 69), []string{"Am", "", "Dm", "", "E7", "", "Am", ""}, "Am"},
		{"E minor, tuned 40 cents flat", sing(63.6, 66.6, 70.6, 68.6, 66.6, 65.6, 63.6, 61.6, 63.6), []string{"Em", "C", "D", "Em"}, "Em"},
		{"Bb, flats", sing(70, 72, 74, 75, 77, 74, 70), []string{"Bb", "Eb", "F7", "Bb"}, "Bb"},
		{"no chords: the melody alone", sing(62, 64, 66, 67, 69, 71, 73, 74, 69, 66, 62), nil, "D"},
	}
	for _, c := range cases {
		ev := ChordEvidence(c.chords)
		ev.Notes = NotePitchClasses(c.notes)
		g, ok := GuessKey(ev)
		if !ok || g.Name(false) != c.want {
			t.Errorf("%s: %s (%v), want %s", c.name, g.Name(false), ok, c.want)
		}
	}
	if _, ok := GuessKey(KeyEvidence{First: -1, Last: -1}); ok {
		t.Error("a key guessed from nothing")
	}
}

func TestChordTones(t *testing.T) {
	for sym, want := range map[string][]int{
		"C":     {0, 4, 7},
		"Am":    {9, 0, 4},
		"G7":    {7, 11, 2, 5},
		"Fmaj7": {5, 9, 0, 4},
		"Bm7b5": {11, 2, 5, 9},
		"Dsus4": {2, 7, 9},
		"C/E":   {0, 4, 7},
		"F#m7":  {6, 9, 1, 4},
		"Bb6":   {10, 2, 5, 7},
		"Edim7": {4, 7, 10, 1},
		"Caug":  {0, 4, 8},
		"E♭△7":  {3, 7, 10, 2},
	} {
		tones, root, _ := chordTones(sym)
		if root < 0 || len(tones) != len(want) {
			t.Errorf("%s: %v", sym, tones)
			continue
		}
		for _, pc := range want {
			if tones[pc] == 0 {
				t.Errorf("%s: no %d in %v", sym, pc, tones)
			}
		}
	}
	for _, sym := range []string{"N.C.", "%", "", "x"} {
		if _, root, _ := chordTones(sym); root >= 0 {
			t.Errorf("%q read as a chord", sym)
		}
	}
}

func TestKeyName(t *testing.T) {
	for in, want := range map[string]string{"G": "G", "Em": "Em", "Bb": "Bb", "F#m": "F#m", "Gb": "Gb", "Ebm": "Ebm", "C": "C", "Db": "Db"} {
		k, _ := KeyOf(in, 0, false)
		if got := KeyName(k, false); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
	if !SameSignature(6, -6) || SameSignature(1, -1) {
		t.Error("SameSignature")
	}
}
