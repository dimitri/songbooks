package httpapi

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"app/internal/store"
	"app/internal/webassets"
)

func TestNavSectionFor(t *testing.T) {
	band := &store.Band{ID: 3}
	var noBand *store.Band
	cases := []struct {
		name string
		page string
		data map[string]any
		want string
	}{
		{"home", "index", map[string]any{}, webassets.NavHome},
		{"calendar", "calendar_page", nil, webassets.NavCalendar},
		{"library", "catalog", nil, webassets.NavLibrary},
		{"band section", "band_events", map[string]any{"Band": band}, webassets.NavBands},
		{"coaching page", "coaching_students", nil, webassets.NavCommunities},
		{"metronome", "metronome", nil, webassets.NavMetronome},
		{"handler says", "event", map[string]any{"NavSection": webassets.NavHome}, webassets.NavHome},
		{"handler beats page", "catalog", map[string]any{"NavSection": webassets.NavCalendar}, webassets.NavCalendar},
		{"unknown page in a band", "something_new", map[string]any{"Band": band}, webassets.NavBands},
		{"unknown page, typed nil band", "something_new", map[string]any{"Band": noBand}, ""},
		{"song from a set list", "song", map[string]any{"SetlistRef": int64(4), "BandRef": int64(3)}, webassets.NavSetlists},
		{"song from a band", "song", map[string]any{"BandRef": int64(3)}, webassets.NavBands},
		{"song from a lesson", "song", map[string]any{"LessonHref": "/x"}, webassets.NavCommunities},
		{"song from a songbook", "song", map[string]any{"SongbookRef": int64(2)}, webassets.NavSongbooks},
		{"song on its own", "song", map[string]any{}, webassets.NavLibrary},
	}
	for _, c := range cases {
		data := c.data
		if data == nil {
			data = map[string]any{}
		}
		if got := navSectionFor(c.page, data); got != c.want {
			t.Errorf("%s: navSectionFor(%q) = %q, want %q", c.name, c.page, got, c.want)
		}
	}
}

// Every page that renders the global menus has a section.
func TestEveryNavPageHasASection(t *testing.T) {
	for _, page := range []string{"index", "calendar_page", "songbooks-index", "my-songbooks", "songbook", "catalog",
		"groups", "bands", "band", "communities", "community", "setlists-index", "setlist-overview",
		"metronome", "tuner", "profile", "admin", "notifications"} {
		if navSectionFor(page, map[string]any{}) == "" {
			t.Errorf("%s has no nav section", page)
		}
	}
}

func TestNavFromReferrer(t *testing.T) {
	cases := []struct {
		url, referer, want string
	}{
		{"/events/5", "http://example.test/home", webassets.NavHome},
		{"/events/5", "http://example.test/calendar?month=2026-11", webassets.NavCalendar},
		{"/events/5", "http://example.test/notifications", webassets.NavNotifications},
		{"/events/5", "http://example.test/communities/7/events", ""},
		{"/events/5", "http://elsewhere.test/home", ""},
		{"/events/5", "", ""},
		{"/events/5?from=calendar", "http://example.test/home", webassets.NavCalendar},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "http://example.test"+c.url, nil)
		if c.referer != "" {
			r.Header.Set("Referer", c.referer)
		}
		if got := navFromReferrer(r); got != c.want {
			t.Errorf("%s from %q: %q, want %q", c.url, c.referer, got, c.want)
		}
	}
	// An event opened from Home is Home's; from elsewhere, its owner's.
	ev := &store.Event{OwnerKind: "community"}
	r := httptest.NewRequest("GET", "http://example.test/events/5", nil)
	if got := eventNavSection(r, ev); got != webassets.NavCommunities {
		t.Errorf("event without a referrer: %q", got)
	}
	r.Header.Set("Referer", "http://example.test/home")
	if got := eventNavSection(r, ev); got != webassets.NavHome {
		t.Errorf("event opened from Home: %q", got)
	}
}

// The menus mark the current section, and only it, server-side.
func TestMenusMarkTheCurrentSection(t *testing.T) {
	me := &store.Person{ID: 1, DisplayName: "Lucas", IsAdmin: true}
	render := func(tmpl, section string) string {
		var b strings.Builder
		data := map[string]any{"Me": me, "Nav": navState{Section: section, Me: me}}
		if err := webassets.TemplatesFor("fr").ExecuteTemplate(&b, tmpl, data); err != nil {
			t.Fatalf("%s: %v", tmpl, err)
		}
		return b.String()
	}
	current := regexp.MustCompile(`<a href="([^"]+)"[^>]*aria-current="page"`)
	for _, c := range []struct{ tmpl, section, want string }{
		{"desktop-sidebar", webassets.NavHome, "/home"},
		{"desktop-sidebar", webassets.NavNotifications, "/home"},
		{"desktop-sidebar", webassets.NavCommunities, "/communities"},
		{"desktop-sidebar", webassets.NavTuner, "/tuner"},
		{"global-tabbar", webassets.NavBands, "/groups"},
		{"global-tabbar", webassets.NavLibrary, "/catalog"},
	} {
		out := render(c.tmpl, c.section)
		m := current.FindAllStringSubmatch(out, -1)
		if c.tmpl == "global-tabbar" {
			// The tab bar comes with its More sheet; look at the bar alone.
			bar := out[:strings.Index(out, "</nav>")]
			m = current.FindAllStringSubmatch(bar, -1)
		}
		if len(m) != 1 || m[0][1] != c.want {
			t.Errorf("%s, section %q: current %v, want only %s", c.tmpl, c.section, m, c.want)
		}
	}
	if out := render("desktop-sidebar", ""); strings.Contains(out, "aria-current") {
		t.Error("no section, yet an entry is current")
	}
	if out := render("global-tabbar", webassets.NavHome); !strings.Contains(out, `href="/admin"`) {
		t.Error("the More sheet lacks Admin for a site admin")
	}
}
