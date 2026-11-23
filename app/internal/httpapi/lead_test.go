package httpapi

import (
	"reflect"
	"strings"
	"testing"

	"app/internal/song"
)

func rowsOf(sec viewerSection) []int {
	var rows []int
	for _, l := range sec.Lines {
		rows = append(rows, len(l.Measures))
	}
	return rows
}

// The song's "lead:" bars ride on its first grid's first row as pickups,
// ahead of bar 1, on top of the row's "bars:" count.
func TestLeadRows(t *testing.T) {
	bars8 := strings.Repeat(" G |", 8)
	cases := []struct {
		name, src string
		rows      []int
		pickups   int
	}{
		{"lead: 1", "lead: 1\n\n[Intro]\ngrid: |" + bars8 + "\n", []int{9}, 1},
		{"lead: 2", "lead: 2\n\n[Intro]\ngrid: |" + bars8 + strings.Repeat(" C |", 8) + "\n", []int{10, 8}, 2},
		{"lead: 1, bars: 4", "lead: 1\n\n[Intro]\nbars: 4\ngrid: |" + bars8 + "\n", []int{5, 4}, 1},
		{"lead: 2, bars: 4,2", "lead: 2\n\n[Intro]\nbars: 4,2\ngrid: |" + bars8 + "\n", []int{6, 2, 2}, 2},
		{"lead written as a grid", "lead: | D7 |\n\n[Intro]\ngrid: |" + bars8 + "\n", []int{9}, 1},
		{"lead in the first section (its first form)", "[Intro]\nlead: 1\ngrid: |" + bars8 + "\n", []int{9}, 1},
		// A written count-in bar is the song's pickup and one of "bars:".
		{"no lead, a count-in bar", "[Intro]\nbars: 4\ngrid: | ? |" + bars8 + "\n", []int{4, 4, 1}, 1},
	}
	for _, c := range cases {
		secs := buildSections(song.Parse(c.src))
		if got := rowsOf(secs[0]); !reflect.DeepEqual(got, c.rows) {
			t.Errorf("%s: rows %v, want %v", c.name, got, c.rows)
		}
		if got := secs[0].Lines[0].Pickups(); got != c.pickups {
			t.Errorf("%s: %d pickups, want %d", c.name, got, c.pickups)
		}
	}
}

// The lead is the song's time before bar 1, in no section: bars 0…K-1,
// the first section starting at K, its bar count and every section's
// (and so a section's loop) without them; a bare repeat of the Intro
// doesn't play them either.
func TestLeadTimeline(t *testing.T) {
	src := "lead: 2\n\n[Intro]\ntimes: 2\ngrid: | G | C |\n\n[Verse]\ngrid: | D | D |\n\n[Intro]\n"
	secs := buildSections(song.Parse(src))
	in := secs[0]
	if in.StartBar != 2 || in.BarCount != 4 || in.LeadBars != 2 {
		t.Fatalf("Intro: %d bars from %d (%d lead drawn), want 4 from 2", in.BarCount, in.StartBar, in.LeadBars)
	}
	var got []int
	for _, l := range in.Lines {
		for _, m := range l.Measures {
			got = append(got, m.GlobalBar)
		}
	}
	if !reflect.DeepEqual(got, []int{0, 1, 2, 3}) {
		t.Errorf("Intro's drawn bars: %v, want the lead's 0 1 then the Intro's 2 3", got)
	}
	ms := in.Lines[0].Measures
	if !ms[0].Lead || !ms[0].Pickup || !ms[0].IsRest || ms[0].Index != -2 || ms[1].Index != -1 || ms[2].Index != 0 || ms[2].Lead {
		t.Errorf("Intro bars: %+v", ms[:3])
	}
	for _, s := range secs {
		if s.BarCount > 0 && s.StartBar < 2 {
			t.Errorf("%s spans bars %d–%d: the lead's are in no section", s.Label, s.StartBar, s.StartBar+s.BarCount-1)
		}
	}
	if secs[1].StartBar != 6 {
		t.Errorf("Verse starts at %d, want 6", secs[1].StartBar)
	}
	if out := secs[2]; !out.IsReference || out.BarCount != 4 || out.StartBar != 8 {
		t.Errorf("the Intro again: reference %v, %d bars from %d, want 4 from 8", out.IsReference, out.BarCount, out.StartBar)
	}
	// The Map names the Intro on its own bar 1, after the lead.
	badge := -1
	for _, l := range buildMapLines(secs) {
		for _, c := range l.Cells {
			if badge < 0 && c.Measure.MapAnnotate {
				badge = c.Measure.GlobalBar
			}
		}
	}
	if badge != 2 {
		t.Errorf("the Map's Intro badge on bar %d, want 2", badge)
	}
}

