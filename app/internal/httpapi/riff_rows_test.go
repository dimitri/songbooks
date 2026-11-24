package httpapi

import (
	"strconv"
	"strings"
	"testing"

	"app/internal/song"
	"app/internal/tab"
)

const riffRowsRiff = `score[Guitar]: \new TabStaff {
   \tabFullNotation
   \tempo 4 = 94
   \set TabStaff.minimumFret = 8
   \set TabStaff.restrainOpenStrings = ##t
   \set Timing.beamExceptions = #'()

   c4 c'16 bes g f~ f8 ees8~ \grace f16( \glissando g8)

   \set TabStaff.minimumFret = 10
   ees'8 c'4 r2.

   \set TabStaff.minimumFret = 8
   c4 c'16 bes g f~ f8 ees8~ \grace f16( \glissando g8)

   \set TabStaff.minimumFret = 10
   bes8 c'4 r2.
}
`

// riffRowsSections renders src the way the Song view does: sections,
// the scores laid on them.
func riffRowsSections(t *testing.T, src string) []viewerSection {
	t.Helper()
	sg := song.Parse(src)
	sections := buildSections(sg)
	riffs, errs := tab.LoadScores(sg)
	if len(errs) > 0 {
		t.Fatalf("scores: %v", errs)
	}
	attachTabRiffs(sections, riffs, 4, 4, "", "")
	return sections
}

// Whiskey, Beer & Wine's Intro: a 4-bar riff over a 12-bar blues
// written as one grid line. The riff's 4 bars are one tab row (alphaTab,
// the grid's chord names on top), the 8 others two chord-grid rows of
// 4 — as the legacy LilyPond book printed it, not a 12-bar row half
// drawn as bare fret numbers.
func TestRiffOverPartOfTheGrid(t *testing.T) {
	src := "title: Whiskey\ntime: 4/4\ntempo: 94\n\n[Intro]\n" + riffRowsRiff +
		"grid: |Cm | % | % | % | F9 | % | Cm | % | Eb | F | Cm | %|\n\n[Verse]\ngrid: |Cm | F9 | Cm | Cm|\n"
	sec := riffRowsSections(t, src)[0]
	if len(sec.Lines) != 3 {
		t.Fatalf("rows = %d, want 3", len(sec.Lines))
	}
	tabRow := sec.Lines[0]
	if !tabRow.TabOnly || len(tabRow.Measures) != 4 || tabRow.AlphaTex == "" {
		t.Fatalf("first row: TabOnly %v, %d bars, alphaTeX %q; want the riff's 4 bars as tab", tabRow.TabOnly, len(tabRow.Measures), tabRow.AlphaTex)
	}
	for _, want := range []string{`{ch "Cm"}`, `\tempo 94`, `(8.5{t}).8`, `(8.5{sl}).32{gr}`, `r.2{d}`, `\barlineright regular`} {
		if !strings.Contains(tabRow.AlphaTex, want) {
			t.Errorf("tab row alphaTeX lacks %q:\n%s", want, tabRow.AlphaTex)
		}
	}
	if strings.Count(tabRow.AlphaTex, `{ch "`) != 1 {
		t.Errorf("chord labels: want Cm only (the other bars are %%):\n%s", tabRow.AlphaTex)
	}
	if tabRow.Systems != "4" || tabRow.BarShift != 0 {
		t.Errorf("tab row Systems %q BarShift %d, want \"4\", 0", tabRow.Systems, tabRow.BarShift)
	}
	for i, l := range sec.Lines[1:] {
		if l.TabOnly || len(l.Measures) != 4 || l.Capacity != 4 {
			t.Errorf("grid row %d: TabOnly %v, %d bars (cap %d); want 4 chord-grid bars", i+2, l.TabOnly, len(l.Measures), l.Capacity)
		}
		for _, m := range l.Measures {
			if m.TabOnly {
				t.Errorf("grid row %d: bar %d marked TabOnly", i+2, m.Index)
			}
		}
	}
	if got := sec.Lines[1].Measures[0].Index; got != 4 {
		t.Errorf("second row starts at bar %d, want 4", got)
	}
	// The rows still slice the section's bars: the Song Map (Measures)
	// sees the same TabOnly flags and played bars.
	for i, m := range sec.Measures {
		if m.TabOnly != (i < 4) {
			t.Errorf("Measures[%d].TabOnly = %v", i, m.TabOnly)
		}
	}
	if sec.Lines[1].PlayedBars() == "" || !strings.HasPrefix(tabRow.PlayedBars(), "0") {
		t.Errorf("played bars: tab row %q, grid row %q", tabRow.PlayedBars(), sec.Lines[1].PlayedBars())
	}
}

