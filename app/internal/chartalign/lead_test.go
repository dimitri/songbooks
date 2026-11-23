package chartalign

import (
	"math"
	"testing"
)

// A song with a "lead:" (Hints.LeadBars): the lead starts the recording,
// on its first downbeat with music, and the first section starts that many
// bars later — not where the chords or the voice put it, even when the
// first section is sung (the voice may come in bars into it).
func TestAlignLeadStartsTheRecording(t *testing.T) {
	r := loadRecording(t, "song334.json")
	_, secs := chartOf(t, "song334-new.chart")
	for i := range secs {
		secs[i].HasLyrics = true // the first section sung too
	}
	free := Align(r, secs, 4, Hints{})
	for _, lead := range []int{1, 2} {
		res := Align(r, secs, 4, Hints{LeadBars: lead})
		// The downbeats from the recording's bar 1.
		var downs []float64
		for i, p := range r.BeatPos {
			if p == 1 && r.Beats[i] >= res.FirstDownbeat-0.01 {
				downs = append(downs, r.Beats[i])
			}
		}
		if len(downs) <= lead {
			t.Fatalf("lead %d: %d downbeats", lead, len(downs))
		}
		want := downs[lead]
		if got := res.Sections[0].Start; math.Abs(got-want) > 0.05 {
			t.Errorf("lead %d: first section at %.2f, want %.2f (bar 1 at %.2f; free alignment %.2f)", lead, got, want, res.FirstDownbeat, free.Sections[0].Start)
		}
		if res.IntroBars != lead {
			t.Errorf("lead %d: %d bars before the first section, want the lead's", lead, res.IntroBars)
		}
	}
}
