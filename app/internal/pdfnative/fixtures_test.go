package pdfnative

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Synthetic, invented content only: every title, artist and lyric word
// below is made up (lyric lines are strings of nonsense syllables), so
// these fixtures never carry any real song's words.

var demoSyllables = []string{"la", "mo", "ri", "ven", "ta", "so", "lu", "ka", "ne", "do", "mi", "ra", "pe", "lo", "zan", "tu"}

// demoWord returns a deterministic nonsense word.
func demoWord(seed int) string {
	n := 1 + seed%3
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(demoSyllables[(seed*7+i*5)%len(demoSyllables)])
	}
	return b.String()
}

// demoLyric returns one chorded nonsense line of about words words.
func demoLyric(seed, words int, chords []string) string {
	var parts []string
	for i := 0; i < words; i++ {
		w := demoWord(seed*13 + i)
		if i%3 == 0 {
			w = "[" + chords[(seed+i/3)%len(chords)] + "]" + w
		}
		parts = append(parts, w)
	}
	return strings.Join(parts, " ")
}

// demoSong builds a .song body with a realistic shape: an instrumental
// intro, verses and choruses with bar grids and lyric lines, a bridge,
// an outro. long makes the sections bigger.
func demoSong(title, artist, key, tempo string, seed int, long bool) string {
	chords := []string{"C", "Am", "F", "G7", "Dm7", "E7", "Bb", "Gsus4"}
	grid := func(n, s int) string {
		var b strings.Builder
		b.WriteString("grid:")
		for i := 0; i < n; i++ {
			b.WriteString(" | " + chords[(s+i)%len(chords)])
		}
		b.WriteString(" |")
		return b.String()
	}
	lines := 4
	if long {
		lines = 7
	}
	var b strings.Builder
	fmt.Fprintf(&b, "title: %s\nartist: %s\nkey: %s\ntempo: %s\ntime: 4/4\n\n", title, artist, key, tempo)
	fmt.Fprintf(&b, "[Intro]\n%s\n\n", grid(4, seed))
	for v, label := range []string{"Verse", "Chorus", "Verse 2", "Chorus", "Bridge", "Chorus", "Outro"} {
		fmt.Fprintf(&b, "[%s]\n", label)
		if label == "Outro" {
			fmt.Fprintf(&b, "%s\nnote: ritardando on the last bar\n\n", grid(4, seed+v))
			continue
		}
		fmt.Fprintf(&b, "%s\n", grid(8, seed+v))
		for l := 0; l < lines; l++ {
			fmt.Fprintf(&b, "%s\n", demoLyric(seed*31+v*7+l, 7+(l%3), chords))
		}
		b.WriteString("\n")
	}
	return b.String()
}

var demoArtists = []string{
	"The Velvet Okapis",
	"Odalys Brightwater & the Copper Lantern Horns",
	"Minnow Parade",
	"Rufus Calloway-Thistlewood Quartet",
	"Saffron Delay",
}

// demoSetlist returns 27 SongRefs grouped Set 1 / Set 2 / Encore.
func demoSetlist() []SongRef {
	keys := []string{"C", "G", "Dm", "Bb", "E", "F#m", "A"}
	var refs []SongRef
	for i := 0; i < 27; i++ {
		group := "Set 1"
		switch {
		case i >= 24:
			group = "Encore"
		case i >= 12:
			group = "Set 2"
		}
		title := fmt.Sprintf("%s %s", strings.Title(demoWord(i*3+1)), strings.Title(demoWord(i*5+2)))
		if i%6 == 0 {
			title += " (Extended Groove Version)"
		}
		refs = append(refs, SongRef{
			Group: group,
			Stem:  fmt.Sprintf("demo-%d", i),
			Body:  demoSong(title, demoArtists[i%len(demoArtists)], keys[i%len(keys)], fmt.Sprint(80+i*3), i, i%4 == 0),
		})
	}
	return refs
}

const demoLongTitle = "The Velvet Okapis — full set (from the spring tour rehearsal notes)"

// TestDumpDemoPDFs writes the demo exports to $PDFNATIVE_DUMP_DIR for a
// human to look at (skipped when unset).
func TestDumpDemoPDFs(t *testing.T) {
	dir := os.Getenv("PDFNATIVE_DUMP_DIR")
	if dir == "" {
		t.Skip("set PDFNATIVE_DUMP_DIR to write the demo PDFs")
	}
	r := New()
	ctx := context.Background()
	refs := demoSetlist()
	front := FrontMatter{EventTitle: demoLongTitle, BandName: "The Velvet Okapis", Date: "2026-05-14", Venue: "The Lantern Room"}
	write := func(name string, data []byte, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []Mode{ModeDefault, ModeJazz, ModeMap, ModeLyrics} {
		name := string(m)
		if name == "" {
			name = "song"
		}
		data, err := r.ExportSetlist(ctx, demoLongTitle, FrontMatter{}, refs, Options{Mode: m, Columns: setlistDumpCols()})
		write("setlist-"+name+".pdf", data, err)
	}
	data, err := r.ExportSetlist(ctx, demoLongTitle, front, refs, Options{Columns: setlistDumpCols()})
	write("setlist-gig.pdf", data, err)
	data, err = r.ExportSetlistPageOnly(ctx, demoLongTitle, "2026-05-14 · The Lantern Room", refs, Options{})
	write("setlist-page.pdf", data, err)
	data, err = r.ExportSong(ctx, "", refs[0].Body, nil, Options{})
	write("single-song.pdf", data, err)
}

// setlistDumpCols mirrors what the setlist export route passes for
// Options.Columns (PDFNATIVE_DUMP_COLS overrides, for before/after
// comparisons).
func setlistDumpCols() int {
	if v := os.Getenv("PDFNATIVE_DUMP_COLS"); v != "" {
		var n int
		fmt.Sscan(v, &n)
		return n
	}
	return 0
}
