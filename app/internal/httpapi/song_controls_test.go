package httpapi

import (
	"strings"
	"testing"

	"app/internal/gpfacts"
	"app/internal/webassets"
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

// TestListPlace: a song's place in the list it was opened from — the
// songs either side, none at either end, "4 of 15"; nothing when it isn't
// in the list; a song twice is where it first comes.
func TestListPlace(t *testing.T) {
	order := make([]int64, 15)
	for i := range order {
		order[i] = int64(100 + i)
	}
	cases := []struct {
		name       string
		order      []int64
		id         int64
		prev, next int64
		pos, total int
	}{
		{"middle", order, 103, 102, 104, 4, 15},
		{"first", order, 100, 0, 101, 1, 15},
		{"last", order, 114, 113, 0, 15, 15},
		{"alone", []int64{7}, 7, 0, 0, 1, 1},
		{"not in it", order, 9, 0, 0, 0, 0},
		{"empty list", nil, 9, 0, 0, 0, 0},
		{"twice", []int64{1, 2, 3, 2, 5}, 2, 1, 3, 2, 5},
	}
	for _, c := range cases {
		p := listPlace(c.order, c.id)
		if p.PrevID != c.prev || p.NextID != c.next || p.Pos != c.pos || p.Total != c.total {
			t.Errorf("%s: got %+v, want prev=%d next=%d %d of %d", c.name, p, c.prev, c.next, c.pos, c.total)
		}
	}
}

// TestSongCrumbFor: the line above the header names the place the song
// was opened from, links back to it and says where the song is in it;
// opened from nowhere in particular, its owner's page, or nothing.
func TestSongCrumbFor(t *testing.T) {
	at := songPlace{PrevID: 3, NextID: 5, Pos: 4, Total: 15}
	cases := []struct {
		name       string
		data       map[string]any
		at         songPlace
		owner      songOwnerPlace
		nilCrumb   bool
		href, nm   string
		pos, total int
	}{
		{"songbook", map[string]any{"SongbookRef": int64(3), "SongbookName": "Les morceaux de Dimitri"}, at, songOwnerPlace{},
			false, "/songbooks/3", "Les morceaux de Dimitri", 4, 15},
		{"set list", map[string]any{"SetlistRef": "7", "SetlistURL": "/bands/14/setlists/7", "SetlistName": "Concert du samedi"}, songPlace{Pos: 1, Total: 12, NextID: 9}, songOwnerPlace{},
			false, "/bands/14/setlists/7", "Concert du samedi", 1, 12},
		{"gig", map[string]any{"SetlistRef": "7", "SetlistName": "Gig", "GigMode": true}, songPlace{PrevID: 8, Pos: 12, Total: 12}, songOwnerPlace{},
			false, "/setlists/7/gig", "Gig", 12, 12},
		{"program", map[string]any{"EventRef": int64(5), "LessonHref": "/communities/7/events/5/program", "LessonLabel": "Jam du Canal"}, songPlace{Pos: 2, Total: 6}, songOwnerPlace{},
			false, "/communities/7/events/5/program", "Jam du Canal", 2, 6},
		{"band repertoire: no position", map[string]any{"BandRef": int64(14), "BandName": "Silver Tramline"}, songPlace{}, songOwnerPlace{},
			false, "/bands/14/catalog", "Silver Tramline", 0, 0},
		{"community songbook", map[string]any{"CommunityRef": int64(7), "CommunityName": "La Nouvelle Méthode"}, songPlace{}, songOwnerPlace{},
			false, "/communities/7/songbooks/default", "La Nouvelle Méthode", 0, 0},
		{"a songbook it isn't in", map[string]any{"SongbookRef": int64(3), "SongbookName": "Mine"}, songPlace{}, songOwnerPlace{},
			false, "/songbooks/3", "Mine", 0, 0},
		{"directly, the band's song", map[string]any{}, songPlace{}, songOwnerPlace{Href: "/bands/14/catalog", Name: "Silver Tramline"},
			false, "/bands/14/catalog", "Silver Tramline", 0, 0},
		{"directly, my song", map[string]any{}, songPlace{}, songOwnerPlace{Href: "/songbooks/2", Name: "My songbook"},
			false, "/songbooks/2", "My songbook", 0, 0},
		{"directly, a catalogue song", map[string]any{}, songPlace{}, songOwnerPlace{}, true, "", "", 0, 0},
	}
	for _, c := range cases {
		got := songCrumbFor(songBackFor(c.data, "fr"), c.at, c.owner, "fr")
		if c.nilCrumb {
			if got != nil {
				t.Errorf("%s: got %+v, want none", c.name, got)
			}
			continue
		}
		if got == nil || got.Href != c.href || got.Name != c.nm || got.Pos != c.pos || got.Total != c.total || got.Title != "Retour à "+c.nm {
			t.Errorf("%s: got %+v, want %s %q %d of %d", c.name, got, c.href, c.nm, c.pos, c.total)
		}
	}
}

// TestSongNavQuery: the previous and next links keep the list the song
// was opened from, and the band it was reached through.
func TestSongNavQuery(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
		want string
	}{
		{"nowhere", map[string]any{}, ""},
		{"set list", map[string]any{"SetlistRef": "7"}, "?setlist=7"},
		{"gig", map[string]any{"SetlistRef": "7", "GigMode": true}, "?setlist=7&gig=1"},
		{"set list wins over a band", map[string]any{"SetlistRef": "7", "BandRef": int64(14)}, "?setlist=7"},
		{"songbook", map[string]any{"SongbookRef": int64(3)}, "?songbook=3"},
		{"a band's songbook", map[string]any{"SongbookRef": int64(3), "BandRef": int64(14)}, "?songbook=3&band=14"},
		{"program", map[string]any{"EventRef": int64(5)}, "?event=5"},
		{"band", map[string]any{"BandRef": int64(14)}, "?band=14"},
	}
	for _, c := range cases {
		if got := songNavQuery(c.data); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// The header's pieces: the line above it ("‹ name · 4 of 15", in French
// "4 sur 15"), and the previous and next cards — the title on both, no
// tempo, the list kept on the link, greyed at either end of a list,
// nothing outside one.
func TestSongHeaderTemplates(t *testing.T) {
	render := func(lang, name string, data any) string {
		var b strings.Builder
		if err := webassets.TemplatesFor(lang).ExecuteTemplate(&b, name, data); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	crumb := render("fr", "song-crumb", &songCrumb{Href: "/songbooks/3", Name: "Les morceaux de Dimitri", Title: "Retour à Les morceaux de Dimitri", Pos: 4, Total: 15})
	for _, want := range []string{`href="/songbooks/3"`, `class="song-back song-crumb-link"`, ">Les morceaux de Dimitri<", ">4 sur 15<"} {
		if !strings.Contains(crumb, want) {
			t.Errorf("crumb: no %q in\n%s", want, crumb)
		}
	}
	if owner := render("en", "song-crumb", &songCrumb{Href: "/bands/14/catalog", Name: "Silver Tramline", Title: "Back to Silver Tramline"}); strings.Contains(owner, "song-crumb-pos") {
		t.Errorf("an owner has no position:\n%s", owner)
	}

	song := &songNeighbor{VariantID: 42, Title: "The Boxer", Tempo: "92", Key: "C"}
	prev := render("fr", "song-nav-card", map[string]any{"Dir": "prev", "Song": song, "Query": "?setlist=7&gig=1", "InList": true})
	for _, want := range []string{`class="topbar-prev setlist-nav-card prev"`, `href="/song/42?setlist=7&amp;gig=1"`, "<small>Précédent</small><strong>The Boxer</strong>", `title="précédent : The Boxer"`} {
		if !strings.Contains(prev, want) {
			t.Errorf("previous: no %q in\n%s", want, prev)
		}
	}
	next := render("en", "song-nav-card", map[string]any{"Dir": "next", "Song": song, "Query": "?songbook=3", "InList": true})
	for _, want := range []string{`class="topbar-next setlist-nav-card next"`, `href="/song/42?songbook=3"`, "<small>Next</small><strong>The Boxer</strong>"} {
		if !strings.Contains(next, want) {
			t.Errorf("next: no %q in\n%s", want, next)
		}
	}
	if strings.Contains(next, "92") || strings.Contains(prev, "bpm") {
		t.Errorf("a card shows the tempo:\n%s\n%s", prev, next)
	}
	end := render("en", "song-nav-card", map[string]any{"Dir": "next", "Song": (*songNeighbor)(nil), "Query": "", "InList": true})
	if !strings.Contains(end, `class="topbar-next is-disabled"`) || strings.Contains(end, "setlist-nav-card") || strings.Contains(end, "href") {
		t.Errorf("the last song's next:\n%s", end)
	}
	if none := render("en", "song-nav-card", map[string]any{"Dir": "prev", "Song": (*songNeighbor)(nil), "Query": "", "InList": false}); strings.TrimSpace(none) != "" {
		t.Errorf("outside a list:\n%s", none)
	}
}
