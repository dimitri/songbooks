package song

import "testing"

// What "tempo:" counts: the metre's beat, a dotted quarter in compound
// metres; Guitar Pro's and alphaTab's quarters converted at the boundary.
func TestTempoBeat(t *testing.T) {
	for _, c := range []struct {
		sig      string
		compound bool
		quarters float64
		note     string
	}{
		{"4/4", false, 1, "4"},
		{"3/4", false, 1, "4"},
		{"", false, 1, "4"},
		{"C", false, 1, "4"},
		{"2/2", false, 2, "2"},
		{"C|", false, 2, "2"},
		{"6/8", true, 1.5, "4."},
		{"9/8", true, 1.5, "4."},
		{"12/8", true, 1.5, "4."},
		{" 12 / 8 ", true, 1.5, "4."},
		{"3/8", false, 0.5, "8"},
		{"7/8", false, 0.5, "8"},
		{"6/4", false, 1, "4"},
		{"x/8", false, 1, "4"},
		{"6/7", false, 1, "4"},
	} {
		if got := CompoundTime(c.sig); got != c.compound {
			t.Errorf("CompoundTime(%q) = %v", c.sig, got)
		}
		if got := BeatQuarters(c.sig); got != c.quarters {
			t.Errorf("BeatQuarters(%q) = %v, want %v", c.sig, got, c.quarters)
		}
		if got := BeatNote(c.sig); got != c.note {
			t.Errorf("BeatNote(%q) = %q, want %q", c.sig, got, c.note)
		}
	}
}

func TestQuarterTempo(t *testing.T) {
	// "Wake Up Alone": 6/8 at 46 dotted quarters is 69 quarters, and back.
	if got := QuarterTempo(46, "6/8"); got != 69 {
		t.Errorf("QuarterTempo(46, 6/8) = %d", got)
	}
	if got := FromQuarterTempo(69, "6/8"); got != 46 {
		t.Errorf("FromQuarterTempo(69, 6/8) = %d", got)
	}
	// 12/8 at 92 dotted quarters: a Guitar Pro file says 138.
	if got := QuarterTempo(92, "12/8"); got != 138 {
		t.Errorf("QuarterTempo(92, 12/8) = %d", got)
	}
	// Simple metres in quarters don't move.
	for _, sig := range []string{"4/4", "3/4", ""} {
		if got := QuarterTempo(120, sig); got != 120 {
			t.Errorf("QuarterTempo(120, %q) = %d", sig, got)
		}
		if got := FromQuarterTempo(120, sig); got != 120 {
			t.Errorf("FromQuarterTempo(120, %q) = %d", sig, got)
		}
	}
	// 7/8 counts eighths: 240 of them are 120 quarters.
	if got := QuarterTempo(240, "7/8"); got != 120 {
		t.Errorf("QuarterTempo(240, 7/8) = %d", got)
	}
}
