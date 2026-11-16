package songdiff

import (
	"strings"
	"testing"
)

const v1 = `title: T
tempo: 96

[Verse]
grid: | Am | Am | G | E7 |
la la
la di

[Chorus]
grid: | F | G | C | C |
score[Bass]: f,1 r1
gp[Lead]: 1-4
`

const v2 = `title: T
tempo: 102

[Verse]
grid: | Am | Am7 | G | E7 |
la la
la do

[Bridge]
grid: | Dm | E |

[Chorus]
grid: | F | G | C | C |
score[Bass]: f,1 g,1
score[Drums]: bd1
gp[Lead]: 1-8
gp[Rhythm]: 1-8
`

func TestDescribe(t *testing.T) {
	var got []string
	for _, c := range Describe(v1, v2) {
		got = append(got, c.Where+": "+c.What)
	}
	want := []string{
		"Header: tempo 96 → 102",
		"Verse: chords, bar 2: Am → Am7",
		"Verse: lyrics: 1 line changed",
		"Bridge: section added (2 bars)",
		"Bass · Chorus: notes changed in bar 2",
		"Drums · Chorus: track added (1 bar)",
		"Lead · Chorus: bars from the Guitar Pro file 1-4 → 1-8",
		"Rhythm · Chorus: bars from the Guitar Pro file added (1-8)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestLinesFoldsTabData(t *testing.T) {
	// Revisions from before gp[...] lines held notes as JSON.
	a := "[Song]\nscore[Gtr]: {\"Tuning\":[64],\"Bars\":[]}\n"
	b := "[Song]\nscore[Gtr]: {\"Tuning\":[64,59],\"Bars\":[]}\n"
	var minus, plus int
	for _, l := range Lines(a, b) {
		if strings.Contains(l.Text, `{"`) {
			t.Fatalf("tab data shown as text: %q", l.Text)
		}
		switch l.Op {
		case "-":
			minus++
		case "+":
			plus++
		}
	}
	if minus == 0 || plus == 0 {
		t.Fatal("no changes in the line diff")
	}
}
