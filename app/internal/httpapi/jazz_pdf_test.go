package httpapi

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"app/internal/pdfnative"
	"app/internal/song"
)

// jazzPDFOf is src's Jazz chart PDF, as the song's export route makes
// it, and the chart handed to pdfnative.
func jazzPDFOf(t *testing.T, src string) ([]byte, *pdfnative.JazzChart) {
	t.Helper()
	sg := song.Parse(src)
	ch := jazzPDFChart(sg, buildJazzChart(sg, buildSections(sg)), "en")
	data, err := pdfnative.New().ExportJazzChart(context.Background(), ch)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "%PDF") {
		t.Fatal("not a PDF")
	}
	return data, ch
}

var pdfPageRe = regexp.MustCompile(`/Type /Page[^s]`)

func pdfPageCount(data []byte) int { return len(pdfPageRe.FindAll(data, -1)) }

// pdfText is the PDF's text (pdftotext, poppler), or a skip where it
// isn't installed.
func pdfText(t *testing.T, data []byte) string {
	t.Helper()
	bin, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("pdftotext not installed")
	}
	f := filepath.Join(t.TempDir(), "chart.pdf")
	if err := os.WriteFile(f, data, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, f, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

const jzLyric = "Zebrafinch marmalade wanders"

// A form with repeats and endings, a simile, in 6/8: the PDF carries the
// web chart's own cells — endings, repeat barlines, "%", the time
// signature, the tempo's dotted quarter — on one page, and no lyric.
func TestJazzPDFEndingsSimileCompound(t *testing.T) {
	src := "title: Velvet Test\nartist: Nobody\nkey: Bb\ntempo: 72\ntime: 6/8\n\n" +
		"[Intro]\ngrid: | A | % | C#m | C |\n\n" +
		"[Verse]\ngrid: | A | Ab | C#m | C | Emaj7 | C#m | C | F#m . F . |\n" + jzLyric + "\n\n" +
		"[Chorus]\ngrid: |: (1,2,3.) D | G . E9 . | (4.) C . Bm7b5/F# . | Ebmaj7 :|\n\n" +
		"[Verse]\n\n[Chorus]\n"
	if sg := song.Parse(src); len(sg.Sections[1].Lyrics) == 0 {
		t.Fatal("the test song has no lyric line to leave out")
	}
	data, ch := jazzPDFOf(t, src)
	if !ch.Compound || ch.Tempo != "72" || ch.KeyRoot != (pdfnative.JazzNote{Letter: "B", Acc: "b"}) || ch.KeyWord != "Key" {
		t.Errorf("header: %+v", ch)
	}
	var volta, simile, repStart, repEnd, ts bool
	var slash pdfnative.JazzChord
	for _, ln := range ch.Lines {
		for _, c := range ln.Cells {
			volta = volta || (c.VoltaSpan > 0 && c.Volta == "1, 2, 3.")
			simile = simile || c.Simile == 1
			repStart = repStart || c.Left == "rep-start"
			repEnd = repEnd || c.Right == "rep-end"
			ts = ts || (c.TSNum == "6" && c.TSDen == "8")
			for _, chd := range c.Chords {
				if chd.Bass.Letter != "" {
					slash = chd
				}
			}
		}
	}
	if !volta || !simile || !repStart || !repEnd || !ts {
		t.Errorf("volta %v simile %v repeats %v/%v time %v", volta, simile, repStart, repEnd, ts)
	}
	// B−7♭5/F♯: the root, the minor sign, the extension with its flat as a
	// glyph, the bass and its sharp — the page's jzChordHTML, run for run.
	want := pdfnative.JazzChord{Root: pdfnative.JazzNote{Letter: "B"}, Bass: pdfnative.JazzNote{Letter: "F", Acc: "#"}, Quality: []pdfnative.JazzRun{
		{S: jzGlyphMin, Kind: pdfnative.JazzRunMinor}, {S: "7", Kind: pdfnative.JazzRunText}, {S: jzGlyphFlat, Kind: pdfnative.JazzRunAcc}, {S: "5", Kind: pdfnative.JazzRunText},
	}}
	slash.X, slash.Pad = 0, 0
	if got := slash; got.Root != want.Root || got.Bass != want.Bass || len(got.Quality) != len(want.Quality) {
		t.Errorf("slash chord: %+v", got)
	} else {
		for i := range want.Quality {
			if got.Quality[i] != want.Quality[i] {
				t.Errorf("quality run %d: %+v, want %+v", i, got.Quality[i], want.Quality[i])
			}
		}
	}
	if n := pdfPageCount(data); n != 1 {
		t.Errorf("%d pages, want 1", n)
	}
	text := pdfText(t, data)
	for _, w := range []string{"Velvet Test", "Nobody", "Key", "Intro"} {
		if !strings.Contains(text, w) {
			t.Errorf("PDF text lacks %q:\n%s", w, text)
		}
	}
	for _, w := range strings.Fields(jzLyric) {
		if strings.Contains(text, w) {
			t.Errorf("PDF prints the lyric %q", w)
		}
	}
}

// A form too long for one page runs on over pages (full size, more than
// one page), each continued page under the song's title and "(cont.)".
func TestJazzPDFLongForm(t *testing.T) {
	var b strings.Builder
	b.WriteString("title: Long Form\ntempo: 120\n\n")
	roots := []string{"C", "D", "E", "F", "G", "A", "B"}
	for i := 0; i < 26; i++ {
		// Every section its own grid: nothing for the form to fold.
		r1, r2 := roots[i%7], roots[(i/7+i)%7]
		b.WriteString("[Part " + string(rune('A'+i)) + "]\n")
		b.WriteString("grid: | " + r1 + "m7 | " + r2 + "7 | " + r1 + " | " + r2 + "m | " + r2 + " | " + r1 + "7 |\n" + jzLyric + "\n\n")
	}
	data, ch := jazzPDFOf(t, b.String())
	if len(ch.Lines) < 40 {
		t.Fatalf("%d lines: the form folded more than expected", len(ch.Lines))
	}
	if n := pdfPageCount(data); n < 2 {
		t.Errorf("%d pages, want the form over 2 or more", n)
	}
	text := pdfText(t, data)
	if strings.Contains(text, "Zebrafinch") {
		t.Error("PDF prints a lyric")
	}
	// Each continued page opens with the title, small: "Long Form (cont.)".
	if got, want := strings.Count(text, "Long Form (cont.)"), pdfPageCount(data)-1; got != want {
		t.Errorf("%d continued titles, want %d", got, want)
	}
	if fr := jazzPDFChart(song.Parse("title: X\n"), nil, "fr"); fr.ContWord != "suite" || fr.KeyWord != "Tonalité" {
		t.Errorf("fr words: %q %q", fr.ContWord, fr.KeyWord)
	}
}

// A song with no grid prints its header, as its Jazz view shows no bars.
func TestJazzPDFNoGrid(t *testing.T) {
	data, ch := jazzPDFOf(t, "title: Words Only\nartist: Someone\nkey: Am\n\n[Verse]\n"+jzLyric+"\n")
	if len(ch.Lines) != 0 || ch.Title != "Words Only" || ch.Key != "m" || ch.KeyRoot.Letter != "A" {
		t.Errorf("chart: %+v", ch)
	}
	if n := pdfPageCount(data); n != 1 {
		t.Errorf("%d pages", n)
	}
}

// The tempo says its beat: a dotted quarter in a compound meter, on the
// page as in the PDF.
func TestJazzTempoCompound(t *testing.T) {
	for _, tc := range []struct {
		time string
		want bool
	}{{"6/8", true}, {"12/8", true}, {"9/8", true}, {"3/8", false}, {"4/4", false}, {"", false}} {
		sg := song.Parse("title: T\ntempo: 72\ntime: " + tc.time + "\n\n[A]\ngrid: | C | G |\n")
		ch := buildJazzChart(sg, buildSections(sg))
		if ch.Compound != tc.want {
			t.Errorf("%q: compound %v", tc.time, ch.Compound)
		}
		if p := jazzPDFChart(sg, ch, "en"); p.Compound != tc.want {
			t.Errorf("%q: PDF compound %v", tc.time, p.Compound)
		}
	}
}
