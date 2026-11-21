package chartalign

import (
	"strconv"
	"strings"

	"app/internal/song"
)

// SectionInfo is a section as the player numbers its bars (httpapi's
// SyncSections, from the code the chart renders with).
type SectionInfo struct {
	Index     int
	Label     string
	StartBar  int
	BarCount  int
	HasLyrics bool
}

// BeatsOf is how many beats a time signature's bar has, the way the
// player counts them (compound metres in dotted quarters).
func BeatsOf(sig string) int {
	n, d, ok := strings.Cut(strings.TrimSpace(sig), "/")
	if !ok {
		return 4
	}
	num, err := strconv.Atoi(strings.TrimSpace(n))
	if err != nil || num <= 0 {
		return 4
	}
	if strings.TrimSpace(d) == "8" && num%3 == 0 && num > 3 {
		return num / 3
	}
	return num
}

// MaxBars is the most bars of a chart that are laid on a recording: a
// song is a few hundred. Past it (a "|:2000000" typed in, a "times:
// 100000"), the chart isn't read further and the alignment says it's too
// long rather than unrolling it.
const MaxBars = 2000

// Chart reads the song's sections as they're played: every bar in order
// (repeats and endings unrolled as the song's bar count reads them,
// song.PlayOrder; "times:" applied; a bare section given the bars of the
// earlier one of the same name), a chord per beat. The bars of each
// section number from info's StartBar, as in the player. No more than
// MaxBars bars are read in all: a section cut short says so (Cut).
func Chart(sg *song.Song, info []SectionInfo) []Section {
	global := BeatsOf(orDefault(sg.Get("time"), "4/4"))
	var out []Section
	budget := MaxBars
	for _, in := range info {
		if in.Index < 0 || in.Index >= len(sg.Sections) {
			continue
		}
		// The bars this section may take: the player's count, within what's
		// left of MaxBars.
		want := in.BarCount
		if want <= 0 {
			want = MaxBars
		}
		limit := min(want, budget)
		sec := sg.Sections[in.Index]
		def := in.Index
		if len(sec.Measures) == 0 {
			// A bare section plays the bars of the earlier one of its name;
			// a bare solo, those of the section it names ("Solo (Chorus)"),
			// else of the nearest one with a grid (as the player counts it).
			want := strings.TrimSpace(sec.Label)
			solo := strings.HasPrefix(strings.ToLower(want), "solo")
			if solo {
				want = ""
				if o, c := strings.IndexByte(sec.Label, '('), strings.LastIndexByte(sec.Label, ')'); o >= 0 && c > o {
					want = strings.TrimSpace(sec.Label[o+1 : c])
				}
			}
			def = -1
			for j := in.Index - 1; j >= 0 && want != ""; j-- {
				if strings.EqualFold(strings.TrimSpace(sg.Sections[j].Label), want) && len(sg.Sections[j].Measures) > 0 {
					def = j
					break
				}
			}
			if def < 0 && !solo && in.BarCount > 0 {
				// Lyrics without a grid that the player still gives bars
				// to: the changes of the nearest grid of that length.
				for j := in.Index - 1; j >= 0; j-- {
					d := sg.Sections[j]
					if len(d.Measures) == 0 {
						continue
					}
					if len(unroll(d.Measures, j, global, in.BarCount+1))*max(1, sec.Times) == in.BarCount {
						def = j
						break
					}
				}
			}
			if def < 0 && solo {
				for j := in.Index - 1; j >= 0; j-- {
					if len(sg.Sections[j].Measures) > 0 {
						def = j
						break
					}
				}
			}
		}
		s := Section{Index: in.Index, Label: in.Label, StartBar: in.StartBar, HasLyrics: in.HasLyrics, Count: max(0, in.BarCount),
			Intro: strings.Contains(strings.ToLower(sec.Label), "intro")}
		if def >= 0 && limit > 0 {
			d := sg.Sections[def]
			B := global
			if d.Time != "" {
				B = BeatsOf(d.Time)
			}
			once := unroll(d.Measures, def, B, limit)
			// "times:" plays it again, as far as the limit.
			for t := 0; t < max(1, sec.Times) && len(once) > 0 && len(s.Bars) < limit; t++ {
				s.Bars = append(s.Bars, once[:min(len(once), limit-len(s.Bars))]...)
			}
		}
		// The player's count wins: past it, bar numbers would be wrong;
		// short of it, unknown bars keep the next sections in place.
		if in.BarCount > 0 {
			if len(s.Bars) > in.BarCount {
				s.Bars = s.Bars[:in.BarCount]
			}
			for len(s.Bars) < limit {
				s.Bars = append(s.Bars, Bar{Beats: make([]string, global), DefSection: -1, Measure: -1})
			}
		}
		if len(s.Bars) > limit {
			s.Bars = s.Bars[:limit]
		}
		s.Cut = (in.BarCount > 0 && in.BarCount > limit) || (in.BarCount <= 0 && limit > 0 && len(s.Bars) >= limit)
		budget -= len(s.Bars)
		out = append(out, s)
	}
	return out
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// unroll plays a grid's measures, at most limit bars: repeats, their
// counts and endings in song.PlayOrder's order, "%" and "%%" the bars
// played just before.
func unroll(ms []song.Measure, def, B, limit int) []Bar {
	order := song.PlayOrder(ms, limit)
	out := make([]Bar, 0, len(order))
	for _, i := range order {
		m := ms[i]
		switch {
		case len(m.Tokens) == 1 && m.Tokens[0] == "%" && len(out) > 0:
			out = append(out, out[len(out)-1])
		case len(m.Tokens) == 1 && m.Tokens[0] == "%%" && len(out) > 1:
			out = append(out, out[len(out)-2], out[len(out)-1])
		default:
			bb := B
			if m.Meter != "" {
				bb = BeatsOf(m.Meter)
			}
			beats, slots := expand(m.Tokens, bb)
			out = append(out, Bar{Beats: beats, Slots: slots, DefSection: def, Measure: i})
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// expand gives a measure's chord on each beat and the token it comes
// from: one chord fills the bar, two share it, "." holds the chord
// before; "R" is a rest (N.C.), "?" unknown.
func expand(tokens []string, B int) ([]string, []int) {
	toks := make([]string, len(tokens))
	for i, t := range tokens {
		toks[i] = song.TokenChord(t)
	}
	if len(toks) == 0 {
		return make([]string, B), fill(B, -1)
	}
	hasDot := false
	for _, t := range toks {
		if t == "." {
			hasDot = true
		}
	}
	norm := func(t string) string {
		switch t {
		case "R", "r":
			return "N.C."
		case "?":
			return ""
		}
		return t
	}
	var beats []string
	var slots []int
	if !hasDot && len(toks) < B && B%len(toks) == 0 {
		span := B / len(toks)
		for i, t := range toks {
			for k := 0; k < span; k++ {
				beats = append(beats, norm(t))
				slots = append(slots, i)
			}
		}
		return beats, slots
	}
	last, lastSlot := "", -1
	for i, t := range toks {
		if t == "." {
			beats = append(beats, last)
			slots = append(slots, lastSlot)
			continue
		}
		last, lastSlot = norm(t), i
		beats = append(beats, last)
		slots = append(slots, i)
	}
	for len(beats) < B {
		beats = append(beats, last)
		slots = append(slots, lastSlot)
	}
	return beats, slots
}

func fill(n, v int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = v
	}
	return out
}
