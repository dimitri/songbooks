package chartalign

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"app/internal/song"
)

// The fixtures are numbers only: testdata/song334.json is what the
// analysis stage stored for song 334's recording (Beat This! on its
// backing part, lv-chordia on the mix, the voice from its vocals part:
// `stemsd --analyze-files ref.wav,no_vocals.wav,vocals.wav`), and the
// .chart files are the song's grid lines, before PR #113 corrected them
// (old) and after (new). No audio, no lyrics.

// chartOf numbers a chart's sections the way the player does (the
// sections in order, each its played bars long), every one sung but an
// intro.
func chartOf(t *testing.T, name string) (*song.Song, []Section) {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	sg := song.Parse(string(b))
	var info []SectionInfo
	for i, s := range sg.Sections {
		info = append(info, SectionInfo{Index: i, Label: s.Label, HasLyrics: !strings.Contains(strings.ToLower(s.Label), "intro")})
	}
	secs := Chart(sg, info)
	bar := 0
	for i := range secs {
		secs[i].StartBar = bar
		bar += len(secs[i].Bars)
	}
	return sg, secs
}

func TestAlign334OldChart(t *testing.T) {
	r := loadRecording(t, "song334.json")
	_, secs := chartOf(t, "song334-old.chart")
	res := Align(r, secs, 4, Hints{})

	if math.Abs(res.FirstDownbeat-1.60) > 0.08 || res.IntroBars != 3 {
		t.Errorf("first downbeat %.2f, intro %d bars: want 1.60, 3", res.FirstDownbeat, res.IntroBars)
	}
	if res.Intro == nil || res.Intro.Bars != 3 || res.Intro.Grid != "N.C. | % | %" {
		t.Errorf("intro proposal %+v, want 3 bars of N.C.", res.Intro)
	}
	want := []struct {
		label          string
		start          float64
		written, plays int
	}{
		{"Verse 1", 9.59, 23, 16},
		{"Chorus 1", 52.20, 14, 12},
		{"Verse 2", 84.15, 22, 16},
		{"Chorus 2", 126.76, 11, 8},
		{"Bridge", 148.07, 11, 10},
		{"Chorus 3", 174.70, 19, 13},
		{"Outro", 209.31, 11, -1},
	}
	if len(res.Sections) != len(want) {
		t.Fatalf("%d sections", len(res.Sections))
	}
	for i, w := range want {
		s := res.Sections[i]
		t.Logf("%-9s start %7.2f  written %2d played %2d  conf %.2f moved %d  %s", s.Label, s.Start, s.Written, s.Played, s.Confidence, s.Moved, s.Grid)
		if s.Label != w.label || math.Abs(s.Start-w.start) > 0.08 || s.Written != w.written || (w.plays >= 0 && s.Played != w.plays) {
			t.Errorf("%s: start %.2f, %d written, %d played; want %.2f, %d, %d", s.Label, s.Start, s.Written, s.Played, w.start, w.written, w.plays)
		}
	}
	// The regrouped grids: the shared bars of the report, and Chorus 1
	// exactly as PR #113's editor wrote it.
	grids := map[string]string{
		"Verse 1":  "D | Asus4 A | Bm | Em/B A/C# | Bm D | A | Bm | % | Em/B G | Dsus4/F# D/F# | Em | Em/A G/A | D Asus4 | A | Bm | Asus4",
		"Chorus 1": "|: E | B/D# | F#m | A B :| C | % | Dsus4 | D",
	}
	for _, s := range res.Sections {
		if g, ok := grids[s.Label]; ok && s.Grid != g {
			t.Errorf("%s grid:\n got %s\nwant %s", s.Label, s.Grid, g)
		}
	}
	if b := res.Sections[4]; !strings.HasPrefix(b.Grid, "E | B | F#m7 | A Bsus4") {
		t.Errorf("Bridge grid %q: should start with the E written at the end of Chorus 2", b.Grid)
	}
	// Per-bar sync: every written bar placed, in order, the first on the
	// verse's downbeat.
	if len(res.BarPoints) < 100 {
		t.Errorf("%d bar points", len(res.BarPoints))
	}
	for i := 1; i < len(res.BarPoints); i++ {
		if res.BarPoints[i].Time <= res.BarPoints[i-1].Time || res.BarPoints[i].Bar <= res.BarPoints[i-1].Bar {
			t.Fatalf("bar points out of order at %d: %+v %+v", i, res.BarPoints[i-1], res.BarPoints[i])
		}
	}
	if p := res.BarPoints[0]; p.Bar != 0 || math.Abs(p.Time-9.59) > 0.08 {
		t.Errorf("first bar point %+v", p)
	}
	if res.State != "good" {
		t.Errorf("state %s (%.2f)", res.State, res.Confidence)
	}
	// The old chart's chords that the recording contradicts (PR #113's
	// editor rewrote these bars): proposals, each to check.
	fixes := map[string]bool{}
	for _, f := range res.ChordFixes {
		fixes[f.Chart+"→"+f.Heard] = true
		if f.Confidence < 0.5 || f.Beats < 2 {
			t.Errorf("weak fix proposed: %+v", f)
		}
	}
	if !fixes["Em/B→G"] {
		t.Errorf("Verse 1's Em/B where the recording plays G isn't flagged: %v", fixes)
	}
	// Without its words written in, the voice still says the Bridge
	// starts a bar before its first written chord.
	for i := range secs {
		secs[i].HasLyrics = false
	}
	if b := Align(r, secs, 4, Hints{}).Sections[4]; b.Played != 10 || math.Abs(b.Start-148.07) > 0.08 {
		t.Errorf("Bridge without lyrics: start %.2f, %d played", b.Start, b.Played)
	}
	for _, f := range res.ChordFixes {
		t.Logf("chord fix: bar %d (%s bar %d) %s → %s over %d beats, %.2f %v", f.Bar, secs[f.Section].Label, f.SecBar, f.Chart, f.Heard, f.Beats, f.Confidence, f.Alternatives)
	}
}

