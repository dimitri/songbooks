package pdfnative

import (
	"context"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/signintech/gopdf"
)

// jzTestChart is a chart of n lines, a section every 2.
func jzTestChart(n int) *JazzChart {
	ch := &JazzChart{Title: "Test", Composer: "Someone", Tempo: "120", KeyRoot: JazzNote{Letter: "E", Acc: "b"}, KeyWord: "Key"}
	for i := 0; i < n; i++ {
		ln := JazzLine{Gap: i%2 == 0 && i > 0}
		for b := 0; b < 4; b++ {
			c := JazzCell{Left: "single", Chords: []JazzChord{{X: 0, Pad: 0.22, Root: JazzNote{Letter: "C"}, Quality: []JazzRun{{S: "7", Kind: JazzRunText}}}}}
			if b == 0 && i%2 == 0 {
				c.Left, c.MarkParts, c.Name = "sec-start", []string{"A"}, "Verse"
			}
			if b == 3 {
				c.Right = "single"
			}
			ln.Cells = append(ln.Cells, c)
		}
		ch.Lines = append(ch.Lines, ln)
	}
	return ch
}

func jzMeasureDoc() *gopdf.GoPdf {
	doc := &gopdf.GoPdf{}
	doc.Start(gopdf.Config{PageSize: gopdf.Rect{W: pageW, H: pageH}})
	registerFonts(doc)
	return doc
}

// A short chart fills the page's width; a little too long, it is set
// smaller (no less than jzMinShrink) and centred on one page; longer, it
// runs on at full width.
func TestJazzLayoutFits(t *testing.T) {
	doc := jzMeasureDoc()
	full := (pageW - 2*jzMargin) / jzEmPerPage
	c := newCanvas(doc, false, lightTheme, 1, false)

	p, pages, _, _ := jazzLayout(c, jzTestChart(6))
	if len(pages) != 1 || p.E != full || p.x0 != jzMargin {
		t.Errorf("6 lines: E %.2f x0 %.2f pages %v", p.E, p.x0, pages)
	}
	p, pages, _, _ = jazzLayout(c, jzTestChart(12))
	if len(pages) != 1 || p.E >= full || p.E < full*jzMinShrink || p.x0 <= jzMargin {
		t.Errorf("12 lines: E %.2f (full %.2f) x0 %.2f pages %v", p.E, full, p.x0, pages)
	}
	// 18 lines need about two thirds of the full size: one page still.
	p, pages, _, _ = jazzLayout(c, jzTestChart(18))
	if len(pages) != 1 || p.E >= full*0.7 || p.E < full*jzMinShrink {
		t.Errorf("18 lines: E %.2f (full %.2f) pages %v", p.E, full, pages)
	}
	p, pages, _, _ = jazzLayout(c, jzTestChart(30))
	if len(pages) < 2 || p.E != full {
		t.Errorf("30 lines: E %.2f pages %v", p.E, pages)
	}
	if got := jazzPages(doc, jzTestChart(30)); got != len(pages) {
		t.Errorf("jazzPages %d, layout %d", got, len(pages))
	}
}

// Pages end at a section when one starts near the bottom, and the last
// page never holds a single line.
func TestJazzPaginate(t *testing.T) {
	line := func(gap bool) (float64, float64) {
		if gap {
			return jzGapTop, jzSysH
		}
		return 0, jzSysH
	}
	build := func(gaps ...bool) (tops, heights []float64) {
		for i, g := range gaps {
			tp, h := line(g)
			if i == 0 {
				tp = jzHeadGap
			}
			tops, heights = append(tops, tp), append(heights, h)
		}
		return
	}
	// 10 lines fit on a page of 25em (header 1.32 + .9 + 10 × 2.3 = 25.22: no).
	tops, heights := build(false, false, false, false, false, false, false, true, false, false, false)
	pages := jazzPaginate(tops, heights, 25)
	if pages[0].to != 7 {
		t.Errorf("break at the section (line 7): %v", pages)
	}
	// 10 lines, 9 fit: not 9 + 1, but 8 + 2.
	tops, heights = build(false, false, false, false, false, false, false, false, false, false)
	pages = jazzPaginate(tops, heights, 1.32+0.9+9*2.3+0.1)
	if want := []jzPage{{0, 8}, {8, 10}}; !reflect.DeepEqual(pages, want) {
		t.Errorf("orphan: %v, want %v", pages, want)
	}
}

