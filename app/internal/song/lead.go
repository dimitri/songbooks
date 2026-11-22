package song

import "strconv"

// A section's "lead:" line: bars played before its first bar — a count-in,
// a drummer's sticks, a guitar's pickup into the intro — written without
// an extra section or an extra counted bar:
//
//	[Intro]
//	lead: 1            one bar of rest before the Intro's bar 1
//	lead: 2            two of them
//	lead: | G |        one bar, playing G
//	lead: | . | D7 |   two bars, the second playing D7
//
// On the song's opening section (OpeningSection) the lead bars are the
// song's lead-in: drawn on the section's first row, ahead of its first
// bar, played (a full bar each), and never counted — bar 1 stays the
// section's first written bar, the lead bars before it read 0 (and -1, …
// for more than one). A "bars:" line counts the section's own bars only:
// the lead rides on the first row on top of them. On a later section
// there is no "before the song" for them to be: they are played as that
// section's first bars, numbered like the rest.

// maxLead bounds a "lead: N" count: a lead-in is a bar or two, and a
// typo ("lead: 1000") mustn't draw a thousand bars.
const maxLead = 16

// LeadLine is the value of the section's "lead:" line as Write writes it:
// the bars as a grid when they have chords, else the bare count; "" for
// none.
func (sec *Section) LeadLine() string {
	switch {
	case sec.Lead <= 0:
		return ""
	case len(sec.LeadGrid) > 0:
		return writeGrid(sec.LeadGrid)
	}
	return strconv.Itoa(sec.Lead)
}

// LeadMeasures is the section's lead bars, each marked Lead: the written
// ones ("lead: | G |"), or Lead bars of rest ("lead: 2").
func (sec *Section) LeadMeasures() []Measure {
	if sec.Lead <= 0 || sec.Repeat {
		return nil
	}
	out := make([]Measure, 0, sec.Lead)
	for i := 0; i < sec.Lead; i++ {
		m := Measure{Tokens: []string{"R"}}
		if i < len(sec.LeadGrid) {
			m = sec.LeadGrid[i]
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

// OpeningSection is the index of the section the song starts on — the
// first with bars of its own or a lead — whose "lead:" bars are the
// song's lead-in; -1 when no section has either.
func (s *Song) OpeningSection() int {
	for i, sec := range s.Sections {
		if sec.Repeat {
			continue
		}
		if len(sec.Measures) > 0 || sec.Lead > 0 {
			return i
		}
	}
	return -1
}

// LeadIn is how many bars the song plays before its bar 1: its opening
// section's "lead:" count (0 when it has none). On a song WithLeads
// already expanded, it counts the opening section's leading Lead bars.
func (s *Song) LeadIn() int {
	i := s.OpeningSection()
	if i < 0 {
		return 0
	}
	sec := &s.Sections[i]
	if sec.Lead > 0 {
		return len(sec.LeadMeasures())
	}
	return LeadBars(sec.Measures)
}

// LeadBars counts the Lead bars ms opens with.
func LeadBars(ms []Measure) int {
	n := 0
	for n < len(ms) && ms[n].Lead {
		n++
	}
	return n
}

// WithLeads is s as it is played: every section's lead bars written into
// its grid ahead of its first bar, marked Lead, and its Lead cleared. The
// song itself is left untouched (the copy shares everything but the
// sections it changes); a song without a "lead:" line is returned as is.
// Renderers that lay out or time bars read this; Write must never be
// given it (the lead would come back as grid bars).
func WithLeads(s *Song) *Song {
	var out *Song
	for i := range s.Sections {
		lead := s.Sections[i].LeadMeasures()
		if len(lead) == 0 {
			continue
		}
		if out == nil {
			cp := *s
			cp.Sections = append([]Section(nil), s.Sections...)
			out = &cp
		}
		sec := &out.Sections[i]
		sec.Measures = append(lead, sec.Measures...)
		sec.Lead, sec.LeadGrid = 0, nil
	}
	if out == nil {
		return s
	}
	return out
}
