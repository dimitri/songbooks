package tab

import "math"

// BeamGroup is a run of 2+ consecutive eighth-note-or-shorter beats that
// share one beam — grouped by METRIC PULSE (one beat-unit "cell" per
// group: a quarter note in simple time, a dotted quarter in compound
// 6/8|9/8|12/8), not merely "every consecutive flagged note until a rest
// or a longer note."
//
// This fixes a real engraving bug found this session: this package's
// earlier callers (both the web chart's CSS/SVG tab-only row and the
// PDF renderer) beamed an ENTIRE run of consecutive eighths/sixteenths
// into one continuous beam bar spanning the whole phrase — confirmed
// wrong against the vendored alphaTab library's own TabRhythmMode.
// ShowWithBars output on the exact same real riffs (Bad Spell, Kawasaki)
// this package's tests use: real engraving breaks the beam at every
// beat-unit boundary (e.g. 8 straight eighths in 4/4 beam as 2+2+2+2,
// never one run of 8), the standard LilyPond default-beaming rule.
//
// MinFlags is the shared beam-bar count for the whole group (the
// shortest member's own flag count, e.g. 1 for an eighth-note pair, 2
// for a sixteenth-note pair) — a simplification shared with the pre-
// existing code this replaces: a genuinely mixed-duration group (an
// eighth beamed with a sixteenth) gets one uniform beam weight rather
// than the additional partial/broken beam stub real engraving draws on
// just the shorter note. Real engraving software (LilyPond, alphaTab)
// does draw that partial beam; this package doesn't yet.
type BeamGroup struct {
	StartIdx, EndIdx int // 0-based, inclusive, into the Beats slice passed in
	MinFlags         int
}

// GroupBeams groups beats (one bar's worth, in order) into BeamGroups.
// timeNum/timeDen select the beat unit: compound time (6/8, 9/8, 12/8 —
// numerator a multiple of 3, greater than 3, denominator 8) groups by a
// dotted-quarter pulse (1.5 DurationBeats units); every other meter
// (including the overwhelmingly common 4/4) groups by a plain quarter
// (1.0 units) — not technically correct for every possible odd meter
// (5/8, 7/8, ...), but exactly matches every real riff in this app's
// corpus, all plain simple or 6/8-family compound time.
func GroupBeams(beats []Beat, timeNum, timeDen int) []BeamGroup {
	beatUnit := 1.0
	if timeDen == 8 && timeNum > 3 && timeNum%3 == 0 {
		beatUnit = 1.5
	}

	const eps = 1e-6
	cellOf := func(pos float64) int { return int(math.Floor(pos/beatUnit + eps)) }

	var groups []BeamGroup
	pos := 0.0
	i := 0
	for i < len(beats) {
		flags, _ := DurationGlyph(beats[i].Duration)
		w, err := DurationBeats(beats[i].Duration)
		if err != nil || w <= 0 {
			w = 1
		}
		if beats[i].Rest || flags == 0 {
			pos += w
			i++
			continue
		}
		startCell := cellOf(pos)
		j := i
		minFlags := flags
		p := pos + w
		for j+1 < len(beats) {
			nf, _ := DurationGlyph(beats[j+1].Duration)
			if beats[j+1].Rest || nf == 0 || cellOf(p) != startCell {
				break
			}
			nw, err := DurationBeats(beats[j+1].Duration)
			if err != nil || nw <= 0 {
				nw = 1
			}
			j++
			if nf < minFlags {
				minFlags = nf
			}
			p += nw
		}
		if j > i {
			groups = append(groups, BeamGroup{StartIdx: i, EndIdx: j, MinFlags: minFlags})
		}
		pos = p
		i = j + 1
	}
	return groups
}