// A set list of Jazz charts: each song its chart from a new page, the
// contents numbering them by the pages their charts take.
func TestJazzSetlist(t *testing.T) {
	refs := []SongRef{
		{Stem: "1", Body: "title: One\n\n[A]\ngrid: | C | G |\nla la la\n", Jazz: jzTestChart(4)},
		{Stem: "2", Body: "title: Two\n\n[A]\ngrid: | C | G |\n", Jazz: jzTestChart(30)},
		{Stem: "3", Body: "title: Three\nkey: Am\n\n[A]\nwords only\n"},
	}
	data, err := New().ExportSetlist(context.Background(), "Set", FrontMatter{}, refs, Options{Mode: ModeJazz})
	if err != nil {
		t.Fatal(err)
	}
	pages := len(regexp.MustCompile(`/Type /Page[^s]`).FindAll(data, -1))
	want := frontPages + 1 + 1 + jazzPages(jzMeasureDoc(), refs[1].Jazz) + 1
	if pages != want {
		t.Errorf("%d pages, want %d", pages, want)
	}
	_, entries := buildEntries(refs, lightTheme, true, ModeJazz, false, 2)
	if entries[1].Page != entries[0].Page+1 || entries[2].Page <= entries[1].Page+1 {
		t.Errorf("contents pages: %d %d %d", entries[0].Page, entries[1].Page, entries[2].Page)
	}
	if !strings.HasPrefix(string(data), "%PDF") {
		t.Error("not a PDF")
	}
}

// Every barline kind, sign and chord shape draws.
func TestJazzDrawsEverything(t *testing.T) {
	ch := jzTestChart(2)
	ch.Style, ch.Compound, ch.WithRepeatsWord, ch.LeadWord = "Medium Swing", true, "with repeats", "lead"
	ln := &ch.Lines[0]
	ln.Lead, ln.Notes = 1, []string{"Verse 2: loud, then a very long direction that wraps over the line under it and on again"}
	cells := []JazzCell{
		{Lead: true, LeadTag: true, Left: "single", Rest: true, TSNum: "6", TSDen: "8", MarkParts: []string{"In", "V", "A", "B"}, Name: "Intro / Verse"},
		{Left: "rep-start", Segno: true, Coda: true, VoltaSpan: 2, Volta: "1, 2.", Chords: []JazzChord{{Text: "N.C."}}},
		{Left: "rep-both", Simile: 2, ToCoda: true, TSNum: "3", TSDen: "4", TSMid: true},
		{Left: "single", Pair2: true, Right: "rep-end", Texts: []JazzText{{S: "3x"}, {S: "D.S. al Coda", WithRepeats: true}}, VoltaSpan: 1, VoltaClosed: true, VoltaCont: true},
		{Left: "sec-start", Right: "final", Chords: []JazzChord{
			{X: 0, Pad: 0.45, Root: JazzNote{Letter: "B", Acc: "b"}, Bass: JazzNote{Letter: "F", Acc: "#"}, Quality: []JazzRun{{S: "", Kind: JazzRunMinor}, {S: "7", Kind: JazzRunText}, {S: smuflFlat, Kind: JazzRunAcc}, {S: "5", Kind: JazzRunText}}},
			{X: 0.25, Pad: 0.1, Root: JazzNote{Letter: "E"}, Quality: []JazzRun{{S: "", Kind: JazzRunSign}, {S: "7sus4add13", Kind: JazzRunText}}},
			{X: 0.5, Pad: 0.1, Root: JazzNote{Letter: "C", Acc: "#"}, Quality: []JazzRun{{S: "", Kind: JazzRunSign}}},
			{X: 0.75, Pad: 0.1, Root: JazzNote{Letter: "G"}},
		}},
		{Pad: true},
	}
	ln.Cells = cells
	if _, err := New().ExportJazzChart(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
}
