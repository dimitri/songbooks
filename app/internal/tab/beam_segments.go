package tab

// BeamSegment is one beam LINE drawn at a specific subdivision level
// (0 = the primary/eighth-note beam, 1 = the secondary/sixteenth beam,
// ...) across part or all of a BeamGroup — the piece GroupBeams' own
// MinFlags simplification drops: a mixed-duration group (an eighth beamed
// with a sixteenth) needs the primary beam across BOTH notes but the
// secondary beam only where a note actually subdivides that far, drawn
// as a short stub (a "partial" or "broken" beam, standard notation
// practice) on the lone sixteenth's side when it has no same-level
// neighbor to span to.
type BeamSegment struct {
	Level            int
	StartIdx, EndIdx int // inclusive; StartIdx == EndIdx means a partial/stub beam
	// PartialToward is 0 for a full segment (StartIdx != EndIdx); for a
	// partial (StartIdx == EndIdx) it's -1 (stub points toward the
	// previous beat) or +1 (points toward the next beat) — toward
	// whichever neighbor is still part of this same BeamGroup, which is
	// always determinable since a BeamGroup never starts or ends on an
	// isolated note with no beamed neighbor at all (see ComputeBeamSegments).
	PartialToward int
}

// ComputeBeamSegments expands one BeamGroup into the real per-level beam
// lines real engraving draws, instead of GroupBeams.MinFlags' single
// uniform beam count for the whole group. For each level from 0 up to
// the group's highest flag count minus 1, it finds maximal contiguous
// runs of beats whose own flag count exceeds that level: a run of 2+
// becomes one full BeamSegment; a run of exactly 1 becomes a partial,
// pointing toward whichever side (previous or next beat index) is still
// inside the group — always well-defined, because the lone note is by
// construction flanked by at least one beamed neighbor (it's not the
// group's own only member, or it wouldn't be a BeamGroup at all — see
// GroupBeams, which never returns a single-beat group).
func ComputeBeamSegments(beats []Beat, grp BeamGroup) []BeamSegment {
	flagsOf := make([]int, grp.EndIdx-grp.StartIdx+1)
	maxFlags := 0
	for i := range flagsOf {
		f, _ := DurationGlyph(beats[grp.StartIdx+i].Duration)
		flagsOf[i] = f
		if f > maxFlags {
			maxFlags = f
		}
	}

	var out []BeamSegment
	for level := 0; level < maxFlags; level++ {
		i := 0
		for i < len(flagsOf) {
			if flagsOf[i] <= level {
				i++
				continue
			}
			j := i
			for j+1 < len(flagsOf) && flagsOf[j+1] > level {
				j++
			}
			if j > i {
				out = append(out, BeamSegment{Level: level, StartIdx: grp.StartIdx + i, EndIdx: grp.StartIdx + j})
			} else {
				toward := -1
				if i == 0 {
					toward = 1
				}
				out = append(out, BeamSegment{Level: level, StartIdx: grp.StartIdx + i, EndIdx: grp.StartIdx + i, PartialToward: toward})
			}
			i = j + 1
		}
	}
	return out
}
