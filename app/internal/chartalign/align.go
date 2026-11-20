package chartalign

import (
	"math"
	"sort"
	"strings"
)

// Section is one section of the chart as it's played: its bars in
// order, repeats unrolled, a bare repeat of an earlier section given
// that section's bars.
type Section struct {
	Index     int    `json:"index"`
	Label     string `json:"label"`
	StartBar  int    `json:"startBar"` // the player's number for its first bar
	HasLyrics bool   `json:"hasLyrics"`
	Bars      []Bar  `json:"-"`
	// Intro: the chart calls it an intro (it's then aligned like any
	// other section; no intro is proposed).
	Intro bool `json:"-"`
}

// Bar is one played bar of the chart: a chord per beat ("" none, "N.C.").
type Bar struct {
	Beats []string
	// Where it is written: the section that defines it (the section
	// itself, or the one a bare repeat refers to), its measure there and,
	// per beat, the token's index in that measure (-1: none).
	DefSection, Measure int
	Slots               []int
}

// Hints are an editor's corrections of the beat grid, applied before
// aligning (the Sync sheet's one-tap fixes): what a beat tracker gets
// wrong is always the same few things.
type Hints struct {
	// TempoFactor 2: the tracker heard half the beats (every beat is
	// split in two); 0.5: twice too many. 0 or 1: as detected.
	TempoFactor float64 `json:"tempoFactor,omitempty"`
	// ShiftBeats moves every downbeat by this many beats.
	ShiftBeats int `json:"shiftBeats,omitempty"`
	// Bar1At: the chart's first bar starts at the beat nearest this
	// time (seconds), which becomes a downbeat; 0: free.
	Bar1At float64 `json:"bar1At,omitempty"`
}

// Result is the chart laid on the recording.
type Result struct {
	Tempo       float64 `json:"tempo"`
	TempoSteady bool    `json:"tempoSteady"`
	TempoMin    float64 `json:"tempoMin,omitempty"`
	TempoMax    float64 `json:"tempoMax,omitempty"`
	TimeSig     string  `json:"timeSig"`
	TimeSigConf float64 `json:"timeSigConf"`
	BeatsPerBar int     `json:"beatsPerBar"`
	// FirstDownbeat: bar 1 of the recording, the first downbeat with
	// music; IntroBars: the recording's bars before the chart's first
	// section starts (0 when it starts with the music).
	FirstDownbeat float64 `json:"firstDownbeat"`
	IntroBars     int     `json:"introBars"`
	// Intro: the chart has no intro section but the recording plays bars
	// before its first section: the [Intro] to add.
	Intro *IntroProposal `json:"intro,omitempty"`
	// Transpose: the chart matches the recording best this many
	// semitones up (a capo chart, a recording in another key); 0 mostly.
	Transpose int             `json:"transpose,omitempty"`
	Sections  []SectionResult `json:"sections"`
	// BarPoints: when each of the chart's bars starts (the player's bar
	// numbers), the per-bar sync.
	BarPoints  []BarPoint `json:"barPoints"`
	ChordFixes []ChordFix `json:"chordFixes"`
	// Confidence 0..1, and what to show: "good", "check" (by ear) or
	// "unsure" (no number is worth showing).
	Confidence float64 `json:"confidence"`
	State      string  `json:"state"`
	// The grid used, after hints (for drawing it and the click).
	Downbeats []float64 `json:"downbeats"`
	cost      float64
}

// IntroProposal is an [Intro] to add before the chart's first section.
type IntroProposal struct {
	Bars int    `json:"bars"`
	Grid string `json:"grid"`
}

// SectionResult is one section placed.
type SectionResult struct {
	Index int     `json:"index"`
	Label string  `json:"label"`
	Start float64 `json:"start"` // on a downbeat; -1 when not found
	// Written: the chart's bars (repeats unrolled); Played: the
	// recording's bars from this start to the next section's.
	Written int `json:"written"`
	Played  int `json:"played"`
	// Grid: the section's chords regrouped on the recording's bars,
	// when Played isn't Written (two chords sharing a bar: "Asus4 A").
	Grid string `json:"grid,omitempty"`
	// Moved: the voice put the start this many bars from where the
	// chords alone put it (a bar written in the previous section).
	Moved      int     `json:"moved,omitempty"`
	Confidence float64 `json:"confidence"`
	// Last: the last section, which runs into a fade or an ending: its
	// played count isn't a proposal.
	Last bool `json:"last,omitempty"`
}

