package pdfnative

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"app/internal/song"

	"github.com/signintech/gopdf"
)

func testDoc(t *testing.T) *gopdf.GoPdf {
	t.Helper()
	doc, err := newDoc()
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

var pageObjRE = regexp.MustCompile(`/Type /Page\b`)

func pdfPageCount(data []byte) int { return len(pageObjRE.FindAll(data, -1)) }

// The cover title wraps onto several lines, every one of them within the
// cover's margins, however long the title (even one unbreakable word).
func TestCoverTitleWrapsWithinMargins(t *testing.T) {
	doc := testDoc(t)
	cases := []struct {
		title    string
		minLines int
	}{
		{"Songbook", 1},
		{demoLongTitle, 2},
		{demoLongTitle + " — and the second-night encore medley, rehearsed twice, played once", 3},
		{strings.Repeat("Supercalifragilistic", 6), 2},
	}
	for _, tc := range cases {
		lines, size := coverTitleLayout(doc, tc.title)
		if len(lines) < tc.minLines {
			t.Errorf("%q: %d lines, want at least %d", tc.title, len(lines), tc.minLines)
		}
		if size < coverTitleMin || size > coverTitleMax {
			t.Errorf("%q: size %v out of [%v, %v]", tc.title, size, coverTitleMin, coverTitleMax)
		}
		doc.SetFont(faceSerif, "", size)
		for _, ln := range lines {
			w, _ := doc.MeasureTextWidth(ln)
			if w > coverTextWidth()+0.01 {
				t.Errorf("%q: line %q is %.1fpt wide, over the %.1fpt between the margins", tc.title, ln, w, coverTextWidth())
			}
		}
		if got := strings.Join(strings.Fields(strings.Join(lines, " ")), ""); got != strings.Join(strings.Fields(tc.title), "") {
			t.Errorf("%q: wrapped text %q lost or changed characters", tc.title, lines)
		}
	}
	if lines, size := coverTitleLayout(doc, "Songbook"); len(lines) != 1 || size != coverTitleMax {
		t.Errorf("short title: got %d lines at %vpt, want 1 line at %vpt", len(lines), size, coverTitleMax)
	}
}

// Exactly one cover page, with or without gig details, and the whole
// export is cover + contents + one page per song (Song map layout: every
// song is exactly one page).
func TestExportSetlistHasOneCover(t *testing.T) {
	for _, front := range []FrontMatter{
		{},
		{EventTitle: demoLongTitle, BandName: "The Velvet Okapis", Date: "2026-05-14", Venue: "The Lantern Room"},
	} {
		doc := testDoc(t)
		renderFrontMatter(doc, demoLongTitle, front)
		if n := doc.GetNumberOfPages(); n != 1 {
			t.Errorf("front matter %+v: %d pages, want exactly 1 cover", front, n)
		}
	}

	refs := demoSetlist()
	data, err := New().ExportSetlist(context.Background(), demoLongTitle, FrontMatter{}, refs, Options{Mode: ModeMap})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pdfPageCount(data), 1+1+len(refs); got != want {
		t.Errorf("song-map setlist export: %d pages, want %d (cover, contents, %d songs)", got, want, len(refs))
	}
	_, entries := buildEntries(refs, lightTheme, true, ModeMap, false, 2)
	if entries[0].Page != 3 {
		t.Errorf("first song numbered page %d in the contents, want 3", entries[0].Page)
	}
}

// A set list of ~27 songs in three groups gets one full-width contents
// column; only a list too long for that goes to two columns.
func TestContentsLayoutColumns(t *testing.T) {
	_, entries := buildEntries(demoSetlist(), lightTheme, false, ModeDefault, false, 2)
	// Table height on the real page: the heading, a possible wrapped
	// second heading line and the subtitle take well under 80pt.
	availH := contentBottom() - marginY - 80
	cols, rowH := contentsLayout(entries, availH, 11)
	if cols != 1 {
		t.Fatalf("27 entries: %d columns, want 1", cols)
	}
	if rowH < contentsOneColMinRow {
		t.Errorf("27 entries: row height %v below the one-column minimum %v", rowH, contentsOneColMinRow)
	}
	if w := newCanvas(nil, false, lightTheme, cols, false).colW; w != contentWidth() {
		t.Errorf("one-column contents width %v, want the full text width %v", w, contentWidth())
	}

	var many []contentsEntry
	for i := 0; i < 90; i++ {
		many = append(many, contentsEntry{Title: fmt.Sprint("Song ", i), Group: fmt.Sprint("Set ", i/30+1)})
	}
	if cols, _ := contentsLayout(many, availH, 11); cols != 2 {
		t.Errorf("90 entries: %d columns, want 2", cols)
	}

	// And the page itself renders, the standalone one-page export too.
	if _, err := New().ExportSetlistPageOnly(context.Background(), demoLongTitle, "2026-05-14 · The Lantern Room", demoSetlist(), Options{}); err != nil {
		t.Fatal(err)
	}
}

// The Song layout flows sections down two columns (the setlist export's
// default Options), and a long song really uses the second column and
// fits one page; a chart with no lyrics anywhere takes one column, as on
// the web.
func TestSongLayoutTwoColumns(t *testing.T) {
	s := song.Parse(demoSong("Long Demo", "Saffron Delay", "C", "100", 3, true))
	cols := songCols(s, ModeDefault, Options{}.Cols())
	if cols != 2 {
		t.Fatalf("Song layout: %d columns, want 2", cols)
	}
	doc := testDoc(t)
	size, page2 := fitSongToPage(doc, s, nil, lightTheme, ModeDefault, false, cols)
	if page2 {
		t.Errorf("long demo song needs a second page in two columns (size %v)", size)
	}
	c := newCanvas(doc, false, lightTheme, cols, false)
	renderSongBody(c, s, nil, size, ModeDefault, false)
	if c.maxColIdx != 1 {
		t.Errorf("long demo song never reached column 2")
	}

	noLyrics := song.Parse("title: Groove Only\ntime: 4/4\n\n[A]\ngrid: | C | F | G | C |\n\n[B]\ngrid: | Am | Dm | G | C |\n")
	if got := songCols(noLyrics, ModeDefault, 2); got != 1 {
		t.Errorf("lyric-less chart: %d columns, want 1", got)
	}
	if got := songCols(s, ModeDefault, 1); got != 1 {
		t.Errorf("explicit one column: got %d", got)
	}
}

// Every layout still exports, both as a set list and as a single song.
func TestEveryLayoutExports(t *testing.T) {
	r := New()
	refs := demoSetlist()[:4]
	for _, m := range []Mode{ModeDefault, ModeJazz, ModeMap, ModeLyrics} {
		for _, cols := range []int{0, 1} {
			if _, err := r.ExportSetlist(context.Background(), demoLongTitle, FrontMatter{}, refs, Options{Mode: m, Columns: cols}); err != nil {
				t.Errorf("setlist mode %q cols %d: %v", m, cols, err)
			}
		}
		if _, err := r.ExportSong(context.Background(), "", refs[0].Body, nil, Options{Mode: m}); err != nil {
			t.Errorf("song mode %q: %v", m, err)
		}
	}
}
