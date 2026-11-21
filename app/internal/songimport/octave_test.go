package songimport

import (
	"math"
	"os"
	"strings"
	"testing"

	"app/internal/chartalign"
)

// doubled is song 334's recording as a tracker hearing twice its beats
// reports it (every beat split in two, 4/4 bars of those: 180 bpm), the
// way a half-time chart's recording is heard against its chart (Creep:
// a chart at 46 against a recording tracked at 92).
func doubled(r *chartalign.Recording) *chartalign.Recording {
	d := *r
	d.Beats, d.BeatPos, d.BeatAct, d.BeatChords = nil, nil, nil, nil
	first := -1
	for i, b := range r.Beats {
		d.Beats = append(d.Beats, b)
		if i+1 < len(r.Beats) {
			d.Beats = append(d.Beats, (b+r.Beats[i+1])/2)
		}
		if first < 0 && i < len(r.BeatPos) && r.BeatPos[i] == 1 {
			first = 2 * i
		}
	}
	for i := range d.Beats {
		d.BeatPos = append(d.BeatPos, ((i-first)%4+4)%4+1)
		d.BeatAct = append(d.BeatAct, 0.9)
	}
	for i := 0; i+1 < len(d.Beats); i++ {
		k := min(i/2, len(r.BeatChords)-1)
		if k >= 0 {
			d.BeatChords = append(d.BeatChords, r.BeatChords[k])
		}
	}
	d.Tempo, d.TempoMin, d.TempoMax = 2*r.Tempo, 2*r.TempoMin, 2*r.TempoMax
	return &d
}

// The chart counts at half the tracker's tempo: the alignment reads the
// beats in pairs on its own, the sections start where they start, and
// neither a tempo nor doubled grids are proposed.
func TestAutoSyncHalfTime(t *testing.T) {
	r := doubled(rec334(t))
	b, err := os.ReadFile("../chartalign/testdata/song334-new.chart")
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	if !strings.Contains(body, "tempo: 90") {
		body = SetHeader(body, "tempo", "90", true)
	}
	p := ProposeAutoSync(r, body, syncOf(body), chartalign.Hints{})
	t.Logf("state %s %.2f, auto ×%.2f, tempo %.1f, propose %d, written %d played %d, notes %v", p.State, p.Confidence, p.AutoTempoFactor, p.Tempo, p.ProposeTempo, p.WrittenBars, p.PlayedBars, p.Notes)
	if p.AutoTempoFactor != 0.5 {
		t.Errorf("auto hint %v, want 0.5 (beats read in pairs)", p.AutoTempoFactor)
	}
	if p.ProposeTempo != 0 {
		t.Errorf("tempo %d proposed: the chart's 90 is the recording's, counted in halves", p.ProposeTempo)
	}
	truth := map[string]float64{"Verse 1": 9.59, "Chorus 1": 52.20, "Verse 2": 84.15, "Chorus 2": 126.76, "Bridge": 148.07}
	for _, s := range p.Sections {
		if s.Grid != "" {
			t.Errorf("%s: written %d, played %d, grid %s", s.Label, s.Written, s.Played, s.Grid)
		}
		if w, ok := truth[s.Label]; ok && math.Abs(s.Start-w) > 0.08 {
			t.Errorf("%s at %.2f, want %.2f", s.Label, s.Start, w)
		}
	}
	if p.State == "unsure" {
		t.Errorf("state %s", p.State)
	}
	// As detected (the editor's choice): every section twice as long, and
	// never "good"; without a tempo to say so, ÷2 is offered first.
	plain := ProposeAutoSync(r, body, syncOf(body), chartalign.Hints{TempoFactor: 1})
	if plain.State == "good" || plain.AutoTempoFactor != 0 {
		t.Errorf("as detected: %s, auto %v", plain.State, plain.AutoTempoFactor)
	}
	noTempo := SetHeader(body, "tempo", "", true)
	q := ProposeAutoSync(r, noTempo, syncOf(noTempo), chartalign.Hints{})
	if q.AutoTempoFactor != 0 || q.SuggestTempoFactor != 0.5 {
		t.Errorf("no chart tempo: auto %v, suggest %v; want ÷2 suggested", q.AutoTempoFactor, q.SuggestTempoFactor)
	}
	for _, s := range q.Sections {
		if s.Grid != "" {
			t.Errorf("no chart tempo, %s: a grid proposed before ÷2 (%s)", s.Label, s.Grid)
		}
	}
}

