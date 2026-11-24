package audioanalysis

import (
	"math"
	"testing"
)

// A phrase ends at a rest of half a second or more; shorter breaths
// stay inside it, and every phrase starts on an onset.
func TestVoiceCuesOf(t *testing.T) {
	act := activityFromPhrases(10, [][2]float64{{1, 2}, {2.3, 3}, {4, 5}, {8, 9.5}})
	c := VoiceCuesOf(act, Hop)
	want := [][2]float64{{1, 3}, {4, 5}, {8, 9.5}}
	if len(c.Phrases) != len(want) || len(c.Onsets) != len(want) {
		t.Fatalf("phrases %v onsets %v, want %v", c.Phrases, c.Onsets, want)
	}
	for i, p := range want {
		if math.Abs(c.Phrases[i][0]-p[0]) > 1e-6 || math.Abs(c.Phrases[i][1]-p[1]) > 1e-6 || math.Abs(c.Onsets[i]-p[0]) > 1e-6 {
			t.Errorf("phrase %d: %v (onset %v), want %v", i, c.Phrases[i], c.Onsets[i], p)
		}
	}
	if e := VoiceCuesOf(make([]bool, 10), Hop); len(e.Phrases) != 0 || e.Onsets == nil {
		t.Errorf("silence: %+v", e)
	}
}
