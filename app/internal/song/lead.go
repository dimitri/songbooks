package song

import "strconv"

// The song's "lead:" header line: bars played before it starts — a
// count-in, a drummer's sticks, a guitar's pickup into the intro —
// written without a section or a counted bar of their own:
//
//	title: …
//	lead: 1            one bar of rest before bar 1
//	lead: 2            two of them
//	lead: | G |        one bar, playing G
//	lead: | . | D7 |   two bars, the second playing D7
//
//	[Intro]
//	grid: …            bar 1
//
// The lead belongs to no section: the song starts with it, then its
// first section starts at bar 1. Played (a full bar each), drawn on the
// first section's first row ahead of its bar 1, never counted — the lead
// bars read 0 (and -1, … for more than one) — in no section's bars, so
// looping a section never plays them; playing from the start does.
//
// A "lead:" line inside the song's first section is read as the song's
// (the form it first had); inside a later one it's no field at all.

// maxLead bounds a "lead: N" count: a lead-in is a bar or two, and a
// typo ("lead: 1000") mustn't draw a thousand bars.
const maxLead = 16

// LeadLine is the value of the song's "lead:" line as Write writes it:
// the bars as a grid when they have chords, else the bare count; "" for
// none.
func (s *Song) LeadLine() string {
	switch {
	case s.Lead <= 0:
		return ""
	case len(s.LeadGrid) > 0:
		return writeGrid(s.LeadGrid)
	}
	return strconv.Itoa(s.Lead)
}

// LeadMeasures is the song's lead bars, each marked Lead: the written
// ones ("lead: | G |"), or Lead bars of rest ("lead: 2").
func (s *Song) LeadMeasures() []Measure {
	if s.Lead <= 0 {
		return nil
	}
	out := make([]Measure, 0, s.Lead)
	for i := 0; i < s.Lead; i++ {
		m := Measure{Tokens: []string{"R"}}
		if i < len(s.LeadGrid) {
			m = s.LeadGrid[i]
			m.Tokens = append([]string(nil), m.Tokens...)
			if len(m.Tokens) == 0 {
				m.Tokens = []string{"R"}
			}
		}
		m.Lead = true
		out = append(out, m)
	}
	return out
}

// LeadIn is how many bars the song plays before its bar 1.
func (s *Song) LeadIn() int { return max(0, s.Lead) }

// LeadBars counts the Lead bars ms opens with (a renderer that drew the
// song's lead ahead of its first section's bars).
func LeadBars(ms []Measure) int {
	n := 0
	for n < len(ms) && ms[n].Lead {
		n++
	}
	return n
}
