package latexsong

import (
	"strings"
	"testing"

	"app/internal/song"
)

func sectionLabels(s *song.Song) []string {
	var out []string
	for _, sec := range s.Sections {
		out = append(out, sec.Label)
	}
	return out
}

func TestParseInlineChords(t *testing.T) {
	tex := `\beginsong{The Boxer}[by={Simon \& Garfunkel}]

  \beginverse
  \[C]I am just a poor boy
  Though my story's \[C/B]seldom \[Am]told
  I have \[G]squandered my resistance
  Still a \[G]man hears what he \[F]wants to hear
  And disregards the \[C]rest \[G] \[F] \[G] \[C]
  \endverse

  \beginchorus
  Lie-la-\[Am]lie
  Lie-la-\[Em]lie-lie-lie-lie-lie
  \endchorus
\endsong
`
	out, err := Parse(tex)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d songs, want 1", len(out))
	}
	ps := out[0]
	if ps.Title != "The Boxer" || ps.Artist != "Simon & Garfunkel" {
		t.Fatalf("title/artist = %q/%q", ps.Title, ps.Artist)
	}
	if got := sectionLabels(ps.Song); len(got) != 2 || got[0] != "Verse" || got[1] != "Chorus" {
		t.Fatalf("sections = %v", got)
	}
	verse := ps.Song.Sections[0]
	if len(verse.Lyrics) != 5 {
		t.Fatalf("verse lyric lines = %d, want 5", len(verse.Lyrics))
	}
	first := verse.Lyrics[0]
	if len(first.Segments) == 0 || !first.Segments[0].HasChord || first.Segments[0].Chord != "C" || first.Segments[0].Text != "I am just a poor boy" {
		t.Fatalf("first lyric line segments = %+v", first.Segments)
	}
	// "And disregards the [C]rest [G] [F] [G] [C]" — real text up front,
	// trailing bare chords with empty-text segments after "rest".
	last := verse.Lyrics[4]
	if last.Segments[0].Text != "And disregards the " {
		t.Fatalf("leading plain-text segment = %+v", last.Segments[0])
	}
	if last.Segments[1].Chord != "C" || last.Segments[1].Text != "rest " {
		t.Fatalf("first chord segment = %+v", last.Segments[1])
	}
	if len(verse.Measures) != 11 { // C,C/B,Am,G,G(%),F,C,G,F,G,C — every chord across the whole verse
		t.Fatalf("verse grid = %d measures, want 11: %+v", len(verse.Measures), verse.Measures)
	}
}

func TestParseTextnoteLabelsSection(t *testing.T) {
	tex := `\beginsong{Sultans of Swing}[by={Dire Straits}]
  \textnote{Instrumental}
  \textnote{Bridge}
  \beginverse
  Some words
  \endverse
\endsong
`
	out, err := Parse(tex)
	if err != nil {
		t.Fatal(err)
	}
	labels := sectionLabels(out[0].Song)
	// First \textnote never got a verse/chorus before the SECOND \textnote
	// overwrote it — flushed as its own standalone section.
	if len(labels) != 2 || labels[0] != "Instrumental" || labels[1] != "Bridge" {
		t.Fatalf("labels = %v", labels)
	}
	if len(out[0].Song.Sections[0].Lyrics) != 0 || len(out[0].Song.Sections[0].Measures) != 0 {
		t.Fatalf("standalone textnote section should carry no content: %+v", out[0].Song.Sections[0])
	}
}

func TestParseChordsoffStopsChords(t *testing.T) {
	tex := `\beginsong{Wicked Game}[by={Chris Isaak}]
  \beginverse
  \[Am]The world was on fire
  \endverse
  \chordsoff
  \beginverse
  No one could save me but you
  \endverse
\endsong
`
	out, err := Parse(tex)
	if err != nil {
		t.Fatal(err)
	}
	s := out[0].Song
	if len(s.Sections[0].Measures) != 1 {
		t.Fatalf("first verse should still have its chord: %+v", s.Sections[0])
	}
	if len(s.Sections[1].Measures) != 0 {
		t.Fatalf("second verse (after \\chordsoff) should have no grid: %+v", s.Sections[1])
	}
	if s.Sections[1].Lyrics[0].Segments[0].Text != "No one could save me but you" {
		t.Fatalf("post-chordsoff lyric wrong: %+v", s.Sections[1].Lyrics[0])
	}
}

func TestParseNolyricsAndFlatNotation(t *testing.T) {
	tex := `\beginsong{Nights in White Satin}[by={Moody Blues}]
  \beginverse
  {\nolyrics \[Em] \[D] \[Em] \[D]}
  Gazing at peo^ple, some \[A&]hand in hand
  \endverse
\endsong
`
	out, err := Parse(tex)
	if err != nil {
		t.Fatal(err)
	}
	verse := out[0].Song.Sections[0]
	if len(verse.Lyrics) != 1 {
		t.Fatalf("nolyrics line must not become a Lyrics entry: %+v", verse.Lyrics)
	}
	if verse.Lyrics[0].Segments[0].Text != "Gazing at people, some " {
		t.Fatalf("caret not stripped: %q", verse.Lyrics[0].Segments[0].Text)
	}
	if verse.Lyrics[0].Segments[1].Chord != "Ab" {
		t.Fatalf("'&' flat not normalized to 'b': %+v", verse.Lyrics[0].Segments[1])
	}
	if len(verse.Measures) != 5 { // Em, D, Em, D (nolyrics) + Ab (inline)
		t.Fatalf("grid = %d measures, want 5: %+v", len(verse.Measures), verse.Measures)
	}
}

