package songimport

import (
	"strings"
	"testing"

	"app/internal/chartalign"
)

// An unsteady recording proposes no tempo: "Use this sync" keeps the
// tempo the voice's fit chose against a chart tempo the recording
// contradicts (#113's song 334), and its warning.
func TestUseAutoSyncKeepsContradictedTempo(t *testing.T) {
	p := &SyncProposal{Tempo: 90, TempoMismatch: true, Sections: []SectionTime{{Index: 0, Time: 1}, {Index: 1, Time: 30}}}
	p.note("the chart's tempo says %s, but the recording plays at about %d bpm: %d is proposed instead", "104", 90, 90)
	a := &AutoSync{Result: &chartalign.Result{State: "check", Tempo: 90, TempoSteady: false,
		Sections: []chartalign.SectionResult{{Index: 0, Start: 1.2, Written: 8, Played: 8}, {Index: 1, Start: 30.1, Written: 8, Played: 8, Last: true}}},
		ChartTempo: "104"}
	p.UseAutoSync(a)
	if p.Tempo != 90 || !p.TempoMismatch {
		t.Errorf("tempo %d, mismatch %v: want 90 and the warning kept", p.Tempo, p.TempoMismatch)
	}
	if len(p.Notes) < 2 || !strings.Contains(p.Notes[0], "104") {
		t.Errorf("notes %v", p.Notes)
	}
	if p.Sections[0].Time != 1.2 || !p.Downbeats {
		t.Errorf("starts not taken from the analysis: %+v", p.Sections)
	}
	// The chart's tempo confirmed by the recording: it's the one.
	q := &SyncProposal{Tempo: 97, TempoMismatch: true, Sections: []SectionTime{{Index: 0, Time: 1}}}
	b := &AutoSync{Result: &chartalign.Result{State: "good", Tempo: 91, Sections: []chartalign.SectionResult{{Index: 0, Start: 1.2, Last: true}}}, ChartTempo: "90"}
	q.UseAutoSync(b)
	if q.Tempo != 90 || q.TempoMismatch {
		t.Errorf("confirmed chart tempo: %d, mismatch %v", q.Tempo, q.TempoMismatch)
	}
}
