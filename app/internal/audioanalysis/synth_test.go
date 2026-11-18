package audioanalysis

import (
	"math"
	"math/rand/v2"
	"slices"
)

// Synthetic test signals. Everything here is generated from scratch —
// no recorded or copyrighted audio is involved anywhere in the tests.

func seconds(d float64) int { return int(d * SampleRate) }

// addClick mixes a percussive click (a deterministic noise burst with a
// 12 ms exponential decay, like a stick or a closed hi-hat) of peak
// amplitude amp at time t into x.
func addClick(x []float32, t, amp float64, rng *rand.Rand) {
	start := seconds(t)
	n := seconds(0.08)
	for i := 0; i < n && start+i < len(x); i++ {
		if start+i < 0 {
			continue
		}
		env := math.Exp(-float64(i) / (0.012 * SampleRate))
		x[start+i] += float32(amp * env * (2*rng.Float64() - 1))
	}
}

// groove describes a one-bar drum pattern: kick and snare on the listed
// beats (0-based), and a hi-hat on every one of subdiv ticks per beat.
type groove struct {
	beatsPerBar, subdiv int
	kick, snare         []int
}

var (
	rock4      = groove{4, 2, []int{0, 2}, []int{1, 3}}
	funk4      = groove{4, 4, []int{0}, []int{1, 3}}
	waltz3     = groove{3, 2, []int{0}, []int{1, 2}}
	ballad128  = groove{4, 3, []int{0, 2}, []int{1, 3}}
	shuffle128 = groove{4, 3, []int{0}, []int{1, 2, 3}}
)

// drumTrack renders dur seconds of a drum groove at beatSec seconds per
// beat, starting at 0.5 s: a pitched-down sine thump for the kick, a
// noise burst with a little body for the snare, and a short bright
// noise tick for the hi-hat, plus a faint noise floor. Kick, snare and
// hat sit in different parts of the spectrum, as in a real kit.
func drumTrack(dur, beatSec float64, g groove) []float32 {
	x := make([]float32, seconds(dur))
	rng := rand.New(rand.NewPCG(1, 2))
	tick := beatSec / float64(g.subdiv)
	for i := 0; ; i++ {
		t := 0.5 + float64(i)*tick
		if t >= dur {
			break
		}
		addHat(x, t, 0.15, rng)
		if i%g.subdiv != 0 {
			continue
		}
		beat := (i / g.subdiv) % g.beatsPerBar
		if slices.Contains(g.kick, beat) {
			addKick(x, t, 0.8)
		}
		if slices.Contains(g.snare, beat) {
			addSnare(x, t, 0.4, rng)
		}
	}
	for i := range x {
		x[i] += float32(0.002 * (2*rng.Float64() - 1))
	}
	return x
}

func addKick(x []float32, t, amp float64) {
	start := seconds(t)
	phase := 0.0
	for i := 0; i < seconds(0.3) && start+i < len(x); i++ {
		tt := float64(i) / SampleRate
		f := 45 + 60*math.Exp(-tt/0.03) // the characteristic pitch drop
		phase += 2 * math.Pi * f / SampleRate
		x[start+i] += float32(amp * math.Exp(-tt/0.12) * math.Sin(phase))
	}
}

func addSnare(x []float32, t, amp float64, rng *rand.Rand) {
	start := seconds(t)
	prev := 0.0
	for i := 0; i < seconds(0.2) && start+i < len(x); i++ {
		tt := float64(i) / SampleRate
		n := 2*rng.Float64() - 1
		hp := n - 0.5*prev // gently brightened noise
		prev = n
		body := math.Sin(2 * math.Pi * 190 * tt)
		x[start+i] += float32(amp * math.Exp(-tt/0.06) * (0.7*hp + 0.5*body))
	}
}

func addHat(x []float32, t, amp float64, rng *rand.Rand) {
	start := seconds(t)
	prev := 0.0
	for i := 0; i < seconds(0.05) && start+i < len(x); i++ {
		tt := float64(i) / SampleRate
		n := 2*rng.Float64() - 1
		hp := n - prev // first difference: energy pushed to the top octaves
		prev = n
		x[start+i] += float32(amp * math.Exp(-tt/0.012) * hp)
	}
}

// midiHz converts a MIDI note number to frequency (A4 = 69 = 440 Hz).
func midiHz(n int) float64 { return 440 * math.Pow(2, float64(n-69)/12) }

// addTone mixes a note with a few decaying harmonics (a plucked/organ-ish
// timbre rather than a bare sine) from t0 to t1.
func addTone(x []float32, midi int, t0, t1, amp float64) {
	f := midiHz(midi)
	for i := seconds(t0); i < seconds(t1) && i < len(x); i++ {
		t := float64(i-seconds(t0)) / SampleRate
		v := 0.0
		for h := 1; h <= 4; h++ {
			if f*float64(h) > SampleRate/2 {
				break
			}
			v += math.Sin(2*math.Pi*f*float64(h)*t) / float64(h*h)
		}
		// 10 ms fade in and out to avoid clicks.
		fade := min(1, t/0.01, (t1-t0-t)/0.01)
		x[i] += float32(amp * fade * v)
	}
}

// progression renders chords (MIDI note lists), each chordSec long, with
// the chord's root doubled an octave below as a bass note, repeated
// until dur.
func progression(chords [][]int, chordSec, dur float64) []float32 {
	x := make([]float32, seconds(dur))
	for i := 0; ; i++ {
		t0 := float64(i) * chordSec
		if t0 >= dur {
			break
		}
		ch := chords[i%len(chords)]
		for _, n := range ch {
			addTone(x, n, t0, min(dur, t0+chordSec), 0.1)
		}
		addTone(x, ch[0]-12, t0, min(dur, t0+chordSec), 0.15)
	}
	return x
}

// vocalStem renders a stand-in vocals stem: sung phrases as a 220 Hz
// tone with vibrato over the given [start, end) spans, on a faint noise
// floor (separation bleed) 50 dB down.
func vocalStem(dur float64, phrases [][2]float64) []float32 {
	x := make([]float32, seconds(dur))
	rng := rand.New(rand.NewPCG(3, 4))
	for i := range x {
		x[i] = float32(0.0015 * (2*rng.Float64() - 1))
	}
	for _, p := range phrases {
		for i := seconds(p[0]); i < seconds(p[1]) && i < len(x); i++ {
			t := float64(i) / SampleRate
			f := 220 + 4*math.Sin(2*math.Pi*5.5*t)
			x[i] += float32(0.3 * math.Sin(2*math.Pi*f*t))
		}
	}
	return x
}

// activityFromPhrases builds VocalActivity frames directly from sung
// spans, for FitSections tests that don't need audio.
func activityFromPhrases(dur float64, phrases [][2]float64) []bool {
	act := make([]bool, int(math.Ceil(dur/Hop)))
	for _, p := range phrases {
		for i := int(math.Round(p[0] / Hop)); i < int(math.Round(p[1]/Hop)) && i < len(act); i++ {
			act[i] = true
		}
	}
	return act
}
