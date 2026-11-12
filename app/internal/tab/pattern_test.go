package tab

import (
	"strings"
	"testing"

	"app/internal/song"
)

// TestPatternFillsSection: a one-bar drum pattern in a four-bar section
// is played four times and drawn once, then as three "%" bars.
func TestPatternFillsSection(t *testing.T) {
	src := "title: T\n\n[Verse]\ngrid: | A | A | D | A |\n" +
		`score[Drums]: {"Drums":true,"Pattern":true,"Bars":[{"Beats":[{"Duration":"4","Notes":[{"MIDI":36}]},{"Duration":"4","Notes":[{"MIDI":38}]},{"Duration":"4","Notes":[{"MIDI":36}]},{"Duration":"4","Notes":[{"MIDI":38}]}]}]}` + "\n"
	scores, errs := LoadScores(song.Parse(src))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	riff := scores[0][0].Riff
	if len(riff.Bars) != 4 || riff.Bars[0].Simile || !riff.Bars[3].Simile {
		t.Fatalf("want 4 bars, the last 3 simile: %+v", riff.Bars)
	}
	tex := ToAlphaTexDrums(riff, nil, nil, 120)
	if strings.Contains(tex, "simile") || strings.Count(tex, "Kick") < 9 {
		t.Fatalf("the Drummer view writes every repeat out:\n%s", tex)
	}
	guitar := &Riff{Tuning: []int{64, 59, 55, 50, 45, 40}, Pattern: true, Bars: []Bar{{Beats: []Beat{{Duration: "1", Notes: []Note{{MIDI: 64, String: 1}}}}}}}
	if n := strings.Count(ToAlphaTex(FillPattern(guitar, 4), "", nil), `\simile simple`); n != 3 {
		t.Fatalf("want 3 simile bars on a tab row, got %d", n)
	}
	multi, _ := ToAlphaTexMulti([]NamedRiff{{Name: "Drums", Riff: riff}}, 120, 4, 4, "")
	if strings.Contains(multi, "simile") || strings.Count(multi, "Kick") < 9 {
		t.Fatalf("the Mixer must play every repeat in full:\n%s", multi)
	}
}
