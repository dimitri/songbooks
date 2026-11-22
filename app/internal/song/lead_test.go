package song

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseLead(t *testing.T) {
	src := "title: T\n\n[Intro]\nlead: 1\ngrid: | G | C |\n\n[Verse]\nlead: | D7 | . |\ngrid: | G |\n\n[Bridge]\nlead: 40\ngrid: | Em |\n"
	s := Parse(src)
	if got := s.Sections[0]; got.Lead != 1 || got.LeadGrid != nil || len(got.Measures) != 2 {
		t.Fatalf("Intro: lead %d grid %v, %d bars", got.Lead, got.LeadGrid, len(got.Measures))
	}
	v := s.Sections[1]
	if v.Lead != 2 || len(v.LeadGrid) != 2 || v.LeadGrid[0].Tokens[0] != "D7" || v.LeadGrid[1].Tokens[0] != "." {
		t.Fatalf("Verse lead = %d %+v", v.Lead, v.LeadGrid)
	}
	if s.Sections[2].Lead != maxLead {
		t.Errorf("lead: 40 = %d, want capped at %d", s.Sections[2].Lead, maxLead)
	}
	again := Parse(Write(s))
	if !reflect.DeepEqual(s, again) {
		t.Errorf("round-trip mismatch\n%s", Write(s))
	}
	if w := Write(s); !strings.Contains(w, "lead: 1\n") || !strings.Contains(w, "lead: |D7 | .|\n") {
		t.Errorf("Write:\n%s", w)
	}
}

func TestParseLeadNotALead(t *testing.T) {
	// A lyric that happens to start with "lead:" stays a lyric.
	s := Parse("[Verse]\nlead: the way\n")
	if s.Sections[0].Lead != 0 || len(s.Sections[0].Lyrics) != 1 {
		t.Fatalf("got lead %d, %d lyric lines", s.Sections[0].Lead, len(s.Sections[0].Lyrics))
	}
	// Repeat signs in a lead are dropped: it is played once.
	s = Parse("[Intro]\nlead: |: G :|\n")
	if m := s.Sections[0].LeadGrid[0]; m.Open || m.Close {
		t.Errorf("lead bar kept its repeat sign: %+v", m)
	}
	// A score voice ends at a lead line.
	s = Parse("[Intro]\nscore[gtr]: e4 f g a\nlead: 1\n")
	if s.Sections[0].Lead != 1 || strings.Contains(s.Sections[0].Scores[0].Body, "lead") {
		t.Errorf("score swallowed the lead: %+v", s.Sections[0])
	}
}

func TestWithLeads(t *testing.T) {
	s := Parse("[Intro]\nlead: 2\ngrid: | G | C |\n\n[Verse]\nlead: | D7 |\ngrid: | G |\n\n[Outro] (repeat)\n")
	if s.OpeningSection() != 0 || s.LeadIn() != 2 {
		t.Fatalf("opening %d lead-in %d", s.OpeningSection(), s.LeadIn())
	}
	p := WithLeads(s)
	if p == s {
		t.Fatal("WithLeads returned the song itself")
	}
	if s.Sections[0].Lead != 2 || len(s.Sections[0].Measures) != 2 {
		t.Error("WithLeads changed the song it was given")
	}
	in := p.Sections[0].Measures
	if len(in) != 4 || !in[0].Lead || !in[1].Lead || in[2].Lead || in[0].Tokens[0] != "R" || in[2].Tokens[0] != "G" {
		t.Errorf("Intro played = %+v", in)
	}
	if LeadBars(in) != 2 || p.LeadIn() != 2 {
		t.Errorf("LeadBars = %d, LeadIn = %d", LeadBars(in), p.LeadIn())
	}
	if v := p.Sections[1].Measures; len(v) != 2 || !v[0].Lead || v[0].Tokens[0] != "D7" {
		t.Errorf("Verse played = %+v", v)
	}
	if WithLeads(p) != p {
		t.Error("WithLeads of an expanded song isn't itself")
	}
	plain := Parse("[Intro]\ngrid: | G |\n")
	if WithLeads(plain) != plain || plain.LeadIn() != 0 {
		t.Error("a song without a lead changed")
	}
	// A lead on a section with no grid of its own still opens the song.
	s = Parse("[Intro]\nlead: 1\nsome words\n\n[Verse]\ngrid: | G |\n")
	if s.OpeningSection() != 0 || s.LeadIn() != 1 {
		t.Errorf("lead-only intro: opening %d lead-in %d", s.OpeningSection(), s.LeadIn())
	}
}

func TestTransposeLead(t *testing.T) {
	res, err := TransposeText("key: G\n\n[Intro]\nlead: | G | D7 |\ngrid: | G |\n\n[Verse]\nlead: 1\n", TransposeOptions{To: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "lead: | A | E7 |") || !strings.Contains(res.Text, "lead: 1") {
		t.Errorf("transposed:\n%s", res.Text)
	}
}
