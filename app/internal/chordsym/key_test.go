package chordsym

import "testing"

func TestParseKey(t *testing.T) {
	cases := []struct {
		in    string
		root  string
		minor bool
		ok    bool
	}{
		{"A", "A", false, true},
		{"F#m", "F#", true, true},
		{"Bb", "Bb", false, true},
		{"C minor", "C", true, true},
		{"Ebmaj", "Eb", false, true},
		{"", "", false, false},
		{"D dorian", "", false, false},
		{"H", "", false, false},
	}
	for _, c := range cases {
		k, ok := ParseKey(c.in)
		if ok != c.ok || (ok && (k.Root != c.root || k.Minor != c.minor)) {
			t.Errorf("ParseKey(%q) = %+v, %v; want root %q minor %v ok %v", c.in, k, ok, c.root, c.minor, c.ok)
		}
	}
	if k, _ := ParseKey("A minor"); k.String() != "A minor" {
		t.Errorf("String() = %q, want the key as written", k.String())
	}
}

func TestSameKey(t *testing.T) {
	if !SameKey("A#m", "Bbm") || !SameKey("Gb", "F#") || SameKey("A", "Am") || SameKey("A", "") {
		t.Error("SameKey compares tonic pitch class and mode")
	}
}

// TestSpellInKey checks the chart spelling of each pitch class: diatonic
// notes on their own letter, borrowed chords as flats on the usual
// degrees, and no B#/E#/Cb/Fb or double accidentals.
func TestSpellInKey(t *testing.T) {
	cases := []struct {
		key  string
		want [12]string // C, C#/Db, D, ... B
	}{
		{"C", [12]string{"C", "Db", "D", "Eb", "E", "F", "F#", "G", "Ab", "A", "Bb", "B"}},
		{"G", [12]string{"C", "C#", "D", "Eb", "E", "F", "F#", "G", "Ab", "A", "Bb", "B"}},
		{"E", [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}},
		{"F", [12]string{"C", "Db", "D", "Eb", "E", "F", "Gb", "G", "Ab", "A", "Bb", "B"}},
		{"Ab", [12]string{"C", "Db", "D", "Eb", "E", "F", "Gb", "G", "Ab", "A", "Bb", "B"}},
		{"F#", [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}},
		{"Am", [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "Bb", "B"}},
		{"Dm", [12]string{"C", "C#", "D", "Eb", "E", "F", "F#", "G", "Ab", "A", "Bb", "B"}},
		{"Ebm", [12]string{"C", "Db", "D", "Eb", "E", "F", "Gb", "G", "Ab", "A", "Bb", "B"}},
	}
	for _, c := range cases {
		k, ok := ParseKey(c.key)
		if !ok {
			t.Fatalf("ParseKey(%q) failed", c.key)
		}
		for pc, want := range c.want {
			if got := k.Spell(pc, false); got != want {
				t.Errorf("key %s: Spell(%d) = %q, want %q", c.key, pc, got, want)
			}
		}
	}
	// C and Am have no accidentals of their own: "flats: yes" puts them on
	// the flat side, so the tritone reads as a b5.
	c, _ := ParseKey("C")
	if got := c.Spell(6, true); got != "Gb" {
		t.Errorf("C with flats: Spell(6) = %q, want Gb", got)
	}
}

func TestIsChord(t *testing.T) {
	for _, s := range []string{"A", "Am7", "D/F#", "Bbmaj7(#11)", "C6/9", "F#m7b5", "Esus4", "G7#9", "Cadd9", "Bo7", "E5", "Abm(maj7)"} {
		if !IsChord(s) {
			t.Errorf("IsChord(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "N.C.", "%", ".", "R", "Bridge", "Break", "Coda", "Chorus", "Fade", "Am I", "x2", "(1.)", "{2/4}", "Bass"} {
		if IsChord(s) {
			t.Errorf("IsChord(%q) = true, want false", s)
		}
	}
}

func TestTransposeInKey(t *testing.T) {
	g, _ := ParseKey("G")
	ab, _ := ParseKey("Ab")
	em, _ := ParseKey("Em")
	cases := []struct {
		sym  string
		n    int
		to   Key
		want string
	}{
		{"A", -2, g, "G"},
		{"C#m7", -2, g, "Bm7"},
		{"D/F#", -2, g, "C/E"},
		{"E7", -2, g, "D7"},
		{"F#m", -1, ab, "Fm"},
		{"C#m", -1, ab, "Cm"},
		{"D/F#", -1, ab, "Db/F"},
		{"Am", -5, em, "Em"},
		{"E7", -5, em, "B7"},
		{"Dm/F", -5, em, "Am/C"},
		{"Bridge", 3, g, "Bridge"},
		{"N.C.", 3, g, "N.C."},
	}
	for _, c := range cases {
		if got, _ := TransposeInKey(c.sym, c.n, c.to, false); got != c.want {
			t.Errorf("TransposeInKey(%q, %d, %s) = %q, want %q", c.sym, c.n, c.to, got, c.want)
		}
	}
}

func TestSameChord(t *testing.T) {
	if !SameChord("A#m7", "Bbm7") || !SameChord("D/F#", "D/Gb") || SameChord("Am", "A") || SameChord("D/F#", "D") {
		t.Error("SameChord compares root, quality and bass by pitch")
	}
}
