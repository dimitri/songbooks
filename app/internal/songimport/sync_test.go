package songimport

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"app/internal/audioanalysis"
)

// singing builds a recording's vocal analysis: the voice sings from each
// section's start, in four-bar phrases: three bars sung, one bar's rest.
func singing(dur, bar float64, starts []float64, ends []float64) *audioanalysis.Analysis {
	a := &audioanalysis.Analysis{Duration: dur, AudioStart: 0.4, Hop: audioanalysis.Hop, TimeSig: "4/4"}
	a.VocalActivity = make([]bool, int(math.Ceil(dur/a.Hop)))
	for i, s := range starts {
		for p := s; p < ends[i]-0.1; p += 4 * bar {
			a.VocalOnsets = append(a.VocalOnsets, p)
			for f := int(math.Round(p / a.Hop)); f < int(math.Round((p+3*bar)/a.Hop)) && f < len(a.VocalActivity); f++ {
				a.VocalActivity[f] = true
			}
		}
	}
	return a
}

func chart(tempo int, sections []SyncSection) string {
	var b strings.Builder
	fmt.Fprintf(&b, "title: T\ntime: 4/4\ntempo: %d\n", tempo)
	for _, s := range sections {
		fmt.Fprintf(&b, "\n[%s]\ngrid: %sC\nla la\n", s.Label, strings.Repeat("C | ", s.BarCount-1))
	}
	return b.String()
}

// mismatch334 is song 334 as synthesized: a 90 bpm recording whose verse
// is sung over 17 bars where the chart writes 23 (and so on), with the
// chart's tempo header at chartTempo.
func mismatch334(t *testing.T, chartTempo int) (*SyncProposal, []float64, []int, []int) {
	t.Helper()
	bar := 60.0 / 90 * 4
	played := []int{17, 11, 16, 7}
	written := []int{23, 14, 22, 11}
	var starts, ends []float64
	at := 10.45
	for _, n := range played {
		starts = append(starts, at)
		at += float64(n) * bar
		ends = append(ends, at)
	}
	a := singing(at+30, bar, starts, ends)
	a.Tempo = 90
	var secs []SyncSection
	start := 0
	for i, n := range written {
		secs = append(secs, SyncSection{Index: i, Label: fmt.Sprintf("Part %d", i+1), StartBar: start, BarCount: n, HasLyrics: true})
		start += n
	}
	p, err := ProposeSync(a, true, chart(chartTempo, secs), secs, "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("tempo %d fit %d mismatch %v sections %+v notes %v", p.Tempo, p.FitTempo, p.TempoMismatch, p.Sections, p.Notes)
	return p, starts, played, written
}

// A chart whose sections have more bars than the recording plays (song
// 334: 23 written bars in a verse sung over 17, at 90 bpm where the chart
// says 104). Placing the sections on the singing then spreads the chart's
// bars at 122 bpm, and the player's bar.beat counter ran a third faster
// than the music. The proposal must say the bar counts are wrong, per
// section, and not offer 122 as the chart's tempo. Nor 104: that header
// was itself an earlier wizard's clamped fit, and the recording says 90.
func TestProposeSyncBarCountMismatch(t *testing.T) {
	p, starts, played, written := mismatch334(t, 104)
	if !p.TempoMismatch {
		t.Fatalf("a fit at %d bpm against a 90 bpm recording wasn't flagged", p.FitTempo)
	}
	if p.Tempo != 90 {
		t.Errorf("proposed tempo %d, want the recording's 90 (the chart's 104 contradicts it)", p.Tempo)
	}
	for i, s := range p.Sections[:len(played)-1] {
		if math.Abs(s.Time-starts[i]) > 0.1 {
			t.Errorf("%s at %.2f, want %.2f (where the singing starts)", s.Label, s.Time, starts[i])
		}
		if s.ChartBars != written[i] || s.RecordingBars != played[i] {
			t.Errorf("%s: chart %d / recording %d bars, want %d / %d", s.Label, s.ChartBars, s.RecordingBars, written[i], played[i])
		}
	}
	if len(p.Notes) < 2 || !strings.Contains(p.Notes[0], "90 bpm") {
		t.Fatalf("notes %q don't name the recording's tempo", p.Notes)
	}
	if !strings.Contains(p.Notes[1], "says 104") || !strings.Contains(p.Notes[1], "90 is proposed") {
		t.Errorf("notes %q don't say the chart's tempo is replaced", p.Notes)
	}
	// Said in French.
	p.Localize(func(msg string, args ...any) string { return "FR " + fmt.Sprintf(msg, args...) })
	if !strings.HasPrefix(p.Notes[0], "FR ") || !strings.HasPrefix(p.Notes[1], "FR ") {
		t.Errorf("notes not localized: %q", p.Notes)
	}
}

// The same wrong bar counts under a chart tempo the recording agrees
// with, or one a beat unit away from it (3:2), keep the chart's tempo:
// only the bar counts are flagged.
func TestProposeSyncBarCountMismatchKeepsChartTempo(t *testing.T) {
	for _, chartTempo := range []int{90, 92, 135} {
		p, _, _, _ := mismatch334(t, chartTempo)
		if !p.TempoMismatch {
			t.Errorf("chart %d: the bar counts weren't flagged", chartTempo)
		}
		if p.Tempo != chartTempo {
			t.Errorf("chart %d: proposed tempo %d, want the chart's kept", chartTempo, p.Tempo)
		}
		for _, n := range p.Notes {
			if strings.Contains(n, "is proposed instead") {
				t.Errorf("chart %d: note %q", chartTempo, n)
			}
		}
	}
}

func TestTempoContradicts(t *testing.T) {
	for _, c := range []struct {
		chart, rec float64
		want       bool
	}{
		{104, 90, true},  // song 334
		{120, 100, true}, // 20% off
		{95, 90, false},  // within 8%
		{135, 90, false}, // 3:2, a dotted beat
		{60, 90, false},  // 2:3
		{120, 90, false}, // 4:3
		{68, 90, false},  // 3:4
		{104, 0, false},  // no estimate
		{0, 90, false},   // no chart tempo
	} {
		if got := tempoContradicts(c.chart, c.rec); got != c.want {
			t.Errorf("tempoContradicts(%v, %v) = %v, want %v", c.chart, c.rec, got, c.want)
		}
	}
}

// When the chart's bars do match the recording, nothing is flagged and
// the fitted tempo is proposed.
func TestProposeSyncBarCountsAgree(t *testing.T) {
	bar := 60.0 / 100 * 4
	played := []int{16, 8, 16, 8}
	var starts, ends []float64
	at := 8.0
	for _, n := range played {
		starts = append(starts, at)
		at += float64(n) * bar
		ends = append(ends, at)
	}
	a := singing(at+20, bar, starts, ends)
	a.Tempo = 100
	var secs []SyncSection
	start := 0
	for i, n := range played {
		secs = append(secs, SyncSection{Index: i, Label: fmt.Sprintf("Part %d", i+1), StartBar: start, BarCount: n, HasLyrics: true})
		start += n
	}
	p, err := ProposeSync(a, true, chart(96, secs), secs, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.TempoMismatch || p.FitTempo < 98 || p.FitTempo > 102 {
		t.Fatalf("fit %d mismatch %v, want ~100 and no mismatch (%v)", p.FitTempo, p.TempoMismatch, p.Notes)
	}
	for _, s := range p.Sections {
		if s.RecordingBars != 0 {
			t.Errorf("%s flagged: %d vs %d", s.Label, s.ChartBars, s.RecordingBars)
		}
	}
}
