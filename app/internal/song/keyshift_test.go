package song

import "testing"

// TestKeyShift: how far a save moved a song's own key — the key: header
// in the same mode, else the chart transposed in place; nothing for any
// other edit.
func TestKeyShift(t *testing.T) {
	const g = "title: S\nkey: G\n\n[V]\n[G]la [C]la [D7]la [Em]la\n"
	for _, c := range []struct {
		name, old, new string
		want           int
	}{
		{"header up a tone", g, "title: S\nkey: A\n\n[V]\n[A]la [D]la [E7]la [F#m]la\n", 2},
		{"header alone put right", g, "title: S\nkey: A\n\n[V]\n[G]la [C]la [D7]la [Em]la\n", 2},
		{"down the short way", g, "title: S\nkey: Eb\n\n[V]\n[Eb]la\n", -4},
		{"a fourth up is a fifth down", "key: C\n", "key: F\n", 5},
		{"a tritone", "key: C\n", "key: F#\n", -6},
		{"enharmonic, no move", "key: F#\n", "key: Gb\n", 0},
		{"minor", "key: Am\n", "key: Bm\n", 2},
		{"relabelled to the relative key: the chords say", "key: Am\n\n[V]\n[Am]la [C]la\n", "key: C\n\n[V]\n[Am]la [C]la\n", 0},
		{"no header, chords transposed", "[V]\n[G]la [C/G]la [D7]la\n", "[V]\n[A]la [D/A]la [E7]la\n", 2},
		{"header added with the transposition", "[V]\n[G]la [C]la\n", "key: F\n\n[V]\n[F]la [Bb]la\n", -2},
		{"a chord changed", "[V]\n[G]la [C]la\n", "[V]\n[G]la [Cmaj7]la\n", 0},
		{"a chord added", "[V]\n[G]la [C]la\n", "[V]\n[G]la [C]la [D]la\n", 0},
		{"not uniform", "[V]\n[G]la [C]la\n", "[V]\n[A]la [E]la\n", 0},
		{"same", g, g, 0},
		{"lyrics edited", g, "title: S\nkey: G\n\n[V]\n[G]lo [C]lo [D7]lo [Em]lo\n", 0},
		{"no chords at all", "title: Words\n\nla la\n", "title: Words\n\nlo lo\n", 0},
		{"header taken off, chords kept", g, "title: S\n\n[V]\n[G]la [C]la [D7]la [Em]la\n", 0},
	} {
		if got := KeyShift(c.old, c.new); got != c.want {
			t.Errorf("%s: KeyShift = %d, want %d", c.name, got, c.want)
		}
	}
}
