package songimport

import (
	"strings"
	"testing"
)

func TestChordKey(t *testing.T) {
	for _, c := range []struct {
		chords []string
		want   string
	}{
		{[]string{"C", "Em", "Am", "F", "C", "G", "F", "G", "Am", "F", "C", "G", "C"}, "C"},
		{[]string{"Am", "G", "F", "E7", "Am"}, "Am"},
		{[]string{"Bm", "F#", "A", "E", "G", "D", "Em", "F#"}, "Bm"},
		{[]string{"F", "Bb", "C7", "F"}, "F"},
		{[]string{"Eb", "Ab", "Bb7", "Cm", "Eb"}, "Eb"},
	} {
		if got, _ := ChordKey(c.chords); got != c.want {
			t.Errorf("ChordKey(%v) = %s, want %s", c.chords, got, c.want)
		}
	}
}

func TestCheckKey(t *testing.T) {
	body := "title: T\ncapo: 2\n\n[Verse]\ngrid: C | G | Am | F\n"
	r := CheckKey(body, "D", "D", 0.6)
	if r.Chart != "C" || r.ChartFrom != "chords" || r.Sounding != "D" || !r.AudioAgrees || !r.IRealAgrees || r.ShiftToAudio != 0 {
		t.Fatalf("capo: %+v", r)
	}
	// The chart in C with no capo, the recording in D.
	r = CheckKey("title: T\n\n[Verse]\ngrid: C | G | Am | F\n", "", "D", 0.6)
	if r.AudioAgrees || r.ShiftToAudio != 2 {
		t.Fatalf("shift: %+v", r)
	}
	// A relative minor is the same key signature.
	r = CheckKey("title: T\nkey: C\n\n[Verse]\ngrid: C | G\n", "", "Am", 0.6)
	if !r.AudioAgrees || r.ChartFrom != "header" {
		t.Fatalf("relative: %+v", r)
	}
	// Shift in the chart's own mode: C chart, recording in Bbm → Db, +1.
	r = CheckKey("title: T\nkey: C\n\n[Verse]\ngrid: C\n", "", "Bbm", 0.6)
	if r.ShiftToAudio != 1 {
		t.Fatalf("mode: %+v", r)
	}
}

func TestSetHeader(t *testing.T) {
	body := "title: T\nartist: A\n\n[Verse]\ngrid: C\n"
	got := SetHeader(body, "tempo", "110", false)
	if !strings.HasPrefix(got, "title: T\nartist: A\ntempo: 110\n\n[Verse]") {
		t.Fatalf("add: %q", got)
	}
	if SetHeader(got, "tempo", "90", false) != got {
		t.Fatal("kept without overwrite")
	}
	if !strings.Contains(SetHeader(got, "tempo", "90", true), "tempo: 90\n") {
		t.Fatal("overwrite")
	}
	if !strings.Contains(SetHeader("title: T\ntime:\n\n[V]\n", "time", "3/4", false), "time: 3/4\n") {
		t.Fatal("an empty value is filled")
	}
	// No blank line between header and first section.
	if got := SetHeader("title: T\n[Verse]\n", "time", "4/4", false); got != "title: T\ntime: 4/4\n[Verse]\n" {
		t.Fatalf("no blank: %q", got)
	}
}

func TestBeatsPerBar(t *testing.T) {
	for sig, want := range map[string]int{"4/4": 4, "3/4": 3, "6/8": 2, "12/8": 4, "9/8": 3, "3/8": 3, "": 4, "5/4": 5} {
		if got := beatsPerBar(sig); got != want {
			t.Errorf("beatsPerBar(%q) = %d, want %d", sig, got, want)
		}
	}
}

func TestSameTitle(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"This Love", "So This Is Love", false},
		{"This Love", "this love", true},
		{"Girl from Ipanema, The", "The Girl From Ipanema", true},
		{"Ça c'est vraiment toi", "Ca c'est vraiment toi", true},
		{"Spain", "Spain (Chick Corea)", false},
	} {
		if got := sameTitle(c.a, c.b); got != c.same {
			t.Errorf("sameTitle(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
