package chartalign

import (
	"math"
	"testing"
)

// A song with a "lead:" (Hints.LeadBars): the lead starts the recording,
// on its first downbeat with music, and the first section starts that many
// bars later — not where the chords or the voice put it, even when the
// first section is sung (the voice may come in bars into it).
func TestAlignLeadStartsTheRecording(t *testing.T) {
	r := loadRecording(t, "song334.json")
	_, secs := chartOf(t, "song334-new.chart")
	for i := range secs {
		secs[i].HasLyrics = true // the first section sung too
	}
	free := Align(r, secs, 4, Hints{})
	for _, lead := range []int{1, 2} {
		res := Align(r, secs, 4, Hints{LeadBars: lead})
		// The downbeats from the recording's bar 1.
		var downs []float64
		for i, p := range r.BeatPos {
			if p == 1 && r.Beats[i] >= res.FirstDownbeat-0.01 {
				downs = append(downs, r.Beats[i])
			}
		}
		if len(downs) <= lead {
			t.Fatalf("lead %d: %d downbeats", lead, len(downs))
		}
		want := downs[lead]
		if got := res.Sections[0].Start; math.Abs(got-want) > 0.05 {
			t.Errorf("lead %d: first section at %.2f, want %.2f (bar 1 at %.2f; free alignment %.2f)", lead, got, want, res.FirstDownbeat, free.Sections[0].Start)
		}
		if res.IntroBars != lead {
			t.Errorf("lead %d: %d bars before the first section, want the lead's", lead, res.IntroBars)
		}
	}
}

// The recording's bar 1 is the first downbeat of a regular, sure run of
// beats: a tracker's beats over talk or a room before the music (uneven,
// unsure) aren't it.
func TestFirstMusicDownbeat(t *testing.T) {
	var beats []float64
	var pos []int
	var act []float64
	// Talk: uneven "beats", some sure enough, some called downbeats.
	for i, b := range []float64{0.5, 1.3, 1.6, 2.9, 3.3, 4.1} {
		beats, pos, act = append(beats, b), append(pos, 1+i%2*2), append(act, 0.4)
	}
	// The band from 5 s: a beat every 0.6 s, a downbeat every four.
	music := len(beats)
	for k := 0; k < 40; k++ {
		beats, pos, act = append(beats, 5+0.6*float64(k)), append(pos, 1+k%4), append(act, 0.9)
	}
	g := grid{beats: beats, pos: pos, act: act, B: 4}
	if got := firstMusicDownbeat(&Recording{}, g, len(beats)-1); got != music {
		t.Errorf("bar 1 at beat %d (%.2f s), want %d (5 s)", got, beats[max(0, got)], music)
	}
	// No regular run anywhere (rubato): the first sure downbeat still.
	g2 := grid{beats: beats[:music], pos: pos[:music], act: act[:music], B: 4}
	if got := firstMusicDownbeat(&Recording{}, g2, music-1); got != 0 {
		t.Errorf("rubato: bar 1 at beat %d, want 0", got)
	}
}
