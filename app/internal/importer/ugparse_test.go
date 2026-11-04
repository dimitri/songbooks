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
