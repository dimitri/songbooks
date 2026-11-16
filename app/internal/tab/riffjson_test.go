package tab

import (
	"encoding/json"
	"errors"
	"testing"
)

// riffFromJSON builds a Riff for a test from Go's JSON encoding of it.
func riffFromJSON(t *testing.T, src string) *Riff {
	t.Helper()
	var r Riff
	if err := json.Unmarshal([]byte(src), &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

func TestParseVoice_RiffJSONIsRetired(t *testing.T) {
	_, err := ParseVoice("Guitar", `{"Tuning":[64,59,55,50,45,40],"Bars":[{"Beats":[{"Duration":"4","Rest":true}]}]}`)
	if !errors.Is(err, ErrRiffJSON) {
		t.Fatalf("err = %v, want ErrRiffJSON", err)
	}
}

func TestParseVoice_LilyPondBracesStillWork(t *testing.T) {
	riff, err := ParseVoice("guitar", "{ c'4 d' e' f' }")
	if err != nil {
		t.Fatalf("ParseVoice: %v", err)
	}
	if len(riff.Bars) != 1 {
		t.Fatalf("bars = %d", len(riff.Bars))
	}
}
