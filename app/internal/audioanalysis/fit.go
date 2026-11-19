package audioanalysis

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// Section is one chart section in chart order: its first bar (0-based,
// counted over the whole chart), its length in bars, and whether it has
// lyrics (so the singer is expected to be singing during it).
type Section struct {
	Index     int
	StartBar  int
	BarCount  int
	HasLyrics bool
}

// Marker places a section on the recording: the time, in seconds, at
// which section SectionIndex starts.
type Marker struct {
	SectionIndex int
	Time         float64
}

// Fit is FitSections' result.
type Fit struct {
	SecondsPerBar float64
	Markers       []Marker // one per section, in order
	Score         float64  // 0..1, how well lyric sections line up with vocal activity
	// Clamped: the sung sections' starts asked for a bar length outside
	// the searched range, and SecondsPerBar is that range's edge. The
	// chart's bar counts then don't match the recording at any tempo
	// near the hint: a bar count (or the tempo) in the chart is wrong.
	Clamped bool
}

const (
	// fitHintRange: with a bar-length hint, search ±15% around it.
	fitHintRange = 0.15
	// Without a hint, search every plausible bar length: 1 s is a fast
	// 2-beat bar, 6 s a slow 4/4 ballad bar.
	fitMinSPB = 1.0
	fitMaxSPB = 6.0
	// fitOnsetSigma: a lyric section start within about this many
	// seconds of a vocal onset earns most of the onset bonus.
	fitOnsetSigma = 0.5
	// fitOnsetWeight weighs the onset bonus against the coverage F-score
	// (both 0..1) in the grid search. It is deliberately small: phrases
	// in a song tend to come at regular intervals, so many wrong bar
	// lengths put several section starts on *some* onset. Coverage
	// decides the fit; the onset bonus only breaks near-ties in phase.
	fitOnsetWeight = 0.15
	// fitSnapWindow: a lyric section's start snaps to a vocal onset no
	// further than this fraction of the section's predicted length.
	fitSnapWindow = 0.3
)

