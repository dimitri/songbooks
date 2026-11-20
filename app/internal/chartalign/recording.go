// Package chartalign lines a song's chart up with what its reference
// recording plays: the recording's beats, downbeats and chords, found
// once per recording by the stems worker (cmd/stemsd, after the split:
// deploy/analysis/sbanalyze.py, Beat This! and lv-chordia), and the
// chart's bars and chords, which change with every edit — so the
// alignment runs at request time, in milliseconds.
//
// From the alignment come: where each section starts (on a downbeat;
// the voice only chooses which one), every bar's time in the recording
// (per-bar sync points), how many bars each section really plays against
// what's written ("written 23 · played 16") with a regrouped grid, the
// intro the chart doesn't have, and the bars whose chord the recording
// contradicts. Everything is a proposal for an editor; nothing here
// writes. See app/reports/auto-sync-chords.md.
package chartalign

import (
	"encoding/json"
	"math"
	"sort"
)

// Recording is what the analysis stage stores for one recording
// (song_audio_analysis.data). Times are in seconds.
type Recording struct {
	Analyzer   string  `json:"analyzer"`
	Duration   float64 `json:"duration"`
	AudioStart float64 `json:"audioStart"`
	// The beat grid: every beat, its place in the bar (1 = downbeat, 0
	// before the first downbeat) and the beat tracker's activation there.
	BeatSource string    `json:"beatSource"` // "no_vocals" or "mix"
	Beats      []float64 `json:"beats"`
	BeatPos    []int     `json:"beatPos"`
	BeatAct    []float64 `json:"beatAct,omitempty"`
	// Summary of the grid.
	Tempo       float64 `json:"tempo"`   // bpm, from the median beat
	TempoCV     float64 `json:"tempoCV"` // spread of the beat period: steady < 0.03, rubato > 0.08
	TempoMin    float64 `json:"tempoMin,omitempty"`
	TempoMax    float64 `json:"tempoMax,omitempty"`
	BeatsPerBar int     `json:"beatsPerBar"`
	TimeSig     string  `json:"timeSig"`
	TimeSigConf float64 `json:"timeSigConf"` // share of bars with BeatsPerBar beats
	BeatConf    float64 `json:"beatConf"`    // mean activation at the beats
	Key         string  `json:"key,omitempty"`
	KeyConf     float64 `json:"keyConf,omitempty"`
	// Chords: the recogniser's segments, and per beat (beat i to i+1)
	// the likeliest chords with their share, best first.
	Chords     []ChordSeg     `json:"chords"`
	BeatChords [][]ChordGuess `json:"beatChords,omitempty"`
	// The voice (vocals part): phrase starts, and phrases.
	VoiceOnsets  []float64    `json:"voiceOnsets,omitempty"`
	VoicePhrases [][2]float64 `json:"voicePhrases,omitempty"`
}

// ChordSeg is one chord segment, a Harte label ("D:maj", "A:maj/3", "N").
type ChordSeg struct {
	T float64 `json:"t"`
	E float64 `json:"e"`
	C string  `json:"c"`
}

// ChordGuess is one candidate chord for a beat and its share.
type ChordGuess struct {
	C string
	P float64
}

// MarshalJSON writes ["D:maj", 0.82]: there are a few hundred per song.
func (g ChordGuess) MarshalJSON() ([]byte, error) { return json.Marshal([]any{g.C, g.P}) }

// UnmarshalJSON reads ["D:maj", 0.82].
func (g *ChordGuess) UnmarshalJSON(b []byte) error {
	var a []json.RawMessage
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	if len(a) != 2 {
		return nil
	}
	if err := json.Unmarshal(a[0], &g.C); err != nil {
		return err
	}
	return json.Unmarshal(a[1], &g.P)
}

