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

// A chart whose sections have more bars than the recording plays (song
// 334: 23 written bars in a verse sung over 17, at 90 bpm where the chart
// says 104). Placing the sections on the singing then spreads the chart's
// bars at 122 bpm, and the player's bar.beat counter ran a third faster
// than the music. The proposal must say the bar counts are wrong, per
// section, and not offer 122 as the chart's tempo.
func TestProposeSyncBarCountMismatch(t *testing.T) {
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
	p, err := ProposeSync(a, true, chart(104, secs), secs, "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("tempo %d fit %d mismatch %v sections %+v notes %v", p.Tempo, p.FitTempo, p.TempoMismatch, p.Sections, p.Notes)
	if !p.TempoMismatch {
		t.Fatalf("a fit at %d bpm against a 90 bpm recording wasn't flagged", p.FitTempo)
	}
	if p.Tempo != 104 {
		t.Errorf("proposed tempo %d, want the chart's 104 kept", p.Tempo)
	}
	for i, s := range p.Sections[:len(played)-1] {
		if math.Abs(s.Time-starts[i]) > 0.1 {
			t.Errorf("%s at %.2f, want %.2f (where the singing starts)", s.Label, s.Time, starts[i])
		}
		if s.ChartBars != written[i] || s.RecordingBars != played[i] {
			t.Errorf("%s: chart %d / recording %d bars, want %d / %d", s.Label, s.ChartBars, s.RecordingBars, written[i], played[i])
		}
	}
	if len(p.Notes) == 0 || !strings.Contains(p.Notes[0], "90 bpm") {
		t.Errorf("notes %q don't name the recording's tempo", p.Notes)
	}
	// Said in French.
	p.Localize(func(msg string, args ...any) string { return "FR " + fmt.Sprintf(msg, args...) })
	if !strings.HasPrefix(p.Notes[0], "FR ") {
		t.Errorf("notes not localized: %q", p.Notes)
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