// FitSections places the chart's sections on the recording, using the
// vocal activity found in the vocals stem: sections with lyrics should
// be where the singing is, sections without lyrics where it isn't, and
// a lyric section usually starts with a sung phrase.
//
// It works in two passes.
//
//  1. A global grid search over seconds-per-bar (the hint ±15% when
//     secondsPerBarHint > 0, else 1–6 s) and over the time t0 of the
//     chart's first bar, in Hop steps from AudioStart to half a bar
//     after the first vocal onset: whatever the chart opens with, it
//     opens before the singing does (the half bar of slack lets a
//     lyric-first chart's downbeat follow a sung pickup). For each
//     (spb, t0) every section gets the span [t0 + StartBar·spb,
//     t0 + (StartBar+BarCount)·spb), and the fit is scored as the
//     F-score of "singing happens inside lyric spans" — recall: the
//     share of all sung frames that fall inside lyric spans; precision:
//     the share of lyric-span time that is sung (time past the end of
//     the recording counts as unsung) — plus a small bonus for lyric
//     section starts lying near a vocal onset. Sung frames in
//     instrumental sections, before the chart or after it lower recall;
//     lyric spans stretched over silence lower precision.
//
//  2. Per-section refinement. Real charts are approximate — a chorus
//     notated as 10 bars may be sung as 8 — so one global grid drifts.
//     Walking the sections in order, each lyric section's start is
//     predicted from the last placed anchor (initially t0) plus its bar
//     distance × spb, and snapped to the nearest vocal onset within ±30%
//     of the section's own predicted length (and comfortably after the
//     previous anchor); a snapped section becomes the new anchor, so
//     errors don't accumulate. The bar length is then re-estimated as
//     the bar-weighted median of the anchor-to-anchor rates (a median,
//     so one miscounted section can't skew it). Sections that didn't
//     snap (instrumental ones, or lyric ones with no onset nearby) are
//     placed from their neighbours: between two anchors, by linear
//     interpolation on bar position; before the first anchor, counting
//     back from it; after the last, counting forward.
//
// The first section never starts before AudioStart, and markers are
// forced strictly increasing. With no vocal activity (no vocals stem, an
// instrumental) or no lyric section, placement is tempo-only: AudioStart
// plus StartBar × spb, where spb is the hint or else the Analysis'
// tempo and metre.
//
// Score is the mean of the final placement's coverage F-score and the
// fraction of lyric sections that snapped to a vocal onset.
func FitSections(a *Analysis, sections []Section, secondsPerBarHint float64) (*Fit, error) {
	if a == nil {
		return nil, errors.New("audioanalysis: no analysis")
	}
	if len(sections) == 0 {
		return nil, errors.New("audioanalysis: no sections")
	}
	for i, s := range sections {
		if s.BarCount < 0 {
			return nil, fmt.Errorf("audioanalysis: section %d has a negative bar count", s.Index)
		}
		if i > 0 && s.StartBar < sections[i-1].StartBar {
			return nil, fmt.Errorf("audioanalysis: section %d starts before the previous one", s.Index)
		}
	}
	hop := a.Hop
	if hop <= 0 {
		hop = Hop
	}
	f := &fitter{
		a:        a,
		sections: sections,
		hop:      hop,
		bar0:     sections[0].StartBar,
	}
	f.prepare()

	if f.totalActive == 0 || !f.anyLyrics {
		spb := secondsPerBarHint
		if spb <= 0 {
			spb = a.SecondsPerBar()
		}
		if spb <= 0 {
			return nil, errors.New("audioanalysis: no vocal activity and no tempo to place sections by")
		}
		fit := &Fit{SecondsPerBar: spb}
		for _, s := range sections {
			fit.Markers = append(fit.Markers, Marker{s.Index, a.AudioStart + float64(s.StartBar-f.bar0)*spb})
		}
		enforceIncreasing(fit.Markers, hop)
		return fit, nil
	}

	lo, hi := fitMinSPB, fitMaxSPB
	if secondsPerBarHint > 0 {
		lo, hi = secondsPerBarHint*(1-fitHintRange), secondsPerBarHint*(1+fitHintRange)
	}
	spb, t0 := f.gridSearch(lo, hi)
	return f.refine(spb, t0, lo, hi), nil
}

type fitter struct {
	a        *Analysis
	sections []Section
	hop      float64
	bar0     int

	cum         []float64 // cum[i] = sung frames among frames [0, i)
	totalActive float64
	anyLyrics   bool
	onsets      []float64 // a.VocalOnsets, or derived from the activity
}

func (f *fitter) prepare() {
	act := f.a.VocalActivity
	f.cum = make([]float64, len(act)+1)
	for i, v := range act {
		f.cum[i+1] = f.cum[i]
		if v {
			f.cum[i+1]++
		}
	}
	f.totalActive = f.cum[len(act)]
	f.onsets = f.a.VocalOnsets
	if len(f.onsets) == 0 {
		f.onsets = vocalOnsets(act, f.hop)
	}
	for _, s := range f.sections {
		f.anyLyrics = f.anyLyrics || s.HasLyrics
	}
}

// sungUpTo returns the number of sung frames before time t, counting a
// partially covered frame proportionally so the score varies smoothly
// with t (the grid search then has a well-defined optimum).
func (f *fitter) sungUpTo(t float64) float64 {
	x := t / f.hop
	n := len(f.cum) - 1
	if x <= 0 {
		return 0
	}
	if x >= float64(n) {
		return f.cum[n]
	}
	i := int(x)
	return f.cum[i] + (x-float64(i))*(f.cum[i+1]-f.cum[i])
}

// sung returns the sung time (in frames) inside [t1, t2).
func (f *fitter) sung(t1, t2 float64) float64 {
	if t2 <= t1 {
		return 0
	}
	return f.sungUpTo(t2) - f.sungUpTo(t1)
}