// Raw is sbanalyze.py's output.
type Raw struct {
	Analyzer   string         `json:"analyzer"`
	Duration   float64        `json:"duration"`
	BeatSource string         `json:"beat_source"`
	Beats      []float64      `json:"beats"`
	Downbeats  []float64      `json:"downbeats"`
	BeatAct    []float64      `json:"beat_act"`
	Chords     []ChordSeg     `json:"chords"`
	BeatChords [][]ChordGuess `json:"beat_chords"`
}

// Extra is what the Go analysis of the same files adds (audioanalysis:
// the voice from the vocals part, the key, where the audio starts, and
// its metre heuristic, which tells 6/8 from 3/4 and 12/8 from 4/4).
type Extra struct {
	AudioStart   float64
	Key          string
	KeyConf      float64
	TimeSig      string // audioanalysis' guess
	VoiceOnsets  []float64
	VoicePhrases [][2]float64
}

// FromRaw summarises the models' output into what's stored: each beat's
// place in the bar, the tempo and its steadiness, the metre.
func FromRaw(raw Raw, x Extra) *Recording {
	r := &Recording{
		Analyzer: raw.Analyzer, Duration: raw.Duration, AudioStart: x.AudioStart,
		BeatSource: raw.BeatSource, Beats: raw.Beats, BeatAct: raw.BeatAct,
		Chords: raw.Chords, BeatChords: raw.BeatChords,
		Key: x.Key, KeyConf: x.KeyConf, VoiceOnsets: x.VoiceOnsets, VoicePhrases: x.VoicePhrases,
	}
	if r.Chords == nil {
		r.Chords = []ChordSeg{}
	}
	oldBeats := raw.Beats
	raw.Beats, raw.Downbeats, raw.BeatAct = regularize(raw.Beats, raw.Downbeats, raw.BeatAct)
	r.Beats, r.BeatAct = raw.Beats, raw.BeatAct
	if len(raw.BeatChords) > 0 && !equalF(raw.Beats, oldBeats) {
		// Beats were added or merged: each beat takes the chords of the old
		// beat interval it lies in.
		bc := make([][]ChordGuess, 0, len(raw.Beats))
		for i := 0; i+1 < len(raw.Beats); i++ {
			mid := (raw.Beats[i] + raw.Beats[i+1]) / 2
			k := sort.SearchFloat64s(oldBeats, mid) - 1
			k = max(0, min(k, len(raw.BeatChords)-1))
			bc = append(bc, raw.BeatChords[k])
		}
		r.BeatChords = bc
	}
	r.BeatPos = beatPositions(raw.Beats, raw.Downbeats)
	// The metre: beats between consecutive downbeats.
	counts := map[int]int{}
	bars := 0
	for i := 0; i+1 < len(raw.Downbeats); i++ {
		n := 0
		for _, b := range raw.Beats {
			if b > raw.Downbeats[i]-0.05 && b < raw.Downbeats[i+1]-0.05 {
				n++
			}
		}
		if n > 0 {
			counts[n]++
			bars++
		}
	}
	best := 4
	for n, c := range counts {
		if c > counts[best] || (c == counts[best] && n < best) {
			best = n
		}
	}
	r.BeatsPerBar = best
	if bars > 0 {
		r.TimeSigConf = round3(float64(counts[best]) / float64(bars))
	}
	r.TimeSig = timeSigFor(best, x.TimeSig)
	// The tempo, from the beat period over whole bars (a single beat is
	// only known to the tracker's 20 ms frame), and how much it moves:
	// the middle 80% of the bars, so a fermata or a missed beat doesn't
	// count.
	downs := r.Downbeats()
	var per []float64
	for i := 1; i < len(downs); i++ {
		n := 0
		for j, b := range r.Beats {
			if b >= downs[i-1]-0.01 && b < downs[i]-0.01 && j < len(r.BeatPos) {
				n++
			}
		}
		if d := (downs[i] - downs[i-1]) / float64(n); n > 0 && d > 0.15 && d < 2.5 {
			per = append(per, d)
		}
	}
	if len(per) < 3 {
		per = per[:0]
		for i := 1; i < len(r.Beats); i++ {
			if d := r.Beats[i] - r.Beats[i-1]; d > 0.15 && d < 2.5 {
				per = append(per, d)
			}
		}
	}
	if len(per) > 2 {
		sort.Float64s(per)
		mid := per[len(per)/10 : len(per)-len(per)/10]
		mean, sd := 0.0, 0.0
		for _, d := range mid {
			mean += d
		}
		mean /= float64(len(mid))
		for _, d := range mid {
			sd += (d - mean) * (d - mean)
		}
		sd = math.Sqrt(sd / float64(len(mid)))
		r.Tempo = round2(60 / mean)
		r.TempoCV = round3(sd / mean)
		r.TempoMin, r.TempoMax = round2(60/mid[len(mid)-1]), round2(60/mid[0])
	}
	if len(raw.BeatAct) > 0 {
		s := 0.0
		for _, a := range raw.BeatAct {
			s += a
		}
		r.BeatConf = round3(s / float64(len(raw.BeatAct)))
	}
	return r
}

