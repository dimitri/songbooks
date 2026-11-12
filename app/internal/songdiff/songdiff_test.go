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
score[Bass]: {"Tuning":[43,38,33,28],"Bars":[{"Beats":[{"Duration":"1","Notes":[{"MIDI":29,"String":4,"Fret":1}]}]},{"Beats":[{"Rest":true,"Duration":"1"}]}]}
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
score[Bass]: {"Tuning":[43,38,33,28],"Bars":[{"Beats":[{"Duration":"1","Notes":[{"MIDI":29,"String":4,"Fret":1}]}]},{"Beats":[{"Duration":"1","Notes":[{"MIDI":31,"String":4,"Fret":3}]}]}]}
score[Drums]: {"Drums":true,"Pattern":true,"Bars":[{"Beats":[{"Duration":"1","Notes":[{"MIDI":36}]}]}]}
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
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestLinesFoldsTabData(t *testing.T) {
	var minus, plus int
	for _, l := range Lines(v1, v2) {
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
