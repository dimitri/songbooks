package song

// SimplifyGridText re-renders a grid spec (the same "A | B | C" text
// parseGrid/writeGrid already round-trip) with repeated bars folded into
// the chart notation this app already uses elsewhere: an immediate
// single-bar repeat becomes "%", and a whole phrase of 2+ bars repeated
// back-to-back becomes a "|:N ... :|" bracket — the same bracket a human
// author or the LilyPond importer already writes for an intro/outro loop
// (see Measure.RepeatCount's doc comment). This turns a naively-imported
// grid like "Em7 | G | D | A | Em7 | G | D | A | Em7 | G | D | A | C | D | A"
// (one bar per literal chord change, e.g. straight off an Ultimate Guitar
// tab) into "|:3 Em7 | G | D | A :| C | D | A" — the form a real chart
// would be written in.
func SimplifyGridText(spec string) string {
	return writeGrid(SimplifyMeasures(parseGrid(spec)))
}

// SimplifyMeasures applies the same folding to an already-parsed measure
// sequence. It bails out (returns in unchanged) if any measure already
// carries an explicit repeat bracket, a volta, or a stated meter — that's
// a deliberate structure an author or another importer already chose, and
// re-folding it risks fighting that choice rather than helping it.
func SimplifyMeasures(in []Measure) []Measure {
	for _, m := range in {
		if m.Open || m.Close || m.Volta != "" || m.RepeatCount > 0 {
			return in
		}
	}
	return collapsePercentRepeats(collapseRepeatGroups(in))
}

// collapseRepeatGroups finds runs of 2+ consecutive bars that repeat as a
// whole phrase (group size >= 2 — a lone repeated bar is left for
// collapsePercentRepeats's "%" instead, matching how charts already use
// each notation) and wraps each run in a "|: ... :|" bracket. Greedy,
// left to right: at each position it picks whichever (group size, repeat
// count) covers the most bars, so a long simple loop is preferred over a
// shorter coincidental match.
func collapseRepeatGroups(in []Measure) []Measure {
	var out []Measure
	n := len(in)
	for i := 0; i < n; {
		bestG, bestReps := 0, 0
		for g := 2; g <= (n-i)/2; g++ {
			reps := 1
			for i+(reps+1)*g <= n && measureGroupsEqual(in[i+reps*g:i+(reps+1)*g], in[i:i+g]) {
				reps++
			}
			if reps >= 2 && reps*g > bestReps*bestG {
				bestG, bestReps = g, reps
			}
		}
		if bestReps >= 2 {
			group := cloneMeasures(in[i : i+bestG])
			group[0].Open = true
			if bestReps != 2 {
				group[0].RepeatCount = bestReps
			}
			group[len(group)-1].Close = true
			out = append(out, group...)
			i += bestG * bestReps
		} else {
			out = append(out, in[i])
			i++
		}
	}
	return out
}

// collapsePercentRepeats replaces a plain bar with "%" when it repeats
// the chord actually held by the previous bar — chasing through any
// existing "%" bars to find that chord, so a run of three or more
// identical bars collapses just as well as two.
func collapsePercentRepeats(in []Measure) []Measure {
	out := make([]Measure, len(in))
	var held []string
	for i, m := range in {
		out[i] = m
		isPercent := len(m.Tokens) == 1 && m.Tokens[0] == "%"
		plain := !m.Open && !m.Close && m.Volta == "" && m.RepeatCount == 0 && m.Meter == ""
		if plain && !isPercent && held != nil && tokensEqual(m.Tokens, held) {
			out[i].Tokens = []string{"%"}
			isPercent = true
		}
		if !isPercent && len(m.Tokens) > 0 {
			held = m.Tokens
		}
	}
	return out
}

func measureGroupsEqual(a, b []Measure) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Meter != b[i].Meter || !tokensEqual(a[i].Tokens, b[i].Tokens) {
			return false
		}
	}
	return true
}

func tokensEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func cloneMeasures(in []Measure) []Measure {
	out := make([]Measure, len(in))
	for i, m := range in {
		out[i] = m
		out[i].Tokens = append([]string(nil), m.Tokens...)
	}
	return out
}
