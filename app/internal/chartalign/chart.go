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

// Chart reads the song's sections as they're played: every bar in order
// (repeats and endings unrolled, "times:" applied, a bare section given
// the bars of the earlier one of the same name), a chord per beat. The
// bars of each section number from info's StartBar, as in the player.
func Chart(sg *song.Song, info []SectionInfo) []Section {
	global := BeatsOf(orDefault(sg.Get("time"), "4/4"))
	var out []Section
	for _, in := range info {
		if in.Index < 0 || in.Index >= len(sg.Sections) {
			continue
		}
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
					if len(d.Measures) > 0 && len(unroll(d.Measures, j, global))*max(1, sec.Times) == in.BarCount {
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
		s := Section{Index: in.Index, Label: in.Label, StartBar: in.StartBar, HasLyrics: in.HasLyrics,
			Intro: strings.Contains(strings.ToLower(sec.Label), "intro")}
		if def >= 0 {
			d := sg.Sections[def]
			B := global
			if d.Time != "" {
				B = BeatsOf(d.Time)
			}
			once := unroll(d.Measures, def, B)
			times := sec.Times
			if times < 1 {
				times = 1
			}
			for t := 0; t < times; t++ {
				s.Bars = append(s.Bars, once...)
			}
		}
		// The player's count wins: past it, bar numbers would be wrong;
		// short of it, unknown bars keep the next sections in place.
		if in.BarCount > 0 {
			if len(s.Bars) > in.BarCount {
				s.Bars = s.Bars[:in.BarCount]
			}
			for len(s.Bars) < in.BarCount {
				s.Bars = append(s.Bars, Bar{Beats: make([]string, global), DefSection: -1, Measure: -1})
			}
		}
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

// unroll plays a grid's measures: repeats, their counts and endings,
// "%" and "%%".
func unroll(ms []song.Measure, def, B int) []Bar {
	var out []Bar
	open, pass, total := -1, 1, 2
	for i := 0; i < len(ms); {
		m := ms[i]
		if m.Open && open != i {
			open, pass, total = i, 1, m.RepeatCount
			if total <= 0 {
				total = 2
			}
		}
		if m.Volta != "" && !voltaHas(m.Volta, pass) {
			i++
			continue
		}
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
		if m.Close && open >= 0 && pass < total {
			pass++
			i = open
			continue
		}
		i++
	}
	return out
}

func voltaHas(v string, pass int) bool {
	for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '.' || r == ' ' || r == '-' }) {
		if n, err := strconv.Atoi(f); err == nil && n == pass {
			return true
		}
	}
	return false
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
