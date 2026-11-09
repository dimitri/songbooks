package tab

import (
	"os"
	"strings"
	"testing"
)

func TestToAlphaTex_BadSpell(t *testing.T) {
	data, err := os.ReadFile("/Users/dim/dev/songbooks/Mercy/bad-spell/intro.ly")
	if err != nil {
		t.Skip("legacy corpus not available:", err)
	}
	riff, err := ParseScore(string(data))
	if err != nil {
		t.Fatal(err)
	}
	tex := ToAlphaTex(riff, "", []string{"Dm", "", "G"})
	if !strings.HasPrefix(tex, "\\tuning (") {
		t.Errorf("tex does not start with \\tuning: %s", tex)
	}
	if !strings.Contains(tex, "|") {
		t.Errorf("tex has no bar separators: %s", tex)
	}
	t.Logf("alphaTex:\n%s", tex)
}

func TestToAlphaTexBass_Kawasaki(t *testing.T) {
	data, err := os.ReadFile("/Users/dim/dev/songbooks/Other Lies/2-Surfacing/06-Kawasaki/bass.ly")
	if err != nil {
		t.Skip("legacy corpus not available:", err)
	}
	riff, err := ParseScore(string(data))
	if err != nil {
		t.Fatal(err)
	}
	tex := ToAlphaTexBass(riff, nil)
	if !strings.Contains(tex, "\\clef F4") {
		t.Errorf("expected \\clef F4 for a %d-string riff, got: %s", len(riff.Tuning), tex)
	}
	t.Logf("alphaTex:\n%s", tex)
}

func TestSplitDuration(t *testing.T) {
	cases := []struct {
		in     string
		dur    string
		dotted bool
	}{
		{"4", "4", false},
		{"4.", "4", true},
		{"16", "16", false},
		{"2.", "2", true},
	}
	for _, c := range cases {
		dur, dotted := splitDuration(c.in)
		if dur != c.dur || dotted != c.dotted {
			t.Errorf("splitDuration(%q) = (%q, %v), want (%q, %v)", c.in, dur, dotted, c.dur, c.dotted)
		}
	}
}

func TestWriteBeat_Chord(t *testing.T) {
	var b strings.Builder
	writeBeat(&b, Beat{Duration: "8", Notes: []Note{{Fret: 3, String: 2}, {Fret: 3, String: 1}}}, "")
	if got, want := b.String(), "(3.2 3.1).8"; got != want {
		t.Errorf("writeBeat chord = %q, want %q", got, want)
	}
}

func TestWriteBeat_Rest(t *testing.T) {
	var b strings.Builder
	writeBeat(&b, Beat{Rest: true, Duration: "16"}, "")
	if got, want := b.String(), "r.16"; got != want {
		t.Errorf("writeBeat rest = %q, want %q", got, want)
	}
}

func TestWriteBeat_ChordLabel(t *testing.T) {
	var b strings.Builder
	writeBeat(&b, Beat{Duration: "4.", Notes: []Note{{Fret: 3, String: 6}}}, "Dm")
	if got, want := b.String(), `3.6.4{d ch "Dm"}`; got != want {
		t.Errorf("writeBeat with label = %q, want %q", got, want)
	}
}

func TestMidiToAlphaTexNote(t *testing.T) {
	cases := map[int]string{60: "C4", 64: "E4", 69: "A4", 28: "E1", 43: "G2"}
	for midi, want := range cases {
		if got := midiToAlphaTexNote(midi); got != want {
			t.Errorf("midiToAlphaTexNote(%d) = %q, want %q", midi, got, want)
		}
	}
}

func TestToAlphaTex_HammerPull(t *testing.T) {
	data, err := os.ReadFile("/Users/dim/dev/songbooks/Mercy/always-free/verse.ly")
	if err != nil {
		t.Skip("legacy corpus not available:", err)
	}
	riff, err := ParseScore(string(data))
	if err != nil {
		t.Fatal(err)
	}
	tex := ToAlphaTex(riff, "", nil)
	if !strings.Contains(tex, "{h}") {
		t.Errorf("expected a {h} hammer-on/pull-off marker in alphaTex, got:\n%s", tex)
	}
	t.Logf("alphaTex:\n%s", tex)
}

func TestToAlphaTex_Slide(t *testing.T) {
	data, err := os.ReadFile("/Users/dim/dev/songbooks/Mercy/whiskey-beer-and-wine/intro.ly")
	if err != nil {
		t.Skip("legacy corpus not available:", err)
	}
	riff, err := ParseScore(string(data))
	if err != nil {
		t.Fatal(err)
	}
	tex := ToAlphaTex(riff, "", nil)
	if !strings.Contains(tex, "{sl}") || !strings.Contains(tex, "{gr}") {
		t.Errorf("expected {sl}/{gr} slide markers in alphaTex, got:\n%s", tex)
	}
	t.Logf("alphaTex:\n%s", tex)
}

func TestToAlphaTex_BendAndPalmMute(t *testing.T) {
	src := `
\new TabStaff {
  \set TabStaff.minimumFret = 15
  \relative {
    e''8^"full" e8^"PM" e4
  }
}
`
	riff, err := ParseScore(src)
	if err != nil {
		t.Fatal(err)
	}
	tex := ToAlphaTex(riff, "", nil)
	if !strings.Contains(tex, "{b 4}") {
		t.Errorf("expected {b 4} (full bend = 4 quarter-tones) in alphaTex, got:\n%s", tex)
	}
	if !strings.Contains(tex, "{pm}") {
		t.Errorf("expected {pm} in alphaTex, got:\n%s", tex)
	}
	t.Logf("alphaTex:\n%s", tex)
}
