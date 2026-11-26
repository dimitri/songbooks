package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"app/internal/brand"
	"app/internal/i18n"
	"app/internal/store"
)

// A band's ⚙ Settings (band_settings.html; docs/band-settings-proposal.md,
// docs/band-roles.md): the settings shell, its sections in this order —
// The band, Look, Roles, then the pages run elsewhere ("On other pages":
// Members and invites, the recordings' source), and last, apart, Archive
// or delete. Each person sees the sections they may change, and nothing
// else: a section nobody lets them change isn't listed, a field of a
// section they may open but not change reads as text.

// bandSettingsAccess is which of a band's settings one person may change.
type bandSettingsAccess struct {
	Name  bool // rename_archive: the band's name
	Plays bool // edit_band: what it plays, the language of its pages
	Words bool // edit_band: its tagline and website (kept in its brand)
	Look  bool // edit_look: its logo, card picture and style
	Roles bool // manage_roles: what each role allows, the band's own roles

	Archive bool // rename_archive: archive and unarchive
	Delete  bool // site admins only

	// The "On other pages" rows, links for someone who has the ⚙
	// anyway: the Members page (manage_members) and the recordings'
	// source (manage_recordings).
	Members    bool
	Recordings bool
}

// bandSettingsFor is what privs (the viewer's role's, a site admin's:
// store.AllBandPrivs) let one change in a band's Settings.
func bandSettingsFor(privs store.BandPrivs, isAdmin bool) bandSettingsAccess {
	return bandSettingsAccess{
		Name:       privs.Can(store.BandPrivRenameArchive),
		Plays:      privs.Can(store.BandPrivEditBand),
		Words:      privs.Can(store.BandPrivEditBand),
		Look:       privs.Can(store.BandPrivEditLook),
		Roles:      privs.Can(store.BandPrivManageRoles),
		Archive:    privs.Can(store.BandPrivRenameArchive),
		Delete:     isAdmin,
		Members:    privs.Can(store.BandPrivManageMembers),
		Recordings: privs.Can(store.BandPrivManageRecordings),
	}
}

// Band is whether The band section shows: one of its fields is theirs.
func (a bandSettingsAccess) Band() bool { return a.Name || a.Plays || a.Words }

// Danger is whether Archive or delete shows.
func (a bandSettingsAccess) Danger() bool { return a.Archive || a.Delete }

// Any is whether the band's ⚙ shows: at least one section is theirs.
func (a bandSettingsAccess) Any() bool { return a.Band() || a.Look || a.Roles || a.Danger() }

// Sections is the sections' ids, in the page's order.
func (a bandSettingsAccess) Sections() []string {
	var out []string
	for _, s := range []struct {
		id string
		on bool
	}{{"band", a.Band()}, {"look", a.Look}, {"roles", a.Roles}, {"danger", a.Danger()}} {
		if s.on {
			out = append(out, s.id)
		}
	}
	return out
}

// lookPreset is a preset as the Look section's swatches show it: its
// short name ("Paper"), its whole label, and the colours and fonts the
// live preview draws it with.
type lookPreset struct {
	Key, Name, Label                 string
	Background, Surface, Text, Muted string
	Accent, Accent2, Header          string
	FontDisplay, FontUI, Shape, Mark string
}

// lookPresets is brand.Presets for the swatches, in label order.
func lookPresets(lang string) []lookPreset {
	var out []lookPreset
	for _, k := range brand.PresetKeys() {
		p := brand.Presets[k]
		label := i18n.T(lang, p.Label)
		out = append(out, lookPreset{
			Key: k, Name: presetShortName(label), Label: label,
			Background: p.Brand.Background, Surface: p.Brand.Surface, Text: p.Brand.Text, Muted: p.Brand.Muted,
			Accent: p.Brand.Accent, Accent2: p.Brand.Accent2, Header: p.Brand.Header,
			FontDisplay: p.Brand.FontDisplay, FontUI: p.Brand.FontUI, Shape: p.Brand.Shape, Mark: p.Brand.Mark,
		})
	}
	return out
}

// presetShortName is a preset's label before its description: "Paper"
// for "Paper (cream, ink, serif titles)".
func presetShortName(label string) string {
	if i := strings.Index(label, " ("); i > 0 {
		return label[:i]
	}
	return label
}

