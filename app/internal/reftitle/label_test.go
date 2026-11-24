package reftitle

import "testing"

// TestLabel: a reference label from the recording's video title — what
// tells the recording apart, the upload's wrapping and what repeats the
// song's title or artist taken out; "" when nothing is left.
func TestLabel(t *testing.T) {
	for _, c := range []struct {
		video, title, artist, want string
	}{
		// Nothing but the song and the wrapping: no label.
		{"Ben E. King - Stand By Me (Official Video)", "Stand By Me", "Ben E. King", ""},
		{"Oasis - Wonderwall (Official HD Video)", "Wonderwall", "Oasis", ""},
		{"Queen – Bohemian Rhapsody (Official Video Remastered)", "Bohemian Rhapsody", "Queen", ""},
		{"Jeff Buckley - Hallelujah (Official Video) [HD]", "Hallelujah", "Jeff Buckley", ""},
		{"Stand by me - Ben E King | Lyrics", "Stand By Me", "Ben E. King", ""},
		{"Oasis - Wonderwall (Remastered 2014)", "Wonderwall", "Oasis", ""},
		{"Wonderwall (Lyrics)", "Wonderwall", "Oasis", ""},
		{"Hallelujah Official Video", "Hallelujah", "Jeff Buckley", ""},
		{"Wonderwall by Oasis", "Wonderwall", "Oasis", ""},
		{"Beatles - Let It Be", "Let It Be", "The Beatles", ""},
		{"Édith Piaf – La Vie en rose (Audio officiel)", "La vie en rose", "Edith Piaf", ""},
		{"Stromae - Alors on danse (Clip officiel)", "Alors on danse", "Stromae", ""},
		{"Oasis: Wonderwall", "Wonderwall", "Oasis", ""},
		{"Hurt", "Hurt", "Johnny Cash", ""},
		{"", "Hurt", "Johnny Cash", ""},
		// What tells the recording apart stays.
		{"Adele - Someone Like You (Live at The Royal Albert Hall)", "Someone Like You", "Adele", "Live at The Royal Albert Hall"},
		{"Johnny Cash - Hurt (American IV)", "Hurt", "Johnny Cash", "American IV"},
		{"Nirvana - Smells Like Teen Spirit (Live at Reading 1992) [Official Video]", "Smells Like Teen Spirit", "Nirvana", "Live at Reading 1992"},
		{"Leonard Cohen - Hallelujah - Live in London 2008", "Hallelujah", "Leonard Cohen", "Live in London 2008"},
		{"Some Busker - Wonderwall (Oasis cover)", "Wonderwall", "Some Busker", "Oasis cover"},
		{"Eric Clapton - Layla (Unplugged) [HD]", "Layla", "Eric Clapton", "Unplugged"},
		{"Yves Montand - Les feuilles mortes (1946)", "Les Feuilles mortes", "Yves Montand", "1946"},
		{"Stand By Me (Acoustic) - Ben E. King", "Stand By Me", "Ben E. King", "Acoustic"},
		{"Hotel California (Live on MTV, 1994) - Eagles", "Hotel California", "Eagles", "Live on MTV, 1994"},
		{"Rihanna ft. Someone - Umbrella (Live)", "Umbrella", "Rihanna", "Live"},
		{"Me at the zoo", "Hurt", "Johnny Cash", "Me at the zoo"},
		// A title of its own words keeps them: "Video" isn't the wrapping
		// there.
		{"The Buggles - Video Killed the Radio Star (Official Music Video)", "Video Killed the Radio Star", "The Buggles", ""},
		{"Video Killed the Radio Star - Live 1980", "Radio", "The Buggles", "Video Killed the Radio Star, Live 1980"},
	} {
		if got := Label(c.video, c.title, c.artist); got != c.want {
			t.Errorf("Label(%q, %q, %q) = %q, want %q", c.video, c.title, c.artist, got, c.want)
		}
	}
	long := "Artist - Song (Live at a very long place name that goes on and on and on and on, somewhere far away, 1999)"
	if got := Label(long, "Song", "Artist"); len([]rune(got)) > MaxLabel {
		t.Errorf("a long label: %d characters", len([]rune(got)))
	}
}
