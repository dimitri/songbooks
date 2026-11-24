package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"app/internal/brand"
	"app/internal/i18n"
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
// they run it (a community's staff or a coach, the site admin), whether
// they may change one of a band's settings (BandSettings: one of its
// Settings' sections is theirs, bandSettingsFor), whether they may see
// its members, and who
// they are (a student's own page is theirs).
type spaceRole struct {
	Staff        bool
	BandSettings bool
	SeeMembers   bool
	PersonID     int64
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
	// Filters are the section's own views (a band's Recordings: its
	// rehearsals, its concerts), in a segmented control under the tabs;
	// the first is where the section's tab goes.
	Filters []SpaceSection
	// ViewsInPage: the page draws its views itself, as chips at the head
	// of its list (space-views) rather than as a second row of tabs (a
	// band's Songbook: its songs, those to vote on, those to discover,
	// its influences).
	ViewsInPage bool
}

// spaceTabs is the one declared list of a kind of space's sections,
// filtered by role, in a fixed order: the same on every page of the
// space, never reordered.
func spaceTabs(kind string, role spaceRole) []SpaceSection {
	switch kind {
	case spaceBand:
		return []SpaceSection{
			{Key: "overview", Label: "Overview", Icon: "icon-home"},
			// The band's songs, its songbook (its PDF is the band's
			// book). The songs we might add are views of it, not sections
			// of their own (the old Proposals and Discover), shown as
			// chips at the head of its list.
			{Key: "catalog", Label: "Songbook", Path: "/catalog", Icon: "icon-catalog", ViewsInPage: true, Filters: []SpaceSection{
				{Key: "all", Label: "Songs", Path: "/catalog"},
				{Key: "vote", Label: "To vote", Path: "/catalog/vote"},
				{Key: "discover", Label: "Discover", Path: "/catalog/discover"},
				{Key: "influences", Label: "Influences", Path: "/catalog/influences"},
			}},
			{Key: "setlists", Label: "Set lists", Path: "/setlists", Icon: "icon-setlists"},
			{Key: "events", Label: "Calendar", Path: "/events", Icon: "icon-calendar"},
			// Rehearsals and concerts are both recordings.
			{Key: "recordings", Label: "Recordings", Path: "/recordings/rehearsals", Icon: "icon-play", Filters: []SpaceSection{
				{Key: "rehearsals", Label: "Rehearsals", Path: "/recordings/rehearsals"},
				{Key: "concerts", Label: "Concerts", Path: "/recordings/concerts"},
			}},
			{Key: "members", Label: "Members", Path: "/members", Icon: "icon-bands"},
		}
	case spaceCommunity:
		// A jam's or a course's community: registering for its events is
		// what people come for, then its songbooks and its files.
		out := []SpaceSection{
			{Key: "overview", Label: "Overview", Icon: "icon-home"},
			{Key: "events", Label: "Events", Path: "/events", Icon: "icon-calendar"},
			{Key: "songbooks", Label: "Songbooks", Path: "/songbooks", Icon: "icon-catalog"},
			{Key: "resources", Label: "Resources", Path: "/resources", Icon: "icon-download"},
		}
		if role.SeeMembers {
			out = append(out, SpaceSection{Key: "members", Label: "Members", Path: "/members", Icon: "icon-bands"})
		}
		return out
	case spaceCoaching:
		// A coach's studio. The coach sees their students (cards, a table,
		// song by song, by class), the calendar of lessons and events, then
		// what they teach with; members, invitations and settings are
		// behind ⚙ Admin. A student sees their week, their progress, the
		// same calendar and the same material. Never reordered.
		agenda := SpaceSection{Key: "agenda", Label: "Calendar", Path: "/lessons", Icon: "icon-calendar", Filters: []SpaceSection{
			{Key: "lessons", Label: "Lessons", Path: "/lessons"},
			{Key: "events", Label: "Events", Path: "/events"},
		}}
		material := []SpaceSection{
			{Key: "songbooks", Label: "Songbooks", Path: "/songbooks", Icon: "icon-catalog"},
			{Key: "warmups", Label: "Warm-ups", Path: "/warmups", Icon: "icon-metronome"},
			{Key: "resources", Label: "Resources", Path: "/resources", Icon: "icon-download"},
		}
		if role.Staff {
			return append([]SpaceSection{
				{Key: "overview", Label: "Overview", Icon: "icon-home"},
				{Key: "students", Label: "Students", Path: "/students", Icon: "icon-bands", Filters: []SpaceSection{
					{Key: "cards", Label: "Cards", Path: "/students?view=cards"},
					{Key: "table", Label: "Table", Path: "/students?view=table"},
					{Key: "songs", Label: "By song", Path: "/student-songs"},
					{Key: "classes", Label: "coaching|Groups", Path: "/groups"},
				}},
				agenda,
			}, material...)
		}
		out := []SpaceSection{{Key: "overview", Label: "My week", Icon: "icon-home"}}
		if role.PersonID != 0 { // 0: a member with nothing yet (an admin's preview)
			out = append(out, SpaceSection{Key: "progress", Label: "My progress", Path: fmt.Sprintf("/students/%d", role.PersonID), Icon: "icon-profile"})
		}
		return append(append(out, agenda), material...)
	}
	return nil
}

