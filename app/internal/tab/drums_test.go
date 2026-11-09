package tab

import (
	"strings"
	"testing"
)

func TestParseDrumStaff_BasicGroove(t *testing.T) {
	src := `\new DrumStaff {
  \drummode {
    bd4 hh8 hh8 sn4 hh8 hh8
    bd4 hh8 bd8 sn4 hh8 hh8
  }
}`
	riff, err := ParseScore(src)
	if err != nil {
		t.Fatalf("ParseScore: %v", err)
	}
	if !riff.Drums {
		t.Fatalf("expected Drums riff")
	}
	if len(riff.Bars) != 2 {
		t.Fatalf("expected 2 bars, got %d", len(riff.Bars))
	}
	if len(riff.Bars[0].Beats) != 6 { // bd4 hh8 hh8 sn4 hh8 hh8 = 6 note events summing to 4 quarter-beats
		t.Fatalf("expected 6 beat events in bar 1, got %d", len(riff.Bars[0].Beats))
	}
	if riff.Bars[0].Beats[0].Notes[0].MIDI != 36 { // bd
		t.Errorf("beat 1: expected kick (36), got %d", riff.Bars[0].Beats[0].Notes[0].MIDI)
	}
	if riff.Bars[0].Beats[3].Notes[0].MIDI != 38 { // sn (index 3: bd hh hh [sn] hh hh)
		t.Errorf("beat 4: expected snare (38), got %d", riff.Bars[0].Beats[3].Notes[0].MIDI)
	}
}

func TestParseDrumStaff_Chord(t *testing.T) {
	// A kick and a closed hi-hat on the same beat.
	src := `\new DrumStaff { \drummode { <bd hh>4 sn4 <bd hh>4 sn4 } }`
	riff, err := ParseScore(src)
	if err != nil {
		t.Fatalf("ParseScore: %v", err)
	}
	if len(riff.Bars[0].Beats[0].Notes) != 2 {
		t.Fatalf("expected a 2-note chord, got %d notes", len(riff.Bars[0].Beats[0].Notes))
	}
}

func TestToAlphaTexDrums(t *testing.T) {
	src := `\new DrumStaff { \drummode { bd4 hh8 hh8 sn4 hh8 hh8 } }`
	riff, err := ParseScore(src)
	if err != nil {
		t.Fatalf("ParseScore: %v", err)
	}
	tex := ToAlphaTexDrums(riff, nil)
	for _, want := range []string{
		`\track "Drums" { instrument percussion multiBarRest }`,
		`\articulation (Kick 36)`,
		`\articulation (HiHatClosed 42)`,
		`\articulation (Snare 38)`,
		`Kick.4`,
		`HiHatClosed.8`,
		`Snare.4`,
	} {
		if !strings.Contains(tex, want) {
			t.Errorf("ToAlphaTexDrums output missing %q; got:\n%s", want, tex)
		}
	}
}

func TestToAlphaTexDrums_ChordLabels(t *testing.T) {
	src := `\new DrumStaff { \drummode { bd4 hh8 hh8 sn4 hh8 hh8 bd4 hh8 hh8 sn4 hh8 hh8 } }`
	riff, err := ParseScore(src)
	if err != nil {
		t.Fatalf("ParseScore: %v", err)
	}
	tex := ToAlphaTexDrums(riff, []string{"Dm", "G7"})
	if !strings.Contains(tex, `Kick.4{ch "Dm"}`) {
		t.Errorf("expected bar 1's first beat to carry the Dm chord label; got:\n%s", tex)
	}
	if !strings.Contains(tex, `Kick.4{ch "G7"}`) {
		t.Errorf("expected bar 2's first beat to carry the G7 chord label; got:\n%s", tex)
	}
	// Only the bar's FIRST beat gets the label — a second "HiHatClosed.8"
	// right after the labeled "Kick.4" must not also carry one.
	if strings.Contains(tex, `HiHatClosed.8{ch`) {
		t.Errorf("a non-first beat should never carry a chord label; got:\n%s", tex)
	}
}

