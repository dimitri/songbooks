package tab

import "testing"

// TestParseVoiceBracedLilyPond: "{ c'4 d' }" is LilyPond, not broken JSON.
func TestParseVoiceBracedLilyPond(t *testing.T) {
	riff, err := ParseVoice("guitar", "{ c'4 d'4 e'4 f'4 }")
	if err != nil {
		t.Fatalf("braced LilyPond voice: %v", err)
	}
	if len(riff.Bars) != 1 || len(riff.Bars[0].Beats) != 4 {
		t.Fatalf("want 1 bar of 4 beats, got %+v", riff.Bars)
	}
}

// TestDecodeDrumRiffJSON: a drum riff has no tuning and must still decode.
func TestDecodeDrumRiffJSON(t *testing.T) {
	riff, err := ParseVoice("drums", `{"Drums":true,"Bars":[{"Beats":[{"Duration":"1","Notes":[{"MIDI":36}]}]}]}`)
	if err != nil {
		t.Fatalf("drum riff JSON: %v", err)
	}
	if !riff.Drums || len(riff.Bars) != 1 {
		t.Fatalf("unexpected riff %+v", riff)
	}
}
