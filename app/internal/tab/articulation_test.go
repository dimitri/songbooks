package tab

import "testing"

// These fixtures are hand-written, not pulled from this app's own
// legacy corpus — bends and palm mutes appear nowhere in it (confirmed
// by survey earlier in this project). They're added ahead of a planned
// Guitar Pro importer, which will need to serialize GP's own per-note
// bend/palm-mute data into exactly this syntax.

func TestParseArticulation_Bends(t *testing.T) {
	cases := []struct {
		label         string
		wantSemitones float64
		wantRelease   bool
	}{
		{"full", 2, false},
		{"1/2", 1, false},
		{"1/4", 0.5, false},
		{"3/4", 1.5, false},
		{"1", 2, false},
		{"1 1/2", 3, false},
		{"full (release)", 2, true},
		{"1/2 (release)", 1, true},
	}
	for _, c := range cases {
		semitones, release, pm, err := parseArticulation(c.label)
		if err != nil {
			t.Errorf("parseArticulation(%q) error: %v", c.label, err)
			continue
		}
		if pm {
			t.Errorf("parseArticulation(%q): got PalmMute=true, want false", c.label)
		}
		if semitones != c.wantSemitones || release != c.wantRelease {
			t.Errorf("parseArticulation(%q) = (%v, %v), want (%v, %v)", c.label, semitones, release, c.wantSemitones, c.wantRelease)
		}
	}
}

func TestParseArticulation_PalmMute(t *testing.T) {
	for _, label := range []string{"PM", "pm", "P.M.", "p.m."} {
		semitones, release, pm, err := parseArticulation(label)
		if err != nil {
			t.Fatalf("parseArticulation(%q) error: %v", label, err)
		}
		if !pm || semitones != 0 || release {
			t.Errorf("parseArticulation(%q) = (%v, %v, %v), want (0, false, true)", label, semitones, release, pm)
		}
	}
}

func TestParseArticulation_Unrecognized(t *testing.T) {
	if _, _, _, err := parseArticulation("vibrato"); err == nil {
		t.Error("expected an error for an unrecognized articulation label, got nil")
	}
}

func TestParseScore_Bend(t *testing.T) {
	// Hand-written, LilyPond-subset-valid source exercising a full bend
	// on a single note — this subset's own syntax extension (real
	// LilyPond text-articulation attachment), not found in the real
	// corpus.
	src := `
\new TabStaff {
  \set TabStaff.minimumFret = 15
  \relative {
    e''8^"full" e8 e4
  }
}
`
	riff, err := ParseScore(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(riff.Bars) == 0 || len(riff.Bars[0].Beats) == 0 {
		t.Fatal("expected at least one bar with beats")
	}
	first := riff.Bars[0].Beats[0]
	if first.BendSemitones != 2 {
		t.Errorf("BendSemitones = %v, want 2 (a full/whole-step bend)", first.BendSemitones)
	}
	if first.BendRelease {
		t.Error("BendRelease = true, want false (no \"(release)\" suffix)")
	}
	if first.PalmMute {
		t.Error("PalmMute = true, want false")
	}
}

func TestParseScore_BendRelease(t *testing.T) {
	src := `
\new TabStaff {
  \set TabStaff.minimumFret = 15
  \relative {
    e''8^"1/2 (release)" e8 e4
  }
}
`
	riff, err := ParseScore(src)
	if err != nil {
		t.Fatal(err)
	}
	first := riff.Bars[0].Beats[0]
	if first.BendSemitones != 1 || !first.BendRelease {
		t.Errorf("got (BendSemitones=%v, BendRelease=%v), want (1, true)", first.BendSemitones, first.BendRelease)
	}
}

func TestParseScore_PalmMute(t *testing.T) {
	src := `
\new TabStaff {
  \set TabStaff.minimumFret = 3
  \relative {
    e8^"PM" e8^"PM" e8^"PM" e8^"PM"
  }
}
`
	riff, err := ParseScore(src)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, bar := range riff.Bars {
		for _, b := range bar.Beats {
			if b.PalmMute {
				count++
			}
			if b.BendSemitones != 0 {
				t.Errorf("unexpected bend on a palm-muted note: %v", b.BendSemitones)
			}
		}
	}
	if count != 4 {
		t.Errorf("found %d palm-muted beats, want 4", count)
	}
}

func TestParseScore_UnrecognizedArticulation_IsError(t *testing.T) {
	src := `
\new TabStaff {
  \relative { e8^"vibrato" }
}
`
	if _, err := ParseScore(src); err == nil {
		t.Error("expected an error for an unrecognized articulation, got nil")
	}
}