// A song whose first section is lyrics only draws its lead on its first
// grid; the lead still comes first in time.
func TestLeadBeforeALyricsOnlySection(t *testing.T) {
	secs := buildSections(song.Parse("lead: 1\n\n[Verse]\nla la\n\n[Chorus]\ngrid: | G | C |\n"))
	if secs[1].LeadBars != 1 || secs[1].StartBar != 1 || secs[1].Lines[0].Measures[0].GlobalBar != 0 {
		t.Errorf("Chorus: %d lead drawn, from %d", secs[1].LeadBars, secs[1].StartBar)
	}
}

// The Jazz chart draws the lead ahead of the Intro's bar 1, played as
// bar 0, the Intro's mark on its own bar 1.
func TestLeadJazzChart(t *testing.T) {
	sg := song.Parse("time: 4/4\nlead: 1\n\n[Intro]\ngrid: | G | C :|\n\n[Verse]\ngrid: | D | D |\n")
	secs := buildSections(sg)
	jc := buildJazzChart(sg, secs)
	if jc == nil {
		t.Fatal("no Jazz chart")
	}
	if jc.Bars != 7 {
		t.Errorf("played bars %d, want 7 (1 lead + 2x2 + 2)", jc.Bars)
	}
	first := jc.Lines[0]
	if first.Lead != 1 || !first.Cells[0].Lead || first.Cells[0].First != 0 || !first.Cells[0].Rest || first.Cells[0].TSNum != "4" {
		t.Errorf("lead cell: %+v", first.Cells[0])
	}
	if c := first.Cells[1]; c.Lead || c.Mark == "" || c.First != 1 {
		t.Errorf("the Intro's bar 1: %+v", c)
	}
}

// Sections playing the Intro's grid again keep the chart compact, and
// every played bar maps to one cell.
func TestLeadJazzChartForm(t *testing.T) {
	for _, lead := range []string{"", "lead: 1\n", "lead: | D7 | . |\n"} {
		src := "time: 4/4\n" + lead + "\n[Intro]\nbars: 4,5\ngrid: |: Gm | Bb | Dm | C | Gm | Bb | F | C | (4.) C :|\n\n[Verse]\nla\n\n[Chorus]\nla\n\n[Bridge]\ngrid: |: Gm | C :|\n"
		sg := song.Parse(src)
		secs := buildSections(sg)
		jc := buildJazzChart(sg, secs)
		n := sg.LeadIn()
		total := n
		for _, s := range secs {
			total = max(total, s.StartBar+s.BarCount)
		}
		if !jc.Compact || jc.Bars != total {
			t.Errorf("%q: compact %v, %d bars, want compact, %d", lead, jc.Compact, jc.Bars, total)
		}
		seen := map[string]bool{}
		for _, l := range jc.Lines {
			for _, c := range l.Cells {
				for _, p := range strings.Fields(c.Played) {
					if seen[p] {
						t.Errorf("%q: bar %s in two cells", lead, p)
					}
					seen[p] = true
				}
			}
		}
		if len(seen) != total {
			t.Errorf("%q: %d bars mapped, want %d", lead, len(seen), total)
		}
		if first := jc.Lines[0]; first.Lead != n {
			t.Errorf("%q: first line lead %d, want %d", lead, first.Lead, n)
		}
	}
}