// BarPoint is one bar's start in the recording.
type BarPoint struct {
	Bar  int     `json:"barIndex"`
	Time float64 `json:"timeSeconds"`
}

// ChordFix is a chord the recording contradicts, for an editor to
// check; never applied on its own.
type ChordFix struct {
	Section    int     `json:"section"` // the section heard in
	Bar        int     `json:"bar"`     // the player's bar number
	SecBar     int     `json:"secBar"`  // 1-based, within the section
	Time       float64 `json:"time"`    // where to listen
	Chart      string  `json:"chart"`   // the chart's chord
	Heard      string  `json:"heard"`   // the recording's, spelled for the chart
	Beats      int     `json:"beats"`   // over this many beats
	Confidence float64 `json:"confidence"`
	// Alternatives the recogniser also found likely there.
	Alternatives []string `json:"alternatives,omitempty"`
	// Where it's written (all the times this measure is played agree).
	DefSection int `json:"defSection"`
	Measure    int `json:"measure"`
	Slot       int `json:"slot"` // the token's index in the measure
}

const (
	skipAudio = 0.6 // per beat of the recording before the chart
	// skipEnd: per beat after it, cheaper: a chart often writes an outro
	// once that the recording repeats to a fade.
	skipEnd = 0.3
	skipBar = 4.0 // per bar of the chart the recording doesn't play
)

type grid struct {
	beats []float64
	pos   []int
	act   []float64
	B     int
}

// applyHints is the recording's beat grid with an editor's fixes.
func applyHints(r *Recording, h Hints) grid {
	g := grid{beats: append([]float64{}, r.Beats...), pos: append([]int{}, r.BeatPos...), act: r.BeatAct, B: r.BeatsPerBar}
	if g.B <= 0 {
		g.B = 4
	}
	if len(g.beats) < 2 {
		return g
	}
	rephase := func(first int) {
		for i := range g.pos {
			d := ((i-first)%g.B + g.B) % g.B
			g.pos[i] = d + 1
		}
	}
	firstDown := func() int {
		for i, p := range g.pos {
			if p == 1 {
				return i
			}
		}
		return 0
	}
	switch {
	case h.TempoFactor >= 1.5:
		fd := firstDown()
		var nb []float64
		for i, b := range g.beats {
			nb = append(nb, b)
			if i+1 < len(g.beats) {
				nb = append(nb, (b+g.beats[i+1])/2)
			}
		}
		g.beats, g.pos, g.act = nb, make([]int, len(nb)), nil
		rephase(2 * fd)
	case h.TempoFactor > 0 && h.TempoFactor <= 0.75:
		fd := firstDown()
		var nb []float64
		for i := fd % 2; i < len(g.beats); i += 2 {
			nb = append(nb, g.beats[i])
		}
		g.beats, g.pos, g.act = nb, make([]int, len(nb)), nil
		rephase(fd / 2)
	}
	if h.ShiftBeats != 0 {
		rephase(firstDown() + h.ShiftBeats)
	}
	if h.Bar1At > 0 {
		rephase(nearestBeat(g.beats, h.Bar1At))
	}
	return g
}

func nearestBeat(beats []float64, t float64) int {
	i := sort.SearchFloat64s(beats, t)
	if i >= len(beats) {
		return len(beats) - 1
	}
	if i > 0 && t-beats[i-1] < beats[i]-t {
		return i - 1
	}
	return i
}

// placement is one chart bar laid on the beats: from beat j for d beats
// (d 0: the recording doesn't play it).
type placement struct{ j, d int }

