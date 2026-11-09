package tab

import "testing"

func TestSpacingWeights_LogNotLinear(t *testing.T) {
	// A quarter note next to a sixteenth: linear duration weighting
	// would give the quarter 4x the sixteenth's width (1.0 vs 0.25
	// DurationBeats) — real (Gourlay) spacing gives it only 2x.
	beats := []Beat{
		{Duration: "4", Notes: []Note{{Fret: 1, String: 1}}},
		{Duration: "16", Notes: []Note{{Fret: 1, String: 1}}},
	}
	w := SpacingWeights(beats)
	if len(w) != 2 {
		t.Fatalf("len(w) = %d, want 2", len(w))
	}
	ratio := w[0] / w[1]
	if ratio < 1.9 || ratio > 2.1 {
		t.Errorf("quarter/sixteenth weight ratio = %.3f, want ~2.0 (log2(4)=2 steps of 1 unit over a base of 2)", ratio)
	}
}

func TestSpacingWeights_EqualDurationsEqualWeight(t *testing.T) {
	beats := []Beat{
		{Duration: "8", Notes: []Note{{Fret: 1, String: 1}}},
		{Duration: "8", Notes: []Note{{Fret: 2, String: 1}}},
	}
	w := SpacingWeights(beats)
	if w[0] != w[1] {
		t.Errorf("equal durations got unequal weights: %v", w)
	}
}
