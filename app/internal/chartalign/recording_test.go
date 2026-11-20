package chartalign

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// testdata/song334.json and song81.json are what the analysis stage
// stored for two recordings (Beat This! on the backing part, lv-chordia
// on the mix, the voice from the vocals part: `stemsd --analyze-files
// mix.wav,no_vocals.wav,vocals.wav`). Numbers and chord labels only: no
// audio, no lyrics.

func loadRecording(t *testing.T, name string) *Recording {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var r Recording
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

func TestRecording334(t *testing.T) {
	r := loadRecording(t, "song334.json")
	if math.Abs(r.Tempo-90.1) > 0.3 || r.TimeSig != "4/4" || r.TimeSigConf < 0.9 {
		t.Fatalf("tempo %.2f, %s (%.2f): want ~90.1 bpm in 4/4", r.Tempo, r.TimeSig, r.TimeSigConf)
	}
	// Ground truth (app/reports/auto-sync-chords.md): bar lines at
	// 1.60 s + n × 2.663 s.
	downs := r.Downbeats()
	hit := 0
	for n := 0; n < 78; n++ {
		want := 1.60 + float64(n)*2.663
		for _, d := range downs {
			if math.Abs(d-want) <= 0.07 {
				hit++
				break
			}
		}
	}
	if hit < 76 {
		t.Errorf("only %d of the first 78 bar lines found within 70 ms", hit)
	}
	if math.Abs(downs[0]-1.60) > 0.08 {
		t.Errorf("first downbeat %.2f, want 1.60", downs[0])
	}
}

func TestRecording81(t *testing.T) {
	r := loadRecording(t, "song81.json")
	if math.Abs(r.Tempo-95.3) > 0.5 || r.TimeSig != "4/4" || r.TimeSigConf < 0.95 || r.Key != "Gm" {
		t.Fatalf("tempo %.2f, %s (%.2f), key %s", r.Tempo, r.TimeSig, r.TimeSigConf, r.Key)
	}
}

func TestRegularize(t *testing.T) {
	// A missed beat (at 2) and a doubled one (4.95, 5.0), the first
	// downbeat a beat early.
	beats := []float64{0, 1, 3, 4, 4.95, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	downs := []float64{1, 6, 10, 14}
	act := make([]float64, len(beats))
	for i := range act {
		act[i] = 0.9
	}
	act[4] = 0.4
	nb, nd, _ := regularize(beats, downs, act)
	if len(nb) != 17 || nb[2] != 2 || nb[5] != 5 {
		t.Errorf("beats %v", nb)
	}
	if len(nd) != 4 || nd[0] != 2 || nd[1] != 6 {
		t.Errorf("downbeats %v", nd)
	}
}
