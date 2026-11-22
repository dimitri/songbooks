package pdfnative

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"app/internal/song"
	"app/internal/tab"
)

// TestBarRowsCases: the PDF's rows are the web Song view's — the same
// table of cases bar-rows.js is tested against (jstest/bar-rows.test.mjs).
func TestBarRowsCases(t *testing.T) {
	raw, err := os.ReadFile("../webassets/jstest/bar-rows-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []struct {
			Name string     `json:"name"`
			Rows []int      `json:"rows"`
			Opts barRowOpts `json:"opts"`
			Want []barRow   `json:"want"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Cases) < 10 {
		t.Fatalf("%d cases read", len(table.Cases))
	}
	for _, c := range table.Cases {
		got := planBarRows(c.Rows, c.Opts)
		if len(got) == 0 && len(c.Want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.Want) {
			t.Errorf("%s: %v, want %v", c.Name, got, c.Want)
		}
	}
}

// TestSectionBarRows: a section with no "bars:" line prints in 4-bar
// phrases, a lone last bar joining the row before it, a pickup riding on
// the first row; "bars: 8" prints as two phrases a row, as on screen.
func TestSectionBarRows(t *testing.T) {
	bars := func(rows []barRow) []int {
		var out []int
		for _, r := range rows {
			out = append(out, r.Bars)
		}
		return out
	}
	want := map[int][]int{
		1: {1}, 2: {2}, 3: {3}, 4: {4}, 5: {5}, 6: {4, 2}, 7: {4, 3}, 8: {4, 4},
		9: {4, 5}, 10: {4, 4, 2}, 11: {4, 4, 3}, 12: {4, 4, 4}, 13: {4, 4, 5},
		14: {4, 4, 4, 2}, 15: {4, 4, 4, 3}, 16: {4, 4, 4, 4}, 17: {4, 4, 4, 5},
	}
	for n, w := range want {
		if got := bars(sectionBarRows(nil, n, false)); !reflect.DeepEqual(got, w) {
			t.Errorf("%d bars: %v, want %v", n, got, w)
		}
		// With the song's pickup in front: the same phrases, the pickup on
		// the first row.
		wp := append([]int{w[0] + 1}, w[1:]...)
		if got := bars(sectionBarRows(nil, n+1, true)); !reflect.DeepEqual(got, wp) {
			t.Errorf("pickup + %d bars: %v, want %v", n, got, wp)
		}
	}
	if got := bars(sectionBarRows([]int{8}, 16, false)); !reflect.DeepEqual(got, []int{4, 4, 4, 4}) {
		t.Errorf("bars: 8 over 16 bars: %v", got)
	}
	if got := bars(sectionBarRows([]int{3}, 7, false)); !reflect.DeepEqual(got, []int{3, 3, 1}) {
		t.Errorf("bars: 3 over 7 bars: %v", got)
	}
}

// TestPickupRowsInPrint: the PDF finds the song's pickup where the web
// does (tab.StartsWithPickup) and draws it half a bar wide beside a full
// 4-bar phrase.
func TestPickupRowsInPrint(t *testing.T) {
	sg := song.Parse("time: 4/4\n\n[Intro]\ngrid: |? | Gm | Gm | Gm | Gm | Gm |\n")
	si := tab.FirstGridSection(sg)
	if !tab.StartsWithPickup(sg, si) {
		t.Fatalf("no pickup found")
	}
	rows := sectionBarRows(nil, len(sg.Sections[si].Measures), true)
	if !reflect.DeepEqual(rows, []barRow{{Bars: 6, Cap: 5}}) {
		t.Errorf("pickup + 5 bars: %v, want one row of 6, 5 wide", rows)
	}
	xs, ws := barGeometry(5, 4, true, 450)
	if ws[0] != 50 || ws[1] != 100 || xs[4] != 350 || xs[4]+ws[4] != 450 {
		t.Errorf("pickup + 4: xs %v ws %v", xs, ws)
	}
	// A short last row's bars keep a full row's width.
	xs, ws = barGeometry(2, 4, false, 400)
	if ws[0] != 100 || xs[1]+ws[1] != 200 {
		t.Errorf("a short row of 2: xs %v ws %v", xs, ws)
	}
}

// TestLeadRowsInPrint: a "lead:" line's bars ride on the opening section's
// first row, half a bar wide each, on top of its phrase or its "bars:".
func TestLeadRowsInPrint(t *testing.T) {
	sg := song.WithLeads(song.Parse("time: 4/4\n\n[Intro]\nlead: 2\nbars: 4\ngrid: | G | G | G | G | C | C | C | C |\n"))
	if sg.LeadIn() != 2 || tab.FirstGridSection(sg) != 0 || !tab.StartsWithPickup(sg, 0) {
		t.Fatalf("lead-in %d", sg.LeadIn())
	}
	n := len(sg.Sections[0].Measures)
	if rows := sectionBarRowsN([]int{4}, n, false, 2); !reflect.DeepEqual(rows, []barRow{{Bars: 6, Cap: 4}, {Bars: 4, Cap: 4}}) {
		t.Errorf("lead 2 + bars: 4: %v", rows)
	}
	if rows := sectionBarRowsN(nil, n, false, 2); !reflect.DeepEqual(rows, []barRow{{Bars: 6, Cap: 4}, {Bars: 4, Cap: 4}}) {
		t.Errorf("lead 2, no bars:: %v", rows)
	}
	// lead: 1 isn't one of the "bars:" either (a written pickup bar is).
	one := song.WithLeads(song.Parse("[Intro]\nlead: 1\nbars: 4\ngrid: | G | G | G | G | C | C | C | C |\n"))
	if rows := sectionBarRowsN([]int{4}, len(one.Sections[0].Measures), false, 1); !reflect.DeepEqual(rows, []barRow{{Bars: 5, Cap: 4}, {Bars: 4, Cap: 4}}) {
		t.Errorf("lead 1 + bars: 4: %v", rows)
	}
	xs, ws := barGeometryN(6, 4, 2, 500)
	if ws[0] != 50 || ws[1] != 50 || ws[2] != 100 || xs[2] != 100 || xs[5]+ws[5] != 500 {
		t.Errorf("2 leads + 4: xs %v ws %v", xs, ws)
	}
	// The whole PDF draws, every mode.
	src := "title: Lead\ntime: 4/4\n\n[Intro]\nlead: | D7 | . |\nbars: 4\ngrid: | G | G | G | G | C | C | C | C |\n\n[Verse]\nlead: 1\ngrid: | G | C |\n"
	for _, mode := range []Mode{ModeDefault, ModeJazz, ModeLyrics, ModeMap} {
		if _, err := New().ExportSong(context.Background(), "", src, nil, Options{Mode: mode}); err != nil {
			t.Errorf("render mode %v: %v", mode, err)
		}
	}
}
