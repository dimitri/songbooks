package tab

import (
	"strings"
	"testing"
)

func TestToAlphaTexMulti(t *testing.T) {
	bass := &Riff{Tuning: []int{43, 38, 33, 28}, Bars: []Bar{{Beats: []Beat{{Rest: true, Duration: "1"}}}}}
	gtr := &Riff{Tuning: []int{64, 59, 55, 50, 45, 40}, Bars: []Bar{{Beats: []Beat{{Rest: true, Duration: "1"}}}}}
	drums := &Riff{Drums: true, Bars: []Bar{{Beats: []Beat{{Duration: "4", Notes: []Note{{MIDI: 36}}}, {Rest: true, Duration: "2."}}}}}
	tex, meta := ToAlphaTexMulti([]NamedRiff{{Name: "Electric Bass", Riff: bass}, {Name: "Jazz Guitar", Riff: gtr}, {Name: "Drum", Riff: drums}}, 113, 3, 4)

	for _, want := range []string{
		`\tempo 113`,
		`\track "Electric Bass" { instrument 33 }`,
		`\track "Jazz Guitar" { instrument 26 }`,
		`\track "Drum" { instrument percussion }`,
		`\staff {tabs}`,
		`\staff {score}`,
		`\ts 3 4 `,
		`\articulation (Kick 36)`,
	} {
		if !strings.Contains(tex, want) {
			t.Errorf("missing %q in:\n%s", want, tex)
		}
	}
	if strings.Contains(tex, "multiBarRest") {
		t.Error("mixer scores must never merge bars (multiBarRest)")
	}
	if len(meta) != 3 || meta[0].Program != 33 || meta[1].Program != 26 || !meta[2].Drums || meta[2].Program != -1 {
		t.Errorf("unexpected meta %+v", meta)
	}
}

func TestGMProgramFor(t *testing.T) {
	cases := map[string]int{"Acoustic Guitar": 25, "Nylon": 24, "Lead": 30, "Rhythm": 27, "Fretless Bass": 35}
	for name, want := range cases {
		if got := gmProgramFor(name, 6); got != want {
			t.Errorf("%s: got %d, want %d", name, got, want)
		}
	}
	if got := gmProgramFor("Low", 4); got != 33 {
		t.Errorf("4-string riff should default to finger bass, got %d", got)
	}
}
