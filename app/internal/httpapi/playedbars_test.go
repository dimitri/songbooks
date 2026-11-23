package httpapi

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"app/internal/song"
)

// drawnPlayed is, per section, each drawn bar of the Song view's rows
// (its chord, or "lead"/"…" for a lead or placeholder bar) with the played
// bars it lights for: "G=0,8".
func drawnPlayed(secs []viewerSection) map[string][]string {
	out := map[string][]string{}
	for _, s := range secs {
		key := s.Label
		if s.LabelNumber > 0 {
			key = fmt.Sprintf("%s %d", key, s.LabelNumber)
		}
		for _, l := range s.Lines {
			for _, m := range l.Measures {
				name := "…"
				switch {
				case m.Lead:
					name = "lead"
				case m.IsRepeat:
					name = "%"
				case len(m.Beats) > 0 && !m.Placeholder:
					name = m.Beats[0].Text
				}
				out[key] = append(out[key], name+"="+strings.ReplaceAll(m.PlayedAttr(), " ", ","))
			}
		}
	}
	return out
}

// Every played bar of a section lights the drawn bar it is read from: on
// the second pass through a repeat the first bar of the repeat again, an
// ending on the pass that takes it, each "times:" over again.
func TestPlayedBarsLightTheDrawnBar(t *testing.T) {
	cases := []struct {
		name, src string
		want      map[string][]string
	}{
		{"plain repeat", "[Verse]\ngrid: |: G | % | Em | % :|\n[Chorus]\ngrid: | C | D |\n", map[string][]string{
			"Verse":  {"G=0,4", "%=1,5", "Em=2,6", "%=3,7"},
			"Chorus": {"C=8", "D=9"},
		}},
		{"a counted repeat", "[Verse]\ngrid: |:3 G | C :| D |\n", map[string][]string{
			"Verse": {"G=0,2,4", "C=1,3,5", "D=6"},
		}},
		{"1st and 2nd endings", "[Verse]\ngrid: |: G | C | (1.) D :| (2.) Em |\n[Chorus]\ngrid: | F |\n", map[string][]string{
			"Verse":  {"G=0,3", "C=1,4", "D=2", "Em=5"},
			"Chorus": {"F=6"},
		}},
		{"endings written (1) (2)", "[Solo]\ngrid: |: Bm | A | (1) F# | (2) C# :|\n", map[string][]string{
			"Solo": {"Bm=0,3", "A=1,4", "F#=2", "C#=5"},
		}},
		{"times: over a repeat", "[Verse]\ntimes: 2\ngrid: |: G | C :| D |\n", map[string][]string{
			"Verse": {"G=0,2,5,7", "C=1,3,6,8", "D=4,9"},
		}},
		// A bare repeat draws no bars: the bars of the grid it replays
		// light, pass by pass.
		{"a bare repeat section", "[Verse]\ngrid: |: G | C :|\n[Chorus]\ngrid: | D |\n[Verse]\n[Chorus]\n", map[string][]string{
			"Verse 1":  {"G=0,2,5,7", "C=1,3,6,8"},
			"Chorus 1": {"D=4,9"},
		}},
		{"a bare repeat with times:", "[Verse]\ngrid: | G | C |\n[Verse]\ntimes: 2\n", map[string][]string{
			"Verse 1": {"G=0,2,4", "C=1,3,5"},
		}},
		{"same as an earlier grid", "[Verse]\ngrid: |: G | (1.) C :| (2.) D |\n[Verse]\ngrid: |: G | (1.) C :| (2.) D |\n", map[string][]string{
			"Verse 1": {"G=0,2,4,6", "C=1,5", "D=3,7"},
		}},
		// The previous Chorus, not the first, when the second changed.
		{"the nearest earlier grid", "[Chorus]\ngrid: | D | D |\n[Chorus]\ngrid: | E | E |\n[Chorus]\n", map[string][]string{
			"Chorus 1": {"D=0", "D=1"},
			"Chorus 2": {"E=2,4", "E=3,5"},
		}},
		{"lead:", "lead: 2\n\n[Intro]\ngrid: |: G | C :|\n", map[string][]string{
			"Intro": {"lead=0", "lead=1", "G=2,4", "C=3,5"},
		}},
		// An inferred Solo shows as a chip (its placeholder bars hidden,
		// keeping its passes): the grid it solos over lights.
		{"a Solo over a repeated grid", "[Verse]\ngrid: |: G | C | (1.) D :| (2.) Em |\n[Solo]\n", map[string][]string{
			"Verse": {"G=0,3,6,9", "C=1,4,7,10", "D=2,8", "Em=5,11"},
			"Solo":  {"…=6,9", "…=7,10", "…=8", "…=11"},
		}},
	}
	for _, c := range cases {
		secs := buildSections(song.Parse(c.src))
		if got := drawnPlayed(secs); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %v\nwant %v", c.name, got, c.want)
		}
	}
}

// The Map draws the same bars; a section it recalls in one cell (a bare
// repeat) lights the bars of the grid it replays (the cell keeps them
// all, for a click and the cycle).
func TestPlayedBarsOnTheMap(t *testing.T) {
	secs := buildSections(song.Parse("[Verse]\ngrid: |: G | C :|\n[Chorus]\ngrid: | D |\n[Verse]\n"))
	var got []string
	for _, l := range buildMapLines(secs) {
		for _, c := range l.Cells {
			if c.IsRecall {
				got = append(got, fmt.Sprintf("recall@%d=%s", c.RecallStart, c.RecallPlayed))
				continue
			}
			got = append(got, c.Measure.Beats[0].Text+"="+c.Measure.PlayedAttr())
		}
	}
	want := []string{"G=0 2 5 7", "C=1 3 6 8", "D=4", "recall@5=5 6 7 8"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Map cells %v, want %v", got, want)
	}
}

