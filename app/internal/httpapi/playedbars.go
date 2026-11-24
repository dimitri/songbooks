package httpapi

// Played bars and drawn bars. The player counts the bars it plays (the
// clock, a recording's sync, StartBar/BarCount): a "|: … :|" phrase is
// counted on each pass, a 1st/2nd ending once on the pass that takes it,
// a "times:" section once per time. The chart draws each written bar
// once, and a section replaying an earlier one's grid (a bare "Verse 2",
// "same as Chorus 1") draws none. Lighting the bar being played means
// going from the first to the second: every drawn bar carries the played
// bars it is read from (viewerMeasure.Played, data-played) — its own
// section's passes and those of the sections replaying it — and song.js
// lights the drawn bar whose list holds the played one.

import (
	"slices"
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

// assignPlayedBars sets each section's PlayOrder and GridSame, then gives
// each bar of the Song view's rows its played bars (lightedBars): a
// section shown as a chip (recalled) lights the rows it replays.
// The song's lead bars are played once, before bar 1, at their GlobalBar.
// The Map draws its own set of sections (buildMapLines, mapPlayed).
func assignPlayedBars(sg *song.Song, sections []viewerSection) {
	globalBeats := timeSigBeats(orDefault(sg.Get("time"), "4/4"))
	// The first section writing the same grid as section g.
	same := map[int]int{}
	sameAs := func(g int) int {
		if c, ok := same[g]; ok {
			return c
		}
		c := g
		for j := 0; j < g; j++ {
			a, b := sg.Sections[j], sg.Sections[g]
			if len(a.Measures) > 0 && measuresEqual(a.Measures, b.Measures, sectionBeatsPerBar(a, globalBeats), sectionBeatsPerBar(b, globalBeats)) {
				c = j
				break
			}
		}
		same[g] = c
		return c
	}
	for i := range sections {
		sec := &sections[i]
		sec.PlayOrder, sec.GridSame = nil, -1
		if g := sec.GridFrom; g >= 0 && g < len(sg.Sections) && len(sg.Sections[g].Measures) > 0 {
			sec.PlayOrder = playOrder(sg.Sections[g].Measures, sec.BarCount)
			sec.GridSame = sameAs(g)
		}
	}
	lit := lightedBars(sections, func(i int) bool { return len(sections[i].Lines) > 0 && !recalled(sections[i]) })
	for i := range sections {
		sec := &sections[i]
		for mi := range sec.Measures {
			setPlayed(&sec.Measures[mi], lit[i], mi-sec.LeadBars)
		}
		w := 0
		for li := range sec.Lines {
			for mi := range sec.Lines[li].Measures {
				m := &sec.Lines[li].Measures[mi]
				setPlayed(m, lit[i], w)
				if !m.Lead {
					w++
				}
			}
		}
	}
}

// setPlayed gives m, the drawn bar showing written bar w of its section's
// grid, its played bars from byWritten (lightedBars); a lead bar its own.
func setPlayed(m *viewerMeasure, byWritten map[int][]int, w int) {
	if m.Lead {
		m.Played = []int{m.GlobalBar}
		return
	}
	m.Played = byWritten[w]
}

// lightedBars maps each section's written bars (counted after the lead;
// a placeholder Solo's in the order of the grid it solos over) to the
// played bars they light, for a view drawing the sections drawn(i) says
// it does: the section's own, and on a drawn section's those of each
// section that isn't drawn and replays its grid — read from the nearest
// earlier drawn section writing that grid (the previous Verse for a bare
// "Verse 2"), pass by pass as that section's PlayOrder reads it.
func lightedBars(sections []viewerSection, drawn func(i int) bool) map[int]map[int][]int {
	out := map[int]map[int][]int{}
	add := func(d, w, p int) {
		if out[d] == nil {
			out[d] = map[int][]int{}
		}
		out[d][w] = append(out[d][w], p)
	}
	for i := range sections {
		sec := &sections[i]
		if len(sec.PlayOrder) == 0 {
			continue
		}
		// Its own bars (a recalled section's, hidden, keep its passes).
		for k, w := range sec.PlayOrder {
			add(i, w, sec.StartBar+k)
		}
		if drawn(i) {
			continue
		}
		d := -1
		for j := i - 1; j >= 0; j-- {
			o := &sections[j]
			if drawn(j) && o.GridFrom == j && o.GridSame == sec.GridSame {
				d = j
				break
			}
		}
		if d < 0 {
			continue
		}
		for k, w := range sec.PlayOrder {
			add(d, w, sec.StartBar+k)
		}
	}
	for _, byW := range out {
		for _, ps := range byW {
			slices.Sort(ps)
		}
	}
	return out
}

// PlayedAttr is m.Played for data-played: space-separated, "" for none
// (the bar is then read at its GlobalBar).
func (m viewerMeasure) PlayedAttr() string { return joinBars(m.Played) }

// PlayedBars is, for an alphaTab tab row (play.html), each of its bars'
// played bars, comma-separated, a bar's passes space-separated
// ("0 8,1 9,2,3"); alphatab-tab.js puts its cursor on the bar whose list
// holds the played one.
func (l viewerLine) PlayedBars() string {
	s := make([]string, len(l.Measures))
	for i, m := range l.Measures {
		if len(m.Played) == 0 {
			s[i] = strconv.Itoa(m.GlobalBar)
			continue
		}
		s[i] = joinBars(m.Played)
	}
	return strings.Join(s, ",")
}

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