// A slow 12/8 heard in eighths, three to a bar (3/4 at 200 bpm): its
// bars are read in twelves, no 3/4 is proposed and no tempo of 267.
func TestAutoSyncSlowTwelveEight(t *testing.T) {
	const eighth = 0.3 // 200 bpm
	prog := []string{"C:maj", "F:maj", "G:maj", "C:maj"}
	r := &chartalign.Recording{Analyzer: "test", AudioStart: 1.2, Tempo: 200, TempoCV: 0.01, TempoMin: 198, TempoMax: 202,
		BeatsPerBar: 3, TimeSig: "3/4", TimeSigConf: 1, BeatConf: 0.9}
	start := 1.2
	bars := 16
	for i := 0; i <= bars*12; i++ {
		r.Beats = append(r.Beats, start+float64(i)*eighth)
		r.BeatPos = append(r.BeatPos, i%3+1)
		r.BeatAct = append(r.BeatAct, 0.9)
	}
	for b := 0; b < bars; b++ {
		c := prog[b%4]
		r.Chords = append(r.Chords, chartalign.ChordSeg{T: start + float64(b*12)*eighth, E: start + float64(b*12+12)*eighth, C: c})
		for k := 0; k < 12; k++ {
			r.BeatChords = append(r.BeatChords, []chartalign.ChordGuess{{C: c, P: 0.9}})
		}
	}
	r.Duration = start + float64(bars*12)*eighth + 2
	body := "title: T\ntime: 12/8\ntempo: 67\n\n[Verse]\ngrid: |:4 C | F | G | C :|\nla\n"
	p := ProposeAutoSync(r, body, syncOf(body), chartalign.Hints{})
	t.Logf("state %s %.2f, auto %.3f, tempo %.1f, propose %d/%q, written %d played %d, notes %v", p.State, p.Confidence, p.AutoTempoFactor, p.Tempo, p.ProposeTempo, p.ProposeTime, p.WrittenBars, p.PlayedBars, p.Notes)
	if p.ProposeTime != "" {
		t.Errorf("metre %q proposed for a 12/8 chart", p.ProposeTime)
	}
	if p.ProposeTempo != 0 {
		t.Errorf("tempo %d proposed against the chart's 67", p.ProposeTempo)
	}
	if p.AutoTempoFactor == 0 || p.AutoTempoFactor > 0.4 {
		t.Errorf("auto hint %v, want 1/3", p.AutoTempoFactor)
	}
	if len(p.Sections) != 1 || math.Abs(p.Sections[0].Start-start) > 0.05 {
		t.Errorf("sections %+v", p.Sections)
	}
	if p.State == "unsure" {
		t.Errorf("state %s", p.State)
	}
	// Its tempo off: 12/8 at 100 (in dotted quarters) heard at 200 in
	// eighths isn't 3 to 1, but the bars of three are still read as its
	// eighths, and 3/4 still isn't proposed.
	body2 := strings.Replace(body, "tempo: 67", "tempo: 100", 1)
	if q := ProposeAutoSync(r, body2, syncOf(body2), chartalign.Hints{}); q.ProposeTime != "" || q.ProposeTempo > 150 {
		t.Errorf("chart at 100: metre %q, tempo %d proposed", q.ProposeTime, q.ProposeTempo)
	}
}

// A chart whose bars far outnumber the recording's: unsure, saying it's
// the chart, and nothing proposed.
func TestAutoSyncAbsurdChart(t *testing.T) {
	r := rec334(t)
	body := "title: T\ntime: 4/4\ntempo: 90\n\n[Verse]\ngrid: |:100 D | A | Bm | G :|\nla\n"
	p := ProposeAutoSync(r, body, syncOf(body), chartalign.Hints{})
	if p.State != "unsure" || p.ProposeTempo != 0 || p.Intro != nil || len(p.ChordFixes) != 0 {
		t.Errorf("state %s, tempo %d, intro %v, %d fixes", p.State, p.ProposeTempo, p.Intro, len(p.ChordFixes))
	}
	if len(p.Notes) == 0 || !strings.Contains(p.Notes[0], "400 bars") {
		t.Errorf("notes %v: want the chart's 400 bars named", p.Notes)
	}
}

func TestEighths(t *testing.T) {
	for sig, want := range map[string]int{"4/4": 8, "3/4": 6, "12/8": 12, "6/8": 6, "2/2": 8, "7/8": 7, "x": 0} {
		if got := eighths(sig); got != want {
			t.Errorf("eighths(%s) = %d, want %d", sig, got, want)
		}
	}
	if !octaveApart(46, 92) || !octaveApart(184, 92) || octaveApart(100, 92) {
		t.Error("octaveApart")
	}
}