// spaceGear is the space's ⚙: its settings, for whoever may change them
// (nil for the others). A band's Settings are for whoever may change one
// of their sections (bandSettingsFor: its name and archive, what it
// plays, its look, its roles; deleting it, a site admin); what one's
// own role allows is read on Members. A community's Admin is its
// staff's.
func spaceGear(kind string, role spaceRole) *SpaceSection {
	switch kind {
	case spaceBand:
		if role.BandSettings {
			return &SpaceSection{Key: "settings", Label: "Settings", Path: "/settings", Icon: "icon-settings"}
		}
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
	// Archived: an archived band (docs/band-roles.md), read only — every
	// page of it says so.
	Archived bool
	Sections []SpaceTab
	Gear     *SpaceTab
	// SeeAs is a site admin's "See as…" on a coaching community's pages
	// (coaching_viewer.go), nil for anyone else.
	SeeAs   *seeAsSwitch
	Current string // the open section's Key
	// Filters are the open section's views, the open one Current: a
	// segmented control under the tabs (space-filters), nil when the
	// section has none.
	Filters []SpaceTab
	// ViewsInPage: the page shows Filters itself, as the chips at the
	// head of its list (space-views), not under the tabs.
	ViewsInPage bool
	// Switcher is the page's global navigation (navState), whose Spaces
	// feed the header's space switcher ("space-switcher", its "header"
	// variant). Nil, or no current space (one the viewer isn't a member
	// of), and the header shows the space's name alone.
	Switcher *navState
}

// SpaceTab is a SpaceSection placed in one space: its full path, and
// whether it is the open one.
type SpaceTab struct {
	SpaceSection
	Href    string
	Current bool
	// On the space's home on a phone, each section's row says where it
	// stands ("43 songs", "Saturday"); Due when it waits on the viewer.
	Hint string
	Due  bool
}

// spaceHint is what a space's home says of one of its sections, in the
// viewer's language: data["SpaceHints"], by section Key.
type spaceHint struct {
	Text string
	Due  bool
}

// withHints puts the home page's word on each section (and ⚙).
func (n *SpaceNav) withHints(hints map[string]spaceHint) {
	for i := range n.Sections {
		if h, ok := hints[n.Sections[i].Key]; ok {
			n.Sections[i].Hint, n.Sections[i].Due = h.Text, h.Due
		}
	}
	if n.Gear != nil {
		if h, ok := hints[n.Gear.Key]; ok {
			n.Gear.Hint, n.Gear.Due = h.Text, h.Due
		}
	}
}

// IsHome reports whether the page is the space's home (its overview),
// the page that lists its sections on a phone.
func (n *SpaceNav) IsHome() bool { return n.Current == "overview" }

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
// current is the open section's Key, or "section/filter" for one of its
// views ("catalog/vote").
func newSpaceNav(kind string, id int64, name string, role spaceRole, current string) *SpaceNav {
	home := fmt.Sprintf("/communities/%d", id)
	if kind == spaceBand {
		home = fmt.Sprintf("/bands/%d", id)
	}
	current, filter, _ := strings.Cut(current, "/")
	n := &SpaceNav{Kind: kind, ID: id, Name: name, Home: home, Current: current}
	for _, sec := range spaceTabs(kind, role) {
		n.Sections = append(n.Sections, SpaceTab{SpaceSection: sec, Href: home + sec.Path, Current: sec.Key == current})
		if sec.Key != current {
			continue
		}
		if filter == "-" {
			continue // a page inside the section (one student, one class): no views
		}
		n.ViewsInPage = sec.ViewsInPage
		for i, f := range sec.Filters {
			on := f.Key == filter || filter == "" && i == 0
			n.Filters = append(n.Filters, SpaceTab{SpaceSection: f, Href: home + f.Path, Current: on})
		}
	}
	if kind == spaceBand && current == "settings" {
		role.BandSettings = true // the page open is in the header, whoever opened it
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
	"band_catalog":    "catalog/all",
	"band_setlists":   "setlists",
	"band_events":     "events",
	"band_recordings": "",
	"band_proposals":  "catalog/vote",
	"band_discover":   "catalog/discover",
	"band_influences": "catalog/influences",
	"band_members":    "members",
	"band_settings":   "settings",

	"community":                  "overview",
	"community_songbooks":        "songbooks",
	"community_default_songbook": "songbooks",
	"course":                     "songbooks",
	"community_events":           "events",
	"community_downloads":        "resources",
	"community_members":          "members",
	"community_admin":            "admin",
	"community_stats":            "admin",
	"coaching_students":          "",
	"coaching_student":           "",
	"coaching_repertoire":        "students/songs",
	"coaching_groups":            "students/classes",
	"coaching_group":             "students/-",
	"coaching_lessons":           "agenda/lessons",
	"coaching_warmups":           "warmups",
	"coaching_songbooks":         "songbooks",
	"coaching_book":              "songbooks",
}

// coachingSections are where a coaching studio keeps the pages every
// community has: its events in its calendar, its members with its Admin.
var coachingSections = map[string]string{
	"events":  "agenda/events",
	"members": "admin",
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
			return "recordings/" + tab
		}
		return "recordings/rehearsals"
	case "coaching_students":
		if v, _ := m["StudentsView"].(string); v != "" {
			return "students/" + v
		}
		return "students"
	case "coaching_student":
		// One student's page: the coach's, under Students (no view of its
		// own); the student's own progress.
		if coach, _ := m["IsCoach"].(bool); coach {
			return "students/-"
		}
		return "progress"
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
		// Each admin tab for whoever holds its privilege (a site admin:
		// all of them, but rename_archive's alone while it's archived).
		privs := store.AllBandPrivs(band.Archived())
		if !person.IsAdmin {
			privs, _ = s.store.BandPrivileges(ctx, band.ID, person.ID)
		}
		settings := bandSettingsFor(privs, person.IsAdmin).Any()
		nav := newSpaceNav(spaceBand, band.ID, band.Name, spaceRole{BandSettings: settings, SeeMembers: true, PersonID: person.ID}, section)
		nav.Brand = webassets.BrandLookupFor("band", band.ID)
		nav.Archived = band.Archived()
		if hints, ok := m["SpaceHints"].(map[string]spaceHint); ok {
			nav.withHints(hints)
		}
		if nav.ViewsInPage {
			s.countSongbookViews(lw.r, band.ID, nav)
		}
		nav.Switcher = pageNavState(m)
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
	roleID := person.ID
	var seeAs *seeAsSwitch
	if community.IsCoaching() {
		kind = spaceCoaching
		// The coach's sections or the student's, as the pages decide
		// (coachingViewerOf), an admin's "See as…" included.
		// (The writer's request is the one before requireAuth: the
		// person goes in its context here.)
		pr := lw.r.WithContext(context.WithValue(ctx, personCtxKey, person))
		if v, err := s.coachingViewerOf(pr, community.ID); err == nil {
			staff, roleID = v.Coach, v.PersonID
			seeAs = s.seeAsFor(pr, community, v)
		}
	}
	if kind == spaceCoaching {
		if moved, ok := coachingSections[section]; ok {
			section = moved
		}
		// A student has no Students section: their classes (the only
		// ones they see) are part of their progress.
		if !staff && strings.HasPrefix(section, "students") {
			section = "progress"
		}
	}
	nav := newSpaceNav(kind, community.ID, community.Name, spaceRole{Staff: staff, SeeMembers: see, PersonID: roleID}, section)
	nav.SeeAs = seeAs
	nav.Brand = webassets.BrandLookupFor("community", community.ID)
	if hints, ok := m["SpaceHints"].(map[string]spaceHint); ok {
		nav.withHints(hints)
	}
	nav.Switcher = pageNavState(m)
	m["Space"] = nav
}

// pageNavState is the navState that render's addNav put in a page's
// data, for the space header's switcher; nil when the page has none.
func pageNavState(m map[string]any) *navState {
	if n, ok := m["Nav"].(navState); ok {
		return &n
	}
	return nil
}

// dayHint says when t is in lang, as the Next up card does: today,
// tomorrow, a weekday within the week, a short date after that.
func dayHint(lang string, t time.Time) string {
	now := time.Now()
	switch w := humanDay(t, now); w {
	case "Today", "Tomorrow":
		return i18n.T(lang, w)
	case t.In(now.Location()).Format("Monday 2 Jan"):
		return i18n.Date(lang, t, "Monday 2 Jan")
	}
	return i18n.Date(lang, t, "Mon 2 Jan")
}

// countSongbookViews puts on a band's Songbook chips how many songs it
// holds and how many proposals wait for a vote (Due), so "To vote" says
// when there is something to do. Best effort: no count on an error.
func (s *Server) countSongbookViews(r *http.Request, bandID int64, n *SpaceNav) {
	for i := range n.Filters {
		f := &n.Filters[i]
		switch f.Key {
		case "all":
			if songs, err := s.store.ListBandCatalog(r.Context(), bandID); err == nil {
				f.Hint = fmt.Sprint(len(songs))
			}
		case "vote":
			if open, err := s.store.ListProposals(r.Context(), bandID, store.ProposalFilter{Status: "open"}); err == nil && len(open) > 0 {
				f.Hint, f.Due = fmt.Sprint(len(open)), true
			}
		}
	}
}

// SectionLabel is the open section's name (its English key), for the
// page's heading and its browser tab: "Songbook". The home's is "".
func (n *SpaceNav) SectionLabel() string {
	if n.Gear != nil && n.Gear.Current {
		return n.Gear.Label
	}
	for _, sec := range n.Sections {
		if sec.Current && sec.Key != "overview" {
			return sec.Label
		}
	}
	return ""
}

// spaceExport is a space's songbook as files, for its section's toolbar:
// its PDF (the space-export template: a PDF button, the layout and notes
// chosen in its dialog) and its playlist (a button of its own beside
// it), "" when it has none.
type spaceExport struct {
	PDF      string
	Playlist string
}
