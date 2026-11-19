package main

import (
	"strings"
	"testing"

	"app/internal/song"
)

// An invented song in the app's format: its "lyrics" are made up for
// the test.
const sampleBody = `title: Original Title
artist: Original Artist
reference: https://www.youtube.com/watch?v=xxxxxxxxxxx
cover: https://example.com/cover.jpg
cover-credit: Somebody
gp_source: Original Title (live).gp
tempo: 96
key: Am
map: Intro · Verse · Chorus · Bridge (moon river) · Chorus
# guitar lead: a real name

[Intro]
grid: | Am | F | C | G |

[Verse]
grid: | Am | F | C | G |
the kettle sings on the stove
we count the cars on the hill

[Chorus]
grid: | F | G | Am | Am |
la la la under the bridge
# a commented line

[Bridge (moon river)]
grid: | Dm | E7 |
one more made up line

[Chorus] (repeat)
`

func lyricLines(body string) int {
	n := 0
	for _, s := range song.Parse(body).Sections {
		n += len(s.Lyrics)
	}
	return n
}

func TestDemoBodyWithoutLyrics(t *testing.T) {
	if lyricLines(sampleBody) == 0 {
		t.Fatal("the sample should have lyrics")
	}
	out := DemoBody(sampleBody, "Cobalt Morning", "Velvet Hour", "Cobalt Morning.gp", nil)
	s := song.Parse(out)
	if s.Get("title") != "Cobalt Morning" || s.Get("artist") != "Velvet Hour" || s.Get("gp_source") != "Cobalt Morning.gp" {
		t.Fatalf("header: %q / %q / %q", s.Get("title"), s.Get("artist"), s.Get("gp_source"))
	}
	if s.Get("tempo") != "96" || s.Get("key") != "Am" {
		t.Fatal("tempo and key should stay")
	}
	for _, k := range []string{"reference", "cover", "cover-credit"} {
		if s.Get(k) != "" {
			t.Errorf("%s: should be gone", k)
		}
	}
	if n := lyricLines(out); n != 0 {
		t.Fatalf("%d lyric lines left", n)
	}
	for _, gone := range []string{"Original", "#", "kettle", "moon river", "real name"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q still in the body", gone)
		}
	}
	if len(s.Sections) != 5 {
		t.Fatalf("%d sections, want 5 (sections stay for the recording's markers)", len(s.Sections))
	}
	if s.Sections[3].Label != "Bridge" || !strings.Contains(out, "map: Intro · Verse · Chorus · Bridge · Chorus") {
		t.Fatalf("a label with words should become its kind, in map: too:\n%s", out)
	}
	if len(s.Sections[2].Measures) != 4 {
		t.Fatal("grids should stay")
	}
	// Again: nothing changes.
	if again := DemoBody(out, "Cobalt Morning", "Velvet Hour", "Cobalt Morning.gp", nil); again != out {
		t.Fatalf("not idempotent:\n%s\n---\n%s", out, again)
	}
}

func TestDemoBodyAddsTitleWhenMissing(t *testing.T) {
	out := DemoBody("tempo: 100\n\n[Verse]\ngrid: | C |\nsome invented words\n", "Night Train to Arles", "Nord Express", "", nil)
	s := song.Parse(out)
	if s.Get("title") != "Night Train to Arles" || s.Get("artist") != "Nord Express" || lyricLines(out) != 0 {
		t.Fatalf("got:\n%s", out)
	}
}

func TestDemoBodyInventedLyrics(t *testing.T) {
	out := DemoBody(sampleBody, "Cobalt Morning", "Velvet Hour", "", demoLyrics(0))
	s := song.Parse(out)
	got := map[string]int{}
	for _, sec := range s.Sections {
		got[sec.Label] += len(sec.Lyrics)
	}
	if got["Intro"] != 0 || got["Verse"] != 4 || got["Bridge"] != 2 {
		t.Fatalf("lyrics per section: %v\n%s", got, out)
	}
	// The chorus once, not in its repeat.
	if got["Chorus"] != 4 {
		t.Fatalf("chorus lyrics: %d", got["Chorus"])
	}
	if len(s.Sections[2].Measures) != 4 {
		t.Fatal("a section's grid should stay before its lyrics")
	}
	// Run again (as a second anonymize does): the same body.
	if again := DemoBody(out, "Cobalt Morning", "Velvet Hour", "", demoLyrics(0)); again != out {
		t.Fatalf("not idempotent:\n%s\n---\n%s", out, again)
	}
}

func TestLabelOK(t *testing.T) {
	for label, want := range map[string]bool{
		"Verse 2": true, "Pre-Chorus": true, "Solo (harmonica)": true, "Instrumental, Solo 2, Outro": true,
		"Head (up a semitone)": true, "Bridge (I know)": false, "Bones": false,
	} {
		if got := labelOK(label); got != want {
			t.Errorf("labelOK(%q) = %v", label, got)
		}
	}
}
