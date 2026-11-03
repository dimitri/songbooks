package httpapi

import (
	"testing"

	"app/internal/song"
)

// These exercise measuresEqual's normalized (IR-based) comparison, the
// replacement for the previous literal byte/struct comparison — see that
// function's own doc comment for why. Each case states which of the two
// implementations it distinguishes.

// TestBareSoloTimesDoublesBarCount covers the-thrill-is-gone's outro Solo:
// a bare section labeled "Solo", with no grid of its own, inferring the
// Intro's 12-bar changes but stating "times: 2" to mean it's actually
// played through twice — it should report 24 real bars (for the song-map
// label and the metronome's timing) while showing two 12-bar multi-rest
// segments (RepeatSegmentBars), the same side-by-side display a literal
// "|: ... :|" bracket gets, not one unbroken "24".
func TestBareSoloTimesDoublesBarCount(t *testing.T) {
	src := "title: X\ntime: 4/4\n\n" +
		"[Intro]\ngrid: Bm | % | % | % | Em | % | Bm | % | G7 | F#7 | Bm | %\n\n" +
		"[Solo]\n\n" +
		"[Solo]\ntimes: 2\n"
	sg := song.Parse(src)
	sections := buildSections(sg)
	if len(sections) != 3 {
		t.Fatalf("expected 3 sections, got %d", len(sections))
	}
	intro, solo1, solo2 := sections[0], sections[1], sections[2]

	if intro.BarCount != 12 {
		t.Fatalf("expected Intro BarCount 12, got %d", intro.BarCount)
	}
	if solo1.BarCount != 12 || solo1.RepeatSegmentBars != 0 {
		t.Errorf("expected Solo 1 (times absent) BarCount 12, RepeatSegmentBars 0, got BarCount=%d RepeatSegmentBars=%d", solo1.BarCount, solo1.RepeatSegmentBars)
	}
	if solo2.BarCount != 24 {
		t.Errorf("expected Solo 2 (times: 2) BarCount 24, got %d", solo2.BarCount)
	}
	if solo2.RepeatSegmentBars != 12 {
		t.Errorf("expected Solo 2 RepeatSegmentBars 12 (two 12-bar segments), got %d", solo2.RepeatSegmentBars)
	}
	if solo1.MultiRestSegments != 1 || solo1.MultiRestPer != 12 {
		t.Errorf("expected Solo 1 to render as ONE 12-bar rest glyph, got MultiRestSegments=%d MultiRestPer=%d", solo1.MultiRestSegments, solo1.MultiRestPer)
	}
	if solo2.MultiRestSegments != 2 || solo2.MultiRestPer != 12 {
		t.Errorf("expected Solo 2 to render as TWO 12-bar rest glyphs, got MultiRestSegments=%d MultiRestPer=%d", solo2.MultiRestSegments, solo2.MultiRestPer)
	}
}

func TestMeasuresEqualSubBeatSplit(t *testing.T) {
	// Same music ("G for the whole bar"), written two different ways: one
	// token stretched to fill the bar vs. the same chord repeated at
	// every beat with explicit sustains. The old byte comparison required
	// identical token lists (len 1 vs len 4) and would have reported
	// these unequal; the normalized comparison expands both to the same
	// 4-beat sequence and reports them equal.
	a := []song.Measure{{Tokens: []string{"G"}}}
	b := []song.Measure{{Tokens: []string{"G", ".", ".", "."}}}
	if !measuresEqual(a, b, 4, 4) {
		t.Error("expected a bar stretched to fill vs. the same chord typed at every beat to compare equal")
	}
}

func TestMeasuresEqualDifferentSubBeatGrouping(t *testing.T) {
	// Same music ("C for 2 beats, D for 2 beats"), split two different
	// ways: 2 tokens each spanning 2 beats, vs. 4 tokens with explicit
	// sustains. Different token counts, same resulting per-beat chords.
	a := []song.Measure{{Tokens: []string{"C", "D"}}}
	b := []song.Measure{{Tokens: []string{"C", ".", "D", "."}}}
	if !measuresEqual(a, b, 4, 4) {
		t.Error("expected two different sub-beat groupings of the same per-beat chords to compare equal")
	}
}

func TestMeasuresEqualEnharmonicSpelling(t *testing.T) {
	// F# and Gb are the same pitch class — chordsym.PitchClass already
	// treats them as equal everywhere else in this app (live transpose
	// relies on it); the normalized comparator does too now.
	a := []song.Measure{{Tokens: []string{"F#m7"}}}
	b := []song.Measure{{Tokens: []string{"Gbm7"}}}
	if !measuresEqual(a, b, 4, 4) {
		t.Error("expected F#m7 and Gbm7 (same pitch class, same quality) to compare equal")
	}
}

func TestMeasuresEqualQualitySpellingVariants(t *testing.T) {
	// "m7b5" / "mi7b5" / "-7b5" all decompose to the same half-diminished
	// marker+extension via chordsym.DecomposeQuality.
	a := []song.Measure{{Tokens: []string{"Bm7b5"}}}
	b := []song.Measure{{Tokens: []string{"B-7b5"}}}
	if !measuresEqual(a, b, 4, 4) {
		t.Error("expected m7b5 and -7b5 (same decomposed quality) to compare equal")
	}
}

func TestMeasuresEqualIgnoresRepeatBarlineFlags(t *testing.T) {
	// Identical music; one copy happens to sit at the start/end of an
	// UNRELATED repeat bracket (Open/Close) or carries a volta label.
	// These are repeat-barline/ending notation, not musical content —
	// the old comparison required them to match too and would have
	// reported these unequal.
	a := []song.Measure{{Tokens: []string{"Am7"}, Open: true, Volta: "1."}}
	b := []song.Measure{{Tokens: []string{"Am7"}}}
	if !measuresEqual(a, b, 4, 4) {
		t.Error("expected identical chords with differing Open/Volta metadata to compare equal")
	}
}

