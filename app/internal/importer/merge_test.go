package importer

import (
	"reflect"
	"strings"
	"testing"

	"app/internal/song"
)

// All lyrics below are invented placeholder lines.

// A song map: header with map:, a line of text before the first section,
// Intro and Chorus with grids, everything else bare, one verse sung.
const mapSong = `title:  Quiet Road
artist: Nobody
key:    A
map:    Intro · Verse · Chorus · Verse · Chorus · Solo · Outro

play it loose and loud

[Intro]
grid: |: A | D . G . :|

[Verse]
walking down the quiet road

[Chorus]
grid: | A | D | E | % |

[Verse]

[Chorus]

[Solo]

[Outro]
`

const mapDraft = `title: Quiet Road
artist: Somebody Else
key: A
tempo: 120
time: 4/4

[Intro]
grid: | A | D | A | D |

[Verse 1]
grid: | A | % | D | % |
[A]walking down the quiet road
humming in the [D]rain

[Chorus]
grid: | D | E | A | A |
oh la la la
oh la la la

[Verse 2]
grid: | A | % | D | % |
counting all the paper stars
under neon skies

[Chorus]
oh la la la
oh la la la

[Interlude]
grid: | F | G |

[Outro]
grid: | A | E | A | A |
`

func sectionShape(sg *song.Song) []string {
	var out []string
	for _, s := range sg.Sections {
		g := "-"
		if len(s.Measures) > 0 {
			g = "G"
		}
		out = append(out, s.Label+":"+g)
	}
	return out
}

func TestMergeDraftSongMap(t *testing.T) {
	got, notes := MergeDraft(mapSong, mapDraft)
	before, after := song.Parse(mapSong), song.Parse(got)

	// Same sections, labels, order; existing grids untouched.
	if len(after.Sections) != len(before.Sections) {
		t.Fatalf("sections: %v -> %v", sectionShape(before), sectionShape(after))
	}
	for i, b := range before.Sections {
		a := after.Sections[i]
		if a.Label != b.Label {
			t.Errorf("section %d label %q -> %q", i, b.Label, a.Label)
		}
		if len(b.Measures) > 0 && !reflect.DeepEqual(a.Measures, b.Measures) {
			t.Errorf("%s: existing grid changed", b.Label)
		}
	}
	// Verse 1 had no grid and is the first verse: it takes the draft's.
	// The second verse, given its own lyrics, takes the first verse's
	// grid with them (or the viewer would take its chords from the
	// chorus before it); the second chorus stays bare (it repeats the
	// first); Solo stays bare (over the changes); Outro takes the draft's.
	want := []string{"Intro:G", "Verse:G", "Chorus:G", "Verse:G", "Chorus:-", "Solo:-", "Outro:G"}
	if s := sectionShape(after); !reflect.DeepEqual(s, want) {
		t.Errorf("shape = %v, want %v", s, want)
	}
	if g := after.Sections[1].Measures; len(g) != 4 || g[2].Tokens[0] != "D" {
		t.Errorf("verse grid = %+v", g)
	}
	// Existing lyrics kept, verse 2 gets the draft's second verse, the
	// bare chorus repeat gets nothing, the first chorus gets the chorus.
	lyr := func(i int) string {
		var ls []string
		for _, l := range after.Sections[i].Lyrics {
			var b strings.Builder
			for _, s := range l.Segments {
				b.WriteString(s.Text)
			}
			ls = append(ls, b.String())
		}
		return strings.Join(ls, "/")
	}
	if l := lyr(1); l != "walking down the quiet road" {
		t.Errorf("verse 1 lyrics = %q", l)
	}
	if l := lyr(2); l != "oh la la la/oh la la la" {
		t.Errorf("chorus lyrics = %q", l)
	}
	if l := lyr(3); l != "counting all the paper stars/under neon skies" {
		t.Errorf("verse 2 lyrics = %q", l)
	}
	if l := lyr(4); l != "" {
		t.Errorf("bare chorus repeat got lyrics %q", l)
	}

	// Header: tempo and time added, title/artist/key kept, map and the
	// text before the first section untouched.
	if after.Get("artist") != "Nobody" || after.Get("tempo") != "120" || after.Get("time") != "4/4" || after.Get("key") != "A" {
		t.Errorf("header = %+v", after.Meta)
	}
	if !strings.Contains(got, "map:    Intro · Verse · Chorus · Verse · Chorus · Solo · Outro\n") || !strings.Contains(got, "\nplay it loose and loud\n") {
		t.Errorf("header zone changed:\n%s", got)
	}
	if strings.Index(got, "tempo: 120") > strings.Index(got, "play it loose") {
		t.Errorf("tempo not in the header block:\n%s", got)
	}

	wantNotes := []string{
		"Header: tempo, time from the draft",
		"Verse 1: grid from the draft",
		"Outro: grid from the draft",
		"Chorus 1: lyrics added",
		"Verse 2: lyrics added",
		"The draft’s “Interlude”: no matching section here",
	}
	if !reflect.DeepEqual(notes, wantNotes) {
		t.Errorf("notes =\n%q\nwant\n%q", notes, wantNotes)
	}
	for _, n := range notes {
		for _, w := range []string{"walking", "oh la", "paper", "neon", "humming"} {
			if strings.Contains(n, w) {
				t.Errorf("note quotes lyrics: %q", n)
			}
		}
	}

	// Idempotent.
	again, _ := MergeDraft(got, mapDraft)
	if again != got {
		t.Errorf("second merge changed the text:\n%s\n---\n%s", got, again)
	}
}

