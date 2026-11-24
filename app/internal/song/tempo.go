package song

import (
	"math"
	"strconv"
	"strings"
)

// What a chart's "tempo:" counts: the beat of its "time:".
//
//   - a simple meter's beat is its denominator's note: N/4 counts
//     quarters, N/2 halves, N/8 eighths (5/8, 7/8: the click track's
//     "the bpm counts eighths");
//   - a compound meter, 6/8, 9/8, 12/8 (and 15/8…: an /8 numerator of
//     three eighths a beat, 6 or more), counts dotted quarters, the
//     musicians' convention: a 6/8 bar is two beats, "tempo: 46" is 46
//     dotted quarters, 138 eighths, a minute.
//
// The player's bar grid, its click and count-in, the metronome, the set
// list's click track, the recording's analysis and the import wizard's
// sync all count it so (timeSigBeats, chartalign.BeatsOf, clickMeterFor,
// sync-sheet.js). Guitar Pro, alphaTab and LilyPond count quarter notes:
// their tempo is converted at the boundary, with QuarterTempo and
// FromQuarterTempo.

// CompoundTime: sig is a compound meter, its beat a dotted note.
func CompoundTime(sig string) bool {
	n, d, ok := timeParts(sig)
	return ok && d == 8 && n >= 6 && n%3 == 0
}

// BeatQuarters is how many quarter notes one beat of "tempo:" lasts in
// sig: 1.5 in 6/8, 9/8, 12/8, ½ in 7/8, 2 in 2/2, 1 in 4/4 and in
// anything unreadable (an empty "time:" is 4/4).
func BeatQuarters(sig string) float64 {
	n, d, ok := timeParts(sig)
	if !ok {
		return 1
	}
	if d == 8 && n >= 6 && n%3 == 0 {
		return 1.5
	}
	return 4 / float64(d)
}

// QuarterTempo is the chart's tempo in quarter notes a minute (Guitar
// Pro's, alphaTab's, LilyPond's "\tempo 4 ="), rounded.
func QuarterTempo(tempo float64, sig string) int {
	return int(math.Round(tempo * BeatQuarters(sig)))
}

// FromQuarterTempo is a tempo in quarter notes a minute (a Guitar Pro
// file's) in the chart's beat, rounded.
func FromQuarterTempo(quarters float64, sig string) int {
	return int(math.Round(quarters / BeatQuarters(sig)))
}

// timeParts reads "N/D" (and C, C| for 4/4, 2/2); D a power of two.
func timeParts(sig string) (n, d int, ok bool) {
	s := strings.TrimSpace(sig)
	switch s {
	case "C":
		s = "4/4"
	case "C|", "¢":
		s = "2/2"
	}
	a, b, found := strings.Cut(s, "/")
	if !found {
		return 0, 0, false
	}
	n, err1 := strconv.Atoi(strings.TrimSpace(a))
	d, err2 := strconv.Atoi(strings.TrimSpace(b))
	if err1 != nil || err2 != nil || n <= 0 || d <= 0 || d > 64 || d&(d-1) != 0 {
		return 0, 0, false
	}
	return n, d, true
}

// BeatNote is the note "tempo:" counts in sig, as a LilyPond duration:
// "4." in 6/8, 9/8, 12/8, "8" in 7/8, "2" in 2/2, "4" otherwise.
func BeatNote(sig string) string {
	n, d, ok := timeParts(sig)
	if !ok {
		return "4"
	}
	if d == 8 && n >= 6 && n%3 == 0 {
		return "4."
	}
	return strconv.Itoa(d)
}
