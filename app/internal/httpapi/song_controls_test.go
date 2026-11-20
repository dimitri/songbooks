package httpapi

import "testing"

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
