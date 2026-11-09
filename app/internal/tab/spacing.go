package tab

import "math"

// SpacingWeights returns one proportional-width weight per beat, for
// laying out a bar's notes horizontally — John S. Gourlay's logarithmic
// duration-spacing formula ("Spacing a Line of Music", OSU-CISRC-10/87-
// TR35, 1987), the same one real engraving software uses (confirmed in
// LilyPond's own source, lily/spacing-options.cc's get_duration_space:
// shortest_duration_space_=2.0, weight = shortest_duration_space_ +
// log2(duration/shortest); this package re-derives the formula itself
// from that public algorithm, not from LilyPond's GPL-licensed code).
//
// This is NOT the same as weighting columns directly by DurationBeats
// (plain linear duration) — this package's own first version of both
// the web chart's CSS grid and this package's PDF engraver did exactly
// that, and it reads wrong: a single quarter note claims 4x a sixteenth
// note's width, crushing a beamed run of short notes into a cramped
// sliver next to a bloated long note. Gourlay's log-based weight gives
// short notes a higher proportional floor — a quarter claims only 2x a
// sixteenth's width, not 4x — which is why real engraving doesn't look
// lopsided the way linear spacing does.
func SpacingWeights(beats []Beat) []float64 {
	if len(beats) == 0 {
		return nil
	}
	durs := make([]float64, len(beats))
	shortest := math.Inf(1)
	for i, b := range beats {
		w, err := DurationBeats(b.Duration)
		if err != nil || w <= 0 {
			w = 1
		}
		durs[i] = w
		if w < shortest {
			shortest = w
		}
	}
	const shortestSpace = 2.0 // Gourlay's own constant, see doc comment
	out := make([]float64, len(beats))
	for i, d := range durs {
		ratio := d / shortest
		if ratio < 1 {
			ratio = 1 // shouldn't happen (shortest is the bar's own min), guards float noise
		}
		out[i] = shortestSpace + math.Log2(ratio)
	}
	return out
}