// A full multi-track arrangement for one section — drums, bass, and two
// guitars in the same "score:" source, exactly the shape a band would
// write for one chorus. Exercises ParseScoreTracks end to end.
func TestParseScoreTracks_MultiInstrument(t *testing.T) {
	src := `<<
  \new DrumStaff { \drummode { bd4 hh8 hh8 sn4 hh8 hh8 bd4 hh8 bd8 sn4 hh8 hh8 } }
  \new TabStaff = "Bass" \with { stringTunings = #bass-tuning } {
    \relative { e,4 e e e e e e e }
  }
  \new TabStaff = "Guitar 1" {
    \relative { <e, b e>4 <e b e> <e b e> <e b e> }
  }
  \new TabStaff {
    \relative { e'8 g e g e g e g }
  }
>>`
	tracks, err := ParseScoreTracks(src)
	if err != nil {
		t.Fatalf("ParseScoreTracks: %v", err)
	}
	if len(tracks) != 4 {
		t.Fatalf("expected 4 tracks, got %d", len(tracks))
	}
	names := make([]string, len(tracks))
	for i, tr := range tracks {
		names[i] = tr.Name
	}
	want := []string{"drums", "Bass", "Guitar 1", "guitar"}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("track %d: expected name %q, got %q (all names: %v)", i, w, names[i], names)
		}
	}
	if !tracks[0].Riff.Drums {
		t.Errorf("track 0 (drums) should have Riff.Drums == true")
	}
	if len(tracks[1].Riff.Tuning) != 4 {
		t.Errorf("track 1 (Bass) should have a 4-string tuning, got %d strings", len(tracks[1].Riff.Tuning))
	}
}

func TestParseVoice_DrumShortcut(t *testing.T) {
	riff, err := ParseVoice("drum", "bd4 hh8 hh8 sn4 hh8 hh8")
	if err != nil {
		t.Fatalf("ParseVoice: %v", err)
	}
	if !riff.Drums {
		t.Errorf("expected a drum riff")
	}
	if riff.Bars[0].Beats[0].Notes[0].MIDI != 36 {
		t.Errorf("expected first beat to be a kick (36), got %d", riff.Bars[0].Beats[0].Notes[0].MIDI)
	}
}

func TestParseVoice_BassShortcut(t *testing.T) {
	riff, err := ParseVoice("bass", "d,4 d d d d d d d")
	if err != nil {
		t.Fatalf("ParseVoice: %v", err)
	}
	if riff.Drums {
		t.Errorf("expected a melodic riff, not drums")
	}
	if len(riff.Tuning) != 4 {
		t.Errorf("expected 4-string bass tuning, got %d strings", len(riff.Tuning))
	}
	if len(riff.Bars) != 2 { // 8 quarter notes at 4/4 = 2 bars
		t.Errorf("expected 2 bars, got %d", len(riff.Bars))
	}
}

func TestParseVoice_DefaultGuitarTuning(t *testing.T) {
	riff, err := ParseVoice("guitar-1", "<e, b e>4 <e b e> <e b e> <e b e>")
	if err != nil {
		t.Fatalf("ParseVoice: %v", err)
	}
	if len(riff.Tuning) != 6 {
		t.Errorf("expected standard 6-string guitar tuning for an unrecognized label, got %d strings", len(riff.Tuning))
	}
}

func TestParseVoice_GPImportedJSONRiff(t *testing.T) {
	// The shape handlePutSectionScore (internal/httpapi/handlers_api.go)
	// saves when the GP import wizard (gp-import.js) writes an imported
	// track straight into a section's score[label]: field — label here
	// is the GP track's own name, irrelevant to how a JSON body decodes.
	src := `{"Tuning":[64,59,55,50,45,40],"MinimumFret":0,"Bars":[{"Beats":[
		{"Duration":"4","Notes":[{"MIDI":64,"String":1,"Fret":0}]}
	]}]}`
	riff, err := ParseVoice("Distorted Guitar", src)
	if err != nil {
		t.Fatalf("ParseVoice: %v", err)
	}
	if len(riff.Tuning) != 6 {
		t.Errorf("expected the JSON's own 6-string tuning to be preserved, got %d strings", len(riff.Tuning))
	}
	if riff.Drums {
		t.Errorf("expected a melodic riff")
	}
}

func TestParseVoice_ExplicitFormStillWorks(t *testing.T) {
	riff, err := ParseVoice("drum", `\new DrumStaff { \drummode { bd4 sn4 bd4 sn4 } }`)
	if err != nil {
		t.Fatalf("ParseVoice: %v", err)
	}
	if !riff.Drums {
		t.Errorf("expected a drum riff from the explicit \\new DrumStaff form")
	}
}

