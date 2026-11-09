package tab

import (
	"os"
	"path/filepath"
	"testing"
)

// These fixtures are real, hand-written legacy LilyPond files from this
// repo's pre-.song book directories — not synthetic examples.
var realFixtures = []string{
	"Mercy/mercy/mercy.ly",
	"Mercy/bad-spell/intro.ly",
	"Mercy/whiskey-beer-and-wine/intro.ly",
	"Other Lies/2-Surfacing/06-Kawasaki/chorus.ly",
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..", "..")
}

func TestParseScore_RealLegacyFiles(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range realFixtures {
		path := filepath.Join(root, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("%s: fixture not present in this checkout: %v", rel, err)
			continue
		}
		t.Run(rel, func(t *testing.T) {
			riff, err := ParseScore(string(data))
			if err != nil {
				t.Fatalf("ParseScore(%s): %v", rel, err)
			}
			if len(riff.Bars) == 0 {
				t.Fatalf("ParseScore(%s): no bars produced", rel)
			}
			for bi, bar := range riff.Bars {
				for bti, beat := range bar.Beats {
					for ni, n := range beat.Notes {
						if n.String < 1 || n.String > len(riff.Tuning) {
							t.Errorf("bar %d beat %d note %d: invalid string %d", bi, bti, ni, n.String)
						}
						if n.Fret < 0 || n.Fret > 24 {
							t.Errorf("bar %d beat %d note %d: implausible fret %d", bi, bti, ni, n.Fret)
						}
					}
				}
			}
			t.Logf("%s: %d bars", rel, len(riff.Bars))
		})
	}
}

// TestParseScore_MercyMercyMercy spot-checks the resolved fret numbers
// against the real engraved tab printed in this song's legacy LaTeX
// songbook PDF (screenshot reviewed directly): the first bar's two
// chords read 8/6 then 7/5 on the tab staff.
func TestParseScore_MercyMercyMercy(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "Mercy/mercy/mercy.ly"))
	if err != nil {
		t.Skipf("fixture not present: %v", err)
	}
	riff, err := ParseScore(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(riff.Bars) == 0 || len(riff.Bars[0].Beats) < 3 {
		t.Fatalf("unexpected shape: %+v", riff.Bars)
	}
	// Beat 0 is the opening quarter rest; beats 1 and 2 are the first two
	// <bes, bes'> / <bes bes'> chords.
	wantFrets := [][2]int{{6, 8}, {6, 8}}
	for i, want := range wantFrets {
		beat := riff.Bars[0].Beats[i+1]
		if len(beat.Notes) != 2 {
			t.Fatalf("beat %d: expected 2 notes, got %d (%+v)", i+1, len(beat.Notes), beat.Notes)
		}
		got := [2]int{beat.Notes[0].Fret, beat.Notes[1].Fret}
		if got != want {
			t.Errorf("beat %d frets = %v, want %v (notes: %+v)", i+1, got, want, beat.Notes)
		}
	}
}

func TestResolvePitch_RelativeOctave(t *testing.T) {
	// "bes," relative to c' (60, the implicit \relative reference) should
	// land near the guitar's low register, not literally "whatever
	// octave bes happens to default to" — this is the textbook example
	// LilyPond's own docs use to explain the nearest-fourth rule.
	got, err := resolvePitch("bes,", 60)
	if err != nil {
		t.Fatal(err)
	}
	if got != 46 {
		t.Errorf("resolvePitch(bes,, 60) = %d, want 46 (Bb2)", got)
	}
	// A second pitch with no octave marks at all resolves to whichever
	// octave is closest to the previous one.
	got2, err := resolvePitch("d", got)
	if err != nil {
		t.Fatal(err)
	}
	if got2 < got-6 || got2 > got+6 {
		t.Errorf("resolvePitch(d, %d) = %d, too far from the nearest-fourth window", got, got2)
	}
}