// beatPositions numbers each beat in its bar from the downbeats: 1 on a
// downbeat, then 2, 3…; before the first downbeat, counted back from it.
func beatPositions(beats, downs []float64) []int {
	pos := make([]int, len(beats))
	isDown := make([]bool, len(beats))
	j := 0
	for i, b := range beats {
		for j < len(downs) && downs[j] < b-0.05 {
			j++
		}
		if j < len(downs) && math.Abs(downs[j]-b) <= 0.05 {
			isDown[i] = true
		}
	}
	first := -1
	for i, d := range isDown {
		if d {
			first = i
			break
		}
	}
	if first < 0 {
		return pos
	}
	c := 0
	for i := first; i < len(beats); i++ {
		if isDown[i] {
			c = 1
		} else {
			c++
		}
		pos[i] = c
	}
	// Before the first downbeat: a pickup, counted back (…, 3, 4 | 1).
	n := 0
	for i := first + 1; i < len(beats) && !isDown[i]; i++ {
		n++
	}
	per := n + 1
	if per < 2 {
		per = 4
	}
	for i := first - 1; i >= 0; i-- {
		k := (first - i) % per
		pos[i] = per - k + 1
		if k == 0 {
			pos[i] = 1
		}
	}
	return pos
}

func timeSigFor(beats int, heuristic string) string {
	switch beats {
	case 2:
		if heuristic == "6/8" {
			return "6/8"
		}
		return "2/4"
	case 3:
		return "3/4"
	case 4:
		if heuristic == "12/8" {
			return "12/8"
		}
		return "4/4"
	case 5:
		return "5/4"
	case 6:
		return "6/8"
	case 7:
		return "7/8"
	}
	return "4/4"
}

func round2(x float64) float64 { return math.Round(x*100) / 100 }
func round3(x float64) float64 { return math.Round(x*1000) / 1000 }

// IsDownbeat reports whether beat i starts a bar.
func (r *Recording) IsDownbeat(i int) bool { return i >= 0 && i < len(r.BeatPos) && r.BeatPos[i] == 1 }

// Downbeats returns the downbeat times.
func (r *Recording) Downbeats() []float64 {
	var out []float64
	for i, p := range r.BeatPos {
		if p == 1 {
			out = append(out, r.Beats[i])
		}
	}
	return out
}

// chordAt is the recogniser's segment label at time t.
func (r *Recording) chordAt(t float64) string {
	i := sort.Search(len(r.Chords), func(i int) bool { return r.Chords[i].T > t }) - 1
	if i < 0 || i >= len(r.Chords) {
		return "N"
	}
	return r.Chords[i].C
}

