package importer

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseUGContentGolden runs ParseUGContent + ToSongText against every
// case under testdata/ugimport/<name>/: input.ug.txt is the raw Ultimate
// Guitar tab text (as UG's own page data stores it, [ch]/[/ch] tags and
// all — see FetchAndParseUGTab), expected.song is this app's own
// .song-file text ParseUGContent+ToSongText(every section) must produce.
// Browsable on its own in the repo as a side-by-side "UG input -> our
// format" corpus, not just readable from inside this Go file — add a new
// case by dropping in a new directory with those two files, no test code
// changes needed.
func TestParseUGContentGolden(t *testing.T) {
	dirs, err := os.ReadDir("testdata/ugimport")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		name := d.Name()
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata/ugimport", name, "input.ug.txt"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata/ugimport", name, "expected.song"))
			if err != nil {
				t.Fatal(err)
			}
			song, err := ParseUGContent(string(raw))
			if err != nil {
				t.Fatalf("ParseUGContent: %v", err)
			}
			all := make([]int, len(song.Sections))
			for i := range all {
				all[i] = i
			}
			got := song.ToSongText(all)
			if got != string(want) {
				t.Errorf("ToSongText mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// TestParseUGContentNoFalseSectionFromChord guards a specific regression:
// a bare chord-only line ("[G]" with nothing else) must never be mistaken
// for a section header — covered structurally (not just by text diff) in
// case the golden "bare_chords" case's expected.song is ever edited in a
// way that happens to still read as one section either way.
func TestParseUGContentNoFalseSectionFromChord(t *testing.T) {
	raw := "[Bridge]\n[ch]G[/ch]\n[ch]D[/ch]\n"
	song, err := ParseUGContent(raw)
	if err != nil {
		t.Fatalf("ParseUGContent: %v", err)
	}
	if len(song.Sections) != 1 || song.Sections[0].Label != "Bridge" {
		t.Fatalf("got sections %+v, want one Bridge section", song.Sections)
	}
}

// TestParseUGContentCapoAndRepeatMarkers covers two real bugs found by
// fetching an actual live UG page (Hotel California's own "Chords" tab):
// a pre-first-section "Tabbed by: X" / "Capo N" pair used to become a
// bogus unlabeled leading section whose "lyrics" were the tabber's
// byline, and a bare chord-progression line's trailing "x2"/"x3" marker
// used to leak into Lyrics() as a garbled pseudo-lyric (its own text
// case lives in testdata/ugimport/capo_and_repeat_markers; this checks
// the one outcome — Capo — a plain text diff of ToSongText can't see).
func TestParseUGContentCapoAndRepeatMarkers(t *testing.T) {
	raw := "Tabbed by: Someone\nCapo 2\n\n[Intro]\n[ch]Am[/ch] [ch]G[/ch] x2\n"
	song, err := ParseUGContent(raw)
	if err != nil {
		t.Fatalf("ParseUGContent: %v", err)
	}
	if song.Capo != "2" {
		t.Errorf("Capo = %q, want \"2\"", song.Capo)
	}
	if len(song.Sections) != 1 || song.Sections[0].Label != "Intro" {
		t.Fatalf("got sections %+v, want one Intro section (no bogus leading section from the byline)", song.Sections)
	}
	if lyrics := song.Sections[0].Lyrics(); lyrics != "" {
		t.Errorf("Lyrics() = %q, want empty (the x2 marker must not leak in as a pseudo-lyric)", lyrics)
	}
	if song.Sections[0].RepeatCount != 2 {
		t.Errorf("RepeatCount = %d, want 2", song.Sections[0].RepeatCount)
	}
}

// TestParseUGContentNoSectionLabels guards the other structural case the
// golden corpus's "no_labels" entry exercises: UG text with no [Section]
// markers at all defaults to a single "Verse".
func TestParseUGContentNoSectionLabels(t *testing.T) {
	raw := "[ch]Am[/ch]walking on a thin line\n[ch]F[/ch]walking on a thin line\n"
	song, err := ParseUGContent(raw)
	if err != nil {
		t.Fatalf("ParseUGContent: %v", err)
	}
	if len(song.Sections) != 1 || song.Sections[0].Label != "Verse" {
		t.Fatalf("got sections %+v, want one default-labelled Verse", song.Sections)
	}
}
