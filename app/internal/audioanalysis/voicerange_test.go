package audioanalysis

import (
	"math"
	"testing"
)

// sungLine renders notes (MIDI, each dur seconds) as a voice-like tone:
// a fundamental and a few harmonics, a little vibrato, a breath between
// notes. Generated, no recording involved.
func sungLine(notes []int, dur float64) []float32 {
	var x []float32
	for _, n := range notes {
		hz := 440 * math.Pow(2, float64(n-69)/12)
		for i := 0; i < seconds(dur); i++ {
			t := float64(i) / SampleRate
			f := hz * (1 + 0.006*math.Sin(2*math.Pi*5.5*t))
			v := math.Sin(2*math.Pi*f*t) + 0.5*math.Sin(4*math.Pi*f*t) + 0.25*math.Sin(6*math.Pi*f*t)
			x = append(x, float32(0.3*v))
		}
		x = append(x, make([]float32, seconds(0.15))...) // a breath
	}
	return x
}

// TestMelodyRangeOf: a line from A3 to E5 is found as A3–E5, a stray
// note sung once and short doesn't move it, too little singing is no
// range.
func TestMelodyRangeOf(t *testing.T) {
	var line []int
	for range 4 {
		line = append(line, 57, 60, 62, 64, 67, 69, 71, 72, 74, 76, 72, 69, 64, 60)
	}
	x := sungLine(line, 0.5)
	// A shout, a tenth of a second, an octave above.
	x = append(x, sungLine([]int{88}, 0.1)...)
	r, ok := MelodyRangeOf(x)
	if !ok {
		t.Fatalf("no range from %.1f s", r.Voiced)
	}
	if r.Low != 57 || r.High != 76 {
		t.Errorf("range %d–%d, want 57–76 (voiced %.1f s)", r.Low, r.High, r.Voiced)
	}
	if _, ok := MelodyRangeOf(sungLine([]int{60, 64, 67}, 0.5)); ok {
		t.Error("a range from a second and a half of singing")
	}
	if _, ok := MelodyRangeOf(make([]float32, seconds(20))); ok {
		t.Error("a range from silence")
	}
}
