package chartalign

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"app/internal/song"
)

// A repeat count or a "times:" of millions, typed by an editor (or sent
// in the analysis request's text, which isn't even saved), is never
// played out: the chart stops at the player's count and at MaxBars, and
// the alignment says the chart is too long without aligning it.
func TestChartHugeRepeat(t *testing.T) {
	r := loadRecording(t, "song334.json")
	for _, body := range []string{
		"title: T\ntime: 4/4\n[Verse]\ngrid: |:2000000 A | B :|\n",
		"title: T\ntime: 4/4\n[Verse]\ngrid: |:200000000 A | B :|\n",
		"title: T\ntime: 4/4\n[Verse]\ngrid: A | B\ntimes: 100000000\n",
		"title: T\ntime: 4/4\n[Verse]\ngrid: |:200000000 A | (1-200000000.) B :|\n",
	} {
		sg := song.Parse(body)
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		start := time.Now()
		// What the player counts for it: huge, it's just a number.
		for _, n := range []int{4, 400000000, 0} {
			secs := Chart(sg, []SectionInfo{{Index: 0, Label: "Verse", BarCount: n, HasLyrics: true}})
			if len(secs) != 1 || len(secs[0].Bars) > MaxBars {
				t.Fatalf("%q, %d bars counted: %d bars read", body, n, len(secs[0].Bars))
			}
			if n == 4 && (len(secs[0].Bars) != 4 || secs[0].Cut) {
				t.Errorf("%q: %d bars, cut %v; want the player's 4", body, len(secs[0].Bars), secs[0].Cut)
			}
			res := Align(r, secs, 4, Hints{})
			if n != 4 && res.State != "unsure" {
				t.Errorf("%q, %d bars: state %s, want unsure", body, n, res.State)
			}
			if n > MaxBars && res.Why != "too-long" {
				t.Errorf("%q, %d bars: %q, want too-long", body, n, res.Why)
			}
		}
		runtime.ReadMemStats(&after)
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("%q took %s", body, d)
		}
		if mb := (after.TotalAlloc - before.TotalAlloc) >> 20; mb > 200 {
			t.Errorf("%q allocated %d MB", body, mb)
		}
	}
}

// A chart far longer than its recording isn't "good" because the chords
// it's matched on fit: song 334's recording (about 80 bars) against a
// 400-bar section, and against a 200-bar one, is unsure, saying why.
func TestAlignAbsurdChart(t *testing.T) {
	r := loadRecording(t, "song334.json")
	for _, n := range []int{100, 50} {
		body := fmt.Sprintf("title: T\ntime: 4/4\n[Intro]\ngrid: N.C. | N.C. | N.C.\n[Verse]\ngrid: |:%d D | A | Bm | G :|\n[Outro]\ngrid: D\n", n)
		sg := song.Parse(body)
		info := []SectionInfo{{Index: 0, Label: "Intro", BarCount: 3}, {Index: 1, Label: "Verse", StartBar: 3, BarCount: 4 * n, HasLyrics: true}, {Index: 2, Label: "Outro", StartBar: 3 + 4*n, BarCount: 1}}
		start := time.Now()
		res := Align(r, Chart(sg, info), 4, Hints{})
		t.Logf("%d bars: %s %.2f (%s), written %d played %d, chart %d, in %s", 4*n, res.State, res.Confidence, res.Why, res.WrittenBars, res.PlayedBars, res.ChartBars, time.Since(start))
		if res.State != "unsure" || res.Why == "" {
			t.Errorf("%d-bar verse: state %s (%.2f, %q), want unsure with a reason", 4*n, res.State, res.Confidence, res.Why)
		}
		if res.ChartBars != 4*n+4 {
			t.Errorf("chart bars %d", res.ChartBars)
		}
	}
}

// Endings are read as the song's bar count reads them (song.PlayOrder,
// the Jazz chart's jzExpand): passes 1-3 take the first ending, the 4th
// the second, and an ending runs over all its bars.
func TestChartEndings(t *testing.T) {
	for _, c := range []struct{ grid, want string }{
		{"|: (1,2,3.) Ab | Gm | (4.) Bb | Bb :|", "[0 1 0 1 0 1 2 3]"},
		{"|: C | (1.) F | G :| (2.) Am | Am |", "[0 1 2 0 3 4]"},
	} {
		sg := song.Parse("title: T\n[A]\ngrid: " + c.grid + "\n")
		n := len(song.PlayOrder(sg.Sections[0].Measures, 0))
		secs := Chart(sg, []SectionInfo{{Index: 0, Label: "A", BarCount: n}})
		var got []int
		for _, b := range secs[0].Bars {
			got = append(got, b.Measure)
		}
		if fmt.Sprint(got) != c.want {
			t.Errorf("%s: bars from measures %v, want %s", c.grid, got, c.want)
		}
	}
}

// A perfectly steady beat (a drum machine, a Guitar Pro render: CV 0) is
// steady, not "the tempo moves".
func TestAlignSteadyBeat(t *testing.T) {
	r := loadRecording(t, "song334.json")
	r.TempoCV = 0
	_, secs := chartOf(t, "song334-new.chart")
	if res := Align(r, secs, 4, Hints{}); !res.TempoSteady {
		t.Errorf("CV 0 isn't steady")
	}
}

// The section after one placed on the recording's last beats gets no
// start rather than one past the end.
func TestChooseDownbeatAtTheEnd(t *testing.T) {
	g := grid{beats: []float64{0, 1, 2, 3, 4}, pos: []int{1, 2, 3, 4, 1}, B: 4}
	// The chords put it at beat 3, the previous section took beat 4.
	if st := chooseDownbeat(g, 3, false, nil, 1, 4); st != -1 {
		t.Errorf("start %d, want none (-1)", st)
	}
	if st := chooseDownbeat(g, 2, false, nil, 1, 2); st < 0 || st >= len(g.beats) {
		t.Errorf("start %d", st)
	}
}
