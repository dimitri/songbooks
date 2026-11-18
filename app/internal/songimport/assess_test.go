package songimport

import (
	"reflect"
	"strings"
	"testing"
)

// All lyrics below are invented placeholder lines.

const assessMap = `title: Quiet Road
key: A
tempo: 116
time: 4/4
map: Intro · Verse · Chorus · Verse · Chorus · Solo · Chorus · Outro

play it loud

[Intro]
grid: |: A | D . G . | A | D . G . :|

[Verse]

[Chorus]
grid: | A | D . G . | A | % |

[Verse]

[Chorus]

[Solo]

[Chorus]

[Outro]
`

func TestAssessSongMap(t *testing.T) {
	a := Assess(assessMap, RecordingState{}, "", nil)
	if a.Verdict != VerdictImprove {
		t.Fatalf("verdict %s: %q", a.Verdict, a.Reasons)
	}
	if a.Sections != 8 || a.WithGrid != 2 || a.Sung != 2 || a.WithLyrics != 0 {
		t.Errorf("counts %+v", a)
	}
	if a.Summary[0] != "8 sections, 2 with their own grid, 6 repeat one" {
		t.Errorf("summary %q", a.Summary)
	}
	// The bare verse after the gridded intro carries its chords on: not
	// a missing grid, so no find/review; no lyrics; no recording.
	if want := []string{"lyrics", "recording"}; !reflect.DeepEqual(a.Steps, want) {
		t.Errorf("steps %q", a.Steps)
	}
	if !contains(a.Summary, "chords carried on from earlier: Verse") || !contains(a.Summary, "no reference recording") {
		t.Errorf("summary %q", a.Summary)
	}
}

func TestAssessComplete(t *testing.T) {
	body := "title: T\nkey: C\ntempo: 90\ntime: 4/4\n\n[Intro]\ngrid: | C | G |\n\n[Verse]\ngrid: | C | F |\nwalking down the quiet road\n\n[Chorus]\ngrid: | F | G |\noh la la la\n\n[Verse]\ncounting all the paper stars\n\n[Chorus]\n\n[Solo]\n\n[Outro]\n"
	a := Assess(body, RecordingState{Has: true, StemStatus: "done", Markers: 7}, "C", nil)
	if a.Verdict != VerdictImprove || len(a.Steps) != 0 || a.Sung != 3 || a.WithLyrics != 3 {
		t.Errorf("%+v", a)
	}
	// A recording without markers: sync.
	a = Assess(body, RecordingState{Has: true, StemStatus: "done"}, "", nil)
	if !reflect.DeepEqual(a.Steps, []string{"sync"}) {
		t.Errorf("steps %q", a.Steps)
	}
	// A failed split: recording again.
	a = Assess(body, RecordingState{Has: true, StemStatus: "failed", Markers: 7}, "", nil)
	if !reflect.DeepEqual(a.Steps, []string{"recording"}) {
		t.Errorf("steps %q", a.Steps)
	}
}

func TestAssessNew(t *testing.T) {
	for _, body := range []string{"", "title: T\nartist: A\n\n[Verse]\n"} {
		a := Assess(body, RecordingState{}, "", nil)
		if a.Verdict != VerdictNew || !reflect.DeepEqual(a.Steps, []string{"find", "review", "lyrics", "recording"}) {
			t.Errorf("%q: %+v", body, a)
		}
	}
}

func TestAssessRestart(t *testing.T) {
	// Structure only: no grid, no lyrics anywhere.
	a := Assess("title: T\n\n[Intro]\n\n[Verse]\n\n[Chorus]\n", RecordingState{}, "", nil)
	if a.Verdict != VerdictRestart {
		t.Errorf("structure only: %+v", a)
	}
	// Mostly junk: tab staff and credits.
	junk := "title: T\n\n[Verse]\ngrid: | C | G |\nwalking down the quiet road\n" + strings.Repeat("e|---3---3---|\nB|---0---0---|\n", 6) + "Tabbed by someone\n"
	a = Assess(junk, RecordingState{}, "", nil)
	if a.Verdict != VerdictRestart || a.CleanupNotes == 0 {
		t.Errorf("junk: %+v", a)
	}
	// Grid tokens that aren't chords.
	words := "title: T\n\n[Verse]\ngrid: | walking | down | the | quiet | road | and | on | C |\n"
	a = Assess(words, RecordingState{}, "", nil)
	if a.Verdict != VerdictRestart {
		t.Errorf("word grid: %+v", a)
	}
	// Chords in no key, iReal Pro knowing one.
	chromatic := "title: T\n\n[Verse]\ngrid: | C | C# | D | Eb | E | F | F# | G | Ab | A | Bb | B |\n"
	if a = Assess(chromatic, RecordingState{}, "C", nil); a.Verdict != VerdictRestart {
		t.Errorf("chromatic: %+v", a)
	}
	if a = Assess(chromatic, RecordingState{}, "", nil); a.Verdict != VerdictImprove {
		t.Errorf("chromatic without iReal: %+v", a)
	}
}

func TestAssessKeyDisagreement(t *testing.T) {
	a := Assess(assessMap, RecordingState{}, "F", nil)
	if !contains(a.Summary, "key: the chart says A (header), iReal Pro F") {
		t.Errorf("summary %q", a.Summary)
	}
	if a.Verdict != VerdictImprove {
		t.Errorf("verdict %s", a.Verdict)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// A song whose first sections have nothing to carry on from does need a
// chart: find/review.
func TestAssessNothingToCarryOn(t *testing.T) {
	body := "title: T\n\n[Verse]\nwalking down the quiet road\n\n[Chorus]\noh la la la\n\n[Bridge]\ngrid: | C | G |\n"
	a := Assess(body, RecordingState{}, "", nil)
	if len(a.Steps) < 2 || a.Steps[0] != "find" || a.Steps[1] != "review" {
		t.Fatalf("steps %q, summary %q", a.Steps, a.Summary)
	}
}
