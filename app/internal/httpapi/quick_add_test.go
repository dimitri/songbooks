package httpapi

import "testing"

func TestSongOfVideo(t *testing.T) {
	for _, x := range []struct{ title, channel, wantTitle, wantArtist string }{
		{"Oasis - Wonderwall (Official Video)", "OasisVEVO", "Wonderwall", "Oasis"},
		{"Céline Dion – Pour que tu m'aimes encore (Clip officiel)", "CelineDionVEVO", "Pour que tu m'aimes encore", "Céline Dion"},
		{"Adele - Hello [Lyrics]", "Some Lyrics Channel", "Hello", "Adele"},
		{"Hello", "Adele - Topic", "Hello", "Adele"},
		{"Stand By Me", "Ben E. King", "Stand By Me", "Ben E. King"},
		{"Queen – Bohemian Rhapsody (Official Video Remastered)", "Queen Official", "Bohemian Rhapsody", "Queen"},
		{"Stromae - Alors on danse | Official Video", "StromaeVEVO", "Alors on danse", "Stromae"},
		{"Zaz - Je veux (Clip officiel) HD", "ZazVEVO", "Je veux", "Zaz"},
	} {
		title, artist := songOfVideo(x.title, x.channel)
		if title != x.wantTitle || artist != x.wantArtist {
			t.Errorf("songOfVideo(%q, %q) = %q, %q; want %q, %q", x.title, x.channel, title, artist, x.wantTitle, x.wantArtist)
		}
	}
}

func TestSongOfText(t *testing.T) {
	for _, x := range []struct{ q, title, artist string }{
		{"Stand By Me — Ben E. King", "Stand By Me", "Ben E. King"},
		{"Wonderwall - Oasis", "Wonderwall", "Oasis"},
		{"Hallelujah by Leonard Cohen", "Hallelujah", "Leonard Cohen"},
		{"La Bohème", "La Bohème", ""},
	} {
		if title, artist := songOfText(x.q); title != x.title || artist != x.artist {
			t.Errorf("songOfText(%q) = %q, %q", x.q, title, artist)
		}
	}
	for q, want := range map[string]bool{"https://youtu.be/x": true, "youtube.com/watch?v=x": true, "www.youtube.com/watch?v=x": true, "Hello - Adele": false} {
		if isLink(q) != want {
			t.Errorf("isLink(%q) = %v", q, !want)
		}
	}
}