// nearestOnset returns the distance from t to the closest vocal onset.
func (f *fitter) nearestOnset(t float64) float64 {
	on := f.onsets
	if len(on) == 0 {
		return math.Inf(1)
	}
	i := sort.SearchFloat64s(on, t)
	d := math.Inf(1)
	if i < len(on) {
		d = on[i] - t
	}
	if i > 0 {
		d = min(d, t-on[i-1])
	}
	return d
}

// coverage is the F-score of sung frames against the given lyric spans.
func (f *fitter) coverage(spans [][2]float64) float64 {
	in, length := 0.0, 0.0
	for _, s := range spans {
		in += f.sung(s[0], s[1])
		length += (s[1] - s[0]) / f.hop
	}
	if in == 0 || length == 0 {
		return 0
	}
	recall := in / f.totalActive
	precision := in / length
	return 2 * recall * precision / (recall + precision)
}

// gridScore evaluates one (spb, t0) hypothesis for the grid search.
func (f *fitter) gridScore(spb, t0 float64, spans [][2]float64) float64 {
	spans = spans[:0]
	onset, lyrics := 0.0, 0
	for _, s := range f.sections {
		if !s.HasLyrics {
			continue
		}
		start := t0 + float64(s.StartBar-f.bar0)*spb
		spans = append(spans, [2]float64{start, start + float64(s.BarCount)*spb})
		d := f.nearestOnset(start) / fitOnsetSigma
		onset += math.Exp(-0.5 * d * d)
		lyrics++
	}
	return f.coverage(spans) + fitOnsetWeight*onset/float64(lyrics)
}

func (f *fitter) totalBars() int {
	last := f.sections[len(f.sections)-1]
	return max(1, last.StartBar+last.BarCount-f.bar0)
}

// gridSearch returns the best (spb, t0). The spb step is chosen so that
// stepping it moves the chart's last bar by at most half a Hop.
func (f *fitter) gridSearch(lo, hi float64) (spb, t0 float64) {
	step := max(0.0005, 0.5*f.hop/float64(f.totalBars()))
	start := f.a.AudioStart
	firstSung := start
	if len(f.onsets) > 0 {
		firstSung = max(start, f.onsets[0])
	}
	best := math.Inf(-1)
	spans := make([][2]float64, 0, len(f.sections))
	for s := lo; s <= hi+1e-9; s += step {
		end := firstSung + 0.5*s
		if f.a.Duration > start {
			end = min(end, f.a.Duration)
		}
		for t := start; t <= end+1e-9; t += f.hop {
			if sc := f.gridScore(s, t, spans); sc > best {
				best, spb, t0 = sc, s, t
			}
		}
	}
	return spb, t0
}

type anchor struct {
	bar  int
	time float64
}

