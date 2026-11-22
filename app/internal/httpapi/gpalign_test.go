package httpapi

import (
	"testing"

	"app/internal/gpfacts"
	"app/internal/song"
)

// A file next to its chart: two empty lead-in bars, then Verse (4 bars),
// Chorus (6 bars — the chart has 4), Verse (4), each marked; the Chorus
// is played twice through a repeat.
func besideFacts() *gpfacts.Facts {
	c := func(n string) []gpfacts.BarChord { return []gpfacts.BarChord{{Pos: 0, Name: n}} }
	return &gpfacts.Facts{
		Tempo: 100, Bars: 15, Empty: 2,
		// written: 1-2 lead-in, 3-6 verse, 7-9 chorus (|: 7 8 9 :| twice), 10-13 verse, 14-15 spare
		Played:  []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 7, 8, 9, 10, 11, 12, 13},
		Time:    []gpfacts.TimeSig{{Bar: 1, Num: 4, Den: 4}},
		Markers: []gpfacts.Marker{{Bar: 3, Text: "Verse 1"}, {Bar: 7, Text: "Chorus"}, {Bar: 10, Text: "Verse 2"}},
		Chords: [][]gpfacts.BarChord{
			{}, {}, c("E7"), c("A7"), c("E7"), c("B7"), c("A"), c("D"), c("E"), c("E7"), c("A7"), c("E7"), c("B7"), {}, {},
		},
	}
}

const besideChart = "title: X\ntempo: 100\n\n[Verse]\ngrid: |E7 | A7 | E7 | B7 |\n\n[Chorus]\ngrid: |A | D | E | . |\n\n[Verse]\n"

func alignOf(text string, start int) gpAlignment {
	sg := song.Parse(text)
	return alignChart(sg, buildSections(sg), besideFacts(), start)
}

func TestAlignNeedsTheLeadIn(t *testing.T) {
	al := alignOf(besideChart, 1)
	if al.Aligned || al.FileEmpty != 2 || al.FileBars != 16 || al.ChartBars != 12 {
		t.Fatalf("alignment = %+v", al)
	}
	v := al.Sections[0]
	if v.Match == nil || v.Match.Text != "Verse 1" || v.Shift != 2 || !v.Own || v.Status != "shifted" {
		t.Errorf("verse 1 = %+v", v)
	}
	if al.BestStart != 3 {
		t.Errorf("best start = %d, want 3 (past the two empty bars)", al.BestStart)
	}
}

func TestAlignFromStart3(t *testing.T) {
	al := alignOf(besideChart, 3)
	if len(al.Sections) != 3 {
		t.Fatalf("sections = %+v", al.Sections)
	}
	v1, ch, v2 := al.Sections[0], al.Sections[1], al.Sections[2]
	if v1.Status != "ok" || v1.FileFrom != 3 || len(v1.Written) != 1 || v1.Written[0] != [2]int{3, 6} {
		t.Errorf("verse 1 = %+v", v1)
	}
	// The chart's Chorus is 4 bars, the file's 6 (its repeat played out).
	if ch.Status != "bars" || ch.Match.Bars != 6 || ch.Bars != 4 || ch.Shift != 0 {
		t.Errorf("chorus = %+v", ch)
	}
	// Verse 2 falls two bars early, because of the Chorus: not its own.
	if v2.Status != "shifted" || v2.Shift != 2 || v2.Own {
		t.Errorf("verse 2 = %+v", v2)
	}
	if al.BestStart != 0 {
		t.Errorf("best start = %d", al.BestStart)
	}
}

func TestAlignChords(t *testing.T) {
	al := alignOf("title: X\n\n[Verse]\ngrid: |E7 | A7 | E7 | B7 |\n", 3)
	if !al.Aligned || al.Sections[0].Status != "ok" {
		t.Fatalf("expected in step: %+v", al.Sections[0])
	}
	al = alignOf("title: X\n\n[Verse]\ngrid: |E7 | A7 | E | B7 |\n", 3)
	r := al.Sections[0]
	if r.Status != "chords" || len(r.ChordDiff) != 1 || r.ChordDiff[0] != 2 || r.Chart[2] != "E" || r.File[2] != "E7" {
		t.Errorf("verse = %+v", r)
	}
}

func TestAlignDriftAndStart(t *testing.T) {
	sg := song.Parse(besideChart)
	f := besideFacts()
	got := map[string]driftItem{}
	for _, it := range computeDrift(sg, buildSections(withGPOnlyBars(sg, f)), f, nil) {
		got[it.Fact] = it
	}
	if it, ok := got["start"]; !ok || it.Text != "bar 1" || !it.Take {
		t.Errorf("start item = %+v (all: %v)", it, keys(got))
	}
	sg = song.Parse("title: X\ngp_start: 3\n" + besideChart[len("title: X\n"):])
	got = map[string]driftItem{}
	for _, it := range computeDrift(sg, buildSections(withGPOnlyBars(sg, f)), f, nil) {
		got[it.Fact] = it
	}
	if _, ok := got["start"]; ok {
		t.Errorf("start still reported: %v", keys(got))
	}
	if it, ok := got["bars:1"]; !ok || it.Text != "4 bars" {
		t.Errorf("chorus bar count = %+v (all: %v)", it, keys(got))
	}
	if _, ok := got["shift:2"]; ok {
		t.Errorf("verse 2's shift follows from the chorus, not reported on its own: %v", keys(got))
	}
}

func TestGPBarOrderNextToTheChart(t *testing.T) {
	sg := song.Parse("title: X\ngp_start: 3\n\n[Verse]\ngrid: |E7 | A7 | E7 | B7 |\n")
	secs := buildSections(sg)
	order := gpBarOrder(sg, secs, besideFacts(), 4)
	if len(order) != 4 || order[0] != 3 || order[3] != 6 {
		t.Errorf("order = %v", order)
	}
	setGPStart(sg, 1)
	if sg.Get("gp_start") != "" {
		t.Errorf("gp_start 1 should be taken off: %q", sg.Get("gp_start"))
	}
}
