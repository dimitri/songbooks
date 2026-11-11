package tab

import (
	"strings"
	"testing"
)

// A bar of triplet eighths fills a 4/4 bar exactly once the tuplet ratio
// is applied, and a grace note takes no time; without either the same
// beats read as overfull.
func TestTupletJSON(t *testing.T) {
	src := `{"Tuning":[64,59,55,50,45,40],"MinimumFret":0,"Bars":[{"Beats":[
		{"Duration":"8","TupletNum":3,"TupletDen":2,"Notes":[{"MIDI":67,"String":1,"Fret":3}]},
		{"Duration":"8","TupletNum":3,"TupletDen":2,"Notes":[{"MIDI":69,"String":1,"Fret":5}]},
		{"Duration":"8","TupletNum":3,"TupletDen":2,"Notes":[{"MIDI":67,"String":1,"Fret":3}]},
		{"Duration":"8","Grace":true,"Notes":[{"MIDI":62,"String":2,"Fret":3}]},
		{"Duration":"4.","Notes":[{"MIDI":64,"String":1,"Fret":0}]},
		{"Duration":"16","TupletNum":5,"TupletDen":4,"Rest":true},
		{"Duration":"16","TupletNum":5,"TupletDen":4,"Rest":true},
		{"Duration":"16","TupletNum":5,"TupletDen":4,"Rest":true},
		{"Duration":"16","TupletNum":5,"TupletDen":4,"Rest":true},
		{"Duration":"16","TupletNum":5,"TupletDen":4,"Rest":true},
		{"Duration":"8","Rest":true}
	]}]}`
	riff, err := DecodeScoreSource(src)
	if err != nil {
		t.Fatalf("DecodeScoreSource: %v", err)
	}
	if got := BarBeats(riff.Bars[0]); got < 4-1e-9 || got > 4+1e-9 {
		t.Fatalf("BarBeats = %v, want 4", got)
	}
	tex := ToAlphaTex(riff, "", nil)
	for _, want := range []string{"3.1.8{tu 3}", "3.2.8{gr}", "0.1.4{d}", "r.16{tu (5 4)}"} {
		if !strings.Contains(tex, want) {
			t.Errorf("alphaTeX missing %q:\n%s", want, tex)
		}
	}
}

// LilyPond's \tuplet 3/2 { ... } keeps its ratio on every beat inside it.
func TestTupletLilyPond(t *testing.T) {
	riff, err := ParseScore("\\new TabStaff {\n  \\relative {\n    \\tuplet 3/2 { e8 g a } b4 e,2\n  }\n}")
	if err != nil {
		t.Fatalf("ParseScore: %v", err)
	}
	if len(riff.Bars) != 1 {
		t.Fatalf("bars: got %d, want 1 (the triplet fills one beat)", len(riff.Bars))
	}
	for i, b := range riff.Bars[0].Beats[:3] {
		if b.TupletNum != 3 || b.TupletDen != 2 {
			t.Errorf("beat %d tuplet = %d:%d, want 3:2", i, b.TupletNum, b.TupletDen)
		}
	}
	if b := riff.Bars[0].Beats[3]; b.TupletNum != 0 {
		t.Errorf("beat after the tuplet carries %d:%d", b.TupletNum, b.TupletDen)
	}
}