func (f *fitter) refine(spb, t0, lo, hi float64) *Fit {
	n := len(f.sections)
	rel := func(i int) int { return f.sections[i].StartBar - f.bar0 }

	// Snap lyric sections to vocal onsets, walking forward from t0.
	snapped := make([]bool, n)
	times := make([]float64, n)
	last := anchor{0, t0}
	var anchors []anchor
	lyrics, snaps := 0, 0
	for i, s := range f.sections {
		if !s.HasLyrics {
			continue
		}
		lyrics++
		pred := last.time + float64(rel(i)-last.bar)*spb
		window := fitSnapWindow * max(1, float64(s.BarCount)) * spb
		// Stay clear of the previous anchor: at least half the bars
		// between them must fit, so two sections can't land on the same
		// phrase.
		notBefore := math.Inf(-1)
		if len(anchors) > 0 {
			notBefore = last.time + 0.5*float64(rel(i)-last.bar)*spb
		}
		bestD := math.Inf(1)
		for _, o := range f.onsets {
			if d := math.Abs(o - pred); d <= window && d < bestD && o > notBefore {
				bestD, times[i] = d, o
			}
		}
		if !math.IsInf(bestD, 1) {
			snapped[i] = true
			snaps++
			last = anchor{rel(i), times[i]}
			anchors = append(anchors, last)
		}
	}

	// Re-estimate spb from consecutive anchors: bar-weighted median of
	// their seconds-per-bar, kept inside the searched range.
	clamped := false
	if len(anchors) >= 2 {
		type rate struct{ spb, bars float64 }
		var rates []rate
		total := 0.0
		for j := 1; j < len(anchors); j++ {
			bars := float64(anchors[j].bar - anchors[j-1].bar)
			if bars <= 0 {
				continue
			}
			rates = append(rates, rate{(anchors[j].time - anchors[j-1].time) / bars, bars})
			total += bars
		}
		sort.Slice(rates, func(i, j int) bool { return rates[i].spb < rates[j].spb })
		acc := 0.0
		for _, r := range rates {
			acc += r.bars
			if acc >= total/2 {
				spb = min(hi, max(lo, r.spb))
				// More than a percent outside: the anchors wanted another
				// bar length than any the search allowed.
				clamped = r.spb < lo*0.99 || r.spb > hi*1.01
				break
			}
		}
	}

	// Place everything that didn't snap from its neighbouring anchors.
	for i := range f.sections {
		if snapped[i] {
			continue
		}
		var prev, next *anchor
		for j := len(anchors) - 1; j >= 0; j-- {
			if anchors[j].bar <= rel(i) {
				prev = &anchors[j]
				break
			}
		}
		for j := range anchors {
			if anchors[j].bar > rel(i) {
				next = &anchors[j]
				break
			}
		}
		b := float64(rel(i))
		switch {
		case prev != nil && next != nil:
			frac := (b - float64(prev.bar)) / float64(next.bar-prev.bar)
			times[i] = prev.time + frac*(next.time-prev.time)
		case next != nil:
			times[i] = next.time - (float64(next.bar)-b)*spb
		case prev != nil:
			times[i] = prev.time + (b-float64(prev.bar))*spb
		default:
			times[i] = t0 + b*spb
		}
	}

	fit := &Fit{SecondsPerBar: spb, Clamped: clamped}
	for i, s := range f.sections {
		fit.Markers = append(fit.Markers, Marker{s.Index, times[i]})
	}
	fit.Markers[0].Time = max(fit.Markers[0].Time, f.a.AudioStart)
	// An instrumental opening starts with the sound: placed back from
	// the first sung section by bar counts, it lands late whenever the
	// chart's intro is shorter than the record's (a chart writing a long
	// guitar intro as one pass of its progression).
	if !f.sections[0].HasLyrics && len(f.sections) > 1 && fit.Markers[0].Time > f.a.AudioStart+spb/2 && fit.Markers[1].Time > f.a.AudioStart+spb {
		fit.Markers[0].Time = f.a.AudioStart
	}
	enforceIncreasing(fit.Markers, f.hop)

	// Score the final placement: each section spans to the next marker,
	// the last one for its bar count.
	var spans [][2]float64
	for i, s := range f.sections {
		if !s.HasLyrics {
			continue
		}
		end := fit.Markers[i].Time + float64(s.BarCount)*spb
		if i+1 < n {
			end = fit.Markers[i+1].Time
		}
		spans = append(spans, [2]float64{fit.Markers[i].Time, end})
	}
	fit.Score = clamp01(0.5*f.coverage(spans) + 0.5*float64(snaps)/float64(lyrics))
	return fit
}

// enforceIncreasing nudges any marker that isn't after its predecessor
// to just after it, so the player always sees a strictly increasing
// sequence.
func enforceIncreasing(m []Marker, gap float64) {
	for i := 1; i < len(m); i++ {
		if m[i].Time <= m[i-1].Time {
			m[i].Time = m[i-1].Time + gap
		}
	}
}
