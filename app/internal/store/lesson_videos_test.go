package store

import "testing"

func TestYouTubeID(t *testing.T) {
	for in, want := range map[string]string{
		"https://youtu.be/GpGQ4YTm5bY":                                   "GpGQ4YTm5bY",
		"https://www.youtube.com/watch?v=uU424xUH_Cw&t=228s":             "uU424xUH_Cw",
		"https://www.youtube.com/watch?v=olmeLRNfj30&list=RDolmeLRNfj30": "olmeLRNfj30",
		"https://www.youtube.com/shorts/abc123":                          "abc123",
		"https://florentpassamonti.fr/":                                  "",
		"https://www.youtube.com/@FlorentPassamonti":                     "",
	} {
		if got := YouTubeID(in); got != want {
			t.Errorf("YouTubeID(%q) = %q, want %q", in, got, want)
		}
	}
}

// A book's text often comes a letter at a time, its numbers in figures
// where the files spell them.
func TestNameScore(t *testing.T) {
	spaced := func(s string) string { // as Canva's glyph-by-glyph text reads
		out := ""
		for _, r := range matchKey(s) {
			if r != ' ' {
				out += string(r) + " "
			}
		}
		return out
	}
	riffs := linkText{Own: spaced("3 riffs blues en 1"), Near: spaced("3 riffs blues en 1 Tonalité : A")}
	plan := linkText{Own: spaced("Plan Blues"), Near: spaced("Plan Blues Tonalité : D Quand Freddie, BB King et T-Bone Walker se rencontrent")}
	tbone := linkText{Own: spaced("Rythmique à la T-Bone Walker"), Near: spaced("Rythmique à la T-Bone Walker Tonalité : G")}
	cases := []struct {
		names []string
		at    linkText
		ok    bool
	}{
		{[]string{"Trois riffs blues en un"}, riffs, true},
		{[]string{"Plan Blues 3 en 1"}, riffs, false}, // its figures alone don't name it
		{[]string{"T-Bone Walker Blues"}, tbone, true},
		{[]string{"5 plans Blues"}, linkText{Own: spaced("Cinq Plans Blues")}, true},
		{[]string{"Let It Be (version Joe Walsh)", "Let It Be"}, linkText{Own: spaced("Let it be")}, true},
	}
	for _, c := range cases {
		if got := nameScore(c.names, []linkText{c.at}) >= 0.6; got != c.ok {
			t.Errorf("%v at %q: %v, want %v", c.names, c.at.Own, got, c.ok)
		}
	}
	// A title found in the link itself beats the same words nearby.
	if nameScore([]string{"T-Bone Walker Blues"}, []linkText{tbone}) <= nameScore([]string{"T-Bone Walker Blues"}, []linkText{plan}) {
		t.Error("the lesson's own title should score above a mention near another lesson's")
	}
}
