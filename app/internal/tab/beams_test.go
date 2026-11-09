package tab

import (
	"reflect"
	"testing"
)

func TestGroupBeams_StraightEighths(t *testing.T) {
	// 8 straight eighths in 4/4 — real engraving (confirmed against
	// alphaTab's ShowWithBars output on this exact case) groups them
	// 2+2+2+2, one beam group per quarter-note pulse, never one
	// continuous beam across the whole bar.
	beats := make([]Beat, 8)
	for i := range beats {
		beats[i] = Beat{Duration: "8", Notes: []Note{{Fret: 3, String: 1}}}
	}
	got := GroupBeams(beats, 4, 4)
	want := []BeamGroup{
		{StartIdx: 0, EndIdx: 1, MinFlags: 1},
		{StartIdx: 2, EndIdx: 3, MinFlags: 1},
		{StartIdx: 4, EndIdx: 5, MinFlags: 1},
		{StartIdx: 6, EndIdx: 7, MinFlags: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GroupBeams = %+v, want %+v", got, want)
	}
}

func TestGroupBeams_BadSpell(t *testing.T) {
	// 4, 8., 16, 8, 8, 16, 16 — the real Bad Spell riff's first bar
	// (Mercy/bad-spell/intro.ly), hand-verified this session.
	beats := []Beat{
		{Duration: "4", Notes: []Note{{Fret: 3, String: 6}}},
		{Duration: "8.", Notes: []Note{{Fret: 3, String: 2}, {Fret: 3, String: 1}}},
		{Duration: "16", Notes: []Note{{Fret: 3, String: 6}}},
		{Duration: "8", Notes: []Note{{Fret: 6, String: 6}}},
		{Duration: "8", Notes: []Note{{Fret: 3, String: 5}}},
		{Duration: "16", Notes: []Note{{Fret: 3, String: 4}}},
		{Duration: "16", Notes: []Note{{Fret: 5, String: 4}}},
	}
	got := GroupBeams(beats, 4, 4)
	want := []BeamGroup{
		{StartIdx: 1, EndIdx: 2, MinFlags: 1}, // dotted-8th + 16th fill the 2nd quarter
		{StartIdx: 3, EndIdx: 4, MinFlags: 1}, // two 8ths in the 3rd quarter
		{StartIdx: 5, EndIdx: 6, MinFlags: 2}, // two 16ths in the 4th quarter
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GroupBeams = %+v, want %+v", got, want)
	}
}

func TestGroupBeams_CompoundTime(t *testing.T) {
	// 6/8: two dotted-quarter cells, 3 eighths each — beams 3+3, not 6.
	beats := make([]Beat, 6)
	for i := range beats {
		beats[i] = Beat{Duration: "8", Notes: []Note{{Fret: 1, String: 1}}}
	}
	got := GroupBeams(beats, 6, 8)
	want := []BeamGroup{
		{StartIdx: 0, EndIdx: 2, MinFlags: 1},
		{StartIdx: 3, EndIdx: 5, MinFlags: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GroupBeams = %+v, want %+v", got, want)
	}
}

func TestGroupBeams_RestBreaksGroup(t *testing.T) {
	beats := []Beat{
		{Duration: "16", Notes: []Note{{Fret: 1, String: 1}}},
		{Rest: true, Duration: "16"},
		{Duration: "16", Notes: []Note{{Fret: 1, String: 1}}},
		{Duration: "16", Notes: []Note{{Fret: 1, String: 1}}},
	}
	got := GroupBeams(beats, 4, 4)
	want := []BeamGroup{
		{StartIdx: 2, EndIdx: 3, MinFlags: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GroupBeams = %+v, want %+v", got, want)
	}
}
