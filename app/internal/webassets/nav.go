package webassets

import "strings"

// NavEntry is one destination in the app's global navigation. navEntries
// below is the single source of truth every surface renders from — the
// desktop sidebar, the phone's tab bar and the tablet's rail — instead
// of each hand-rolling its own list (several page templates once kept
// their own copy of the sidebar and it went stale). Each entry says
// which surfaces show it.
type NavEntry struct {
	Label         string
	Path          string
	Icon          string // an icon-* template name, dispatched via the "nav-icon" template
	RequiresAdmin bool
	External      bool // opens in a new tab (target=_blank rel=noopener) — an external link, e.g. a songbook's playlist URL
	// Section is the NavSection this entry is the destination of: the
	// entry is current (highlighted, aria-current="page") on every page
	// whose handler says it belongs to that section — see NavCurrentOn.
	Section string
	// Covers lists the other sections this entry stands for on a surface
	// that has no entry of their own, space-separated: the phone's Groups
	// tab is current on a band's or a community's pages, its Tools tab on
	// the metronome's, its Home tab on the notifications.
	Covers string
	// The surfaces that show it: the desktop sidebar's main list
	// (Sidebar) or its Tools group (SidebarTools), and the phone's tab
	// bar, which is also the tablet's rail (Bar).
	Sidebar, SidebarTools, Bar bool
	// Badge names the count this entry may carry ("notifications"): the
	// menus render an empty, hidden slot for it (data-nav-badge), which
	// the notifications work fills.
	Badge string
}

// The global sections a page can belong to (its NavSection, which the
// server works out per page: see navSectionFor in internal/httpapi). The
// global menus mark the entry for the current one, server-side, instead
// of guessing from the URL in the browser — a URL says nothing about
// where an event was opened from, and most pages' URLs matched no entry.
const (
	NavHome          = "home"
	NavCalendar      = "calendar"
	NavLibrary       = "library"
	NavBands         = "bands"
	NavCommunities   = "communities"
	NavGroups        = "groups"
	NavTools         = "tools"
	NavMetronome     = "metronome"
	NavTuner         = "tuner"
	NavProfile       = "profile"
	NavAdmin         = "admin"
	NavNotifications = "notifications"
)

// navEntries is the app's global navigation, in order (menu proposal
// §3.2): Home, Library, Calendar, Notifications, Groups and the tools on
// desktop; Home, Library, Calendar, Groups and Tools on a phone, five
// tabs and no "More". The account (Profile, Appearance, Language, Admin,
// Log out) is the avatar's menu, not a destination here: see the
// "account-menu" template.
var navEntries = []NavEntry{
	{Label: "Home", Path: "/home", Icon: "icon-home", Section: NavHome, Covers: NavNotifications, Sidebar: true, Bar: true},
	{Label: "Library", Path: "/catalog", Icon: "icon-catalog", Section: NavLibrary, Sidebar: true, Bar: true},
	{Label: "Calendar", Path: "/calendar", Icon: "icon-calendar", Section: NavCalendar, Sidebar: true, Bar: true},
	{Label: "Notifications", Path: "/notifications", Icon: "icon-bell", Section: NavNotifications, Sidebar: true, Badge: "notifications"},
	{Label: "Groups", Path: "/groups", Icon: "icon-bands", Section: NavGroups, Covers: NavBands + " " + NavCommunities, Bar: true},
	{Label: "Tools", Path: "/tools", Icon: "icon-tools", Section: NavTools, Covers: NavMetronome + " " + NavTuner, Bar: true},
	{Label: "Metronome", Path: "/metronome", Icon: "icon-metronome", Section: NavMetronome, SidebarTools: true},
	{Label: "Tuner", Path: "/tuner", Icon: "icon-tuner", Section: NavTuner, SidebarTools: true},
}

// navGlobal is the entries one surface shows, in order: "sidebar",
// "sidebarTools" or "bar" (the phone's tab bar and the tablet's rail).
func navGlobal(surface string) []NavEntry {
	var out []NavEntry
	for _, e := range navEntries {
		if onSurface(e, surface) {
			out = append(out, e)
		}
	}
	return out
}

func onSurface(e NavEntry, surface string) bool {
	switch surface {
	case "sidebar":
		return e.Sidebar
	case "sidebarTools":
		return e.SidebarTools
	case "bar":
		return e.Bar
	}
	return false
}

// NavCurrentOn reports whether e is the entry for section on a surface:
// its own section, or one it covers when that surface has no entry of
// that section's own (Notifications has its own entry in the sidebar,
// so Home only stands for it on the phone's bar). The sidebar's main
// list and its Tools group count as one surface.
func NavCurrentOn(surface string, e NavEntry, section string) bool {
	if section == "" {
		return false
	}
	if e.Section == section {
		return true
	}
	if !strings.Contains(" "+e.Covers+" ", " "+section+" ") {
		return false
	}
	for _, o := range navEntries {
		if o.Section == section && (onSurface(o, surface) || (surface == "sidebar" && o.SidebarTools) || (surface == "sidebarTools" && o.Sidebar)) {
			return false
		}
	}
	return true
}

// ContextualGroup is a page's own "This band"/"This community"/etc.
// action group — rendered by the shared desktop-sidebar partial
// (sidebar-secondary sidebar-song-group) right under the global primary
// list, instead of six page templates each hand-rolling their own copy
// of the ENTIRE sidebar (including the global list, which is exactly
// how it drifted — one copy went stale and listed "Profile" where it
// should have listed "Communities"). A handler builds one of these and
// puts it in its page data as "ContextualGroup"; pages with nothing
// page-specific to show just omit the field.
type ContextualGroup struct {
	Label   string
	Entries []NavEntry
	// PDFExportPath, when set, renders the standard "export this as one
	// PDF" cluster (link + jazz/map/lyrics mode select + notes toggle) —
	// the exact same widget band_catalog.html/community_default_songbook.html/
	// project.html/setlist_overview.html each used to hand-roll their
	// own copy of, varying only in this URL.
	PDFExportPath string
	// PDFExportLabel customizes the link text ("Export songbook (PDF)"
	// by default when empty).
	PDFExportLabel string
}
