package importer

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"app/internal/song"
)

// Invented lyrics only.
var (
	rsVerse1 = []string{"walking down the quiet road", "counting all the paper stars"}
	rsVerse2 = []string{"under neon skies we wander", "every window closed and dark"}
	rsChorus = []string{"oh la la la the night is long", "oh la la la we sing along"}
)

func lyricsPerSection(text string) []int {
	var out []int
	for _, s := range song.Parse(text).Sections {
		out = append(out, len(s.Lyrics))
	}
	return out
}

func allLines(text string) []string {
	var out []string
	for _, s := range song.Parse(text).Sections {
		for _, l := range s.Lyrics {
			var b strings.Builder
			for _, seg := range l.Segments {
				b.WriteString(seg.Text)
			}
			out = append(out, b.String())
		}
	}
	sort.Strings(out)
	return out
}

func join(parts ...[]string) string {
	var all []string
	for _, p := range parts {
		all = append(all, p...)
	}
	return strings.Join(all, "\n")
}

// The first chorus section holds the second verse after its chorus; the
// empty verse after it takes that verse (and its grid), nothing is lost.
func TestResplitOwnMovesAVerseOutOfAChorus(t *testing.T) {
	body := "title: T\n\n[Verse]\ngrid: | C | F |\n" + join(rsVerse1) +
		"\n\n[Chorus]\ngrid: | F | G |\n" + join(rsChorus, rsVerse2) +
		"\n\n[Verse]\n\n[Chorus]\n" + join(rsChorus) + "\n"
	got, ps, notes := ResplitOwn(body)
	if want := []int{2, 2, 2, 2}; !reflect.DeepEqual(lyricsPerSection(got), want) {
		t.Fatalf("lyrics per section %v, want %v, notes %q:\n%s", lyricsPerSection(got), want, notes, got)
	}
	if !reflect.DeepEqual(allLines(got), allLines(body)) {
		t.Fatalf("lines changed:\n%s", got)
	}
	if v := song.Parse(got).Sections[2]; len(v.Measures) != 2 {
		t.Fatalf("the moved verse lost its inherited grid:\n%s", got)
	}
	if placementsString(ps) != "Verse=kept:-1 Chorus=kept:-1 Verse=placed:-1 Chorus=kept:-1" || len(notes) != 1 || !strings.Contains(notes[0], "moved to Verse 2") {
		t.Fatalf("placements %s, notes %q", placementsString(ps), notes)
	}
	// Running it again moves nothing.
	again, _, notes2 := ResplitOwn(got)
	if again != got || !strings.HasPrefix(notes2[0], "nothing to move") {
		t.Fatalf("not idempotent: %q", notes2)
	}
}

// A chorus sung twice in its own section stays; a chorus caught inside
// a verse moves out to the next empty chorus.
func TestResplitOwnChorusInAVerse(t *testing.T) {
	body := "[Verse]\ngrid: | C |\n" + join(rsVerse1, rsChorus) +
		"\n\n[Chorus]\ngrid: | F |\n" + join(rsChorus, rsChorus) +
		"\n\n[Verse]\n" + join(rsVerse2) + "\n\n[Chorus]\n\n[Chorus]\n"
	got, _, notes := ResplitOwn(body)
	if want := []int{2, 4, 2, 2, 0}; !reflect.DeepEqual(lyricsPerSection(got), want) {
		t.Fatalf("lyrics per section %v, want %v, notes %q:\n%s", lyricsPerSection(got), want, notes, got)
	}
	if !reflect.DeepEqual(allLines(got), allLines(body)) {
		t.Fatalf("lines changed:\n%s", got)
	}
}

// With nowhere to go, a part stays where it is: no line is ever lost.
func TestResplitOwnNowhereToGo(t *testing.T) {
	body := "[Verse]\ngrid: | C |\n" + join(rsVerse1) + "\n\n[Chorus]\ngrid: | F |\n" + join(rsChorus, rsVerse2) +
		"\n\n[Chorus]\n" + join(rsChorus) + "\n"
	got, _, notes := ResplitOwn(body)
	if got != body || !strings.HasPrefix(notes[0], "nothing to move") {
		t.Fatalf("changed (%q):\n%s", notes, got)
	}
}

func TestWithoutLyrics(t *testing.T) {
	body := "title: T\n\n[Verse]\ngrid: | C | F |\n" + join(rsVerse1) + "\n\n[Chorus]\ngrid: | F |\n"
	got := WithoutLyrics(body)
	if strings.Contains(got, rsVerse1[0]) || !strings.Contains(got, "grid: | C | F |") || len(song.Parse(got).Sections) != 2 {
		t.Fatalf("%s", got)
	}
}