// lookFont is a font as the live preview loads it.
type lookFont struct {
	Stack  string `json:"stack"`
	Google string `json:"google,omitempty"`
}

func lookFonts() map[string]lookFont {
	out := map[string]lookFont{}
	for k, f := range brand.Fonts {
		out[k] = lookFont{Stack: f.Stack, Google: f.Google}
	}
	return out
}

// bandPlaysLabel is what a band plays, in words.
func bandPlaysLabel(lang, musicType string) string {
	switch musicType {
	case "covers":
		return i18n.T(lang, "Covers")
	case "original":
		return i18n.T(lang, "Original music")
	}
	return i18n.T(lang, "What it plays: not set")
}

// bandLanguageLabel is the language of a band's pages, in words.
func bandLanguageLabel(lang, locale string) string {
	switch locale {
	case "fr":
		return "Français"
	case "en":
		return "English"
	}
	return i18n.T(lang, "language automatic")
}

// bandLookLabel is where a band's look stands: its preset, a look of its
// own, or the app's; with a logo or not.
func bandLookLabel(lang string, b brand.Brand) string {
	look := bandStyleLabel(lang, b)
	if b.LogoImage != "" || b.LogoURL != "" {
		return look + " · " + i18n.T(lang, "logo")
	}
	return look + " · " + i18n.T(lang, "no logo yet")
}

// bandStyleLabel is a band's style: its preset's name, a look of its
// own, or the app's.
func bandStyleLabel(lang string, b brand.Brand) string {
	switch p, ok := brand.Presets[b.Preset]; {
	case ok:
		return presetShortName(i18n.T(lang, p.Label))
	case b.Background != "" || b.Accent != "" || b.Header != "" || b.Text != "" || b.FontDisplay != "" || b.FontUI != "" || b.Shape != "":
		return i18n.T(lang, "A look of its own")
	}
	return i18n.T(lang, "The app’s look")
}

// bandSettingsHint is the band's ⚙ row on its phone home (space-hub):
// what it plays, its style, its roles — "Covers · Paper · 3 roles".
func bandSettingsHint(lang, musicType string, b brand.Brand, roles int) string {
	return bandPlaysLabel(lang, musicType) + " · " + bandStyleLabel(lang, b) + " · " + i18n.N(lang, roles, "%d role", "%d roles")
}

// roleNames is the band's roles' names, as the page shows them.
func roleNames(lang string, roles []store.BandRole) string {
	names := make([]string, 0, len(roles))
	for _, r := range roles {
		switch r.BuiltinKey {
		case "core":
			names = append(names, i18n.T(lang, "role|Core"))
		case "guest":
			names = append(names, i18n.T(lang, "role|Guest"))
		default:
			names = append(names, r.Name)
		}
	}
	return strings.Join(names, ", ")
}

// bandSettingsPage is what the Settings page needs beyond the band.
type bandSettingsPage struct {
	Access     bandSettingsAccess
	Brand      brand.Brand
	Locale     string
	Members    int // current members
	OpenLinks  int // the band's open links (QR codes)
	Setlists   int
	Roles      []store.BandRole
	Source     *store.BandRecordingSource
	BandRoles  *bandRolesData // the Roles section (manage_roles)
	Presets    []lookPreset
	Fonts      map[string]lookFont
	PlaysLabel string
}

