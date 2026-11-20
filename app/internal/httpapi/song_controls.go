package httpapi

import (
	"fmt"

	"app/internal/i18n"
)

// songBack is where a song page's "‹" goes (menu-proposal.md §3.5, §3.10):
// the place the song was opened from — a set list, a songbook, a lesson,
// a band's or a community's repertoire, an event's program — else the
// Bibliothèque. One answer for the desktop header and the phone's song
// bar, which used to keep two copies of the same chain of ifs, in two
// different orders.
type songBack struct {
	Kind  string // "gig", "setlist", "band", "lesson", "program", "community", "songbook", "catalog"
	Href  string // the page
	Name  string // the place's own name, in full ("Set d'automne"), for the header and titles
	Short string // the phone bar's label, a word or two ("Set list", "Bibliothèque", a band's name)
	Title string // "Back to …", for a tooltip and screen readers
}

// songBackFor reads the song page's data (handleSong) for where it was
// opened from, the most specific place first.
func songBackFor(data map[string]any, lang string) songBack {
	str := func(k string) string { s, _ := data[k].(string); return s }
	id := func(k string) (int64, bool) {
		switch v := data[k].(type) {
		case int64:
			return v, v != 0
		case int:
			return int64(v), v != 0
		}
		return 0, false
	}
	named := func(name, fallback string) string {
		if name != "" {
			return name
		}
		return fallback
	}
	var b songBack
	switch {
	case str("SetlistRef") != "" && data["GigMode"] == true:
		b = songBack{Kind: "gig", Href: "/setlists/" + str("SetlistRef") + "/gig", Short: i18n.T(lang, "Set list")}
		b.Name = named(str("SetlistName"), b.Short)
	case str("SetlistRef") != "":
		b = songBack{Kind: "setlist", Href: named(str("SetlistURL"), "/setlists/"+str("SetlistRef")), Short: i18n.T(lang, "Set list")}
		b.Name = named(str("SetlistName"), b.Short)
	case func() bool { _, ok := id("BandRef"); return ok }():
		band, _ := id("BandRef")
		b = songBack{Kind: "band", Href: fmt.Sprintf("/bands/%d/catalog", band), Name: named(str("BandName"), i18n.T(lang, "Band"))}
		b.Short = b.Name
	case str("LessonHref") != "" && data["EventRef"] != nil:
		b = songBack{Kind: "program", Href: str("LessonHref"), Name: named(str("LessonLabel"), i18n.T(lang, "Program")), Short: i18n.T(lang, "Program")}
	case str("LessonHref") != "":
		b = songBack{Kind: "lesson", Href: str("LessonHref"), Name: named(str("LessonLabel"), i18n.T(lang, "Lesson")), Short: i18n.T(lang, "Lesson")}
	case func() bool { _, ok := id("CommunityRef"); return ok }():
		c, _ := id("CommunityRef")
		b = songBack{Kind: "community", Href: fmt.Sprintf("/communities/%d/songbooks/default", c), Short: i18n.T(lang, "Songbook")}
		// CommunityName is "songbook" when the community has no default
		// songbook (handleSong): the word, not a name.
		if n := str("CommunityName"); n != "" && n != "songbook" {
			b.Name = n
		} else {
			b.Name = b.Short
		}
	case func() bool { _, ok := id("SongbookRef"); return ok }():
		sb, _ := id("SongbookRef")
		b = songBack{Kind: "songbook", Href: fmt.Sprintf("/songbooks/%d", sb), Short: i18n.T(lang, "Songbook")}
		b.Name = named(str("SongbookName"), b.Short)
	default:
		b = songBack{Kind: "catalog", Href: "/catalog", Name: i18n.T(lang, "Library"), Short: i18n.T(lang, "Library")}
	}
	b.Title = i18n.T(lang, "Back to %s", b.Name)
	return b
}
