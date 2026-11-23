package song

import (
	"fmt"
	"testing"
	"time"
)

// PlayOrder reads endings as the Jazz chart does (httpapi's jzExpand,
// which the song's bar count follows): these are jzExpand's own cases.
func TestPlayOrder(t *testing.T) {
	for _, c := range []struct{ grid, want string }{
		// The iReal export's way: the repeat sign after the last ending.
		{"|: (1,2,3.) Ab | Gm | (4.) Bb | Bb :|", "[0 1 0 1 0 1 2 3]"},
		// One ending for every pass is a plain 4x repeat.
		{"|: (1,2,3,4.) B | A . E . :|", "[0 1 0 1 0 1 0 1]"},
		// A last-pass-only ending: the bars before it are repeated.
		{"|: Gm | Bb | (4.) C :|", "[0 1 0 1 0 1 0 1 2]"},
		// The usual way, the first ending closed, and an ending of two bars.
		{"|: C | (1.) F | G :| (2.) Am | Am |", "[0 1 2 0 3 4]"},
		{"| C | F | G :|", "[0 1 2 0 1 2]"},
		{"|:3 C | F :| G", "[0 1 0 1 0 1 2]"},
		{"(1-3.) A | B", "[0 1]"},
	} {
		ms := Parse("title: T\n[A]\ngrid: " + c.grid + "\n").Sections[0].Measures
		if got := fmt.Sprint(PlayOrder(ms, 0)); got != c.want {
			t.Errorf("%s: played %s, want %s", c.grid, got, c.want)
		}
	}
}

// A count of millions is never played out: the limit stops it, and the
// range of a volta isn't listed.
func TestPlayOrderLimit(t *testing.T) {
	ms := Parse("title: T\n[A]\ngrid: |:200000000 A | B :|\n").Sections[0].Measures
	start := time.Now()
	if got := len(PlayOrder(ms, 6)); got != 6 {
		t.Errorf("%d bars, want 6", got)
	}
	if got := len(PlayOrder(ms, 0)); got != maxPlaySteps {
		t.Errorf("no limit: %d bars, want the %d cap", got, maxPlaySteps)
	}
	if got := len(VoltaPasses("1-200000000.")); got > maxVoltaRange+1 {
		t.Errorf("a volta range lists %d passes", got)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %s", d)
	}
}