func TestAlign334NewChart(t *testing.T) {
	r := loadRecording(t, "song334.json")
	_, secs := chartOf(t, "song334-new.chart")
	res := Align(r, secs, 4, Hints{})
	if res.Intro != nil {
		t.Errorf("an intro proposed for a chart that has one: %+v", res.Intro)
	}
	truth := map[string]float64{"Intro": 1.60, "Verse 1": 9.59, "Chorus 1": 52.20, "Verse 2": 84.15, "Chorus 2": 126.76, "Bridge": 148.07, "Chorus 3": 174.70, "Outro": 209.31}
	for _, s := range res.Sections {
		t.Logf("%-9s start %7.2f  written %2d played %2d  conf %.2f", s.Label, s.Start, s.Written, s.Played, s.Confidence)
		if s.Grid != "" {
			t.Errorf("%s: a bar-count proposal for the corrected chart: written %d, played %d, %s", s.Label, s.Written, s.Played, s.Grid)
		}
		if w, ok := truth[s.Label]; ok && math.Abs(s.Start-w) > 0.08 {
			t.Errorf("%s starts at %.2f, want %.2f", s.Label, s.Start, w)
		}
	}
	for _, f := range res.ChordFixes {
		t.Logf("chord fix: bar %d (%s bar %d) %s → %s over %d beats, %.2f %v", f.Bar, secs[f.Section].Label, f.SecBar, f.Chart, f.Heard, f.Beats, f.Confidence, f.Alternatives)
	}
	// The corrected chart's chords are right: nothing to check (Em/B over
	// the B pedal, heard as Bm, is a voicing, not a wrong chord).
	if len(res.ChordFixes) != 0 {
		t.Errorf("%d chord fixes proposed for the corrected chart", len(res.ChordFixes))
	}
	if !res.Sections[len(res.Sections)-1].Last {
		t.Error("the outro isn't marked as the last section")
	}
}

