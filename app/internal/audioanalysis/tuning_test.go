package audioanalysis

import (
	"math"
	"testing"
)

// A I–IV–V–I in C played 40 cents sharp, the way an old record can sit
// between two pitches: the tuning estimate takes the offset out, so the
// key is still C, not split between C and C#.
func TestDetunedKey(t *testing.T) {
	const cents = 0.4
	chords := [][]float64{{60, 64, 67, 48}, {65, 69, 72, 53}, {67, 71, 74, 55}, {60, 64, 67, 48}}
	var x []float32
	for rep := 0; rep < 4; rep++ {
		for _, ch := range chords {
			n := 2 * SampleRate
			for i := 0; i < n; i++ {
				v := 0.0
				for _, m := range ch {
					f := 440 * math.Pow(2, (m+cents-69)/12)
					v += 0.2 * math.Sin(2*math.Pi*f*float64(i)/SampleRate)
				}
				x = append(x, float32(v))
			}
		}
	}
	if got := tuning(x); math.Abs(got-cents) > 0.05 {
		t.Fatalf("tuning = %.3f, want %.2f", got, cents)
	}
	if k, _ := estimateKey(x); k != "C" {
		t.Fatalf("key = %s, want C", k)
	}
}
