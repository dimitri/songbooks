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

// A "lead:" line: its bars ride on the opening section's first row as
// pickups, ahead of bar 1, on top of the row's "bars:" count.
func TestLeadRows(t *testing.T) {
	bars8 := strings.Repeat(" G |", 8)
	cases := []struct {
		name, src string
		rows      []int
		pickups   int
	}{
		{"lead: 1", "[Intro]\nlead: 1\ngrid: |" + bars8 + "\n", []int{9}, 1},
		{"lead: 2", "[Intro]\nlead: 2\ngrid: |" + bars8 + strings.Repeat(" C |", 8) + "\n", []int{10, 8}, 2},
		{"lead: 1, bars: 4", "[Intro]\nlead: 1\nbars: 4\ngrid: |" + bars8 + "\n", []int{5, 4}, 1},
		{"lead: 2, bars: 4,2", "[Intro]\nlead: 2\nbars: 4,2\ngrid: |" + bars8 + "\n", []int{6, 2, 2}, 2},
		{"lead written as a grid", "[Intro]\nlead: | D7 |\ngrid: |" + bars8 + "\n", []int{9}, 1},
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

// The lead bars are played (they take the timeline's first bars) but not
// written bars of their section: their Index is negative, the section's
// bar count grows by them once, whatever its "times:", and a section
// repeating the Intro later doesn't play them again.
func TestLeadTimeline(t *testing.T) {
	src := "[Intro]\nlead: 2\ntimes: 2\ngrid: | G | C |\n\n[Verse]\ngrid: | D | D |\n\n[Intro]\n"
	secs := buildSections(song.Parse(src))
	in := secs[0]
	if in.BarCount != 6 || in.LeadBars != 2 || in.StartBar != 0 {
		t.Fatalf("Intro: %d bars (%d lead) from %d, want 6 (2) from 0", in.BarCount, in.LeadBars, in.StartBar)
	}
	ms := in.Lines[0].Measures
	if !ms[0].Lead || !ms[0].Pickup || !ms[0].IsRest || ms[0].Index != -2 || ms[1].Index != -1 || ms[2].Index != 0 || ms[2].Lead {
		t.Errorf("Intro bars: %+v", ms[:3])
	}
	for i, m := range ms {
		if m.GlobalBar != i {
			t.Errorf("bar %d: GlobalBar %d", i, m.GlobalBar)
		}
	}
	if secs[1].StartBar != 6 {
		t.Errorf("Verse starts at %d, want 6", secs[1].StartBar)
	}
	if out := secs[2]; !out.IsReference || out.BarCount != 4 || out.StartBar != 8 {
		t.Errorf("the Intro again: reference %v, %d bars from %d, want 4 from 8 (its 2 passes, no lead)", out.IsReference, out.BarCount, out.StartBar)
	}
}

// On a later section the lead is played as that section's first bars,
// numbered like the rest: no pickups.
func TestLeadOnLaterSection(t *testing.T) {
	secs := buildSections(song.Parse("[Intro]\ngrid: | G | C |\n\n[Chorus]\nlead: | D7 |\ngrid: | G | C |\n"))
	ch := secs[1]
	if ch.BarCount != 3 || ch.StartBar != 2 || ch.Lines[0].Pickups() != 0 || !ch.Measures[0].Lead {
		t.Errorf("Chorus: %d bars from %d, %d pickups", ch.BarCount, ch.StartBar, ch.Lines[0].Pickups())
	}
	if secs[0].Measures[0].Pickup {
		t.Error("the Intro got a pickup")
	}
}

// The Jazz chart reads the lead as the Intro's first bars, played once.
func TestLeadJazzChart(t *testing.T) {
	sg := song.Parse("time: 4/4\n\n[Intro]\nlead: 1\ngrid: | G | C :|\n\n[Verse]\ngrid: | D | D |\n")
	secs := buildSections(sg)
	jc := buildJazzChart(sg, secs)
	if jc == nil {
		t.Fatal("no Jazz chart")
	}
	if jc.Bars != 7 {
		t.Errorf("played bars %d, want 7 (1 lead + 2x2 + 2)", jc.Bars)
	}
	leads := 0
	for _, l := range jc.Lines {
		for _, c := range l.Cells {
			if c.Lead {
				leads++
				if c.First != 0 || !c.Rest {
					t.Errorf("lead cell: %+v", c)
				}
			}
		}
	}
	if leads != 1 {
		t.Errorf("%d lead cells, want 1", leads)
	}
}
