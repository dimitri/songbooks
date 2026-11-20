package httpapi

import (
	"testing"

	"app/internal/gpfacts"
)

// TestSongTunings: the tuner's song tunings (the /tuner page's, and the
// song page's "Accorder"): pitched tracks only, lowest string first, each
// tuning and capo once, a name for an unnamed track; none without a file.
func TestSongTunings(t *testing.T) {
	if got := songTunings(nil, "fr"); got == nil || len(got) != 0 {
		t.Fatalf("no file: %#v", got)
	}
	facts := &gpfacts.Facts{Tracks: []gpfacts.Track{
		{N: 1, Name: "Guitare", Tuning: []int{64, 59, 55, 50, 45, 40}},
		{N: 2, Name: "Guitare 2", Tuning: []int{40, 45, 50, 55, 59, 64}}, // the same strings: once
		{N: 3, Name: "Batterie", Drums: true, Tuning: []int{1}},
		{N: 4, Tuning: []int{43, 38, 33, 28}, Capo: 2},
		{N: 5, Name: "Voix"}, // no tuning
	}}
	got := songTunings(facts, "fr")
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[0].Name != "Guitare" || got[0].Notes[0] != 40 || got[0].Notes[5] != 64 {
		t.Errorf("first: %#v", got[0])
	}
	if got[1].Notes[0] != 28 || got[1].Capo != 2 || got[1].Name == "" {
		t.Errorf("second: %#v", got[1])
	}
	if songCapo(" 3 ") != 3 || songCapo("") != 0 || songCapo("x") != 0 {
		t.Error("songCapo")
	}
}

// TestSongBackFor: a song page's "‹" goes back to the place it was opened
// from, the most specific first, and names it; French labels.
func TestSongBackFor(t *testing.T) {
	cases := []struct {
		name               string
		data               map[string]any
		kind, href, nm, sh string
	}{
		{"nowhere", map[string]any{}, "catalog", "/catalog", "Bibliothèque", "Bibliothèque"},
		{"set list", map[string]any{"SetlistRef": "7", "SetlistURL": "/bands/14/setlists/7", "SetlistName": "Concert du samedi"},
			"setlist", "/bands/14/setlists/7", "Concert du samedi", "Set list"},
		{"set list, no name", map[string]any{"SetlistRef": "7"}, "setlist", "/setlists/7", "Set list", "Set list"},
		{"gig", map[string]any{"SetlistRef": "7", "SetlistURL": "/bands/14/setlists/7", "SetlistName": "Gig", "GigMode": true},
			"gig", "/setlists/7/gig", "Gig", "Set list"},
		{"set list wins over a band", map[string]any{"SetlistRef": "7", "BandRef": int64(14), "BandName": "Silver Tramline"},
			"setlist", "/setlists/7", "Set list", "Set list"},
		{"band", map[string]any{"BandRef": int64(14), "BandName": "Silver Tramline", "SongbookRef": int64(3)},
			"band", "/bands/14/catalog", "Silver Tramline", "Silver Tramline"},
		{"lesson", map[string]any{"SongbookRef": int64(3), "LessonHref": "/songbooks/3/lessons/9?view=lesson", "LessonLabel": "Leçon 2 : Respirer"},
			"lesson", "/songbooks/3/lessons/9?view=lesson", "Leçon 2 : Respirer", "Cours"},
		{"event program", map[string]any{"EventRef": int64(5), "LessonHref": "/communities/7/events/5/program", "LessonLabel": "Jam du Canal"},
			"program", "/communities/7/events/5/program", "Jam du Canal", "Programme"},
		{"community", map[string]any{"CommunityRef": int64(7), "CommunityName": "Repertoire"},
			"community", "/communities/7/songbooks/default", "Repertoire", "Songbook"},
		{"community, no songbook", map[string]any{"CommunityRef": int64(7), "CommunityName": "songbook"},
			"community", "/communities/7/songbooks/default", "Songbook", "Songbook"},
		{"songbook", map[string]any{"SongbookRef": int64(3), "SongbookName": "My songbook"},
			"songbook", "/songbooks/3", "My songbook", "Songbook"},
	}
	for _, c := range cases {
		b := songBackFor(c.data, "fr")
		if b.Kind != c.kind || b.Href != c.href || b.Name != c.nm || b.Short != c.sh {
			t.Errorf("%s: got %+v, want kind=%s href=%s name=%s short=%s", c.name, b, c.kind, c.href, c.nm, c.sh)
		}
		if b.Title == "" {
			t.Errorf("%s: no title", c.name)
		}
	}
	if b := songBackFor(map[string]any{"SongbookRef": int64(3), "SongbookName": "Mine"}, "fr"); b.Title != "Retour à Mine" {
		t.Errorf("title: %q", b.Title)
	}
}
