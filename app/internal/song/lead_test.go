package song

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseLead(t *testing.T) {
	s := Parse("title: T\nlead: | D7 | . |\n\n[Intro]\ngrid: | G | C |\n")
	if s.Lead != 2 || len(s.LeadGrid) != 2 || s.LeadGrid[0].Tokens[0] != "D7" || s.LeadGrid[1].Tokens[0] != "." {
		t.Fatalf("lead = %d %+v", s.Lead, s.LeadGrid)
	}
	if len(s.Meta) != 1 || len(s.Sections[0].Measures) != 2 {
		t.Errorf("meta %v, Intro %d bars", s.Meta, len(s.Sections[0].Measures))
	}
	if got := Parse("title: T\nlead: 40\n").Lead; got != maxLead {
		t.Errorf("lead: 40 = %d, want capped at %d", got, maxLead)
	}
	for _, src := range []string{
		"title: T\nlead: 1\n\n[Intro]\ngrid: | G |\n",
		"title: T\nlead: |D7 | .|\n\n[Intro]\ngrid: | G |\n",
	} {
		s := Parse(src)
		again := Parse(Write(s))
		if !reflect.DeepEqual(s, again) {
			t.Errorf("round-trip mismatch\n%s", Write(s))
		}
		if w := Write(s); !strings.Contains(w, "title: T\nlead: ") {
			t.Errorf("Write puts the lead elsewhere:\n%s", w)
		}
	}
}

// "lead:" in the song's first section (the form it first had) is the
// song's; in a later section it's no field (a lyric line, as it reads).
func TestParseLeadInASection(t *testing.T) {
	s := Parse("[Intro]\nlead: 1\ngrid: | G |\n\n[Verse]\nlead: 2\ngrid: | C |\n")
	if s.Lead != 1 {
		t.Errorf("first section's lead: song lead %d", s.Lead)
	}
	if v := s.Sections[1]; len(v.Lyrics) != 1 {
		t.Errorf("a later section's lead: %d lyric lines", len(v.Lyrics))
	}
	if w := Write(s); !strings.HasPrefix(w, "lead: 1\n") {
		t.Errorf("Write:\n%s", w)
	}
	// A lyric that happens to start with "lead:" stays a lyric.
	s = Parse("[Verse]\nlead: the way\n")
	if s.Lead != 0 || len(s.Sections[0].Lyrics) != 1 {
		t.Fatalf("got lead %d, %d lyric lines", s.Lead, len(s.Sections[0].Lyrics))
	}
	// Repeat signs in a lead are dropped: it is played once.
	s = Parse("lead: |: G :|\n")
	if m := s.LeadGrid[0]; m.Open || m.Close {
		t.Errorf("lead bar kept its repeat sign: %+v", m)
	}
	// A score voice ends at a lead line.
	s = Parse("[Intro]\nscore[gtr]: e4 f g a\nlead: 1\n")
	if s.Lead != 1 || strings.Contains(s.Sections[0].Scores[0].Body, "lead") {
		t.Errorf("score swallowed the lead: %+v", s)
	}
}

func TestLeadMeasures(t *testing.T) {
	s := Parse("lead: | D7 |\n")
	s.Lead = 2 // a written bar and a rest
	ms := s.LeadMeasures()
	if len(ms) != 2 || !ms[0].Lead || ms[0].Tokens[0] != "D7" || ms[1].Tokens[0] != "R" || LeadBars(ms) != 2 || s.LeadIn() != 2 {
		t.Errorf("LeadMeasures = %+v", ms)
	}
	if (&Song{}).LeadMeasures() != nil {
		t.Error("no lead, bars")
	}
}

func TestTransposeLead(t *testing.T) {
	res, err := TransposeText("key: G\nlead: | G | D7 |\n\n[Intro]\ngrid: | G |\n", TransposeOptions{To: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "lead: | A | E7 |") {
		t.Errorf("transposed:\n%s", res.Text)
	}
	res, err = TransposeText("key: G\n\n[Intro]\nlead: | G |\ngrid: | G |\n\n[Verse]\nlead: 1\n", TransposeOptions{To: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "[Intro]\nlead: | A |") || !strings.Contains(res.Text, "lead: 1") {
		t.Errorf("transposed:\n%s", res.Text)
	}
}
