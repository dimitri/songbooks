package song

import "testing"

func TestSimplifyGridTextRepeatGroup(t *testing.T) {
	in := "Em7 | G | Dsus4 | A7sus4 | Em7 | G | Dsus4 | A7sus4 | Em7 | G | Dsus4 | A7sus4 | Cadd9 | Dsus4 | A7sus4"
	want := "|:3 Em7 | G | Dsus4 | A7sus4 :| Cadd9 | Dsus4 | A7sus4|"
	if got := SimplifyGridText(in); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestSimplifyGridTextTwoReps(t *testing.T) {
	// Exactly two reps of a group omits the explicit count, matching the
	// implicit "play it twice" every plain "|: :|" already means.
	in := "Cadd9 | Em7 | G | Em7 | Cadd9 | Em7 | G | Em7 | Cadd9 | G | Em7 | % | Cadd9 | Em7 | % | G"
	want := "|: Cadd9 | Em7 | G | Em7 :| Cadd9 | G | Em7 | % | Cadd9 | Em7 | % | G|"
	if got := SimplifyGridText(in); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestSimplifyGridTextSingleBarRepeat(t *testing.T) {
	in := "Em7 | Em7 | Em7 | G"
	want := "|Em7 | % | % | G|"
	if got := SimplifyGridText(in); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestSimplifyGridTextLeavesExplicitStructureAlone(t *testing.T) {
	in := "|:4 F#m | D | Bm | E :|"
	if got := SimplifyGridText(in); got != "|:4 F#m | D | Bm | E :|" {
		t.Errorf("should be left unchanged, got %q", got)
	}
}

func TestSimplifyGridTextNoRepeats(t *testing.T) {
	in := "C | G | Am | F"
	want := "|C | G | Am | F|"
	if got := SimplifyGridText(in); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
