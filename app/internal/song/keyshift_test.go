package song

import "testing"

// TestKeyShift: how far a save transposed a song — only its chords moving
// all together is a transposition; the key: header alone changed is a
// label fix and moves nothing, nor does any other edit.
func TestKeyShift(t *testing.T) {
	const g = "title: S\nkey: G\n\n[V]\n[G]la [C]la [D7]la [Em]la\n"
	for _, c := range []struct {
		name, old, new string
		want           int
	}{
		{"header and chords up a tone", g, "title: S\nkey: A\n\n[V]\n[A]la [D]la [E7]la [F#m]la\n", 2},
		{"header alone put right: a label fix", g, "title: S\nkey: A\n\n[V]\n[G]la [C]la [D7]la [Em]la\n", 0},
		{"down the short way", g, "title: S\nkey: Eb\n\n[V]\n[Eb]la [Ab]la [Bb7]la [Cm]la\n", -4},
		{"a fourth up is a fifth down", "[V]\n[C]la [G]la\n", "[V]\n[F]la [C]la\n", 5},
		{"a tritone", "[V]\n[C]la [G]la\n", "[V]\n[F#]la [C#]la\n", -6},
		{"enharmonic, no move", "key: F#\n\n[V]\n[F#]la [B]la\n", "key: Gb\n\n[V]\n[Gb]la [Cb]la\n", 0},
		{"minor", "key: Am\n\n[V]\n[Am]la [Dm]la\n", "key: Bm\n\n[V]\n[Bm]la [Em]la\n", 2},
		{"a minor header put right alone", "key: Am\n\n[V]\n[Am]la [Dm]la\n", "key: Bm\n\n[V]\n[Am]la [Dm]la\n", 0},
		{"headers without chords: nothing moves", "key: C\n", "key: F\n", 0},
		{"relabelled to the relative key", "key: Am\n\n[V]\n[Am]la [C]la\n", "key: C\n\n[V]\n[Am]la [C]la\n", 0},
		{"no header, chords transposed", "[V]\n[G]la [C/G]la [D7]la\n", "[V]\n[A]la [D/A]la [E7]la\n", 2},
		{"header added with the transposition", "[V]\n[G]la [C]la\n", "key: F\n\n[V]\n[F]la [Bb]la\n", -2},
		{"header says one thing, chords another: the chords", "key: G\n\n[V]\n[G]la [C]la\n", "key: B\n\n[V]\n[A]la [D]la\n", 2},
		{"one chord changed is a fix", "[V]\n[G]la\n", "[V]\n[A]la\n", 0},
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
