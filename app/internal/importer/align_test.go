package importer

import (
	"reflect"
	"strings"
	"testing"

	"app/internal/song"
)

// Invented lyrics. The old chart had them all in one section; the song
// map built from the guide gets them back section by section, the
// repeated chorus in each chorus, an unmatched line kept with its
// neighbour, the song's own wording kept.
func TestAlignOwnLyrics(t *testing.T) {
	guide := "[Verse 1]\ngrid: | C |\nwalking down the quiet road\ncounting the paper stars\n\n[Chorus]\ngrid: | F |\noh la la the night is long\n\n[Verse 2]\ngrid: | C |\nunder neon skies\n\n[Chorus]\ngrid: | F |\noh la la the night is long\n"
	chart := SongMap(guide)
	old := "[Verse]\nWalking down the quiet road!\ncounting all the paper stars\noh la la the night is long\nunder neon skies\nand a line only we sing\noh la la the night is long\n"
	got, notes, ok := AlignOwnLyrics(chart, guide, old)
	if !ok {
		t.Fatalf("not aligned:\n%s", got)
	}
	var per []int
	for _, s := range song.Parse(got).Sections {
		per = append(per, len(s.Lyrics))
	}
	if want := []int{2, 1, 2, 1}; !reflect.DeepEqual(per, want) {
		t.Fatalf("lines per section %v, want %v (%q):\n%s", per, want, notes, got)
	}
	if !strings.Contains(got, "Walking down the quiet road!") || !strings.Contains(got, "counting all the paper stars") || !strings.Contains(got, "and a line only we sing") {
		t.Fatalf("the song's own wording lost:\n%s", got)
	}
	// A guide whose sections aren't the chart's: left alone.
	if _, _, ok := AlignOwnLyrics("[Intro]\ngrid: | C |\n", guide, old); ok {
		t.Fatal("aligned against another structure")
	}
}
