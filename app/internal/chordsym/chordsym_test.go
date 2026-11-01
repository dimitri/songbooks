package chordsym

import (
	"strings"
	"testing"
)

func TestSplit(t *testing.T) {
	cases := map[string]Parts{
		"Gm":      {Root: "G", Quality: "m"},
		"Bb7/D":   {Root: "Bb", Quality: "7", Bass: "D"},
		"Cmaj7":   {Root: "C", Quality: "maj7"},
		"%":       {Quality: "%"},
		"N.C.":    {Quality: "N.C."},
		"Dbmaj13": {Root: "Db", Quality: "maj13"},
	}
	for in, want := range cases {
		got := Split(in)
		if got != want {
			t.Errorf("Split(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestRenderNoPanic(t *testing.T) {
	for _, s := range []string{"Gm7", "Cmaj7", "Bb7/D", "%", "", "N.C.", "F#dim", "Ebmaj9"} {
		Render(s) // must not panic; markup correctness is covered by TestSplit
	}
}

// TestHalfDimAndDim checks the liberal-input/standard-output rule: whatever
// ASCII-typeable spelling a human or a tab site uses for half-diminished or
// diminished, the rendered glyph is the same (ø / °) — nobody should have
// to type ø or ° to get them to show up.
func TestHalfDimAndDim(t *testing.T) {
	halfDim := []string{"Am7b5", "Ami7b5", "Amin7b5", "A-7b5", "Aø7", "Aø"}
	for _, s := range halfDim {
		got := Render(s)
		if !strings.Contains(got, halfDimSym) {
			t.Errorf("Render(%q) = %q, want it to contain %q (half-diminished)", s, got, halfDimSym)
		}
		if strings.Contains(got, dimSym) {
			t.Errorf("Render(%q) = %q, half-diminished should not also render the full-diminished glyph", s, got)
		}
	}

	dim := []string{"Adim7", "Adim", "Ao7", "Ao", "A°7"}
	for _, s := range dim {
		got := Render(s)
		if !strings.Contains(got, dimSym) {
			t.Errorf("Render(%q) = %q, want it to contain %q (diminished)", s, got, dimSym)
		}
	}

	// A plain minor seventh must NOT be mistaken for half-diminished — only
	// an explicit flat/minus 5 (or ø) means half-diminished.
	if got := Render("Am7"); strings.Contains(got, halfDimSym) || strings.Contains(got, dimSym) {
		t.Errorf("Render(\"Am7\") = %q, plain m7 must not render as (half-)diminished", got)
	}

	// The extension after the quality (e.g. the 9 in m9b5) must survive.
	got := Render("Am9b5")
	if !strings.Contains(got, halfDimSym) || !strings.Contains(got, ">9<") {
		t.Errorf("Render(\"Am9b5\") = %q, want ø glyph with a surviving \"9\" extension", got)
	}
}
