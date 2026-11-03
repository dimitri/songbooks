package song

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestRoundTripFixtures parses every real .song file in the repo, writes it
// back out, and reparses — the two parsed Songs must be structurally
// identical. This is a semantic round-trip, not a byte-for-byte one: Write
// regenerates canonical formatting, so it doesn't need to reproduce a human
// author's original whitespace/line-wrapping, only preserve meaning.
func TestRoundTripFixtures(t *testing.T) {
	paths, err := filepath.Glob("../../../v2/*/songs/*.song")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no .song fixtures found — check the glob path")
	}

	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			first, err := ParseFile(path)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			again := Parse(Write(first))
			if !reflect.DeepEqual(first, again) {
				t.Errorf("round-trip mismatch for %s\n--- first ---\n%+v\n--- again ---\n%+v", path, first, again)
			}
		})
	}
}

func TestParseGridBasics(t *testing.T) {
	s := Parse("title: X\n\n[Verse]\ngrid: |: Gm | Bb . D . :|\n[Gm]Some [Bb]words\n")
	if len(s.Sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(s.Sections))
	}
	sec := s.Sections[0]
	if len(sec.Measures) != 2 {
		t.Fatalf("expected 2 measures, got %d", len(sec.Measures))
	}
	if !sec.Measures[0].Open {
		t.Error("expected first measure to open the repeat")
	}
	if !reflect.DeepEqual(sec.Measures[0].Tokens, []string{"Gm"}) {
		t.Errorf("unexpected tokens: %v", sec.Measures[0].Tokens)
	}
	if !reflect.DeepEqual(sec.Measures[1].Tokens, []string{"Bb", ".", "D", "."}) {
		t.Errorf("unexpected tokens: %v", sec.Measures[1].Tokens)
	}
	if !sec.Measures[1].Close {
		t.Error("expected second measure to close the repeat")
	}
	if len(sec.Lyrics) != 1 || len(sec.Lyrics[0].Segments) != 2 {
		t.Fatalf("unexpected lyrics: %+v", sec.Lyrics)
	}
}

func TestParseRepeatSectionEmptyBody(t *testing.T) {
	s := Parse("title: X\n\n[Chorus]\ngrid: | G | C |\nla la\n\n[Chorus] (repeat)\n")
	if len(s.Sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(s.Sections))
	}
	rep := s.Sections[1]
	if !rep.Repeat || rep.Label != "Chorus" {
		t.Errorf("unexpected repeat section: %+v", rep)
	}
	if len(rep.Measures) != 0 || len(rep.Lyrics) != 0 {
		t.Errorf("expected empty body for (repeat) section, got %+v", rep)
	}
}

func TestParseBarsOverride(t *testing.T) {
	s := Parse("title: X\n\n[Verse]\nbars: 2\ngrid: |: Em | G :|\n")
	if !reflect.DeepEqual(s.Sections[0].BarsPerLine, []int{2}) {
		t.Errorf("unexpected BarsPerLine: %v", s.Sections[0].BarsPerLine)
	}
}

func TestParseTimesOnBareSection(t *testing.T) {
	s := Parse("title: X\n\n[Intro]\ngrid: | Gm | Bb |\n\n[Solo]\ntimes: 2\n")
	if len(s.Sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(s.Sections))
	}
	sec := s.Sections[1]
	if sec.Times != 2 {
		t.Errorf("expected Times 2, got %d", sec.Times)
	}
	if len(sec.Measures) != 0 || len(sec.Lyrics) != 0 {
		t.Errorf("expected a bare section (times: alone shouldn't count as content), got %+v", sec)
	}
}

func TestWriteTimesRoundTrip(t *testing.T) {
	s := Parse("title: X\n\n[Solo]\ntimes: 2\n")
	again := Parse(Write(s))
	if !reflect.DeepEqual(s, again) {
		t.Errorf("round-trip mismatch\n--- first ---\n%+v\n--- again ---\n%+v", s, again)
	}
}