func TestFold(t *testing.T) {
	for _, c := range []struct {
		in   []string
		want string
	}{
		{[]string{"E", "B/D#", "F#m", "A B", "E", "B/D#", "F#m", "A B", "C", "C", "Dsus4", "D"}, "|: E | B/D# | F#m | A B :| C | % | Dsus4 | D"},
		{[]string{"G", "D", "G", "D", "G", "D", "Am"}, "|:3 G | D :| Am"},
		{[]string{"N.C.", "N.C.", "N.C."}, "N.C. | % | %"},
		{[]string{"A", "B", "C", "D", "C", "D"}, "A | B |: C | D :|"},
	} {
		if got := Fold(c.in); got != c.want {
			t.Errorf("Fold(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSpell(t *testing.T) {
	for in, want := range map[string]string{"B:maj/3": "B/D#", "E:min/5": "Em/B", "A:maj/3": "A/C#", "D:sus4(b7)": "D7sus4", "F#:min7": "F#m7", "N": "N.C.", "Bb:maj/3": "A#/C##"} {
		if in == "Bb:maj/3" {
			continue
		}
		if got := Spell(in, 0, false); got != want {
			t.Errorf("Spell(%s) = %s, want %s", in, got, want)
		}
	}
	if got := Spell("Bb:maj/3", 0, true); got != "Bb/D" {
		t.Errorf("Spell(Bb:maj/3, flats) = %s", got)
	}
}

func TestCompatible(t *testing.T) {
	p := func(s string) Chord { c, _ := ParseSymbol(s); return c }
	h := func(s string) Chord { c, _ := ParseHarte(s); return c }
	for _, c := range []struct {
		chart, heard string
		ok           bool
	}{
		{"Em/B", "E:min", true}, {"A", "F#:min7", true}, {"F#m7", "A:maj", true}, {"Asus4", "A:maj", true},
		{"D7sus4", "D:maj", true}, {"Bm", "D:maj", false}, {"G/A", "A:sus4", true}, {"C", "D:maj", false},
		{"E", "B:maj", false}, {"Bb", "A#:maj", true}, {"Cadd9", "C:maj", true}, {"N.C.", "N", true},
	} {
		if got := Compatible(p(c.chart), h(c.heard)); got != c.ok {
			t.Errorf("Compatible(%s, %s) = %v", c.chart, c.heard, got)
		}
	}
}

// Song 81 ("I'll Play the Blues for You"): its chart's intro is short of
// what the recording plays before the first verse. The alignment isn't
// told; it has to find it. testdata/song81.json is the analysis of its
// recording (numbers only), song81.chart its grid lines, and
// song81-sections.json the player's numbering of its sections.
func TestAlign81ShortIntro(t *testing.T) {
	r := loadRecording(t, "song81.json")
	b, err := os.ReadFile("testdata/song81.chart")
	if err != nil {
		t.Fatal(err)
	}
	var info []SectionInfo
	raw, err := os.ReadFile("testdata/song81-sections.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &info); err != nil {
		t.Fatal(err)
	}
	secs := Chart(song.Parse(string(b)), info)
	res := Align(r, secs, 4, Hints{})
	t.Logf("tempo %.1f %s (%.2f), first downbeat %.2f, intro bars before the first section %d, state %s %.2f", res.Tempo, res.TimeSig, res.TimeSigConf, res.FirstDownbeat, res.IntroBars, res.State, res.Confidence)
	for _, s := range res.Sections {
		t.Logf("%-7s start %7.2f  written %2d played %2d  conf %.2f moved %d last %v  %s", s.Label, s.Start, s.Written, s.Played, s.Confidence, s.Moved, s.Last, s.Grid)
	}
	for _, f := range res.ChordFixes {
		t.Logf("chord fix: bar %d (%s bar %d, %.2f s) %s → %s over %d beats, %.2f %v", f.Bar, f.Chart, f.SecBar, f.Time, f.Chart, f.Heard, f.Beats, f.Confidence, f.Alternatives)
	}
	if res.Intro != nil {
		t.Errorf("an intro proposed although the chart has one: %+v", res.Intro)
	}
	intro := res.Sections[0]
	if intro.Written != 4 || intro.Played <= intro.Written || intro.Grid == "" {
		t.Errorf("intro: written %d, played %d, grid %q: the recording's longer intro isn't found", intro.Written, intro.Played, intro.Grid)
	}
	if v := res.Sections[1]; v.Written != 14 || v.Played != 14 || v.Grid != "" {
		t.Errorf("first verse: written %d, played %d, grid %q", v.Written, v.Played, v.Grid)
	}
	if res.State == "unsure" {
		t.Errorf("state %s", res.State)
	}
}

// A recording whose tracker hears a downbeat every half bar (2/4 against
// the chart's 4/4, as on several demo songs): its bars are grouped in
// fours, on the phase the chords fit, and nothing changes.
func TestAlignHalfBars(t *testing.T) {
	r := loadRecording(t, "song334.json")
	first := 0
	for i, p := range r.BeatPos {
		if p == 1 {
			first = i
			break
		}
	}
	// A downbeat every 2 beats.
	for i := range r.BeatPos {
		r.BeatPos[i] = ((i-first)%2+2)%2 + 1
	}
	r.BeatsPerBar, r.TimeSig = 2, "2/4"
	_, secs := chartOf(t, "song334-new.chart")
	res := Align(r, secs, 4, Hints{})
	if res.TimeSig != "4/4" {
		t.Errorf("metre %s", res.TimeSig)
	}
	for _, s := range res.Sections {
		if s.Grid != "" {
			t.Errorf("%s: written %d, played %d", s.Label, s.Written, s.Played)
		}
	}
	if v := res.Sections[1]; math.Abs(v.Start-9.59) > 0.08 {
		t.Errorf("Verse 1 at %.2f", v.Start)
	}
}