// bandSettingsShell is the band's Settings in the shell, each row saying
// where it stands.
func bandSettingsShell(lang string, band *store.Band, p *bandSettingsPage) settingsShell {
	a := p.Access
	sh := settingsShell{Label: i18n.T(lang, "Settings sections"), Back: i18n.T(lang, "Settings")}
	if a.Band() {
		sh.Rows = append(sh.Rows, settingsRow{Section: "band", Icon: "icon-bands", Label: i18n.T(lang, "The band"),
			State: bandPlaysLabel(lang, band.MusicType) + " · " + bandLanguageLabel(lang, p.Locale)})
	}
	if a.Look {
		sh.Rows = append(sh.Rows, settingsRow{Section: "look", Icon: "icon-palette", Label: i18n.T(lang, "Look"), State: bandLookLabel(lang, p.Brand)})
	}
	if a.Roles {
		sh.Rows = append(sh.Rows, settingsRow{Section: "roles", Icon: "icon-roles", Label: i18n.T(lang, "Roles"), State: roleNames(lang, p.Roles)})
	}
	group := i18n.T(lang, "On other pages")
	if a.Members {
		state := i18n.N(lang, p.Members, "%d member", "%d members")
		if p.OpenLinks > 0 {
			state += " · " + i18n.N(lang, p.OpenLinks, "%d open link", "%d open links")
		}
		sh.Rows = append(sh.Rows, settingsRow{Href: fmt.Sprintf("/bands/%d/members", band.ID), Icon: "icon-community",
			Label: i18n.T(lang, "Members and invites"), State: state, Group: group})
		group = ""
	}
	if a.Recordings {
		state := i18n.T(lang, "Not set yet")
		if p.Source != nil {
			state = "pCloud"
			if p.Source.SyncedAt != nil {
				state += " · " + i18n.T(lang, "read %s", i18n.Date(lang, *p.Source.SyncedAt, "2 Jan"))
			}
		}
		sh.Rows = append(sh.Rows, settingsRow{Href: fmt.Sprintf("/bands/%d/recordings/rehearsals", band.ID), Icon: "icon-play",
			Label: i18n.T(lang, "Where the recordings come from"), State: state, Group: group})
	}
	if a.Danger() {
		state := i18n.T(lang, "The band is active")
		if band.Archived() {
			state = i18n.T(lang, "Archived on %s", i18n.Date(lang, *band.ArchivedAt, "2 January 2006"))
		}
		label := i18n.T(lang, "Archive or delete")
		if !a.Archive {
			label = i18n.T(lang, "Delete the band")
		}
		sh.Rows = append(sh.Rows, settingsRow{Section: "danger", Icon: "icon-archive", Label: label, State: state, Danger: true})
	}
	// An archived band opens on what can still be done: unarchive it.
	if band.Archived() && a.Danger() {
		sh.Default = "danger"
	}
	return sh
}

// handleBandSettingsPage is GET /bands/{id}/settings, the band's ⚙: for
// whoever may change one of its settings; anyone else goes to Members,
// where what their role allows is.
func (s *Server) handleBandSettingsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	person := personFromContext(r)
	band, err := s.store.GetBand(ctx, bandID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	privs, err := s.bandPrivs(r, bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	p := &bandSettingsPage{Access: bandSettingsFor(privs, person.IsAdmin)}
	if !p.Access.Any() {
		http.Redirect(w, r, fmt.Sprintf("/bands/%d/members", bandID), http.StatusSeeOther)
		return
	}
	fail := func(err error) bool {
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return true
		}
		return false
	}
	if p.Locale, err = s.store.BandLocale(ctx, bandID); fail(err) {
		return
	}
	if p.Brand, _, err = s.store.GetBrand(ctx, "band", bandID); fail(err) {
		return
	}
	if p.Roles, err = s.store.ListBandRoles(ctx, bandID); fail(err) {
		return
	}
	members, err := s.store.ListBandMembers(ctx, bandID, false)
	if fail(err) {
		return
	}
	p.Members = len(members)
	invites, err := s.store.ListGroupInvitesForTarget(ctx, "band", bandID)
	if fail(err) {
		return
	}
	p.OpenLinks = len(bandLinks(invites))
	if p.Access.Delete {
		setlists, err := s.store.ListSetlistsForBand(ctx, bandID)
		if fail(err) {
			return
		}
		p.Setlists = len(setlists)
	}
	if p.Access.Recordings {
		src, err := s.store.GetBandRecordingSource(ctx, bandID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			fail(err)
			return
		}
		p.Source = src
	}
	if p.Access.Roles {
		if p.BandRoles, err = s.bandRolesEditor(r, bandID); fail(err) {
			return
		}
	}
	lang := s.langOf(w)
	if p.Access.Look {
		p.Presets, p.Fonts = lookPresets(lang), lookFonts()
	}
	p.PlaysLabel = bandPlaysLabel(lang, band.MusicType)
	s.render(w, "band_settings", map[string]any{
		"Me": person, "Band": band, "Settings": p,
		"Shell": bandSettingsShell(lang, band, p),
		// Set lists use the live versions (albums.go).
		"SetlistVersions": s.setlistVersionsSettingFor(r.Context(), bandID, p.Access.Plays),
	})
}