func TestParseCapoAndTranspose(t *testing.T) {
	tex := `\beginsong{I Hope I Don't Fall In Love}[by={Tom Waits}]
  \capo{2}
  \transpose{1}
  \beginverse
  \[Am]Well I hope that I don't fall in love with you
  \endverse
\endsong
`
	out, err := Parse(tex)
	if err != nil {
		t.Fatal(err)
	}
	s := out[0].Song
	if s.Get("capo") != "2" {
		t.Fatalf("capo = %q, want 2", s.Get("capo"))
	}
	if s.Get("transpose") != "1" {
		t.Fatalf("transpose = %q, want 1", s.Get("transpose"))
	}
}

func TestParseAmpersandUnescape(t *testing.T) {
	tex := `\beginsong{Whiskey, Beer \& Wine}[by={Parov Stelar}]
\endsong
`
	out, err := Parse(tex)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Title != "Whiskey, Beer & Wine" {
		t.Fatalf("title = %q", out[0].Title)
	}
}

func TestParseWriteRoundTrip(t *testing.T) {
	tex := `\beginsong{The Boxer}[by={Simon \& Garfunkel}]
  \beginverse
  \[C]I am just a poor boy
  \endverse
\endsong
`
	out, err := Parse(tex)
	if err != nil {
		t.Fatal(err)
	}
	body := song.Write(out[0].Song)
	reparsed := song.Parse(body)
	if len(reparsed.Sections) != 1 || reparsed.Sections[0].Lyrics[0].Segments[0].Chord != "C" {
		t.Fatalf("round-tripped song wrong: %+v", reparsed.Sections)
	}
}

func TestExportParseRoundTrip(t *testing.T) {
	tex := `\beginsong{The Boxer}[by={Simon \& Garfunkel}]
  \capo{2}
  \beginverse
  \[C]I am just a poor boy
  Though my story's \[C/B]seldom \[Am]told
  \endverse

  \beginchorus
  Lie-la-\[Am]lie
  \endchorus

  \textnote{Outro}
\endsong
`
	out, err := Parse(tex)
	if err != nil {
		t.Fatal(err)
	}
	exported := Export(out[0].Song)
	reimported, err := Parse(exported)
	if err != nil {
		t.Fatalf("Export output didn't re-Parse: %v\n%s", err, exported)
	}
	if len(reimported) != 1 {
		t.Fatalf("got %d songs back, want 1", len(reimported))
	}
	back := reimported[0]
	if back.Title != "The Boxer" || back.Artist != "Simon & Garfunkel" {
		t.Fatalf("title/artist lost: %q / %q", back.Title, back.Artist)
	}
	if back.Song.Get("capo") != "2" {
		t.Fatalf("capo lost: %q", back.Song.Get("capo"))
	}
	wantLabels := []string{"Verse", "Chorus", "Outro"}
	gotLabels := sectionLabels(back.Song)
	if len(gotLabels) != len(wantLabels) {
		t.Fatalf("section labels = %v, want %v", gotLabels, wantLabels)
	}
	for i, l := range wantLabels {
		if gotLabels[i] != l {
			t.Fatalf("section[%d] = %q, want %q (all: %v)", i, gotLabels[i], l, gotLabels)
		}
	}
	verse := back.Song.Sections[0]
	if verse.Lyrics[0].Segments[0].Chord != "C" || verse.Lyrics[0].Segments[0].Text != "I am just a poor boy" {
		t.Fatalf("verse first segment wrong: %+v", verse.Lyrics[0].Segments[0])
	}
}

func TestParseChordsoffLeavesStrayMarkupStripped(t *testing.T) {
	tex := `\beginsong{Every Breath You Take}[by={The Police}]
  \beginverse
  \[G]Every breath you take
  \endverse
  \chordsoff
  \beginverse
  Every move you \[G]make
  And every vow you \[Em]break
  I'll be watching you \[G]
  \endverse
  \textnote{Bridge}
  \chordson
  \beginverse
  \[Eb]Since you've gone
  \endverse
\endsong
`
	out, err := Parse(tex)
	if err != nil {
		t.Fatal(err)
	}
	s := out[0].Song
	// sections: Verse (chorded), Verse (chordsoff), Bridge (chordson again)
	if len(s.Sections) != 3 {
		t.Fatalf("got %d sections, want 3: %+v", len(s.Sections), s.Sections)
	}
	offVerse := s.Sections[1]
	if len(offVerse.Measures) != 0 {
		t.Fatalf("chordsoff verse should have no grid: %+v", offVerse.Measures)
	}
	for _, ln := range offVerse.Lyrics {
		for _, seg := range ln.Segments {
			if strings.Contains(seg.Text, `\[`) || strings.Contains(seg.Text, `]`) {
				t.Fatalf("stray LaTeX chord markup leaked into lyric text: %q", seg.Text)
			}
		}
	}
	if offVerse.Lyrics[0].Segments[0].Text != "Every move you make" {
		t.Fatalf("first chordsoff line = %q", offVerse.Lyrics[0].Segments[0].Text)
	}
	bridge := s.Sections[2]
	if bridge.Label != "Bridge" || len(bridge.Measures) == 0 {
		t.Fatalf("chordson bridge should have its grid back: %+v", bridge)
	}
}
