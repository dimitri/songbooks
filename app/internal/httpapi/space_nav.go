package httpapi

import (
	"fmt"
	"net/http"

	"app/internal/brand"
	"app/internal/store"
	"app/internal/webassets"
)

// A space is a band or a community: the place a group of people share,
// with its own sections (docs/navigation.md, the menu proposal's §3.3).
// Every page of a space shows the same header (its avatar, its name with
// the switcher, ⚙ for whoever runs it) and the same section tabs, built
// here once instead of by each page: a tab that showed on some pages and
// not others (Admin missing on Downloads, a coach shown the student's
// "My voice") came from each page working out who the viewer is on its
// own.

// Kinds of space. A coaching community is a community whose sections
// differ: a coach and their students.
const (
	spaceBand      = "band"
	spaceCommunity = "community"
	spaceCoaching  = "coaching"
)

// spaceRole is who is looking, as far as a space's sections care: whether
// they run it (a band's core member, a community's staff or a coach, the
// site admin), whether they may see its members, and who they are (a
// student's own page is theirs).
type spaceRole struct {
	Staff      bool
	SeeMembers bool
	PersonID   int64
}

// SpaceSection is one section of a space: a tab on a wide screen, a row
// of the space's home on a phone. Label is the English key the template
// translates; Path is relative to the space's home ("" for the home
// itself).
type SpaceSection struct {
	Key   string
	Label string
	Path  string
	Icon  string // an icon-* template name, for the phone's list
}

// spaceTabs is the one declared list of a kind of space's sections,
// filtered by role, in a fixed order: the same on every page of the
// space, never reordered.
func spaceTabs(kind string, role spaceRole) []SpaceSection {
	switch kind {
	case spaceBand:
		return []SpaceSection{
			{Key: "overview", Label: "Overview", Icon: "icon-home"},
			{Key: "catalog", Label: "Repertoire", Path: "/catalog", Icon: "icon-catalog"},
			{Key: "setlists", Label: "Set lists", Path: "/setlists", Icon: "icon-setlists"},
			{Key: "events", Label: "Calendar", Path: "/events", Icon: "icon-calendar"},
			{Key: "rehearsals", Label: "Rehearsals", Path: "/rehearsals", Icon: "icon-play"},
			{Key: "concerts", Label: "Concerts", Path: "/concerts", Icon: "icon-play"},
			{Key: "proposals", Label: "Proposals", Path: "/proposals", Icon: "icon-plus"},
			{Key: "discover", Label: "Discover", Path: "/listening-party", Icon: "icon-play"},
			{Key: "members", Label: "Members", Path: "/members", Icon: "icon-bands"},
		}
	case spaceCommunity, spaceCoaching:
		out := []SpaceSection{
			{Key: "overview", Label: "Overview", Icon: "icon-home"},
			{Key: "songbooks", Label: "Songbooks", Path: "/songbooks", Icon: "icon-catalog"},
		}
		if kind == spaceCoaching {
			if role.Staff {
				out = append(out,
					SpaceSection{Key: "students", Label: "Students", Path: "/students", Icon: "icon-bands"},
					SpaceSection{Key: "repertoire", Label: "Students’ songs", Path: "/student-songs", Icon: "icon-catalog"})
			} else {
				out = append(out, SpaceSection{Key: "voice", Label: "My voice", Path: fmt.Sprintf("/students/%d", role.PersonID), Icon: "icon-profile"})
			}
			out = append(out,
				SpaceSection{Key: "groups", Label: "coaching|Groups", Path: "/groups", Icon: "icon-bands"},
				SpaceSection{Key: "warmups", Label: "Warm-ups", Path: "/warmups", Icon: "icon-metronome"},
				SpaceSection{Key: "lessons", Label: "Lessons", Path: "/lessons", Icon: "icon-calendar"})
		}
		if role.SeeMembers {
			out = append(out, SpaceSection{Key: "members", Label: "Members", Path: "/members", Icon: "icon-bands"})
		}
		return append(out,
			SpaceSection{Key: "downloads", Label: "Downloads", Path: "/downloads", Icon: "icon-download"},
			SpaceSection{Key: "events", Label: "Events", Path: "/events", Icon: "icon-calendar"})
	}
	return nil
}

// spaceGear is the space's ⚙: its settings, for whoever may change them
// (nil for the others). A band's Settings are every member's (any member
// renames the band; core members change its look); a community's Admin is
// its staff's.
func spaceGear(kind string, role spaceRole) *SpaceSection {
	switch kind {
	case spaceBand:
		return &SpaceSection{Key: "settings", Label: "Settings", Path: "/settings", Icon: "icon-settings"}
	case spaceCommunity, spaceCoaching:
		if role.Staff {
			return &SpaceSection{Key: "admin", Label: "Admin", Path: "/admin", Icon: "icon-settings"}
		}
	}
	return nil
}

// SpaceNav is a space page's header and tabs, for the "space-nav"
// template: data["Space"].
type SpaceNav struct {
	Kind     string // spaceBand, spaceCommunity or spaceCoaching
	ID       int64
	Name     string
	Home     string // the space's home page, every section's path is under it
	Brand    *brand.Theme
	Sections []SpaceTab
	Gear     *SpaceTab
	Current  string // the open section's Key
}