// regularize repairs the two slips a beat tracker makes that throw a
// bar count out: a beat it missed (a gap of two or three beats gets the
// missing ones back, evenly spaced) and a downbeat on the wrong beat on
// its own (song 334's first bar: a downbeat a beat early, the bars
// either side agreeing on another phase), moved to the phase both its
// neighbours have. A real change of metre (two downbeats in a row on a
// new phase) is left alone.
func regularize(beats, downs, act []float64) ([]float64, []float64, []float64) {
	if len(beats) < 8 {
		return beats, downs, act
	}
	ibi := make([]float64, 0, len(beats))
	for i := 1; i < len(beats); i++ {
		ibi = append(ibi, beats[i]-beats[i-1])
	}
	sorted := append([]float64{}, ibi...)
	sort.Float64s(sorted)
	med := sorted[len(sorted)/2]
	// Two beats far closer than a beat apart are one (song 334's fade:
	// 207.92 and 208.00): keep the surer.
	{
		var kb, ka []float64
		for i, b := range beats {
			a := 0.0
			if i < len(act) {
				a = act[i]
			}
			if n := len(kb); n > 0 && b-kb[n-1] < 0.4*med {
				if a > ka[n-1] {
					kb[n-1], ka[n-1] = b, a
				}
				continue
			}
			kb, ka = append(kb, b), append(ka, a)
		}
		beats, act = kb, ka
	}
	var nb, na []float64
	for i, b := range beats {
		if i > 0 {
			d := b - beats[i-1]
			k := int(math.Round(d / med))
			if k >= 2 && k <= 3 && math.Abs(d/float64(k)-med) < 0.15*med {
				for j := 1; j < k; j++ {
					nb = append(nb, round3(beats[i-1]+d*float64(j)/float64(k)))
					na = append(na, math.Min(act[i-1], act[i]))
				}
			}
		}
		nb = append(nb, b)
		a := 0.0
		if i < len(act) {
			a = act[i]
		}
		na = append(na, a)
	}
	idx := func(t float64) int {
		best, bd := -1, 0.06
		for i, b := range nb {
			if d := math.Abs(b - t); d < bd {
				best, bd = i, d
			}
		}
		return best
	}
	var di []int
	for _, d := range downs {
		if i := idx(d); i >= 0 && (len(di) == 0 || i > di[len(di)-1]) {
			di = append(di, i)
		}
	}
	if len(di) < 4 {
		return nb, downs, na
	}
	// The usual bar length, in beats.
	counts := map[int]int{}
	for k := 1; k < len(di); k++ {
		counts[di[k]-di[k-1]]++
	}
	B, bc := 4, 0
	for n, c := range counts {
		if n >= 2 && n <= 7 && (c > bc || (c == bc && n < B)) {
			B, bc = n, c
		}
	}
	ph := func(k int) int { return di[k] % B }
	for k := range di {
		var prev, next = -1, -1
		if k > 0 {
			prev = ph(k - 1)
		}
		if k+1 < len(di) {
			next = ph(k + 1)
		}
		ref := -1
		switch {
		case k == 0 && len(di) > 2 && ph(1) == ph(2):
			ref = ph(1)
		case k == len(di)-1 && k >= 2 && ph(k-1) == ph(k-2):
			ref = ph(k - 1)
		case prev >= 0 && prev == next:
			ref = prev
		}
		if ref < 0 || ph(k) == ref {
			continue
		}
		// The nearest beat on the neighbours' phase.
		best := -1
		for d := 1; d < B; d++ {
			for _, c := range []int{di[k] - d, di[k] + d} {
				if c >= 0 && c < len(nb) && c%B == ref && best < 0 {
					best = c
				}
			}
		}
		if best >= 0 && (k == 0 || best > di[k-1]) && (k+1 >= len(di) || best < di[k+1]) {
			di[k] = best
		}
	}
	// A downbeat missed inside a run of regular bars.
	var full []int
	for k, i := range di {
		if k > 0 {
			gap := i - di[k-1]
			if gap%B == 0 && gap/B >= 2 && gap/B <= 4 && ph(k) == ph(k-1) {
				for j := di[k-1] + B; j < i; j += B {
					full = append(full, j)
				}
			}
		}
		full = append(full, i)
	}
	nd := make([]float64, len(full))
	for k, i := range full {
		nd[k] = nb[i]
	}
	return nb, nd, na
}

func equalF(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
