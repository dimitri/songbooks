package httpapi

import (
	"bytes"
	"net/url"
	"strings"
	"testing"

	"app/internal/webassets"
)

func sectionKeys(secs []SpaceSection) string {
	keys := make([]string, len(secs))
	for i, s := range secs {
		keys[i] = s.Key
	}
	return strings.Join(keys, " ")
}

// The same space shows the same sections on every page; only the
// viewer's role filters them.
func TestSpaceTabsByRole(t *testing.T) {
	cases := []struct {
		kind string
		role spaceRole
		want string
		gear string
	}{
		// A band has six sections: Proposals and Discover are views of its
		// Songbook, Rehearsals and Concerts of its Recordings.
		{spaceBand, spaceRole{}, "overview catalog setlists events recordings members", ""},
		{spaceBand, spaceRole{BandSettings: true}, "overview catalog setlists events recordings members", "settings"},
		// A jam's community: its events first, then songbooks, resources.
		{spaceCommunity, spaceRole{}, "overview events songbooks resources", ""},
		{spaceCommunity, spaceRole{SeeMembers: true}, "overview events songbooks resources members", ""},
		{spaceCommunity, spaceRole{Staff: true, SeeMembers: true}, "overview events songbooks resources members", "admin"},
		// A coaching studio has six sections either way: the coach's
		// Students (their songs and classes among its views) and Calendar
		// (lessons and events), Members behind ⚙ Admin; the student's
		// progress instead of Students, never Students nor Admin.
		{spaceCoaching, spaceRole{Staff: true, SeeMembers: true}, "overview students agenda songbooks warmups resources", "admin"},
		{spaceCoaching, spaceRole{PersonID: 42}, "overview progress agenda songbooks warmups resources", ""},
		{spaceCoaching, spaceRole{PersonID: 42, SeeMembers: true}, "overview progress agenda songbooks warmups resources", ""},
	}
	for _, c := range cases {
		if got := sectionKeys(spaceTabs(c.kind, c.role)); got != c.want {
			t.Errorf("spaceTabs(%s, %+v) = %q, want %q", c.kind, c.role, got, c.want)
		}
		gear := ""
		if g := spaceGear(c.kind, c.role); g != nil {
			gear = g.Key
		}
		if gear != c.gear {
			t.Errorf("spaceGear(%s, %+v) = %q, want %q", c.kind, c.role, gear, c.gear)
		}
	}
	if p := spaceTabs(spaceCoaching, spaceRole{PersonID: 42})[1].Path; p != "/students/42" {
		t.Errorf("a student's My voice goes to %q, want their own page", p)
	}
}

func TestSpaceSectionOf(t *testing.T) {
	cases := []struct {
		page string
		data map[string]any
		want string
	}{
		{"band", nil, "overview"},
		{"band_catalog", nil, "catalog/all"},
		{"band_proposals", nil, "catalog/vote"},
		{"band_discover", nil, "catalog/discover"},
		{"band_influences", nil, "catalog/influences"},
		{"band_recordings", map[string]any{"Tab": "concerts"}, "recordings/concerts"},
		{"band_recordings", map[string]any{}, "recordings/rehearsals"},
		{"band_settings", nil, "settings"},
		{"community_downloads", nil, "resources"},
		{"community_stats", nil, "admin"},
		{"coaching_student", map[string]any{"IsCoach": true}, "students/-"},
		{"coaching_student", map[string]any{"IsCoach": false}, "progress"},
		{"coaching_students", map[string]any{"StudentsView": "table"}, "students/table"},
		{"coaching_students", map[string]any{"StudentsView": ""}, "students"},
		{"coaching_repertoire", nil, "students/songs"},
		{"coaching_groups", nil, "students/classes"},
		{"coaching_lessons", nil, "agenda/lessons"},
		{"community", map[string]any{"SpaceSection": "events"}, "events"},
	}
	for _, c := range cases {
		if c.data == nil {
			c.data = map[string]any{}
		}
		if got := spaceSectionOf(c.page, c.data); got != c.want {
			t.Errorf("spaceSectionOf(%s, %v) = %q, want %q", c.page, c.data, got, c.want)
		}
	}
}