// SpaceTab is a SpaceSection placed in one space: its full path, and
// whether it is the open one.
type SpaceTab struct {
	SpaceSection
	Href    string
	Current bool
}

// Initials are the avatar's letters when the space has no logo or mark
// (the same as its card's: list_cards.go).
func (n *SpaceNav) Initials() string {
	return initials(n.Name)
}

// KindLabel names the kind of space under its name.
func (n *SpaceNav) KindLabel() string {
	switch n.Kind {
	case spaceBand:
		return "Band"
	case spaceCoaching:
		return "Voice coaching"
	}
	return "Community"
}

// newSpaceNav places kind's sections, as role sees them, in the space id
// with current open.
func newSpaceNav(kind string, id int64, name string, role spaceRole, current string) *SpaceNav {
	home := fmt.Sprintf("/communities/%d", id)
	if kind == spaceBand {
		home = fmt.Sprintf("/bands/%d", id)
	}
	n := &SpaceNav{Kind: kind, ID: id, Name: name, Home: home, Current: current}
	for _, sec := range spaceTabs(kind, role) {
		n.Sections = append(n.Sections, SpaceTab{SpaceSection: sec, Href: home + sec.Path, Current: sec.Key == current})
	}
	if g := spaceGear(kind, role); g != nil {
		n.Gear = &SpaceTab{SpaceSection: *g, Href: home + g.Path, Current: g.Key == current}
	}
	return n
}

// spacePages are the pages that show a space's header and tabs, and the
// section each belongs to ("" when it depends on the page's data: see
// spaceSectionOf).
var spacePages = map[string]string{
	"band":            "overview",
	"band_catalog":    "catalog",
	"band_setlists":   "setlists",
	"band_events":     "events",
	"band_recordings": "",
	"band_proposals":  "proposals",
	"listening_party": "discover",
	"band_members":    "members",
	"band_settings":   "settings",

	"community":                  "overview",
	"community_songbooks":        "songbooks",
	"community_default_songbook": "songbooks",
	"course":                     "songbooks",
	"community_events":           "events",
	"community_downloads":        "downloads",
	"community_members":          "members",
	"community_admin":            "admin",
	"community_stats":            "admin",
	"coaching_students":          "students",
	"coaching_student":           "",
	"coaching_repertoire":        "repertoire",
	"coaching_groups":            "groups",
	"coaching_group":             "groups",
	"coaching_lessons":           "lessons",
	"coaching_warmups":           "warmups",
}

// spaceSectionOf is the section the page name shows: its entry in
// spacePages, else what its data says (a band's recordings are its
// rehearsals or its concerts; a student's page is the coach's Students or
// the student's own My voice).
func spaceSectionOf(name string, m map[string]any) string {
	if v, ok := m["SpaceSection"].(string); ok && v != "" {
		return v
	}
	if sec := spacePages[name]; sec != "" {
		return sec
	}
	switch name {
	case "band_recordings":
		if tab, _ := m["Tab"].(string); tab != "" {
			return tab
		}
		return "rehearsals"
	case "coaching_student":
		if coach, _ := m["IsCoach"].(bool); coach {
			return "students"
		}
		return "voice"
	}
	return ""
}

// addSpaceNav puts the space's header and tabs in a space page's data
// (data["Space"]), from the page's Band or Community and the viewer: the
// one place that decides which sections and which ⚙ someone sees, so
// every page of a space shows the same ones. Runs after addMembersTab
// (CanSeeMembers).
func (s *Server) addSpaceNav(w http.ResponseWriter, name string, data any) {
	m, ok := data.(map[string]any)
	if !ok {
		return
	}
	if _, isSpacePage := spacePages[name]; !isSpacePage {
		return
	}
	if _, set := m["Space"]; set {
		return
	}
	lw, ok := w.(*langWriter)
	if !ok || lw.person == nil {
		return
	}
	person := lw.person
	ctx := lw.r.Context()
	section := spaceSectionOf(name, m)
	var band *store.Band
	switch b := m["Band"].(type) {
	case *store.Band:
		band = b
	case store.Band:
		band = &b
	}
	if band != nil && band.ID != 0 {
		core := person.IsAdmin
		if !core {
			core, _ = s.store.IsCurrentBandCore(ctx, band.ID, person.ID)
		}
		nav := newSpaceNav(spaceBand, band.ID, band.Name, spaceRole{Staff: core, SeeMembers: true, PersonID: person.ID}, section)
		nav.Brand = webassets.BrandLookupFor("band", band.ID)
		m["Space"] = nav
		return
	}
	var community *store.Community
	switch c := m["Community"].(type) {
	case *store.Community:
		community = c
	case store.Community:
		community = &c
	}
	if community == nil || community.ID == 0 {
		return
	}
	staff := person.IsAdmin
	if !staff {
		staff, _ = s.store.HasAnyCommunityManagePrivilege(ctx, community.ID, person.ID)
	}
	see, _ := m["CanSeeMembers"].(bool)
	kind := spaceCommunity
	if community.IsCoaching() {
		kind = spaceCoaching
	}
	nav := newSpaceNav(kind, community.ID, community.Name, spaceRole{Staff: staff, SeeMembers: see, PersonID: person.ID}, section)
	nav.Brand = webassets.BrandLookupFor("community", community.ID)
	m["Space"] = nav
}
