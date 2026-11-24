package songimport

import (
	"os"
	"testing"
)

// Song 334's recording against its two charts: the old one (no intro, a
// verse written 23 bars where the recording plays 16) differs; the
// corrected one follows.
func TestRecordingFit334(t *testing.T) {
	r := rec334(t)
	for _, c := range []struct {
		chart, want string
	}{
		{"song334-old.chart", FitDiffers},
		{"song334-new.chart", FitFollows},
	} {
		b, err := os.ReadFile("../chartalign/testdata/" + c.chart)
		if err != nil {
			t.Fatal(err)
		}
		f := RecordingFit(r, string(b), syncOf(string(b)))
		t.Logf("%s: %+v", c.chart, f)
		if f.Fit != c.want {
			t.Errorf("%s: %s, want %s (%+v)", c.chart, f.Fit, c.want, f.Reasons)
		}
		if c.want == FitDiffers {
			intro, verse := false, false
			for _, w := range f.Reasons {
				intro = intro || (w.Kind == "intro" && w.Bars == 3)
				verse = verse || (w.Kind == "section" && w.Label == "Verse 1" && w.Written == 23 && w.Played == 16)
			}
			if !intro || !verse {
				t.Errorf("%s: reasons %+v, want the 3-bar intro and Verse 1's 23 bars played 16", c.chart, f.Reasons)
			}
		}
	}
	if f := RecordingFit(nil, "[Verse]\n| C |", nil); f.Fit != FitUnknown {
		t.Errorf("no analysis: %s, want unknown", f.Fit)
	}
}