func TestMeasuresEqualRejectsRealDifferences(t *testing.T) {
	cases := []struct {
		name string
		a, b []song.Measure
	}{
		{"different chord", []song.Measure{{Tokens: []string{"G"}}}, []song.Measure{{Tokens: []string{"A"}}}},
		{"different quality", []song.Measure{{Tokens: []string{"Gm"}}}, []song.Measure{{Tokens: []string{"G"}}}},
		{"different meter", []song.Measure{{Tokens: []string{"G"}, Meter: "2/4"}}, []song.Measure{{Tokens: []string{"G"}}}},
		{"different length", []song.Measure{{Tokens: []string{"G"}}, {Tokens: []string{"C"}}}, []song.Measure{{Tokens: []string{"G"}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if measuresEqual(c.a, c.b, 4, 4) {
				t.Errorf("%s: expected measures to compare unequal", c.name)
			}
		})
	}
}

// detectSegnoRepeat exercises the Segno/D.S. al Coda structural
// detection — see that function's own doc comment for the LilyPond-
// grounded semantics (Segno = jump-back target, D.S. = jump-from point,
// Coda = where play resumes). These are synthetic songs, not real
// catalog ones, specifically to pin down the boundary cases without
// depending on the current catalog happening to contain one.

func sec(label string, tokens ...string) song.Section {
	var measures []song.Measure
	for _, t := range tokens {
		measures = append(measures, song.Measure{Tokens: []string{t}})
	}
	return song.Section{Label: label, Measures: measures}
}

func TestDetectSegnoRepeatFindsLongDistanceRun(t *testing.T) {
	// Intro, Verse(G), Chorus(C) ... Bridge(D) ... Verse(G), Chorus(C), Outro(Am)
	// The Verse+Chorus pair recurs later, with a Bridge genuinely between
	// the two occurrences — the textbook Segno/D.S. al Coda case: Segno
	// at the first Verse, D.S. al Coda at the second, Coda at Outro.
	sg := &song.Song{Sections: []song.Section{
		sec("Intro", "Em"),
		sec("Verse", "G"),
		sec("Chorus", "C"),
		sec("Bridge", "D"),
		sec("Verse", "G"),
		sec("Chorus", "C"),
		sec("Outro", "Am"),
	}}
	m, ok := detectSegnoRepeat(sg, 4)
	if !ok {
		t.Fatal("expected a Segno/D.S. match")
	}
	if m.SegnoAt != 1 || m.DSAt != 4 || m.Len != 2 {
		t.Errorf("got SegnoAt=%d DSAt=%d Len=%d, want SegnoAt=1 DSAt=4 Len=2", m.SegnoAt, m.DSAt, m.Len)
	}
}

func TestDetectSegnoRepeatRejectsAdjacentRepeat(t *testing.T) {
	// Verse+Chorus immediately followed by the SAME Verse+Chorus again,
	// no gap — this is \repeat volta territory (play it twice), not
	// Segno, and must NOT fire here; it's already correctly handled by
	// the existing bracket-repeat treatment.
	sg := &song.Song{Sections: []song.Section{
		sec("Verse", "G"),
		sec("Chorus", "C"),
		sec("Verse", "G"),
		sec("Chorus", "C"),
	}}
	if _, ok := detectSegnoRepeat(sg, 4); ok {
		t.Error("expected no Segno match for an immediately-adjacent repeat")
	}
}

func TestDetectSegnoRepeatRejectsSingleSectionRepeat(t *testing.T) {
	// A single repeated section with real material between two
	// occurrences — still not Segno-worthy; the directive is explicit
	// that only a genuinely MULTI-section run should trigger this, a
	// lone section stays on the existing single-section recall
	// treatment.
	sg := &song.Song{Sections: []song.Section{
		sec("Chorus", "C"),
		sec("Verse", "G"),
		sec("Bridge", "D"),
		sec("Chorus", "C"),
		sec("Outro", "Am"),
	}}
	if _, ok := detectSegnoRepeat(sg, 4); ok {
		t.Error("expected no Segno match for a single repeated section")
	}
}

func TestDetectSegnoRepeatNoMatch(t *testing.T) {
	sg := &song.Song{Sections: []song.Section{
		sec("Intro", "Em"),
		sec("Verse", "G"),
		sec("Chorus", "C"),
		sec("Outro", "Am"),
	}}
	if _, ok := detectSegnoRepeat(sg, 4); ok {
		t.Error("expected no Segno match when nothing actually repeats")
	}
}

func TestDetectSegnoRepeatPrefersLongestMatch(t *testing.T) {
	// Both a 1-section match (Chorus alone) and a 2-section match
	// (Verse+Chorus together) exist; the longer, more structurally real
	// one should win, and the lone Chorus repeat shouldn't also fire
	// independently.
	sg := &song.Song{Sections: []song.Section{
		sec("Intro", "Em"),
		sec("Verse", "G"),
		sec("Chorus", "C"),
		sec("Bridge", "D"),
		sec("Verse", "G"),
		sec("Chorus", "C"),
	}}
	m, ok := detectSegnoRepeat(sg, 4)
	if !ok {
		t.Fatal("expected a match")
	}
	if m.Len != 2 || m.SegnoAt != 1 || m.DSAt != 4 {
		t.Errorf("got SegnoAt=%d DSAt=%d Len=%d, want the 2-section Verse+Chorus run (SegnoAt=1 DSAt=4 Len=2)", m.SegnoAt, m.DSAt, m.Len)
	}
}
