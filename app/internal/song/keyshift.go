package song

import "app/internal/chordsym"

// KeyShift is how far a save transposed a song, in semitones (-6…+5), 0
// when it didn't: what the songbooks keeping the song in a key of their
// own (a student's, a group's, a band's) take back so that key sounds
// where it did (store.keepEntryKeys).
//
// Only chords actually moving is a transposition: every chord the chart
// plays moved by the same interval, in the same order, the same
// qualities (at least two of them: one chord changed is a fix). The
// "key:" header alone changed is a label fix — the header was wrong, the
// chords and the reference recording don't move — so it moves nothing:
// the keys kept in songbooks stay as they are (the same semitones, the
// same sound), only their names follow the header. A chart edited any
// other way moved nothing either.
func KeyShift(oldSrc, newSrc string) int {
	return chordsShift(UsedChords(Parse(oldSrc)), UsedChords(Parse(newSrc)))
}

// chordsShift is the one interval taking each chord of a to the chord of
// b in the same place, 0 when there's none (or none needed), or when
// there are fewer than two chords to tell a transposition from a fix.
func chordsShift(a, b []string) int {
	if len(a) < 2 || len(a) != len(b) {
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
