package httpapi

import (
	"strconv"
	"strings"
)

// clickMeter is how the set list's click track clicks a song's time
// signature (setlist-clicktrack.js reads it from the page's JSON):
//
//   - simple meters, N/4 and N/2 (2/4, 3/4, 4/4, 5/4, 6/4, 7/4, 2/2,
//     3/2…): N clicks of a quarter (or half) note, accent on 1;
//   - compound meters, 6/8, 9/8, 12/8 (and 15/8…): every eighth, accent
//     on 1, a lighter accent in the middle of the bar (4 in 6/8, 7 in
//     12/8) or, with an odd number of dotted quarters, on each of them
//     (4 and 7 in 9/8). The songs' bpm is entered in dotted quarters
//     (the 12/8 songs say 58 to 110, the 6/8 ones 60 to 72), so the
//     eighths click three times as fast as the bpm (PerBpm 3), and Tap
//     taps the dotted quarter;
//   - odd meters in eighths (5/8, 7/8, 11/8…): every eighth, accent on
//     1, a lighter accent on each group's first eighth, by a default
//     grouping (5/8 = 3+2, 7/8 = 2+2+3, 11/8 = 3+3+3+2); the bpm counts
//     eighths.
//
// Anything it can't read clicks in 4/4 with Known false.
type clickMeter struct {
	Beats   int    `json:"beats"`             // clicks a bar
	Groups  []int  `json:"groups,omitempty"`  // the bar's groups of eighths (a space between them in the dots)
	Accents []int  `json:"accents,omitempty"` // the clicks with the lighter accent (0-based; 0 always has the strong one)
	PerBpm  int    `json:"perBpm"`            // clicks for each beat the bpm counts
	Unit    string `json:"unit"`              // the note the bpm counts: quarter, half, eighth, dotted-quarter
	Known   bool   `json:"known"`
}

// defaultEighthGroups is the usual grouping of an odd meter in eighths.
var defaultEighthGroups = map[int][]int{
	5:  {3, 2},
	7:  {2, 2, 3},
	8:  {3, 3, 2},
	10: {3, 3, 2, 2},
	11: {3, 3, 3, 2},
	13: {3, 3, 3, 2, 2},
}

func clickMeterFor(timeSig string) clickMeter {
	fallback := clickMeter{Beats: 4, PerBpm: 1, Unit: "quarter"}
	t := strings.TrimSpace(timeSig)
	switch t {
	case "C":
		t = "4/4"
	case "C|", "¢":
		t = "2/2"
	}
	i := strings.IndexByte(t, '/')
	if i <= 0 {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(t[:i]))
	if err != nil || n <= 0 || n > 32 {
		return fallback
	}
	d, err := strconv.Atoi(strings.TrimSpace(t[i+1:]))
	if err != nil {
		return fallback
	}
	switch d {
	case 4:
		return clickMeter{Beats: n, PerBpm: 1, Unit: "quarter", Known: true}
	case 2:
		return clickMeter{Beats: n, PerBpm: 1, Unit: "half", Known: true}
	case 8:
		if n >= 6 && n%3 == 0 {
			m := clickMeter{Beats: n, PerBpm: 3, Unit: "dotted-quarter", Known: true}
			pulses := n / 3
			for p := 0; p < pulses; p++ {
				m.Groups = append(m.Groups, 3)
			}
			if pulses%2 == 0 {
				m.Accents = []int{n / 2} // the middle of the bar: 4 in 6/8, 7 in 12/8
			} else {
				for p := 1; p < pulses; p++ {
					m.Accents = append(m.Accents, 3*p) // 4 and 7 in 9/8
				}
			}
			return m
		}
		m := clickMeter{Beats: n, PerBpm: 1, Unit: "eighth", Known: true}
		if g, ok := defaultEighthGroups[n]; ok {
			m.Groups = g
		} else if n > 4 {
			// Twos, the last one a three when the count is odd.
			for left := n; left > 0; {
				if left == 3 {
					m.Groups = append(m.Groups, 3)
					break
				}
				m.Groups = append(m.Groups, 2)
				left -= 2
			}
		}
		at := 0
		for k, g := range m.Groups {
			if k > 0 {
				m.Accents = append(m.Accents, at)
			}
			at += g
		}
		return m
	}
	return fallback
}