// The rows' markup carries it: data-played on each bar of a repeat.
func TestPlayedBarsInTheMarkup(t *testing.T) {
	m := viewerMeasure{GlobalBar: 3, Played: []int{3, 11}}
	if got := m.PlayedAttr(); got != "3 11" {
		t.Errorf("PlayedAttr %q", got)
	}
	if got := (viewerMeasure{}).PlayedAttr(); got != "" {
		t.Errorf("no played bars: %q", got)
	}
}

// The Song view, the Map and the Jazz chart read every played bar of the
// song, each exactly once, from one drawn bar (the Song view and the Jazz
// chart) — the Jazz chart's compacted form (a section replaying an earlier
// grid is shown once, its repeat raised, a D.S. read again) included. A
// Stand By Me-like song: a repeated verse, the verse again bare, a chorus
// with endings.
func TestPlayedBarsEveryViewEveryBar(t *testing.T) {
	src := jzHead + `
lead: 1

[Verse 1]
grid: |: G | % | Em | % | C | D | G | % :|
[Chorus]
grid: |: G | Em | (1.) C | D :| (2.) C | D | G | G |
[Verse 2]
grid: |: G | % | Em | % | C | D | G | % :|
[Chorus]
[Solo]
[Chorus]
times: 2
`
	sg := song.Parse(src)
	secs := buildSections(sg)
	total := 0
	for _, s := range secs {
		total = max(total, s.StartBar+s.BarCount)
	}
	// The Song view: each played bar of a section drawing bars of its
	// own lights one of them.
	rows := map[int]int{}
	for _, s := range secs {
		if recalled(s) {
			continue // a chip: its grid, if any, isn't shown
		}
		for _, l := range s.Lines {
			for _, m := range l.Measures {
				for _, p := range m.Played {
					rows[p]++
				}
			}
		}
	}
	for p := 0; p < total; p++ {
		if rows[p] != 1 {
			t.Errorf("Song view: played bar %d lights %d bars", p, rows[p])
		}
	}
	// The Map: on one bar (a recall cell only stands for its section).
	cellsLit := map[int]int{}
	for _, l := range buildMapLines(secs) {
		for _, c := range l.Cells {
			if !c.IsRecall {
				for _, p := range c.Measure.Played {
					cellsLit[p]++
				}
			}
		}
	}
	for p := 0; p < total; p++ {
		if cellsLit[p] != 1 {
			t.Errorf("Map: played bar %d lights %d bars", p, cellsLit[p])
		}
	}
	// Verse 2 writes Verse 1's grid again ("same as Verse 1"): its passes
	// light Verse 1's bars.
	if v2 := secs[2]; v2.GridDuplicateOf == "" || len(v2.Lines) != 0 {
		t.Fatalf("Verse 2: want a duplicate of Verse 1's grid, drawn as such")
	}
	// (The Solo, over Verse 2's grid that draws no bars, lights them too.)
	if first := secs[0].Lines[0].Measures[1]; !reflect.DeepEqual(first.Played[:4], []int{1, 9, secs[2].StartBar, secs[2].StartBar + 8}) {
		t.Errorf("Verse 1's bar 1 lights for %v, want 1, 9 and Verse 2's two passes first", first.Played)
	}
	// The Jazz chart: every played bar, lead included, on one cell.
	ch := buildJazzChart(sg, secs)
	if ch == nil {
		t.Fatal("no Jazz chart")
	}
	cells := map[int]int{}
	for _, ln := range ch.Lines {
		for _, c := range ln.Cells {
			for _, f := range strings.Fields(c.Played) {
				n, _ := strconv.Atoi(f)
				cells[n]++
			}
		}
	}
	for p := 0; p < total; p++ {
		if cells[p] != 1 {
			t.Errorf("Jazz chart: played bar %d on %d cells", p, cells[p])
		}
	}
	if len(cells) != total {
		t.Errorf("Jazz chart: %d played bars, want %d", len(cells), total)
	}
	if !ch.Compact {
		t.Errorf("want the compacted form")
	}
	// Same drawn bar in both views on a Chorus pass read from the first
	// chorus's grid: its 2nd ending, after the 1st ending's two bars.
	ch2 := secs[1]
	var end2 viewerMeasure
	for _, l := range ch2.Lines {
		for _, m := range l.Measures {
			if m.Volta == "2" || m.Volta == "2." {
				end2 = m
				break
			}
		}
	}
	if len(end2.Played) == 0 || end2.Played[0] != ch2.StartBar+6 {
		t.Errorf("Chorus's 2nd ending lights for %v, want %d", end2.Played, ch2.StartBar+6)
	}
}

// A tab row's bars: each one's played bars, for the tab cursor
// (alphatab-tab.js), a repeat's bars on every pass.
func TestPlayedBarsOfATabRow(t *testing.T) {
	l := viewerLine{Measures: []viewerMeasure{
		{GlobalBar: 0, Played: []int{0, 4}},
		{GlobalBar: 1, Played: []int{1, 5}},
		{GlobalBar: 2, Played: []int{2}},
		{GlobalBar: 3},
	}}
	if got := l.PlayedBars(); got != "0 4,1 5,2,3" {
		t.Errorf("PlayedBars %q", got)
	}
}
