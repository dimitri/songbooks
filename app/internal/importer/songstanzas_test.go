package importer

import (
	"reflect"
	"strings"
	"testing"
)

func stanzaShape(st []Stanza) []string {
	var out []string
	for _, s := range st {
		out = append(out, s.Label+":"+strings.Join(s.Lines, "/"))
	}
	return out
}

// Invented lyrics. A whole song left in one section, no blank lines: the
// chorus is found by its repeats, the verses between it.
func TestSongStanzasOneSection(t *testing.T) {
	body := "[Verse]\ngrid: | C |\nv one a\nv one b\nla la la one\nla la la two\nv two a\nv two b\nla la la one\nla la la two\n"
	got := stanzaShape(SongStanzas(body))
	want := []string{"Verse:v one a/v one b", "Chorus:la la la one/la la la two", "Verse:v two a/v two b", "Chorus:la la la one/la la la two"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// Spread over sections, with blank lines: labels kept, blocks split.
func TestSongStanzasSections(t *testing.T) {
	body := "[Verse]\nv one a\nv one b\n\nv two a\nv two b\n\n[Bridge]\nhigh above the town\n"
	got := stanzaShape(SongStanzas(body))
	want := []string{"Verse:v one a/v one b", "Verse:v two a/v two b", "Bridge:high above the town"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// Placed into a song map: verse into verse, the chorus into every chorus.
func TestSongStanzasIntoAMap(t *testing.T) {
	old := "[Verse]\nv one a\nv one b\nla la la one\nla la la two\nv two a\nv two b\nla la la one\nla la la two\n"
	songMap := "map: Intro · Verse 1 · Chorus · Verse 2 · Chorus\n\n[Intro]\ngrid: | C |\n\n[Verse 1]\ngrid: | C | F |\n\n[Chorus]\ngrid: | F | G |\n\n[Verse 2]\ngrid: | C | F |\n\n[Chorus]\ngrid: | F | G |\n"
	got, ps, unused := PlaceLyrics(songMap, SongStanzas(old), false)
	if len(unused) != 0 || placementsString(ps) != "Intro=instrumental:-1 Verse 1=placed:0 Chorus=placed:1 Verse 2=placed:2 Chorus=reused:1" {
		t.Fatalf("placements %s unused %v:\n%s", placementsString(ps), unused, got)
	}
}
