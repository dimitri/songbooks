package httpapi

// Played bars and drawn bars. The player counts the bars it plays (the
// clock, a recording's sync, StartBar/BarCount): a "|: … :|" phrase is
// counted on each pass, a 1st/2nd ending once on the pass that takes it,
// a "times:" section once per time. The chart draws each written bar
// once. Lighting the bar being played means going from the first to the
// second: every drawn bar carries the played bars it is read from
// (viewerMeasure.Played, data-played), and song.js lights the drawn bar
// whose list holds the played one.

import (
	"strconv"
	"strings"

	"app/internal/song"
)

// playOrder is, for each of a section's played bars in turn, the written
// bar of its grid (an index into ms) it is read from: the grid's bars in
// the order its repeat signs and endings play them (jzExpand), pass after
// pass ("times:", a bare repeat of the grid) for as many bars as the
// section plays. nil when the grid has no bars.
func playOrder(ms []song.Measure, bars int) []int {
	exp := jzExpand(ms)
	if len(exp) == 0 || bars <= 0 {
		return nil
	}
	out := make([]int, bars)
	for k := range out {
		out[k] = exp[k%len(exp)]
	}
	return out
}

// assignPlayedBars gives each drawn bar of the Song view's rows (and so
// of the Map, which draws the same measures) the played bars it is read
// from: section by section, its grid's play order (playOrder) from its
// StartBar. The song's lead bars are played once, before bar 1, at their
// GlobalBar. A section drawing a stand-in for another's grid (a Solo's
// placeholder bars) is read the same way, on that grid. A section drawing
// no bars of its own (a bare repeat, "same as …") has nothing here; its
// section lights instead (and the Map's recall cell, mapCell.Played).
func assignPlayedBars(sg *song.Song, sections []viewerSection) {
	for i := range sections {
		sec := &sections[i]
		byWritten := map[int][]int{}
		if g := sec.GridFrom; g >= 0 && g < len(sg.Sections) {
			for k, w := range playOrder(sg.Sections[g].Measures, sec.BarCount) {
				byWritten[w] = append(byWritten[w], sec.StartBar+k)
			}
		}
		// The drawn bars after the lead are the grid's written bars in
		// order (a placeholder Solo's too, whose Index restarts on each
		// row): counted, not read from Index.
		w := 0
		set := func(m *viewerMeasure) {
			if m.Lead {
				m.Played = []int{m.GlobalBar}
				return
			}
			m.Played = byWritten[w]
			w++
		}
		for mi := range sec.Measures {
			set(&sec.Measures[mi])
		}
		w = 0
		for li := range sec.Lines {
			for mi := range sec.Lines[li].Measures {
				set(&sec.Lines[li].Measures[mi])
			}
		}
	}
}

// PlayedAttr is m.Played for data-played: space-separated, "" for none
// (the bar is then read at its GlobalBar).
func (m viewerMeasure) PlayedAttr() string { return joinBars(m.Played) }

func joinBars(bars []int) string {
	s := make([]string, len(bars))
	for i, b := range bars {
		s[i] = strconv.Itoa(b)
	}
	return strings.Join(s, " ")
}

// spanBars is the played bars from..from+n-1.
func spanBars(from, n int) []int {
	out := make([]int, max(0, n))
	for i := range out {
		out[i] = from + i
	}
	return out
}