func TestMergeDraftKeepsExistingGridsAndLyrics(t *testing.T) {
	existing := "title: T\n\n[Verse]\ngrid: | C | G |\nwalking down the quiet road\n\n[Verse]\nhumming in the rain\n\n[Chorus]\ngrid: | F | C |\nsinging to the moon\n"
	draft := "[Verse]\ngrid: | Am | Am |\nsomething else entirely\n\n[Verse]\ngrid: | Dm | Dm |\nanother thing\n\n[Chorus]\ngrid: | Bb | Bb |\nla la la\n"
	got, notes := MergeDraft(existing, draft)
	if got != existing {
		t.Errorf("a complete song changed:\n%s\nnotes %q", got, notes)
	}
	if len(notes) != 0 {
		t.Errorf("notes = %q", notes)
	}
}

func TestMergeDraftHeaderNeverOverwrites(t *testing.T) {
	existing := "title: Mine\nkey:\ncapo: 0\nflats: true\n\n[Verse]\ngrid: | C |\n"
	draft := "title: Theirs\nkey: G\ncapo: 2\ntempo: 90\n\n[Verse]\ngrid: | G |\n"
	got, notes := MergeDraft(existing, draft)
	sg := song.Parse(got)
	if sg.Get("title") != "Mine" || sg.Get("capo") != "0" || sg.Get("key") != "G" || sg.Get("tempo") != "90" || sg.Get("flats") != "true" {
		t.Errorf("header = %+v\n%s", sg.Meta, got)
	}
	// The empty key: line is filled where it is.
	if !strings.HasPrefix(got, "title: Mine\nkey: G\ncapo: 0\nflats: true\ntempo: 90\n\n[Verse]") {
		t.Errorf("got\n%s", got)
	}
	if notes[0] != "Header: key, tempo from the draft" {
		t.Errorf("notes = %q", notes)
	}
}

func TestMergeDraftKeyMismatchNote(t *testing.T) {
	existing := "key: A\n\n[Verse]\n\n[Chorus]\ngrid: | A |\n"
	draft := "key: G\n\n[Verse]\ngrid: | G | C |\n"
	_, notes := MergeDraft(existing, draft)
	found := false
	for _, n := range notes {
		if strings.HasPrefix(n, "Key: the song says A, the draft G") {
			found = true
		}
	}
	if !found {
		t.Errorf("no key note: %q", notes)
	}
}

func TestMergeDraftEmptySongIsReplace(t *testing.T) {
	for _, existing := range []string{"", "title: Quiet Road\nartist: Nobody\n\n[Verse]\n", "title: Quiet Road\n"} {
		got, notes := MergeDraft(existing, mapDraft)
		sg := song.Parse(got)
		if len(sg.Sections) != 7 || sg.Sections[1].Label != "Verse 1" {
			t.Errorf("%q: sections %v", existing, sectionShape(sg))
		}
		if notes[0] != "The song had no sections yet: took the draft's" {
			t.Errorf("%q: notes %q", existing, notes)
		}
		if strings.Contains(existing, "artist") && sg.Get("artist") != "Nobody" {
			t.Errorf("%q: artist overwritten: %+v", existing, sg.Meta)
		}
		if sg.Get("tempo") != "120" {
			t.Errorf("%q: tempo not merged: %+v", existing, sg.Meta)
		}
	}
}

func TestReplaceDraftKeepsHeaderZone(t *testing.T) {
	got, notes := ReplaceDraft(mapSong, mapDraft)
	sg := song.Parse(got)
	if sg.Get("artist") != "Nobody" || sg.Get("tempo") != "120" || len(sg.Sections) != 7 {
		t.Errorf("got %+v %v", sg.Meta, sectionShape(sg))
	}
	if !strings.Contains(got, "play it loose and loud") {
		t.Errorf("text before the sections lost")
	}
	if notes[len(notes)-1] != "map: kept from the song: check it against the new sections" {
		t.Errorf("notes %q", notes)
	}
}

func TestMergeDraftExtraVerses(t *testing.T) {
	existing := "[Verse]\ngrid: | C |\n\n[Chorus]\ngrid: | F |\n"
	draft := "[Verse 1]\nwalking down the quiet road\n\n[Chorus]\noh la la la\n\n[Verse 2]\ncounting all the paper stars\n\n[Verse 3]\nunder neon skies\n"
	got, notes := MergeDraft(existing, draft)
	want := []string{"Verse: lyrics added", "Chorus: lyrics added",
		"The draft’s “Verse 2”: no section left for it here, its lyrics not placed",
		"The draft’s “Verse 3”: no section left for it here, its lyrics not placed"}
	if !reflect.DeepEqual(notes, want) {
		t.Errorf("notes = %q\n%s", notes, got)
	}
	if len(song.Parse(got).Sections) != 2 {
		t.Errorf("sections added:\n%s", got)
	}
}
