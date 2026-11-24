package httpapi

import (
	"fmt"

	"app/internal/i18n"
	"app/internal/store"
)

// The settings shell (settings_shell.html, static/js/settings-shell.js):
// a space's ⚙ page in sections — a sidebar of them on a wide screen, each
// with where it stands, a list to tap into on a phone, one section at a
// time by the URL's #hash. A community's Admin and a band's Settings
// share it (docs/band-settings-proposal.md §3.1).

// settingsRow is one row of the shell's list: a section of the page
// (Section, its element's id) or a link to another page (Href).
type settingsRow struct {
	Section string // the section's id, its #hash; "" for a link out
	Href    string // another page, when Section is ""
	Icon    string // an icon-* template name ("settings-icon" draws it)
	Label   string // translated
	State   string // where it stands, translated
	// Group is a heading shown above this row ("On other pages"),
	// translated; Danger sets the row apart, after a divider, in the
	// danger colour (archive, delete).
	Group  string
	Danger bool
}

// settingsShell is the shell's list and its words.
type settingsShell struct {
	Label string // the nav's accessible name, translated
	Back  string // a section's way back to the list on a phone, translated
	// Default is the section a wide screen opens on without a #hash
	// ("" : the first).
	Default string
	Rows    []settingsRow
}

// communityAdminShell is a community's Admin in the shell: its name and
// look, its members, joining, its roles, its options, and its activity
// (a page of its own).
func communityAdminShell(lang string, c *store.Community, members, pending, roles int, locale string) settingsShell {
	memberState := i18n.N(lang, members, "%d member", "%d members")
	if pending > 0 {
		memberState += " · " + i18n.N(lang, pending, "%d invite pending", "%d invites pending")
	}
	language := i18n.T(lang, "Language: automatic")
	switch locale {
	case "fr":
		language = "Français"
	case "en":
		language = "English"
	}
	return settingsShell{
		Label: i18n.T(lang, "Admin sections"),
		Back:  i18n.T(lang, "All sections"),
		Rows: []settingsRow{
			{Section: "identity", Icon: "icon-community", Label: i18n.T(lang, "Name and look"), State: c.Name},
			{Section: "members", Icon: "icon-bands", Label: i18n.T(lang, "Members"), State: memberState},
			{Section: "joining", Icon: "icon-plus", Label: i18n.T(lang, "Joining and invites"), State: i18n.T(lang, "links, QR codes, adding people")},
			{Section: "roles", Icon: "icon-profile", Label: i18n.T(lang, "Roles and staff"), State: i18n.N(lang, roles, "%d role", "%d roles")},
			{Section: "options", Icon: "icon-settings", Label: i18n.T(lang, "Options"), State: language},
			{Href: fmt.Sprintf("/communities/%d/admin/stats", c.ID), Icon: "icon-view", Label: i18n.T(lang, "Activity"), State: i18n.T(lang, "who viewed and downloaded what")},
		},
	}
}
