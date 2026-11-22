package chartalign

import (
	"testing"

	"app/internal/song"
)

// A "lead:" line: the opening section plays it once, before its passes
// (the player counts it in the section's bars, before bar 1); its bars
// have no written measure to fix, and the written bars keep their own
// measure numbers.
func TestChartLead(t *testing.T) {
	sg := song.Parse("time: 4/4\n\n[Intro]\nlead: | D7 |\ntimes: 2\ngrid: | G | C |\n\n[Intro]\n")
	secs := Chart(sg, []SectionInfo{
		{Index: 0, Label: "Intro", StartBar: 0, BarCount: 5},
		{Index: 1, Label: "Intro", StartBar: 5, BarCount: 4},
	})
	in := secs[0]
	if len(in.Bars) != 5 || in.Lead != 1 {
		t.Fatalf("Intro: %d bars, lead %d", len(in.Bars), in.Lead)
	}
	if b := in.Bars[0]; b.Beats[0] != "D7" || b.DefSection != -1 || b.Measure != -1 {
		t.Errorf("lead bar: %+v", b)
	}
	want := []string{"G", "C", "G", "C"}
	for k, w := range want {
		if b := in.Bars[1+k]; b.Beats[0] != w || b.Measure != k%2 || b.DefSection != 0 {
			t.Errorf("bar %d: %+v, want %s (measure %d)", 1+k, b, w, k%2)
		}
	}
	// The bare Intro again: its passes, not the lead.
	if again := secs[1]; again.Lead != 0 || again.Bars[0].Beats[0] != "G" {
		t.Errorf("the Intro again: lead %d, first %v", again.Lead, again.Bars[0].Beats)
	}
}
