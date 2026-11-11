package webassets

// NavEntry is one destination in the app's global navigation — the
// single source of truth both the desktop sidebar and the mobile tab
// bar/"More" sheet render from, instead of each hand-rolling its own
// list (the drift this fixes: several page templates duplicated their
// own copy of the sidebar's primary list and it had already gone stale
// — e.g. one copy listed "Profile" where it should have listed
// "Communities"). See the navigation IA research this implements.
type NavEntry struct {
	Label         string
	Path          string
	Icon          string // an icon-* template name, dispatched via the "nav-icon" template below
	RequiresAdmin bool
	External      bool // opens in a new tab (target=_blank rel=noopener) — an external link, e.g. a songbook's playlist URL
}

// navDesktopPrimary is the desktop sidebar's primary destination list.
var navDesktopPrimary = []NavEntry{
	{Label: "Home", Path: "/home", Icon: "icon-home"},
	{Label: "Songbooks", Path: "/songbooks", Icon: "icon-catalog"},
	{Label: "Library", Path: "/catalog", Icon: "icon-view"},
	{Label: "Bands", Path: "/bands", Icon: "icon-bands"},
	{Label: "Communities", Path: "/communities", Icon: "icon-community"},
}

// navMobileTab is the mobile bottom tab bar's fixed slots (icon-only;
// Label is still used for the accessible data-label). Deliberately a
// narrower set than navDesktopPrimary — Apple HIG/Material both treat
// "fewer fixed items on a narrower surface, the rest in overflow" as
// normal, not drift, as long as the full destination set (desktop
// primary ∪ mobile tab ∪ mobile more) is declared once.
var navMobileTab = []NavEntry{
	{Label: "Home", Path: "/home", Icon: "icon-home"},
	{Label: "Songbooks", Path: "/songbooks", Icon: "icon-catalog"},
	{Label: "Library", Path: "/catalog", Icon: "icon-view"},
	{Label: "Bands", Path: "/bands", Icon: "icon-bands"},
}

// navMobileMore is the mobile "More" full-page sheet's row list —
// everything not in navMobileTab's 4 fixed slots, plus the handful of
// items (Metronome, Profile) desktop reaches via its own secondary/
// account areas rather than the primary list.
var navMobileMore = []NavEntry{
	{Label: "Home", Path: "/home", Icon: "icon-home"},
	{Label: "Songbooks", Path: "/songbooks", Icon: "icon-catalog"},
	{Label: "Library", Path: "/catalog", Icon: "icon-view"},
	{Label: "Bands", Path: "/bands", Icon: "icon-bands"},
	{Label: "Communities", Path: "/communities", Icon: "icon-community"},
	{Label: "Metronome", Path: "/metronome", Icon: "icon-metro-full"},
	{Label: "Profile", Path: "/profile", Icon: "icon-profile"},
	{Label: "Admin", Path: "/admin", RequiresAdmin: true},
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

// navGlobal returns one of the three declared lists by name — the only
// thing templates call; admin-gating (RequiresAdmin) is applied in the
// template itself, which already has the viewer in scope.
func navGlobal(surface string) []NavEntry {
	switch surface {
	case "desktop":
		return navDesktopPrimary
	case "mobileTab":
		return navMobileTab
	case "mobileMore":
		return navMobileMore
	}
	return nil
}
