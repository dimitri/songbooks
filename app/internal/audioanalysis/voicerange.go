package audioanalysis

import (
	"context"
	"fmt"
	"math"
	"slices"
)

// A song's melody range, from its recording's vocals part: the voice
// tracked note by note (YIN, as the song page's "Check my singing" does
// in the browser), its lowest and highest notes held, the odd octave
// slip and breath left out. What a coach's key suggestion puts inside a
// student's comfortable zone (httpapi/coaching_keys.go).

// MelodyRange is the sung part of a recording, MIDI notes.
type MelodyRange struct {
	Low, High int
	// Voiced: how many seconds of steady singing it was found from; a
	// range found from little is no range (ok false).
	Voiced float64
}

// MelodyRangeAnalyzer names this method, kept with each result.
const MelodyRangeAnalyzer = "yin:1"

const (
	yinWindow    = 512 // samples at SampleRate: 46 ms
	yinHop       = 256
	yinThreshold = 0.15
	yinMinHz     = 70.0
	yinMaxHz     = 1100.0
	// A note counts when held: so many frames in a row within a
	// semitone of each other (about 0.14 s).
	steadyFrames = 6
	// Less steady singing than this, no range.
	minVoicedSeconds = 8.0
)

// AnalyzeMelodyRange decodes a vocals part and finds its range.
func AnalyzeMelodyRange(ctx context.Context, vocals []byte) (MelodyRange, bool, error) {
	x, err := Decode(ctx, vocals)
	if err != nil {
		return MelodyRange{}, false, fmt.Errorf("vocals part: %w", err)
	}
	r, ok := MelodyRangeOf(x)
	return r, ok, nil
}

// MelodyRangeOf is the range of samples at SampleRate: the 5th and 95th
// percentiles of the held notes' pitches (an octave error, a shout, a
// backing voice's stray note don't move it).
func MelodyRangeOf(x []float32) (MelodyRange, bool) {
	pitches := yinTrack(x)
	var held []float64
	run := 0
	for i := range pitches {
		if pitches[i] > 0 && i > 0 && pitches[i-1] > 0 && math.Abs(pitches[i]-pitches[i-1]) <= 1 {
			run++
		} else if pitches[i] > 0 {
			run = 1
		} else {
			run = 0
		}
		if run == steadyFrames {
			held = append(held, pitches[i-steadyFrames+1:i+1]...)
		} else if run > steadyFrames {
			held = append(held, pitches[i])
		}
	}
	r := MelodyRange{Voiced: float64(len(held)*yinHop) / SampleRate}
	if r.Voiced < minVoicedSeconds {
		return r, false
	}
	slices.Sort(held)
	pct := func(p float64) float64 { return held[int(p*float64(len(held)-1))] }
	r.Low, r.High = int(math.Round(pct(0.05))), int(math.Round(pct(0.95)))
	return r, true
}

// yinTrack is a pitch per hop, as a fractional MIDI note, 0 where
// there's no clear pitch (silence, breath, a consonant).
func yinTrack(x []float32) []float64 {
	minTau := int(math.Floor(SampleRate / yinMaxHz))
	maxTau := int(math.Ceil(SampleRate / yinMinHz))
	if len(x) < yinWindow+maxTau {
		return nil
	}
	n := (len(x) - yinWindow - maxTau) / yinHop
	out := make([]float64, n)
	d := make([]float64, maxTau+1)
	// A quiet frame has no pitch: below 1/30 of the loudest frames.
	var peak float64
	energy := make([]float64, n)
	for f := range n {
		var e float64
		for _, v := range x[f*yinHop : f*yinHop+yinWindow] {
			e += float64(v) * float64(v)
		}
		energy[f] = e
		peak = max(peak, e)
	}
	for f := range n {
		if energy[f] < peak/900 {
			continue
		}
		s := x[f*yinHop:]
		for tau := 1; tau <= maxTau; tau++ {
			var sum float64
			for j := range yinWindow {
				diff := float64(s[j]) - float64(s[j+tau])
				sum += diff * diff
			}
			d[tau] = sum
		}
		// Cumulative mean normalized difference; the first dip under
		// the threshold, then its bottom.
		var running float64
		best := -1
		for tau := 1; tau <= maxTau; tau++ {
			running += d[tau]
			cm := 1.0
			if running > 0 {
				cm = d[tau] * float64(tau) / running
			}
			d[tau] = cm
		}
		for tau := minTau; tau <= maxTau; tau++ {
			if d[tau] < yinThreshold {
				for tau+1 <= maxTau && d[tau+1] < d[tau] {
					tau++
				}
				best = tau
				break
			}
		}
		if best < 0 {
			continue
		}
		// Parabolic interpolation around the dip.
		t := float64(best)
		if best > 1 && best < maxTau {
			a, b, c := d[best-1], d[best], d[best+1]
			if den := a - 2*b + c; den != 0 {
				t += 0.5 * (a - c) / den
			}
		}
		hz := SampleRate / t
		out[f] = 69 + 12*math.Log2(hz/440)
	}
	return out
}