// Bad Spell's Intro: a score and no grid — every bar the riff's, one tab
// row as before, and the song's last row keeps alphaTab's final barline
// only when it ends the song.
func TestRiffOnlySectionUnchanged(t *testing.T) {
	src := "title: Bad Spell\ntime: 4/4\n\n[Intro]\nscore[Guitar]: \\new TabStaff {\n   \\set TabStaff.minimumFret = 3\n   g,4 <d' g'>8. g,16 bes,8 c d f16 g\n   g,4 <d' g'>8. bes16( c'4) bes8 \\tuplet 3/2 { g16 f d } g,4\n}\n\n[Verse]\ngrid: |G | C | G | G|\n"
	secs := riffRowsSections(t, src)
	intro := secs[0]
	if len(intro.Lines) != 1 || !intro.Lines[0].TabOnly || len(intro.Lines[0].Measures) != 3 {
		t.Fatalf("intro rows: %d, first TabOnly %v with %d bars; want one tab row of 3", len(intro.Lines), intro.Lines[0].TabOnly, len(intro.Lines[0].Measures))
	}
	if !strings.Contains(intro.Lines[0].AlphaTex, `\barlineright regular`) {
		t.Errorf("an intro row followed by the song: want a plain closing barline:\n%s", intro.Lines[0].AlphaTex)
	}
	last := riffRowsSections(t, "time: 4/4\n\n[Riff]\nscore[Guitar]: e4 e e e e e e e\n")[0]
	if strings.Contains(last.Lines[0].AlphaTex, `\barlineright`) {
		t.Errorf("the song's last row: want alphaTab's final barline:\n%s", last.Lines[0].AlphaTex)
	}
}

// With "bars:" the author's rows are kept, only cut where the riff ends.
func TestRiffRowsKeepAuthorsBars(t *testing.T) {
	src := "time: 4/4\n\n[Intro]\nbars: 6\nscore[Guitar]: e1 e e\ngrid: |E | A | E | E | B | E | A | E|\n"
	sec := riffRowsSections(t, src)[0]
	var got []string
	for _, l := range sec.Lines {
		got = append(got, map[bool]string{true: "tab", false: "grid"}[l.TabOnly]+":"+strconv.Itoa(len(l.Measures)))
	}
	if strings.Join(got, " ") != "tab:3 grid:3 grid:2" {
		t.Errorf("rows = %v, want tab:3 grid:3 grid:2", got)
	}
	// Repeat signs on a riff bar reach alphaTab.
	sec = riffRowsSections(t, "time: 4/4\n\n[Intro]\nscore[Guitar]: e1 e\ngrid: |: E | A :| B | B | B | B|\n")[0]
	if tex := sec.Lines[0].AlphaTex; !strings.Contains(tex, `\ro`) || !strings.Contains(tex, `\rc 2`) {
		t.Errorf("repeat signs missing:\n%s", tex)
	}
}

func TestRiffPhraseRows(t *testing.T) {
	bars := func(n, pickups int) []viewerMeasure {
		out := make([]viewerMeasure, n)
		for i := 0; i < pickups; i++ {
			out[i].Pickup = true
		}
		return out
	}
	for _, c := range []struct {
		n, pickups int
		want       string
	}{
		{8, 0, "4 4"}, {5, 0, "5"}, {9, 0, "4 5"}, {10, 0, "4 4 2"}, {3, 0, "3"}, {5, 1, "5"}, {1, 1, "1"},
	} {
		var got []string
		for _, s := range riffPhraseRows(bars(c.n, c.pickups)) {
			got = append(got, strconv.Itoa(s))
		}
		if strings.Join(got, " ") != c.want {
			t.Errorf("riffPhraseRows(%d bars, %d pickups) = %v, want %s", c.n, c.pickups, got, c.want)
		}
	}
}
