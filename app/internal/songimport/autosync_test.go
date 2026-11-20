package songimport

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"app/internal/chartalign"
	"app/internal/song"
)

// syncOf numbers a chart's sections as the player does, for charts
// without bare sections (httpapi.SyncSections can't be imported here).
func syncOf(body string) []SyncSection {
	sg := song.Parse(body)
	var info []chartalign.SectionInfo
	for i, s := range sg.Sections {
		info = append(info, chartalign.SectionInfo{Index: i, Label: s.Label})
	}
	secs := chartalign.Chart(sg, info)
	var out []SyncSection
	bar := 0
	for i, s := range secs {
		sung := !strings.Contains(strings.ToLower(s.Label), "intro")
		out = append(out, SyncSection{Index: i, Label: s.Label, StartBar: bar, BarCount: len(s.Bars), HasLyrics: sung})
		bar += len(s.Bars)
	}
	return out
}

func rec334(t *testing.T) *chartalign.Recording {
	t.Helper()
	b, err := os.ReadFile("../chartalign/testdata/song334.json")
	if err != nil {
		t.Fatal(err)
	}
	var r chartalign.Recording
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

// Song 334's old chart, every proposal taken: the intro added, the
// sections regrouped on the recording's bars, the tempo. Aligned again,
// the chart has nothing left to propose about its bars, and its
// sections and bars sit where the recording plays them.
func TestApplyAutoFix334(t *testing.T) {
	r := rec334(t)
	b, err := os.ReadFile("../chartalign/testdata/song334-old.chart")
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	p := ProposeAutoSync(r, body, syncOf(body), chartalign.Hints{})
	if p.ProposeTempo != 90 || p.ProposeTime != "" {
		t.Errorf("proposed tempo %d, time %q: want 90 and nothing", p.ProposeTempo, p.ProposeTime)
	}
	fix := AutoFix{Tempo: true, Intro: true}
	for _, s := range p.Sections {
		if s.Grid != "" {
			fix.Sections = append(fix.Sections, s.Index)
		}
	}
	out, changes := ApplyAutoFix(r, body, syncOf, chartalign.Hints{}, fix)
	for _, c := range changes {
		t.Logf("%s: %q → %q", c.Where, c.Before, c.After)
	}
	if !strings.Contains(out, "tempo: 90") || !strings.Contains(out, "[Intro]\ngrid: N.C. | % | %") {
		t.Errorf("tempo or intro missing:\n%s", out)
	}
	if len(changes) != 9 { // tempo, intro, 6 sections, … and Chorus 3
		t.Logf("%d changes", len(changes))
	}
	again := ProposeAutoSync(r, out, syncOf(out), chartalign.Hints{})
	if again.Intro != nil || again.ProposeTempo != 0 {
		t.Errorf("proposed again: intro %+v, tempo %d", again.Intro, again.ProposeTempo)
	}
	for _, s := range again.Sections {
		if s.Grid != "" {
			t.Errorf("%s: still written %d, played %d", s.Label, s.Written, s.Played)
		}
	}
}

func TestReplaceToken(t *testing.T) {
	body := "time: 4/4\n[Verse]\ngrid: D | Asus4 | A |\ngrid: |: Bm Em/B | C:2 . :|\nla la\n[Chorus]\ngrid: E"
	got := replaceToken(body, 0, 3, 1, "G")
	if !strings.Contains(got, "grid: |: Bm G | C:2 . :|") {
		t.Errorf("got\n%s", got)
	}
	got = replaceToken(body, 0, 4, 0, "Am")
	if !strings.Contains(got, "| Am:2 . :|") {
		t.Errorf("got\n%s", got)
	}
	if replaceGrid(body, 0, "D | A") != "time: 4/4\n[Verse]\ngrid: D | A\nla la\n[Chorus]\ngrid: E" {
		t.Errorf("replaceGrid: %q", replaceGrid(body, 0, "D | A"))
	}
}
