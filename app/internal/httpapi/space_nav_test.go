package httpapi

import (
	"bytes"
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
		{spaceBand, spaceRole{}, "overview catalog setlists events rehearsals concerts proposals discover members", "settings"},
		{spaceBand, spaceRole{Staff: true}, "overview catalog setlists events rehearsals concerts proposals discover members", "settings"},
		{spaceCommunity, spaceRole{}, "overview songbooks downloads events", ""},
		{spaceCommunity, spaceRole{SeeMembers: true}, "overview songbooks members downloads events", ""},
		{spaceCommunity, spaceRole{Staff: true, SeeMembers: true}, "overview songbooks members downloads events", "admin"},
		// A coach never sees the student's "My voice", a student never
		// sees Students or the students' songs.
		{spaceCoaching, spaceRole{Staff: true, SeeMembers: true}, "overview songbooks students repertoire groups warmups lessons members downloads events", "admin"},
		{spaceCoaching, spaceRole{PersonID: 42}, "overview songbooks voice groups warmups lessons downloads events", ""},
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
	if p := spaceTabs(spaceCoaching, spaceRole{PersonID: 42})[2].Path; p != "/students/42" {
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
		{"band_catalog", nil, "catalog"},
		{"band_recordings", map[string]any{"Tab": "concerts"}, "concerts"},
		{"band_recordings", map[string]any{}, "rehearsals"},
		{"band_settings", nil, "settings"},
		{"community_downloads", nil, "downloads"},
		{"community_stats", nil, "admin"},
		{"coaching_student", map[string]any{"IsCoach": true}, "students"},
		{"coaching_student", map[string]any{"IsCoach": false}, "voice"},
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
	nav := newSpaceNav(spaceCoaching, 7, "Studio Lumen", spaceRole{Staff: true, SeeMembers: true, PersonID: 1}, "lessons")
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
	if strings.Count(html, `aria-current="page"`) != 1 {
		t.Errorf("want one open section:\n%s", html)
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