// dp aligns the bars to the beat intervals' observations; returns the
// placements, the total cost and the beat the path ends at.
func dp(bars [][][13]float64, obs [][13]float64, pos []int, B int, fixFirst int) ([]placement, float64, int) {
	N, M := len(bars), len(obs)
	W := M + 1
	inf := math.Inf(1)
	C := make([]float64, (N+1)*W)
	from := make([]int32, (N+1)*W)
	dur := make([]int8, (N+1)*W)
	for i := range C {
		C[i] = inf
	}
	for j := 0; j <= M; j++ {
		if fixFirst >= 0 && j != fixFirst {
			continue
		}
		C[j] = skipAudio * float64(j)
		if fixFirst >= 0 {
			C[j] = 0
		}
	}
	startCost := func(j int) float64 {
		switch {
		case pos[j] == 1:
			return 0
		case B%2 == 0 && pos[j] == B/2+1:
			return 0.3
		}
		return 1
	}
	for i := 0; i < N; i++ {
		tb := bars[i]
		nb := len(tb)
		durs := durations(nb)
		for j := 0; j <= M; j++ {
			c := C[i*W+j]
			if math.IsInf(c, 1) {
				continue
			}
			k := (i+1)*W + j
			if c+skipBar < C[k] {
				C[k], from[k], dur[k] = c+skipBar, int32(j), 0
			}
			if j >= M {
				continue
			}
			sc := startCost(j)
			for _, dd := range durs {
				if j+dd.d > M {
					continue
				}
				v := c + dd.pen + sc
				for q := 0; q < dd.d; q++ {
					v += 1 - dot(tb[(q*nb)/dd.d], obs[j+q])
				}
				k := (i+1)*W + j + dd.d
				if v < C[k] {
					C[k], from[k], dur[k] = v, int32(j), int8(dd.d)
				}
			}
		}
	}
	end, best := 0, inf
	for j := 0; j <= M; j++ {
		if v := C[N*W+j] + skipEnd*float64(M-j); v < best {
			end, best = j, v
		}
	}
	place := make([]placement, N)
	j := end
	for i := N; i > 0; i-- {
		k := i*W + j
		place[i-1] = placement{int(from[k]), int(dur[k])}
		j = int(from[k])
	}
	return place, best, end
}

type durPen struct {
	d   int
	pen float64
}

// durations a chart bar of n beats may last in the recording, and what
// each costs: its own length free, half of it cheap (two chords written
// as two bars, played in one), double, one beat more or less dearer.
func durations(n int) []durPen {
	if n <= 0 {
		n = 4
	}
	seen := map[int]bool{}
	var out []durPen
	add := func(d int, p float64) {
		if d >= 1 && !seen[d] {
			seen[d] = true
			out = append(out, durPen{d, p})
		}
	}
	add(n, 0)
	if n%2 == 0 {
		add(n/2, 0.4)
	}
	add(2*n, 0.8)
	add(n-1, 1.2)
	if n%2 == 0 {
		add(n+n/2, 1.2)
	}
	add(1, 2.0)
	return out
}

// Align lays the chart on the recording. beatsPerBar is the chart's.
func Align(r *Recording, secs []Section, beatsPerBar int, h Hints) *Result {
	g := applyHints(r, h)
	// A recording heard in shorter bars than the chart's (2 beats a bar
	// against 4/4: a downbeat on every half bar): its bars are grouped in
	// the chart's, on the phase the chords fit best.
	if beatsPerBar > g.B && beatsPerBar%g.B == 0 && h.ShiftBeats == 0 && h.Bar1At == 0 {
		var best *Result
		for p := 0; p < beatsPerBar/g.B; p++ {
			res := alignOn(r, groupBars(g, beatsPerBar, p), secs, h)
			if best == nil || res.cost < best.cost {
				best = res
			}
		}
		if best != nil {
			best.TimeSig = timeSigFor(beatsPerBar, "")
			return best
		}
	}
	return alignOn(r, g, secs, h)
}

// groupBars makes bars of B beats from a grid's shorter ones, starting
// at its (phase)-th downbeat.
func groupBars(g grid, B, phase int) grid {
	out := grid{beats: g.beats, act: g.act, B: B, pos: make([]int, len(g.pos))}
	first, n := 0, 0
	for i, p := range g.pos {
		if p == 1 {
			if n == phase {
				first = i
				break
			}
			n++
		}
	}
	for i := range out.pos {
		out.pos[i] = ((i-first)%B+B)%B + 1
	}
	return out
}