// The header and tabs mark the open section (aria-current), put Admin
// behind ⚙ rather than among the tabs, and link the name to the switcher.
func TestSpaceNavRenders(t *testing.T) {
	nav := newSpaceNav(spaceCoaching, 7, "Studio Lumen", spaceRole{Staff: true, SeeMembers: true, PersonID: 1}, "agenda/lessons")
	var buf bytes.Buffer
	if err := webassets.TemplatesFor("fr").ExecuteTemplate(&buf, "space-nav", nav); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{
		`href="/communities/7/lessons" aria-current="page"`,
		`class="space-gear" href="/communities/7/admin"`,
		`Studio Lumen`, `SL`, `Cours de chant`, `Élèves`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("space-nav lacks %q:\n%s", want, html)
		}
	}
	tabs := html[strings.Index(html, `class="space-tabs"`):]
	tabs = tabs[:strings.Index(tabs, "</nav>")]
	if strings.Count(tabs, `aria-current="page"`) != 1 {
		t.Errorf("want one open section:\n%s", html)
	}
	if !strings.Contains(html, `class="space-filters"`) || !strings.Contains(html, `href="/communities/7/events"`) {
		t.Errorf("the calendar's views aren't there:\n%s", html)
	}
	if strings.Contains(html, "Ma voix") {
		t.Errorf("a coach is shown the student's tab:\n%s", html)
	}
	nav = newSpaceNav(spaceBand, 14, "The Paper Lanterns", spaceRole{}, "settings")
	buf.Reset()
	if err := webassets.TemplatesFor("fr").ExecuteTemplate(&buf, "space-nav", nav); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `class="space-gear active" href="/bands/14/settings" aria-current="page"`) {
		t.Errorf("the band's ⚙ isn't the open section on its settings:\n%s", buf.String())
	}
}

