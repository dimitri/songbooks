// Package melody writes the lead voice's notes as a score, the song's
// lyrics under them (docs/melody-sheet.md). The notes come from the
// recording's vocals part, found once per split in the background
// (audioanalysis.MelodyNotesOf); everything here runs at request time,
// in milliseconds, since it follows what changes without the recording
// changing: the song's sync with it (the bar grid), its chart (bars,
// repeats, lead, time signature) and its lyrics.
//
//	notes (seconds) ──Grid──▶ played bars ──Quantize──▶ Sheet (bars of events)
//	lyrics lines ──Syllables──▶ ──AnchorLines (chords on bars), PlaceLines──▶ syllables on notes
//	Sheet + syllables ──AlphaTex / MusicXML──▶ the page, the downloads
package melody

import "math"

// Grid is when each played bar starts in the recording: Grid[b] for bar
// b (the player's bar numbers: a repeat counted on each pass, the song's
// "lead:" bars first), as the song page's player builds it
// (audio-player.js rebuildGrid) — the last entry the end of the last bar.
type Grid []float64

// Point is one bar's start (a per-bar sync point).
type Point struct {
	Bar  int
	Time float64
}

// Mark is a section's start: its first played bar, and when.
type Mark struct {
	StartBar int
	Time     float64
}

// maxBars bounds a grid (the player's own bound).
const maxBars = 5000

// FromPoints is the grid through per-bar sync points (a Guitar Pro
// file's, an auto-align's), interpolated between them and extrapolated
// past the first and the last, up to duration. Fewer than two points:
// none.
func FromPoints(pts []Point, duration float64) Grid {
	if len(pts) < 2 || duration <= 0 {
		return nil
	}
	at := func(b int) float64 {
		i := 0
		for i < len(pts)-2 && pts[i+1].Bar <= b {
			i++
		}
		p, q := pts[i], pts[i+1]
		return p.Time + float64(b-p.Bar)/float64(q.Bar-p.Bar)*(q.Time-p.Time)
	}
	return build(at, duration)
}

// FromMarkers is the grid through section starts: each section's bars
// spread evenly up to the next section's start (so a chart whose tempo
// isn't the recording's still meets every section on time); before the
// first and after the last, secondsPerBar (the chart's tempo), else the
// pace between the first and last starts.
func FromMarkers(marks []Mark, secondsPerBar, duration float64) Grid {
	if len(marks) == 0 || duration <= 0 {
		return nil
	}
	pace := secondsPerBar
	if len(marks) > 1 {
		first, last := marks[0], marks[len(marks)-1]
		if last.StartBar > first.StartBar && pace <= 0 {
			pace = (last.Time - first.Time) / float64(last.StartBar-first.StartBar)
		}
	}
	if pace <= 0 {
		return nil
	}
	at := func(b int) float64 {
		i := 0
		for i < len(marks)-1 && marks[i+1].StartBar <= b {
			i++
		}
		m := marks[i]
		if b >= m.StartBar && i+1 < len(marks) && marks[i+1].StartBar > m.StartBar {
			next := marks[i+1]
			return m.Time + float64(b-m.StartBar)/float64(next.StartBar-m.StartBar)*(next.Time-m.Time)
		}
		return m.Time + float64(b-m.StartBar)*pace
	}
	return build(at, duration)
}

func build(at func(int) float64, duration float64) Grid {
	var g Grid
	for b := range maxBars {
		t := at(b)
		g = append(g, t)
		if t > duration {
			break
		}
	}
	return g
}

// PlaceLead puts the song's "lead:" bars (lead of them) back from bar
// lead, each pace seconds long, whatever the sync placed there
// (lead-time.js placeLead): squeezed into the time before bar lead when
// it's shorter, counted in before the recording when bar lead is at 0:00.
func PlaceLead(g Grid, lead int, pace float64) Grid {
	if lead <= 0 || len(g) <= lead || pace <= 0 {
		return g
	}
	out := append(Grid(nil), g...)
	first := out[lead]
	start := first - float64(lead)*pace
	for b := range lead {
		if start >= 0 || first <= 0 {
			out[b] = first - float64(lead-b)*pace
		} else {
			out[b] = first * float64(b) / float64(lead)
		}
	}
	return out
}

// FirstPace is the seconds per bar of the first section's bars (count of
// them from bar lead, or as far as the grid goes); 0 when it can't tell.
func FirstPace(g Grid, lead, count int) float64 {
	k := min(max(1, count), len(g)-1-lead)
	if k < 1 {
		return 0
	}
	p := (g[lead+k] - g[lead]) / float64(k)
	return math.Max(0, p)
}

// BarAt is the fractional played bar at t (2.5: halfway through bar 2);
// before bar 0 and after the last, extrapolated from the nearest bar.
func (g Grid) BarAt(t float64) float64 {
	if len(g) < 2 {
		return math.NaN()
	}
	lo, hi := 0, len(g)-1
	switch {
	case t < g[0]:
		hi = 1
	case t >= g[hi]:
		lo = hi - 1
	default:
		for hi-lo > 1 {
			mid := (lo + hi) / 2
			if g[mid] <= t {
				lo = mid
			} else {
				hi = mid
			}
		}
	}
	span := g[lo+1] - g[lo]
	if span <= 0 {
		return float64(lo)
	}
	return float64(lo) + (t-g[lo])/span
}

// TimeAt is when fractional bar b is reached.
func (g Grid) TimeAt(b float64) float64 {
	if len(g) < 2 {
		return math.NaN()
	}
	n := len(g) - 1
	i := max(0, min(n-1, int(math.Floor(b))))
	return g[i] + (b-float64(i))*(g[i+1]-g[i])
}

// Bars is how many whole bars the grid has.
func (g Grid) Bars() int { return max(0, len(g)-1) }
