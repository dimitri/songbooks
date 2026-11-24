package httpapi

import (
	"net/http"
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
		{"song from a band's set list", "song", map[string]any{"SetlistRef": int64(4), "SetlistURL": "/bands/3/setlists/4", "BandRef": int64(3)}, webassets.NavBands},
		{"song from my set list", "song", map[string]any{"SetlistRef": int64(4), "SetlistURL": "/setlists/4"}, webassets.NavLibrary},
		{"a band's set list", "setlist-overview", map[string]any{"IsBand": true, "Path": "/setlists/4"}, webassets.NavBands},
		{"my set list", "setlist-overview", map[string]any{"IsBand": false, "Path": "/setlists/4"}, webassets.NavLibrary},
		{"a band's click track", "setlist-clicktrack", map[string]any{"Path": "/bands/3/setlists/4"}, webassets.NavBands},
		{"songbooks segment", "songbooks-index", nil, webassets.NavLibrary},
		{"a songbook", "songbook", nil, webassets.NavLibrary},
		{"song from a band", "song", map[string]any{"BandRef": int64(3)}, webassets.NavBands},
		{"song from a lesson", "song", map[string]any{"LessonHref": "/x"}, webassets.NavCommunities},
		{"song from a songbook", "song", map[string]any{"SongbookRef": int64(2)}, webassets.NavLibrary},
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
		"groups", "bands", "band", "communities", "community", "setlists-index",
		"metronome", "tuner", "profile", "admin", "notifications"} {
		if navSectionFor(page, map[string]any{}) == "" {
			t.Errorf("%s has no nav section", page)
		}
	}
}

// /songbooks and /setlists open the Library's segments, keeping their
// query; their pages below (/songbooks/2) don't move.
func TestLibraryRedirects(t *testing.T) {
	for _, c := range []struct{ view, url, want string }{
		{libraryViewSongbooks, "/songbooks", "/catalog?view=songbooks"},
		{libraryViewSetlists, "/setlists", "/catalog?view=setlists"},
		{libraryViewSetlists, "/setlists?lang=fr", "/catalog?lang=fr&view=setlists"},
	} {
		rec := httptest.NewRecorder()
		libraryRedirect(c.view)(rec, httptest.NewRequest("GET", c.url, nil))
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != c.want {
			t.Errorf("%s: %d %s, want 302 %s", c.url, rec.Code, rec.Header().Get("Location"), c.want)
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
		{"desktop-sidebar", webassets.NavNotifications, "/notifications"},
		{"global-tabbar", webassets.NavNotifications, "/home"},
		{"global-tabbar", webassets.NavTuner, "/tools"},
		{"desktop-sidebar", webassets.NavCommunities, "/groups"},
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
		t.Error("the account menu lacks Admin for a site admin")
	}
}

// A song and a click track get the sidebar as a rail; other pages don't.
func TestFocusedPagesRail(t *testing.T) {
	for page, want := range map[string]bool{"song": true, "setlist-clicktrack": true, "setlist-overview": false, "catalog": false} {
		var b strings.Builder
		data := map[string]any{"Nav": navState{Page: page, Focused: focusedPages[page]}}
		if err := webassets.TemplatesFor("fr").ExecuteTemplate(&b, "desktop-sidebar", data); err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(b.String(), `class="desktop-sidebar is-rail"`); got != want {
			t.Errorf("%s: rail %v, want %v", page, got, want)
		}
	}
}