// On a phone a space's home lists its sections, each with where it
// stands, ⚙ apart; the overview itself isn't one of the rows.
func TestSpaceHub(t *testing.T) {
	nav := newSpaceNav(spaceCommunity, 3, "Jam du Canal", spaceRole{Staff: true, SeeMembers: true}, "overview")
	nav.withHints(map[string]spaceHint{"events": {Text: "samedi", Due: true}, "admin": {Text: "3 to review"}})
	var buf bytes.Buffer
	if err := webassets.TemplatesFor("fr").ExecuteTemplate(&buf, "space-hub", nav); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{
		`href="/communities/3/events"`, `<span class="space-hub-state is-due">samedi</span>`,
		`href="/communities/3/admin"`, `3 to review`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("space-hub lacks %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, `href="/communities/3"`) {
		t.Errorf("the home lists itself:\n%s", html)
	}
}

// A section's views: the open one marked, the section's tab open for
// each of them, and a section's own tab going to its first view.
func TestSpaceFilters(t *testing.T) {
	nav := newSpaceNav(spaceBand, 14, "Night Owls", spaceRole{}, "catalog/vote")
	var on []string
	for _, f := range nav.Filters {
		if f.Current {
			on = append(on, f.Key)
		}
	}
	if len(nav.Filters) != 4 || strings.Join(on, ",") != "vote" {
		t.Errorf("Songbook's views: %+v", nav.Filters)
	}
	if nav.SectionLabel() != "Songbook" || !nav.Sections[1].Current {
		t.Errorf("To vote isn't in the Songbook: %q", nav.SectionLabel())
	}
	nav = newSpaceNav(spaceBand, 14, "Night Owls", spaceRole{}, "recordings")
	if len(nav.Filters) != 2 || !nav.Filters[0].Current || nav.Sections[4].Href != "/bands/14/recordings/rehearsals" {
		t.Errorf("Recordings: %+v %+v", nav.Filters, nav.Sections[4])
	}
	if nav := newSpaceNav(spaceBand, 14, "Night Owls", spaceRole{}, "members"); nav.Filters != nil {
		t.Errorf("Members has views: %+v", nav.Filters)
	}
}

// Every old address of a consolidated section redirects to the page that
// holds it now, the old query kept and the new view's own winning.
func TestMovedTarget(t *testing.T) {
	cases := []struct{ target, oldQuery, want string }{
		{"/bands/3/catalog/vote", "", "/bands/3/catalog/vote"},
		{"/bands/3/recordings/rehearsals", "date=2026-01-02", "/bands/3/recordings/rehearsals?date=2026-01-02"},
		{"/communities/7/students?view=songs", "view=x&q=1", "/communities/7/students?q=1&view=songs"},
	}
	for _, c := range cases {
		q, _ := url.ParseQuery(c.oldQuery)
		if got := movedTarget(c.target, q); got != c.want {
			t.Errorf("movedTarget(%q, %q) = %q, want %q", c.target, c.oldQuery, got, c.want)
		}
	}
}

// A coaching studio's pages every community has: its events are a view of
// its Calendar, its members its Admin's; a student or class page shows
// no views.
func TestCoachingSections(t *testing.T) {
	if coachingSections["events"] != "agenda/events" || coachingSections["members"] != "admin" {
		t.Errorf("coachingSections: %v", coachingSections)
	}
	nav := newSpaceNav(spaceCoaching, 7, "Studio", spaceRole{Staff: true}, "students/-")
	if nav.Filters != nil || !nav.Sections[1].Current {
		t.Errorf("a student's page: %+v, %+v", nav.Filters, nav.Sections[1])
	}
	nav = newSpaceNav(spaceCoaching, 7, "Studio", spaceRole{PersonID: 9}, "overview")
	if nav.SectionLabel() != "" || nav.Sections[0].Label != "My week" {
		t.Errorf("a student's home: %q %q", nav.SectionLabel(), nav.Sections[0].Label)
	}
}

// A band's Songbook draws its views itself, as chips at the head of its
// list with their counts, To vote marked while votes wait: no second row
// of tabs under the space's.
func TestSongbookViewsInPage(t *testing.T) {
	nav := newSpaceNav(spaceBand, 14, "The Paper Lanterns", spaceRole{}, "catalog/all")
	if !nav.ViewsInPage {
		t.Fatalf("the Songbook's views are under the tabs: %+v", nav)
	}
	nav.Filters[0].Hint = "43"
	nav.Filters[1].Hint, nav.Filters[1].Due = "3", true
	var buf bytes.Buffer
	if err := webassets.TemplatesFor("fr").ExecuteTemplate(&buf, "space-nav", nav); err != nil {
		t.Fatal(err)
	}
	if html := buf.String(); strings.Contains(html, "space-filters") || !strings.Contains(html, ">Songbook</a>") {
		t.Errorf("the Songbook's tab or its views:\n%s", html)
	}
	buf.Reset()
	if err := webassets.TemplatesFor("fr").ExecuteTemplate(&buf, "space-views", nav); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{
		`href="/bands/14/catalog" class="space-view active" aria-current="page">Morceaux <span class="space-view-count">43</span>`,
		`href="/bands/14/catalog/vote" class="space-view is-due">À voter <span class="space-view-count">3</span>`,
		`href="/bands/14/catalog/discover" class="space-view">Découvrir</a>`,
		`href="/bands/14/catalog/influences" class="space-view">Influences</a>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("space-views lacks %q:\n%s", want, html)
		}
	}
	// The Recordings keep their views under the tabs.
	if nav := newSpaceNav(spaceBand, 14, "The Paper Lanterns", spaceRole{}, "recordings"); nav.ViewsInPage {
		t.Errorf("the Recordings' views moved into the page")
	}
}
