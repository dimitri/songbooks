package httpapi

import "testing"

func TestSplitTitleArtist(t *testing.T) {
	cases := []struct {
		line, title, artist string
	}{
		{"Zombie - Yungblud", "Zombie", "Yungblud"},
		{"Sweet Dreams - Marilyn Manson", "Sweet Dreams", "Marilyn Manson"},
		{"  The Final Countdown  -  Europe  ", "The Final Countdown", "Europe"},
		{"Self-Control - Laura Branigan", "Self-Control", "Laura Branigan"},
		{"Mr. Brightside – The Killers", "Mr. Brightside", "The Killers"},
		{"Freebird", "Freebird", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		title, artist := splitTitleArtist(c.line)
		if title != c.title || artist != c.artist {
			t.Errorf("splitTitleArtist(%q) = (%q, %q), want (%q, %q)", c.line, title, artist, c.title, c.artist)
		}
	}
}
