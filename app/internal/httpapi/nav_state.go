package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	"app/internal/store"
	"app/internal/webassets"
)

// navState is what the global menus need to render a page: which global
// section it belongs to (the entry marked current, server-side, in the
// sidebar, the phone's tab bar and its sheets) and who is looking (the
// account chip, Admin). render puts it in every page's data as "Nav", so
// the menus never depend on each handler remembering to pass .Me.
type navState struct {
	Section string
	Me      *store.Person
}

// pageNavSections is the global section of each page, by template name.
// A handler that knows better (an event opened from Home) says so with
// "NavSection" in its data; pages missing here fall back on the space
// they show (navSectionFor).
var pageNavSections = map[string]string{
	"index":         webassets.NavHome,
	"welcome":       webassets.NavHome,
	"notifications": webassets.NavNotifications,

	"calendar_page":      webassets.NavCalendar,
	"calendar_subscribe": webassets.NavCalendar,

	"songbooks-index":    webassets.NavSongbooks,
	"my-songbooks":       webassets.NavSongbooks,
	"songbook":           webassets.NavSongbooks,
	"playlist":           webassets.NavSongbooks,
	"mp3-playlist":       webassets.NavSongbooks,
	"metronome-playlist": webassets.NavSongbooks,

	"catalog":         webassets.NavLibrary,
	"work_versions":   webassets.NavLibrary,
	"variant_history": webassets.NavLibrary,
	"gp-edit":         webassets.NavLibrary,

	"groups": webassets.NavGroups,

	"bands":                webassets.NavBands,
	"band":                 webassets.NavBands,
	"band_catalog":         webassets.NavBands,
	"band_setlists":        webassets.NavBands,
	"band_events":          webassets.NavBands,
	"band_members":         webassets.NavBands,
	"band_settings":        webassets.NavBands,
	"band_proposals":       webassets.NavBands,
	"band_proposal":        webassets.NavBands,
	"listening_party":      webassets.NavBands,
	"band_recordings":      webassets.NavBands,
	"band_song_recordings": webassets.NavBands,
	"band_recording_fixes": webassets.NavBands,

	"setlists-index":     webassets.NavSetlists,
	"setlist-overview":   webassets.NavSetlists,
	"setlist-clicktrack": webassets.NavSetlists,

	"communities":                webassets.NavCommunities,
	"community":                  webassets.NavCommunities,
	"community_songbooks":        webassets.NavCommunities,
	"community_default_songbook": webassets.NavCommunities,
	"songbook_access":            webassets.NavCommunities,
	"community_admin":            webassets.NavCommunities,
	"community_stats":            webassets.NavCommunities,
	"community_stats_member":     webassets.NavCommunities,
	"community_members":          webassets.NavCommunities,
	"community_downloads":        webassets.NavCommunities,
	"community_download_browse":  webassets.NavCommunities,
	"community_events":           webassets.NavCommunities,
	"coaching_students":          webassets.NavCommunities,
	"coaching_student":           webassets.NavCommunities,
	"coaching_groups":            webassets.NavCommunities,
	"coaching_group":             webassets.NavCommunities,
	"coaching_lessons":           webassets.NavCommunities,
	"coaching_repertoire":        webassets.NavCommunities,
	"coaching_program":           webassets.NavCommunities,
	"coaching_warmups":           webassets.NavCommunities,
	"coaching_exercise":          webassets.NavCommunities,
	"coaching_exercise_edit":     webassets.NavCommunities,
	"coaching_checking":          webassets.NavCommunities,
	"course":                     webassets.NavCommunities,
	"lesson":                     webassets.NavCommunities,

	"metronome": webassets.NavMetronome,
	"tuner":     webassets.NavTuner,

	"profile": webassets.NavProfile,

	"admin":        webassets.NavAdmin,
	"admin_person": webassets.NavAdmin,
	"dev_outbox":   webassets.NavAdmin,
}

// navSectionFor is the global section of page name showing data: the
// handler's own "NavSection" first, then the page's (pageNavSections),
// then the space the page is in.
func navSectionFor(name string, data map[string]any) string {
	if s, ok := data["NavSection"].(string); ok && s != "" {
		return s
	}
	if name == "song" {
		return songNavSection(data)
	}
	if s, ok := pageNavSections[name]; ok {
		return s
	}
	if present(data["Band"]) {
		return webassets.NavBands
	}
	if present(data["Community"]) {
		return webassets.NavCommunities
	}
	return ""
}

// songNavSection: a song belongs where it was opened from — its set
// list, band, lesson or community, songbook — and to the Library
// otherwise (the handler records that context: SetlistRef, BandRef…).
func songNavSection(data map[string]any) string {
	switch {
	case data["SetlistRef"] != nil:
		return webassets.NavSetlists
	case data["BandRef"] != nil:
		return webassets.NavBands
	case data["LessonHref"] != nil, data["CommunityRef"] != nil:
		return webassets.NavCommunities
	case data["SongbookRef"] != nil:
		return webassets.NavSongbooks
	}
	return webassets.NavLibrary
}

// present: v is a non-nil value (a typed nil pointer in an any is nil
// too).
func present(v any) bool {
	switch p := v.(type) {
	case nil:
		return false
	case *store.Band:
		return p != nil
	case *store.Community:
		return p != nil
	}
	return true
}

// navFromReferrer is the section of the app page r was opened from, ""
// when it doesn't say: an event opened from Home or the calendar belongs
// there, not to its band's or community's pages. "?from=home" (or
// calendar) says it outright, for a link that wants to; the Referer
// header otherwise, same site only.
func navFromReferrer(r *http.Request) string {
	from := r.URL.Query().Get("from")
	if from == "" {
		ref, err := url.Parse(r.Referer())
		if err != nil || ref.Host != r.Host {
			return ""
		}
		from = strings.Trim(ref.Path, "/")
		if i := strings.Index(from, "/"); i >= 0 {
			from = from[:i]
		}
	}
	switch from {
	case "home", "welcome":
		return webassets.NavHome
	case "notifications":
		return webassets.NavNotifications
	case "calendar":
		return webassets.NavCalendar
	}
	return ""
}

// eventNavSection is the global section of an event's page: Home or the
// calendar when it was opened from there, else its band's or community's.
func eventNavSection(r *http.Request, ev *store.Event) string {
	if s := navFromReferrer(r); s != "" {
		return s
	}
	return ownerNavSection(ev.OwnerKind)
}

// ownerNavSection is the section of a band's or a community's things.
func ownerNavSection(kind string) string {
	if kind == "band" {
		return webassets.NavBands
	}
	return webassets.NavCommunities
}

// addNav puts the page's navState in data as "Nav" (see navState).
func (s *Server) addNav(w http.ResponseWriter, name string, data any) {
	m, ok := data.(map[string]any)
	if !ok || m == nil {
		return
	}
	var me *store.Person
	if lw, ok := w.(*langWriter); ok {
		me = lw.person
	}
	if me == nil {
		me, _ = m["Me"].(*store.Person)
	}
	m["Nav"] = navState{Section: navSectionFor(name, m), Me: me}
}
