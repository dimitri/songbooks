package song

import "app/internal/chordsym"

// KeyShift is how far a save moved a song's own key, in semitones
// (-6…+5), 0 when it didn't: what the songbooks keeping the song in a key
// of their own (a student's, a group's, a band's) take back so that key
// stays where it was (store.keepEntryKeys).
//
//   - Both sides with a "key:" header in the same mode: the tonic moved
//     ("G" → "A": +2), whether the chart was transposed with it or the
//     header alone was put right.
//   - Otherwise (no header on one side or either, or the mode changed,
//     "Am" → "C" being a relabel): the chart transposed in place, every
//     chord it plays moved by the same interval, in the same order, the
//     same qualities. A chart edited any other way moved nothing.
func KeyShift(oldSrc, newSrc string) int {
	o, n := Parse(oldSrc), Parse(newSrc)
	if ko, ok := chordsym.ParseKey(o.Get("key")); ok {
		if kn, ok := chordsym.ParseKey(n.Get("key")); ok && ko.Minor == kn.Minor {
			return shortest(kn.PitchClass() - ko.PitchClass())
		}
	}
	return chordsShift(UsedChords(o), UsedChords(n))
}

// chordsShift is the one interval taking each chord of a to the chord of
// b in the same place, 0 when there's none (or none needed).
func chordsShift(a, b []string) int {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	d := -1
	for i := range a {
		pa, pb := chordsym.Split(a[i]), chordsym.Split(b[i])
		ra, rb := chordsym.PitchClass(pa.Root), chordsym.PitchClass(pb.Root)
		if ra < 0 || rb < 0 || pa.Quality != pb.Quality {
			return 0
		}
		if (pa.Bass == "") != (pb.Bass == "") {
			return 0
		}
		x := ((rb-ra)%12 + 12) % 12
		if pa.Bass != "" && ((chordsym.PitchClass(pb.Bass)-chordsym.PitchClass(pa.Bass))%12+12)%12 != x {
			return 0
		}
		if d >= 0 && x != d {
			return 0
		}
		d = x
	}
	return shortest(d)
}

// shortest is an interval as the shorter way round, -6…+5.
func shortest(d int) int {
	d = ((d % 12) + 12) % 12
	if d > 5 {
		d -= 12
	}
	return d
}