func alignOn(r *Recording, g grid, secs []Section, h Hints) *Result {
	res := &Result{TimeSig: r.TimeSig, TimeSigConf: r.TimeSigConf, BeatsPerBar: g.B, cost: math.Inf(1), TempoMin: r.TempoMin, TempoMax: r.TempoMax, BarPoints: []BarPoint{}, ChordFixes: []ChordFix{}}
	res.Tempo = r.Tempo
	if h.TempoFactor >= 1.5 {
		res.Tempo, res.TempoMin, res.TempoMax = r.Tempo*2, r.TempoMin*2, r.TempoMax*2
	} else if h.TempoFactor > 0 && h.TempoFactor <= 0.75 {
		res.Tempo, res.TempoMin, res.TempoMax = r.Tempo/2, r.TempoMin/2, r.TempoMax/2
	}
	res.Tempo = round2(res.Tempo)
	res.TempoSteady = r.TempoCV > 0 && r.TempoCV < 0.04
	for i, p := range g.pos {
		if p == 1 {
			res.Downbeats = append(res.Downbeats, g.beats[i])
		}
	}
	M := len(g.beats) - 1
	if M < 4 {
		res.State = "unsure"
		return res
	}
	// What's heard on each beat: the recogniser's chord at its middle.
	obs := make([][13]float64, M)
	heard := make([]Chord, M)
	for j := 0; j < M; j++ {
		c, ok := ParseHarte(r.chordAt((g.beats[j] + g.beats[j+1]) / 2))
		if !ok {
			c = NoChord
		}
		heard[j] = c
		obs[j] = c.vector()
	}
	// The chart's bars, flattened, as vectors per beat.
	var all []flatBar
	for si, s := range secs {
		for k, b := range s.Bars {
			all = append(all, flatBar{si, k, b})
		}
	}
	if len(all) == 0 {
		res.State = "unsure"
		return res
	}
	vectors := func(shift int) [][][13]float64 {
		out := make([][][13]float64, len(all))
		for i, f := range all {
			vs := make([][13]float64, len(f.bar.Beats))
			prev := NoChord
			for q, sym := range f.bar.Beats {
				if sym == "" {
					// Unknown ("?", a bar with nothing written): fits
					// nothing and contradicts nothing.
					vs[q] = [13]float64{}
					continue
				}
				c, ok := ParseSymbol(sym)
				if !ok {
					c = prev
				}
				prev = c
				vs[q] = transpose(c, shift).vector()
			}
			if len(vs) == 0 {
				vs = [][13]float64{NoChord.vector()}
			}
			out[i] = vs
		}
		return out
	}
	fixFirst := -1
	if h.Bar1At > 0 {
		fixFirst = nearestBeat(g.beats[:M], h.Bar1At)
	}
	place, cost, end := dp(vectors(0), obs, g.pos, g.B, fixFirst)
	defer func() { res.cost = cost }()
	// A chart in another key than the recording (written for a capo, a
	// recording a semitone off): the chords only fit transposed.
	for s := 1; s < 12; s++ {
		p2, c2, e2 := dp(vectors(s), obs, g.pos, g.B, fixFirst)
		if c2 < 0.75*cost {
			place, cost, end, res.Transpose = p2, c2, e2, s
		}
	}
	// Each beat's chart chord, from the placements.
	chartAt := make([]string, M)
	barAt := make([]int, M) // the flat bar sounding on beat j, -1 none
	for j := range barAt {
		barAt[j] = -1
	}
	for i, p := range place {
		nb := len(all[i].bar.Beats)
		for q := 0; q < p.d; q++ {
			chartAt[p.j+q] = all[i].bar.Beats[(q*nb)/p.d]
			barAt[p.j+q] = i
		}
	}
	// Ties ("" after a chord) take the chord before.
	for j := 1; j < M; j++ {
		if chartAt[j] == "" && barAt[j] >= 0 && barAt[j] == barAt[j-1] {
			chartAt[j] = chartAt[j-1]
		}
	}
	// Per-bar sync points.
	for i, p := range place {
		if p.d > 0 {
			f := all[i]
			res.BarPoints = append(res.BarPoints, BarPoint{Bar: secs[f.sec].StartBar + f.k, Time: round3(g.beats[p.j])})
		}
	}
	// Sections: where the chords put them, then on a downbeat the voice
	// agrees with.
	sp := make([]secPlace, len(secs))
	for si := range secs {
		sp[si] = secPlace{-1, -1}
	}
	for i, p := range place {
		f := all[i]
		if p.d > 0 && sp[f.sec].raw < 0 {
			sp[f.sec].raw = p.j
		}
	}
	beatLen := 60 / math.Max(1, res.Tempo)
	prevStart := -1
	// Where the chords put each section's end: the next one's start.
	rawEnd := func(si int) float64 {
		for k := si + 1; k < len(secs); k++ {
			if sp[k].raw >= 0 {
				return g.beats[sp[k].raw]
			}
		}
		return g.beats[min(end, len(g.beats)-1)]
	}
	for si, s := range secs {
		if sp[si].raw < 0 {
			continue
		}
		// Sung: it has lyrics, or the voice sings most of it (a chart
		// whose words aren't written in yet).
		sung := s.HasLyrics || voiceShare(r.VoicePhrases, g.beats[sp[si].raw], rawEnd(si)) > 0.5
		st := chooseDownbeat(g, sp[si].raw, sung, r.VoiceOnsets, beatLen, prevStart)
		sp[si].start = st
		prevStart = st
	}
	// Played bars: downbeats from this start to the next found start.
	downIdx := func(a, b int) int {
		n := 0
		for j := a; j < b && j < len(g.pos); j++ {
			if g.pos[j] == 1 {
				n++
			}
		}
		return n
	}
	totalCost, totalBeats := 0.0, 0
	for si, s := range secs {
		out := SectionResult{Index: s.Index, Label: s.Label, Start: -1, Written: len(s.Bars)}
		if sp[si].start < 0 {
			res.Sections = append(res.Sections, out)
			continue
		}
		st := sp[si].start
		next := end
		last := true
		for k := si + 1; k < len(secs); k++ {
			if sp[k].start >= 0 {
				next, last = sp[k].start, false
				break
			}
		}
		out.Start = round3(g.beats[st])
		out.Played = downIdx(st, next)
		if !g.isDown(st) {
			out.Played++
		}
		out.Last = last
		// The voice's move, in bars.
		if sp[si].start != sp[si].raw {
			d := downIdx(min(st, sp[si].raw), max(st, sp[si].raw))
			if st < sp[si].raw {
				d = -d
			}
			out.Moved = d
		}
		// Confidence: how well the chart's chords match over its beats.
		c, n := 0.0, 0
		for j := st; j < next && j < M; j++ {
			if chartAt[j] == "" {
				continue
			}
			cc, ok := ParseSymbol(chartAt[j])
			if !ok {
				continue
			}
			c += 1 - dot(transpose(cc, res.Transpose).vector(), obs[j])
			n++
		}
		if n > 0 {
			out.Confidence = round2(clamp01(1 - (c/float64(n))/0.6))
			totalCost += c
			totalBeats += n
		}
		if out.Played != out.Written && !last {
			out.Grid = regroup(chartAt[st:next], g.pos[st:next], g.B)
		}
		res.Sections = append(res.Sections, out)
	}
	// The intro: bars before the first section, from the first downbeat
	// with music.
	first := -1
	for si := range secs {
		if sp[si].start >= 0 {
			first = sp[si].start
			break
		}
	}
	fd := -1
	for j := 0; j < M; j++ {
		if g.pos[j] == 1 && g.beats[j] >= r.AudioStart-0.15 && (g.act == nil || j >= len(g.act) || g.act[j] >= 0.3) {
			fd = j
			break
		}
	}
	if fd >= 0 {
		res.FirstDownbeat = round3(g.beats[fd])
		if first > fd {
			res.IntroBars = downIdx(fd, first)
			hasIntro := len(secs) > 0 && secs[0].Intro
			if !hasIntro && res.IntroBars > 0 {
				res.Intro = &IntroProposal{Bars: res.IntroBars, Grid: introGrid(r, g, fd, first, res.Transpose)}
			}
		}
	}
	// Chord check.
	if res.Transpose == 0 {
		res.ChordFixes = chordFixes(r, g, secs, all, place, heard, sp, M)
	}
	// Overall confidence: the chords' fit, the beat tracker's, the metre's.
	fit := 0.0
	if totalBeats > 0 {
		fit = clamp01(1 - (totalCost/float64(totalBeats))/0.6)
	}
	beatConf := r.BeatConf
	if beatConf == 0 {
		beatConf = 0.7
	}
	res.Confidence = round2(fit * clamp01(beatConf/0.9) * clamp01(0.4+0.6*r.TimeSigConf))
	switch {
	case res.Confidence >= 0.7:
		res.State = "good"
	case res.Confidence >= 0.4:
		res.State = "check"
	default:
		res.State = "unsure"
	}
	return res
}

