package lilychord

import (
	"strings"
	"testing"
)

func join(tokens []string) string { return strings.Join(tokens, " ") }

// TestHotelCalifornia is the real Hotel California verse.ly — a plain
// \repeat volta 2 with no alternative, chosen because its chords are
// public knowledge (Bm F# A E G D Em F#), an independent check this
// parser isn't just internally self-consistent.
func TestHotelCalifornia(t *testing.T) {
	src := `
\chordmode {
  \tempo 4 = 74
  \repeat volta 2 { \bar ".|:"
    b1:m fis a e \break
    g d e:m fis
  }
}
`
	got, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Bm", "F#", "A", "E", "G", "D", "Em", "F#"}
	if len(got) != len(want) {
		t.Fatalf("got %d measures, want %d: %+v", len(got), len(want), got)
	}
	for i, m := range got {
		if join(m.Tokens) != want[i] {
			t.Errorf("measure %d = %q, want %q", i, join(m.Tokens), want[i])
		}
	}
	if !got[0].Open {
		t.Error("first measure should be Open")
	}
	if !got[len(got)-1].Close {
		t.Error("last measure should be Close")
	}
}

// TestTransposeAndAlternative is Still Got The Blues' verse.ly — exercises
// \transpose, inherited durations, and \alternative/\volta endings, with
// expected values computed by hand against the real published chords.
func TestTransposeAndAlternative(t *testing.T) {
	src := `
\chordmode {
  \transpose d b {
  \repeat volta 2 { \bar ".|:"
    d2:m7 f/g c:maj7 f:maj7 \break
    \alternative {
      \volta 1 { b2:m7.5- e2 a1:m }
      \volta 2 { b1:m7.5- e2:sus4 e }
    }
  }
  }
}
`
	got, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		tokens string
		volta  string
		open   bool
		close  bool
	}
	wants := []want{
		{"Bm7 D/E", "", true, false},
		{"Amaj7 Dmaj7", "", false, false},
		{"G#m7b5 C#", "1.", false, false},
		{"F#m", "", false, false},
		{"G#m7b5", "2.", false, false},
		{"C#sus4 C#", "", false, true},
	}
	if len(got) != len(wants) {
		t.Fatalf("got %d measures, want %d: %+v", len(got), len(wants), got)
	}
	for i, w := range wants {
		m := got[i]
		if join(m.Tokens) != w.tokens {
			t.Errorf("measure %d tokens = %q, want %q", i, join(m.Tokens), w.tokens)
		}
		if m.Volta != w.volta {
			t.Errorf("measure %d volta = %q, want %q", i, m.Volta, w.volta)
		}
		if m.Open != w.open {
			t.Errorf("measure %d open = %v, want %v", i, m.Open, w.open)
		}
		if m.Close != w.close {
			t.Errorf("measure %d close = %v, want %v", i, m.Close, w.close)
		}
	}
}
