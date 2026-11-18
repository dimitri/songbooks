package importer

import (
	"strings"
	"testing"

	"app/internal/song"
)

func TestSongMap(t *testing.T) {
	draft := "title: T\nartist: A\ncapo: 2\nwalking down the quiet road\n\n[Intro]\ngrid: | C | G |\n\n[Verse 1]\ngrid: | C | F |\nwalking down the quiet road\ncounting all the paper stars\n\n[Chorus]\noh la la la\n\n[Verse 2]\ngrid: | C | F |\nunder neon skies\n"
	got := SongMap(draft)
	for _, w := range []string{"walking", "counting", "la la", "neon"} {
		if strings.Contains(got, w) {
			t.Fatalf("lyric %q left in:\n%s", w, got)
		}
	}
	sg := song.Parse(got)
	var labels []string
	for _, s := range sg.Sections {
		labels = append(labels, s.Label)
	}
	if strings.Join(labels, ",") != "Intro,Verse 1,Chorus,Verse 2" {
		t.Fatalf("sections %v:\n%s", labels, got)
	}
	if len(sg.Sections[0].Measures) != 2 || len(sg.Sections[2].Measures) != 0 || sg.Get("capo") != "2" {
		t.Fatalf("grids or header lost:\n%s", got)
	}
	if sg.Get("map") != "Intro · Verse 1 · Chorus · Verse 2" {
		t.Fatalf("map = %q", sg.Get("map"))
	}
	if SongMap(got) != got {
		t.Fatalf("not idempotent")
	}
}

func TestSongMapDropsLeadingEmpty(t *testing.T) {
	got := SongMap("title: T\n\n[Verse]\n\n[Intro]\ngrid: | Am | Dm |\n\n[Verse 1]\ngrid: | Am |\n\n[Chorus]\n")
	sg := song.Parse(got)
	if len(sg.Sections) != 3 || sg.Sections[0].Label != "Intro" || sg.Get("map") != "Intro · Verse 1 · Chorus" {
		t.Fatalf("%s", got)
	}
	body := "title: T\nmap: Verse · Intro · Verse 1\n\n[Verse]\n\n[Intro]\ngrid: | Am |\n\n[Verse 1]\ngrid: | Am |\nwalking down the quiet road\n"
	tidy, gone := TidyMap(body)
	if gone != 1 || song.Parse(tidy).Get("map") != "Intro · Verse 1" || !strings.Contains(tidy, "walking down the quiet road") {
		t.Fatalf("gone %d:\n%s", gone, tidy)
	}
	if again, n := TidyMap(tidy); n != 0 || again != tidy {
		t.Fatal("not idempotent")
	}
}