func (g grid) isDown(j int) bool { return j >= 0 && j < len(g.pos) && g.pos[j] == 1 }

// chooseDownbeat puts a section that the chords start at beat raw on a
// downbeat: the one at or before raw, or a neighbour, as the voice says
// for a sung section (it comes in within two beats of its downbeat,
// before for a pickup or after); else the nearest.
func chooseDownbeat(g grid, raw int, sung bool, onsets []float64, beatLen float64, after int) int {
	prevDown := func(j int) int {
		for ; j >= 0; j-- {
			if g.pos[j] == 1 {
				return j
			}
		}
		return -1
	}
	nextDown := func(j int) int {
		for ; j < len(g.pos); j++ {
			if g.pos[j] == 1 {
				return j
			}
		}
		return -1
	}
	d0 := prevDown(raw)
	dn := nextDown(raw + 1)
	if g.isDown(raw) {
		dn = raw
	}
	var cands []int
	if g.isDown(raw) {
		cands = []int{raw}
		if p := prevDown(raw - 1); p >= 0 {
			cands = append(cands, p)
		}
		if n := nextDown(raw + 1); n >= 0 {
			cands = append(cands, n)
		}
	} else {
		// Nearest first.
		if d0 >= 0 && dn >= 0 && raw-d0 > dn-raw {
			cands = []int{dn, d0}
		} else {
			cands = []int{d0, dn}
		}
	}
	valid := cands[:0]
	for _, c := range cands {
		if c > after && c >= 0 {
			valid = append(valid, c)
		}
	}
	if len(valid) == 0 {
		if raw > after {
			return raw
		}
		return after + 1
	}
	if !sung || len(onsets) == 0 {
		return valid[0]
	}
	voiced := func(c int) bool {
		t := g.beats[c]
		for _, o := range onsets {
			if o >= t-2*beatLen-0.1 && o <= t+2*beatLen {
				return true
			}
		}
		return false
	}
	for _, c := range valid {
		if voiced(c) {
			return c
		}
	}
	return valid[0]
}

