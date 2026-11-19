package httpapi

import (
	"strings"
	"testing"
	"time"

	"app/internal/brand"
	"app/internal/store"
	"app/internal/webassets"
)

func TestInitials(t *testing.T) {
	for name, want := range map[string]string{
		"The Paper Lanterns": "PL",
		"Velvet Hour":        "VH",
		"Les Fanfarons":      "LF",
		"léa":                "L",
		"élise blackwood":    "ÉB",
		"  ":                 "",
		"Jam du Canal":       "JC",
	} {
		if got := initials(name); got != want {
			t.Errorf("initials(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestActivityLabel(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	if got := activityLabel("en", now.Add(-3*time.Hour), now); got == "" || strings.Contains(got, "Nov") {
		t.Errorf("3 hours ago: %q", got)
	}
	if got := activityLabel("fr", time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC), now); got != "12 oct." {
		t.Errorf("this year: %q", got)
	}
	if got := activityLabel("en", time.Date(2025, 10, 12, 9, 0, 0, 0, time.UTC), now); got != "12 Oct 2025" {
		t.Errorf("last year: %q", got)
	}
}

// The cards render in their look, scoped to the card, and without one
// in the app's.
func TestListCardTemplates(t *testing.T) {
	theme := brand.Resolve(brand.Brand{Preset: "stage", Mark: "☾", Tagline: "Late sets"}, "Night Owls")
	next := &store.CalendarItem{Kind: "rehearsal", Title: "Rehearsal", Starts: time.Date(2026, 11, 23, 19, 0, 0, 0, time.UTC), Place: "Studio", Link: "/events/9"}
	band := bandListCard{
		bandCardView: bandCardView{ID: 4, Name: "Night Owls", Members: 7, SetlistCount: 2},
		listCardLook: listCardLook{Brand: theme, Mark: theme.Mark},
		People:       []listCardPerson{{Name: "Léa Martin", Initials: "LM"}, {Name: "Sam", Initials: "S", Guest: true}},
		MorePeople:   1, SongCount: 12, LastActivity: "il y a 2 j", Next: next,
	}
	var b strings.Builder
	if err := webassets.TemplatesFor("fr").ExecuteTemplate(&b, "band-list-card", band); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	for _, want := range []string{`class="list-card is-branded is-dark has-header shape-soft"`, `style="--bg: #141210`, "☾", "Late sets", "12 morceaux", "+1", "Répétition", "/events/9", `href="/bands/4/setlists"`} {
		if !strings.Contains(html, want) {
			t.Errorf("band card: no %q in\n%s", want, html)
		}
	}

	community := communityListCard{
		communityCardView: communityCardView{CommunityCardRow: store.CommunityCardRow{ID: 7, Name: "Voice Lab", MemberCount: 5, SongCount: 3, MyRole: "member", Kind: "coaching"}},
		listCardLook:      listCardLook{Mark: "VL", Initials: true},
		Staff:             []listCardPerson{{Name: "Inès", Initials: "I"}},
		Practice:          []string{"Lip trills"}, PracticeCount: 3, MorePractice: 2,
		StudentPage: "/communities/7/students/1",
	}
	b.Reset()
	if err := webassets.TemplatesFor("en").ExecuteTemplate(&b, "community-list-card", community); err != nil {
		t.Fatal(err)
	}
	html = b.String()
	for _, want := range []string{`<article class="list-card">`, "Student", "Coaching", "Inès", "3 things to practise", "Lip trills", "and 2 more", "/communities/7/students/1", "Nothing on the calendar"} {
		if !strings.Contains(html, want) {
			t.Errorf("community card: no %q in\n%s", want, html)
		}
	}
	if strings.Contains(html, "style=") || strings.Contains(html, "staff admin") {
		t.Errorf("an unbranded member's card:\n%s", html)
	}
}
