package lilypond

import (
	"strings"
	"testing"

	"app/internal/song"
)

func TestRenderBasic(t *testing.T) {
	src := `title: Test Song
artist: Someone
tempo: 120
time: 4/4

[Verse]
grid: |: Gm | Bb . D . :|
[Gm]Some [Bb]words here

[Chorus]
grid: | C | G7 | Am | % |
la la la la
`
	s := song.Parse(src)
	out := Render(s)

	for _, want := range []string{
		`title = "Test Song"`,
		`composer = "Someone"`,
		`\tempo 4 = 120`,
		`\chordmode`,
		`\lyricmode`,
		`g1*1/1:m`, // whole bar Gm: k=1, span=1 -> 4*1/(4*1) = 1 whole note
		`bes1*1/2`, // "Bb ." merged into one span-2 group of a 4-token bar -> 4*2/(4*4) = 1/2
		`c1*1/1`,   // single-token "C" bar in the Chorus grid -> whole note
		`g4:m`,     // lyric-driven slot ("Some"): fixed quarter note, not grid-derived
		`\mark \markup { \bold "Verse" }`,
		`\mark \markup { \bold "Chorus" }`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- full output ---\n%s", want, out)
		}
	}
}

func TestRenderNoLyrics(t *testing.T) {
	src := "title: Instrumental\n\n[Intro]\ngrid: | F:maj7 | Bb:7 |\n"
	s := song.Parse(src)
	out := Render(s)
	if strings.Contains(out, "\\lyricmode") {
		t.Errorf("expected no lyrics context for a lyric-less song:\n%s", out)
	}
	if !strings.Contains(out, "f1*1/1:maj7") {
		t.Errorf("expected an f:maj7 chord atom with its duration spliced in before the colon:\n%s", out)
	}
}