// regroup writes a section's beats as a grid on the recording's bars.
func regroup(chords []string, pos []int, B int) string {
	var bars [][]string
	var cur []string
	for j, c := range chords {
		if pos[j] == 1 && len(cur) > 0 {
			bars = append(bars, cur)
			cur = nil
		}
		if c == "" {
			if len(cur) > 0 {
				c = cur[len(cur)-1]
			} else {
				c = "N.C."
			}
		}
		cur = append(cur, c)
	}
	if len(cur) > 0 {
		bars = append(bars, cur)
	}
	texts := make([]string, len(bars))
	for i, b := range bars {
		texts[i] = formatBar(b, B)
	}
	return Fold(texts)
}

// formatBar writes one bar's chords per beat the way charts do: "D",
// "Asus4 A" (half and half), "Bm . . A/C#".
func formatBar(b []string, B int) string {
	same := true
	for _, c := range b {
		if c != b[0] {
			same = false
		}
	}
	if same {
		return b[0]
	}
	if len(b) == 4 && b[0] == b[1] && b[2] == b[3] {
		return b[0] + " " + b[2]
	}
	out := []string{b[0]}
	for k := 1; k < len(b); k++ {
		if b[k] == b[k-1] {
			out = append(out, ".")
		} else {
			out = append(out, b[k])
		}
	}
	return strings.Join(out, " ")
}

