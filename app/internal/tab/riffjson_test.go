package tab

import "testing"

func TestDecodeScoreSource_JSON(t *testing.T) {
	src := `{"Tuning":[64,59,55,50,45,40],"MinimumFret":0,"Bars":[{"Beats":[
		{"Duration":"4","Notes":[{"MIDI":64,"String":1,"Fret":0}]},
		{"Duration":"4","Notes":[{"MIDI":67,"String":1,"Fret":3}]}
	]}]}`
	riff, err := DecodeScoreSource(src)
	if err != nil {
		t.Fatalf("DecodeScoreSource: %v", err)
	}
	if len(riff.Tuning) != 6 {
		t.Fatalf("tuning: got %d strings, want 6", len(riff.Tuning))
	}
	if len(riff.Bars) != 1 || len(riff.Bars[0].Beats) != 2 {
		t.Fatalf("bars/beats: %+v", riff.Bars)
	}
	if riff.Bars[0].Beats[1].Notes[0].Fret != 3 {
		t.Fatalf("fret not preserved: got %d, want 3", riff.Bars[0].Beats[1].Notes[0].Fret)
	}
}

func TestDecodeScoreSource_LilyPondStillWorks(t *testing.T) {
	src := "\\new TabStaff {\n  \\relative {\n    e4 g b e'\n  }\n}"
	riff, err := DecodeScoreSource(src)
	if err != nil {
		t.Fatalf("DecodeScoreSource: %v", err)
	}
	if len(riff.Bars) == 0 {
		t.Fatalf("no bars parsed")
	}
}
