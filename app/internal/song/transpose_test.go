package song

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"app/internal/chordsym"
)

const transposeSrc = `title: Test tune
artist: Nobody
key: A
tempo: 96
chord: A X02220
chord: D/F# 2X0232
chord[G]: G 320003

# a comment with [A] in it stays as written
[Intro]
grid: |: A | D/F# :| (1.) E7:4. N.C. | (2.) % | R |
bars: 4

[Verse]
time: 6/8
note: Bass plays A pedal
score[bass]: \relative {
  A4 E C# D
}

| C#m7 | Bm7 . E |
Placeholder [A]words over [D/F#]here [Bridge] and [E7sus4]more
|but this bar line [F#m] is a lyric

[Chorus]
grid: | Dmaj7 | E~ | A:2 E:2 :|
`

func TestTransposeText(t *testing.T) {
	res, err := TransposeText(transposeSrc, TransposeOptions{To: "G"})
	if err != nil {
		t.Fatal(err)
	}
	want := `title: Test tune
artist: Nobody
key: G
tempo: 96
chord[A]: A X02220
chord[A]: D/F# 2X0232
chord: G 320003

# a comment with [A] in it stays as written
[Intro]
grid: |: G | C/E :| (1.) D7:4. N.C. | (2.) % | R |
bars: 4

[Verse]
time: 6/8
note: Bass plays A pedal
score[bass]: \relative {
  A4 E C# D
}

| Bm7 | Am7 . D |
Placeholder [G]words over [C/E]here [Bridge] and [D7sus4]more
|but this bar line [Em] is a lyric

[Chorus]
grid: | Cmaj7 | D~ | G:2 D:2 :|
`
	if res.Text != want {
		t.Errorf("TransposeText to G:\n%s\nwant:\n%s", res.Text, want)
	}
	if res.From != "A" || res.To != "G" || res.Semitones != -2 || res.Kept != 2 || res.Restored != 1 {
		t.Errorf("result = %+v", res)
	}

	s := Parse(res.Text)
	if got := names(s.Chords); !reflect.DeepEqual(got, []string{"G"}) {
		t.Errorf("own diagrams after transpose = %v, want [G]", got)
	}
	if got := names(s.DiagramsFor("A")); !reflect.DeepEqual(got, []string{"A", "D/F#"}) {
		t.Errorf("diagrams kept for A = %v", got)
	}

	// And back again: the original text, byte for byte.
	back, err := TransposeText(res.Text, TransposeOptions{To: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if back.Text != transposeSrc {
		t.Errorf("G back to A:\n%s\nwant the original:\n%s", back.Text, transposeSrc)
	}
}

func TestTransposeSpelling(t *testing.T) {
	cases := []struct {
		src, to, want string
	}{
		// Down to a flat key: everything spelled with flats.
		{"key: A\n[V]\ngrid: | A | F#m | C#m/G# | E |\n", "Ab", "key: Ab\n[V]\ngrid: | Ab | Fm | Cm/G | Eb |\n"},
		// Up to a sharp key.
		{"key: F\n[V]\ngrid: | F | Bb | Dm | C7 |\n", "E", "key: E\n[V]\ngrid: | E | A | C#m | B7 |\n"},
		// Minor keys, with a borrowed major V.
		{"key: Am\n[V]\ngrid: | Am | Dm/F | E7 | G |\n", "Em", "key: Em\n[V]\ngrid: | Em | Am/C | B7 | D |\n"},
		// The mode stays written the way the author wrote it.
		{"key: C minor\n[V]\n[Cm]la [Ab]la [G7]la\n", "Dm", "key: D minor\n[V]\n[Dm]la [Bb]la [A7]la\n"},
		// A borrowed bVII reads as a flat in a sharp key.
		{"key: C\n[V]\ngrid: | C | Bb | F | C |\n", "D", "key: D\n[V]\ngrid: | D | C | G | D |\n"},
		{"key: D\n[V]\ngrid: | D | C | G/B | D |\n", "E", "key: E\n[V]\ngrid: | E | D | A/C# | E |\n"},
	}
	for _, c := range cases {
		res, err := TransposeText(c.src, TransposeOptions{To: c.to})
		if err != nil {
			t.Errorf("%q to %s: %v", c.src, c.to, err)
			continue
		}
		if res.Text != c.want {
			t.Errorf("%q to %s =\n%q\nwant\n%q", c.src, c.to, res.Text, c.want)
		}
	}
}

func TestTransposeNoKeyHeader(t *testing.T) {
	src := "title: X\nartist: Y\nreference: https://example.com\nchord: Em 022000\n\n[V]\ngrid: | Em | C | G | D |\n"
	res, err := TransposeText(src, TransposeOptions{Semitones: 2})
	if err != nil {
		t.Fatal(err)
	}
	want := "title: X\nartist: Y\nkey: F#m\nreference: https://example.com\nchord[Em]: Em 022000\n\n[V]\ngrid: | F#m | D | A | E |\n"
	if res.Text != want {
		t.Errorf("got\n%q\nwant\n%q", res.Text, want)
	}
}

func TestTransposeErrors(t *testing.T) {
	if _, err := TransposeText("key: A\n[V]\n[A]x\n", TransposeOptions{To: "Em"}); !errors.Is(err, ErrModeChange) {
		t.Errorf("major to minor: err = %v, want ErrModeChange", err)
	}
	if _, err := TransposeText("key: A\n[V]\n[A]x\n", TransposeOptions{To: "H"}); !errors.Is(err, ErrUnknownKey) {
		t.Errorf("unknown key: err = %v, want ErrUnknownKey", err)
	}
	if _, err := TransposeText("title: words only\n[V]\nla la\n", TransposeOptions{Semitones: 1}); !errors.Is(err, ErrUnknownKey) {
		t.Errorf("no chords, no key: err = %v, want ErrUnknownKey", err)
	}
}

// TestTransposeMatchesParse runs every real fixture through TransposeText
// and checks, through Parse, that exactly the chords moved: same sections,
// bars, lyric text and score voices, and each chord token equal to that
// token transposed on its own. This is what keeps TransposeText's line
// classification in step with Parse's.
func TestTransposeMatchesParse(t *testing.T) {
	paths, err := filepath.Glob("testdata/*.song")
	if err != nil || len(paths) == 0 {
		t.Fatal("no fixtures")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			orig, _ := ParseFile(path)
			src := Write(orig)
			res, err := TransposeText(src, TransposeOptions{Semitones: 3})
			if errors.Is(err, ErrUnknownKey) {
				t.Skip("no key to start from")
			}
			if err != nil {
				t.Fatal(err)
			}
			to, _ := chordsym.ParseKey(res.To)
			tr := transposer{n: res.Semitones, to: to, flats: orig.Get("flats") == "yes"}
			got := Parse(res.Text)
			if len(got.Sections) != len(orig.Sections) {
				t.Fatalf("sections: %d, want %d", len(got.Sections), len(orig.Sections))
			}
			for i, sec := range orig.Sections {
				g := got.Sections[i]
				if !reflect.DeepEqual(g.Scores, sec.Scores) || !reflect.DeepEqual(g.Notes, sec.Notes) || g.Label != sec.Label {
					t.Errorf("section %d: scores, notes or label changed", i)
				}
				if len(g.Measures) != len(sec.Measures) || len(g.Lyrics) != len(sec.Lyrics) {
					t.Fatalf("section %d: bar or lyric count changed", i)
				}
				for j, m := range sec.Measures {
					want := make([]string, len(m.Tokens))
					for k, tok := range m.Tokens {
						want[k] = tr.token(tok)
					}
					gm := g.Measures[j]
					if !reflect.DeepEqual(gm.Tokens, want) || gm.Open != m.Open || gm.Close != m.Close || gm.Volta != m.Volta || gm.RepeatCount != m.RepeatCount {
						t.Errorf("section %d bar %d: %+v, want tokens %v of %+v", i, j, gm, want, m)
					}
				}
				for j, l := range sec.Lyrics {
					gl := g.Lyrics[j]
					if len(gl.Segments) != len(l.Segments) {
						t.Fatalf("section %d lyric %d: segment count changed", i, j)
					}
					for k, seg := range l.Segments {
						gs := gl.Segments[k]
						if gs.Text != seg.Text || gs.HasChord != seg.HasChord || (seg.HasChord && gs.Chord != tr.token(seg.Chord)) {
							t.Errorf("section %d lyric %d seg %d: %+v from %+v", i, j, k, gs, seg)
						}
					}
				}
			}
			if !reflect.DeepEqual(got.Chords, []ChordRef(nil)) && len(orig.Chords) > 0 {
				t.Errorf("own diagrams should all be kept under the old key, got %+v", got.Chords)
			}
			if len(got.OtherChords) != len(orig.Chords)+len(orig.OtherChords) {
				t.Errorf("diagrams: %d kept, want %d", len(got.OtherChords), len(orig.Chords)+len(orig.OtherChords))
			}
		})
	}
}