// Fold writes bars as a grid line, folding repeats: a run of bars
// played again right after is "|: … :|" ("|:3 … :|" for three times), a
// bar played again is "%".
func Fold(bars []string) string {
	var parts []string
	prev := ""
	for i := 0; i < len(bars); {
		bestP, bestR := 0, 0
		for p := 2; i+2*p <= len(bars); p++ {
			if uniform(bars[i : i+p]) {
				continue // "Gm | % | % | %", not "|: Gm | Gm :|"
			}
			r := 1
			for i+(r+1)*p <= len(bars) && equal(bars[i:i+p], bars[i+r*p:i+(r+1)*p]) {
				r++
			}
			if r >= 2 && p*r > bestP*bestR {
				bestP, bestR = p, r
			}
		}
		if bestR >= 2 {
			for k := 0; k < bestP; k++ {
				t := bars[i+k]
				if k > 0 && t == bars[i+k-1] && !strings.Contains(t, " ") {
					t = "%"
				}
				if k == 0 {
					if bestR == 2 {
						t = ": " + t
					} else {
						t = ":" + itoa(bestR) + " " + t
					}
				}
				if k == bestP-1 {
					t += " :"
				}
				parts = append(parts, t)
			}
			i += bestP * bestR
			prev = ""
			continue
		}
		t := bars[i]
		if t == prev && !strings.Contains(t, " ") {
			t = "%"
		}
		parts = append(parts, t)
		prev = bars[i]
		i++
	}
	var b strings.Builder
	for i, p := range parts {
		opens := strings.HasPrefix(p, ":")
		if i == 0 {
			if opens {
				b.WriteString("|")
			}
		} else {
			closed := strings.HasSuffix(parts[i-1], " :")
			switch {
			case closed && opens:
				b.WriteString("|")
			case closed:
				b.WriteString("| ")
			case opens:
				b.WriteString(" |")
			default:
				b.WriteString(" | ")
			}
		}
		b.WriteString(p)
	}
	if len(parts) > 0 && strings.HasSuffix(parts[len(parts)-1], " :") {
		b.WriteString("|")
	}
	return b.String()
}

func equal(a, b []string) bool {
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

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for ; n > 0; n /= 10 {
		d = append([]byte{byte('0' + n%10)}, d...)
	}
	return string(d)
}

// introGrid: the recording's bars before the first section, one chord
// each (the one heard most), N.C. where none is.
func introGrid(r *Recording, g grid, from, to, shift int) string {
	var bars []string
	var cur map[string]int
	var order []string
	flush := func() {
		if cur == nil {
			return
		}
		best, bc := "N", -1
		for _, l := range order {
			if cur[l] > bc {
				best, bc = l, cur[l]
			}
		}
		c, ok := ParseHarte(best)
		if !ok || c.NC {
			bars = append(bars, "N.C.")
		} else {
			bars = append(bars, Spell(best, -shift, false))
		}
	}
	for j := from; j < to && j+1 < len(g.beats); j++ {
		if g.pos[j] == 1 {
			flush()
			cur, order = map[string]int{}, nil
		}
		l := r.chordAt((g.beats[j] + g.beats[j+1]) / 2)
		if cur[l] == 0 {
			order = append(order, l)
		}
		cur[l]++
	}
	flush()
	return Fold(bars)
}

func transpose(c Chord, s int) Chord {
	if c.NC || s == 0 {
		return c
	}
	out := c
	out.Root = (c.Root + s) % 12
	out.Bass = (c.Bass + s) % 12
	out.PCs = 0
	for i := 0; i < 12; i++ {
		if c.PCs&(1<<uint(i)) != 0 {
			out.PCs |= 1 << uint((i+s)%12)
		}
	}
	return out
}

func clamp01(x float64) float64 { return math.Max(0, math.Min(1, x)) }

type flatBar struct {
	sec, k int
	bar    Bar
}

type secPlace struct{ raw, start int }

func uniform(bars []string) bool {
	for _, b := range bars {
		if b != bars[0] {
			return false
		}
	}
	return true
}

// voiceShare is how much of [a, b) the voice sings.
func voiceShare(phrases [][2]float64, a, b float64) float64 {
	if b <= a {
		return 0
	}
	s := 0.0
	for _, p := range phrases {
		s += math.Max(0, math.Min(b, p[1])-math.Max(a, p[0]))
	}
	return s / (b - a)
}
