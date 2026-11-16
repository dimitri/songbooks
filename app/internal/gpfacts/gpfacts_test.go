package gpfacts

import (
	"reflect"
	"testing"
)

func TestParseRef(t *testing.T) {
	cases := []struct {
		spec string
		want Ref
		err  bool
	}{
		{"9-12", Ref{Ranges: [][2]int{{9, 12}}}, false},
		{"9-12 staff", Ref{Ranges: [][2]int{{9, 12}}, Notation: "staff"}, false},
		{"bars 1-4, 9-12 Staff+Tab", Ref{Ranges: [][2]int{{1, 4}, {9, 12}}, Notation: "tab+staff"}, false},
		{"7", Ref{Ranges: [][2]int{{7, 7}}}, false},
		{"tab", Ref{Notation: "tab"}, true},
		{"12-9", Ref{}, true},
		{"verse", Ref{}, true},
	}
	for _, c := range cases {
		got, err := ParseRef("", c.spec)
		if (err != nil) != c.err {
			t.Errorf("%q: err = %v", c.spec, err)
			continue
		}
		if !c.err && !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q = %+v, want %+v", c.spec, got, c.want)
		}
	}
	if s := (Ref{Ranges: [][2]int{{1, 4}, {9, 9}}, Notation: "tab"}).String(); s != "1-4, 9 tab" {
		t.Errorf("String = %q", s)
	}
}

func TestFindTrackAndPlayed(t *testing.T) {
	f := &Facts{
		Tracks: []Track{{N: 1, Name: "Guitare Rythmique", Short: "rhy"}, {N: 2, Name: "Basse"}},
		Played: []int{1, 2, 3, 2, 3, 4},
	}
	for label, want := range map[string]int{"guitare rythmique": 0, "RHY": 0, "#2": 1, "basse": 1, "#3": -1, "Drums": -1} {
		if got := f.FindTrack(label); got != want {
			t.Errorf("FindTrack(%q) = %d, want %d", label, got, want)
		}
	}
	if got := f.PlayedBars(Ref{Ranges: [][2]int{{2, 3}}}); !reflect.DeepEqual(got, []int{2, 3, 2, 3}) {
		t.Errorf("PlayedBars = %v", got)
	}
}

func TestKeyName(t *testing.T) {
	for k, want := range map[KeySig]string{
		{Sig: 0}: "C", {Sig: 1}: "G", {Sig: 4}: "E", {Sig: -2}: "Bb", {Sig: -3, Minor: true}: "Cm",
		{Sig: 0, Minor: true}: "Am", {Sig: 6}: "F#", {Sig: -6}: "Gb",
	} {
		if got := KeyName(k); got != want {
			t.Errorf("KeyName(%+v) = %q, want %q", k, got, want)
		}
	}
}

func TestChordName(t *testing.T) {
	for in, want := range map[string]string{"E7/9#": "E7#9", "Eb7/9b": "Eb7b9", "C7M": "Cmaj7", "D9/A": "D9/A", "Ab6/9": "Ab6/9", "A7": "A7"} {
		if got := ChordName(in); got != want {
			t.Errorf("ChordName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDiagramSpec(t *testing.T) {
	cases := []struct {
		d    Diagram
		want string
	}{
		// A7 from the high string: e3 B2 G2 D2 A0 Ex, barre at 2.
		{Diagram{Frets: []int{3, 2, 2, 2, 0, -1}, Barres: []int{2}}, "XO(2223)"},
		// D9 at the 9th fret: e-x B10 G9 D10 A9 E-x... written high first.
		{Diagram{Frets: []int{-1, 10, 9, 10, 9, -1}, Barres: []int{9}}, "9:X(1212)X"},
		{Diagram{Frets: []int{0, 1, 0, 2, 3, -1}}, "X32O1O"},
		{Diagram{Frets: []int{-1, -1, -1, -1, -1, -1}}, ""},
	}
	for _, c := range cases {
		got, _ := c.d.Spec()
		if got != c.want {
			t.Errorf("Spec(%v) = %q, want %q", c.d.Frets, got, c.want)
		}
	}
}