func TestParseKeyedDiagrams(t *testing.T) {
	src := "title: T\nkey: G\nchord: G 320003\nchord[A]: A X02220\nchord[G]: C X32010\nchord[Bbm]: Bbm X13321\nchord[A]: E 022100\n\n[V]\n[G]x\n"
	s := Parse(src)
	want := []ChordRef{{Name: "G", Spec: "320003"}, {Name: "C", Spec: "X32010", Key: "G"}}
	if !reflect.DeepEqual(s.Chords, want) {
		t.Errorf("Chords = %+v, want %+v", s.Chords, want)
	}
	if got := names(s.DiagramsFor("A")); !reflect.DeepEqual(got, []string{"A", "E"}) {
		t.Errorf("DiagramsFor(A) = %v", got)
	}
	if got := names(s.DiagramsFor("A#m")); !reflect.DeepEqual(got, []string{"Bbm"}) {
		t.Errorf("DiagramsFor(A#m) = %v (any spelling of the key)", got)
	}
	if got := names(s.DiagramsFor("G")); !reflect.DeepEqual(got, []string{"G", "C"}) {
		t.Errorf("DiagramsFor(G) = %v", got)
	}
	if got := s.DiagramsFor("D"); got != nil {
		t.Errorf("DiagramsFor(D) = %v, want none", got)
	}
	sets := s.DiagramSets()
	if len(sets) != 2 || sets[0].Key != "A" || len(sets[0].Chords) != 2 || sets[1].Key != "Bbm" {
		t.Errorf("DiagramSets = %+v", sets)
	}
	if again := Parse(Write(s)); !reflect.DeepEqual(s, again) {
		t.Errorf("round trip:\n%+v\n%+v", s, again)
	}
	if !strings.Contains(Write(s), "chord[A]: A X02220\n") {
		t.Errorf("Write lost the key tag:\n%s", Write(s))
	}
}

func TestAnalyze(t *testing.T) {
	a := Analyze("title: T\nchord: Am X02210\nchord[C]: C X32010\n\n[V]\ngrid: | Am:2 G:2 | F~ | N.C. | % |\n[Am]la [E7]la [Am]la\n")
	want := Analysis{
		Key: "Am", KeyGuessed: true,
		Chords:      []string{"Am", "G", "F", "E7"},
		Missing:     []string{"G", "F", "E7"},
		DiagramKeys: []string{"Am", "C"},
	}
	if !reflect.DeepEqual(a, want) {
		t.Errorf("Analyze = %+v, want %+v", a, want)
	}
}

func names(refs []ChordRef) []string {
	var out []string
	for _, r := range refs {
		out = append(out, r.Name)
	}
	return out
}
