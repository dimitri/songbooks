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

// songPlace is where a song sits in the list it was opened from (a
// songbook, a set list, a program): the songs either side of it and "4 of
// 15". Zero when it was opened from no list, or isn't in it.
type songPlace struct {
	PrevID, NextID int64 // 0 at either end
	Pos, Total     int   // Pos counts from 1
}

// listPlace finds variantID in a list's running order (its first
// occurrence: a song twice in a set list is where it first comes).
func listPlace(order []int64, variantID int64) songPlace {
	for i, id := range order {
		if id != variantID {
			continue
		}
		p := songPlace{Pos: i + 1, Total: len(order)}
		if i > 0 {
			p.PrevID = order[i-1]
		}
		if i < len(order)-1 {
			p.NextID = order[i+1]
		}
		return p
	}
	return songPlace{}
}

// songCrumb is the line above the song's header (play.html .song-crumb):
// "‹ Les morceaux de Dimitri · 4 of 15", the place the song was opened
// from, its position there, a link back. Opened from nowhere in
// particular, the song's owner (its band, its community, the reader's own
// songbook) without a position; a catalogue song, none (nil).
type songCrumb struct {
	Href, Name, Title string
	Pos, Total        int
}

// songOwnerPlace is the song's owner for songCrumbFor, Href "" for none.
type songOwnerPlace struct{ Href, Name string }

func songCrumbFor(back songBack, at songPlace, owner songOwnerPlace, lang string) *songCrumb {
	if back.Kind == "catalog" {
		if owner.Href == "" {
			return nil
		}
		return &songCrumb{Href: owner.Href, Name: owner.Name, Title: i18n.T(lang, "Back to %s", owner.Name)}
	}
	c := &songCrumb{Href: back.Href, Name: back.Name, Title: back.Title}
	if at.Total > 0 {
		c.Pos, c.Total = at.Pos, at.Total
	}
	return c
}

// songNavQuery is the address's query a song's previous and next links
// carry, so the next song is opened in the same list: the set list (and
// the gig view), the songbook (and the band it was reached through), the
// program, or the band.
func songNavQuery(data map[string]any) string {
	str := func(k string) string { s, _ := data[k].(string); return s }
	num := func(k string) int64 {
		switch v := data[k].(type) {
		case int64:
			return v
		case int:
			return int64(v)
		}
		return 0
	}
	switch {
	case str("SetlistRef") != "":
		q := "?setlist=" + str("SetlistRef")
		if data["GigMode"] == true {
			q += "&gig=1"
		}
		return q
	case num("SongbookRef") != 0:
		q := fmt.Sprintf("?songbook=%d", num("SongbookRef"))
		if b := num("BandRef"); b != 0 {
			q += fmt.Sprintf("&band=%d", b)
		}
		return q
	case num("EventRef") != 0:
		return fmt.Sprintf("?event=%d", num("EventRef"))
	case num("BandRef") != 0:
		return fmt.Sprintf("?band=%d", num("BandRef"))
	}
	return ""
}
