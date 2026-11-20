package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"app/internal/i18n"
	"app/internal/store"
)

// Search (menu proposal §3.8): one palette, opened with ⌘K or Ctrl-K, the
// sidebar's "Rechercher… ⌘K" field, or the 🔍 in the phone's top bar of
// Accueil and Bibliothèque, finds songs, songbooks, set lists, groups,
// events and, for a coach, students — only what the person may see
// (store.Search) — and commands, each with its shortcut. The current
// space's finds come first. /api/search feeds the palette
// (static/js/search.js); /search is the same results as a page, for a
// phone's full screen without scripts and for the sidebar's field
// submitted as a form. Page-level filters (a repertoire's, a set list's)
// stay as they are.

// searchItem is one row of the palette.
type searchItem struct {
	Kind     string   `json:"kind"` // a store.SearchHit kind, or "command"
	Title    string   `json:"title"`
	Subtitle string   `json:"subtitle,omitempty"`
	URL      string   `json:"url,omitempty"`
	Action   string   `json:"action,omitempty"` // a command done on the page itself: "stage"
	Keys     []string `json:"keys,omitempty"`   // a command's shortcut, key by key
	Here     bool     `json:"here,omitempty"`   // in the space the person is in
}

// searchGroup is a heading and its rows.
type searchGroup struct {
	Kind  string       `json:"kind"`
	Label string       `json:"label"`
	Items []searchItem `json:"items"`
}

// searchKinds are the finds' groups, in the palette's order.
var searchKinds = []struct{ kind, label string }{
	{"song", "Songs"}, {"songbook", "Songbooks"}, {"setlist", "Set lists"},
	{"group", "search|Groups"}, {"student", "Students"}, {"event", "Events"},
}

// searchSpace is the space the person is searching from ("band:17",
// "community:7"; the palette reads it off the page's address).
type searchSpace struct {
	kind string
	id   int64
}

func parseSearchSpace(s string) searchSpace {
	kind, id, ok := strings.Cut(s, ":")
	n, err := strconv.ParseInt(id, 10, 64)
	if !ok || err != nil || (kind != "band" && kind != "community") {
		return searchSpace{}
	}
	return searchSpace{kind, n}
}

func (sp searchSpace) holds(bandID, communityID int64) bool {
	return sp.id != 0 && ((sp.kind == "band" && bandID == sp.id) || (sp.kind == "community" && communityID == sp.id))
}

// searchHitItem is a find as a row: where it opens, and what's under it.
func searchHitItem(lang string, h store.SearchHit, now time.Time) searchItem {
	it := searchItem{Kind: h.Kind, Title: h.Title, Subtitle: h.Subtitle}
	switch h.Kind {
	case "song":
		it.URL = fmt.Sprintf("/song/%d", h.ID)
		if h.BandID != 0 {
			it.URL += fmt.Sprintf("?band=%d", h.BandID)
		} else if h.CommunityID != 0 {
			it.URL += fmt.Sprintf("?community=%d", h.CommunityID)
		}
	case "songbook":
		switch {
		case h.IsDefault && h.BandID != 0:
			it.URL = fmt.Sprintf("/bands/%d/catalog", h.BandID)
		case h.IsDefault && h.CommunityID != 0:
			it.URL = fmt.Sprintf("/communities/%d/songbooks/default", h.CommunityID)
		default:
			it.URL = fmt.Sprintf("/songbooks/%d", h.ID)
		}
	case "setlist":
		it.URL = fmt.Sprintf("/setlists/%d", h.ID)
	case "band":
		it.URL = fmt.Sprintf("/bands/%d", h.ID)
		it.Subtitle = i18n.T(lang, "Band")
	case "community":
		it.URL = fmt.Sprintf("/communities/%d", h.ID)
		it.Subtitle = i18n.T(lang, "Community")
	case "event":
		it.URL = fmt.Sprintf("/events/%d", h.ID)
		if h.EventType == "lesson" {
			it.URL = fmt.Sprintf("/communities/%d/lessons", h.CommunityID)
		}
		if h.At != nil {
			it.Subtitle = joinDot(i18n.Date(lang, h.At.In(now.Location()), "Mon 2 Jan")+" "+h.At.In(now.Location()).Format("15:04"), h.Subtitle)
		}
	case "student":
		it.URL = fmt.Sprintf("/communities/%d/students/%d", h.CommunityID, h.PersonID)
	}
	return it
}

// searchResults are the groups of finds for q, the current space's first
// in each group, at most perKind a group.
func (s *Server) searchResults(ctx context.Context, lang string, person *store.Person, q string, space searchSpace, perKind int) ([]searchGroup, error) {
	hits, err := s.store.Search(ctx, person.ID, q, perKind)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	byKind := map[string][]searchItem{}
	for _, h := range hits {
		it := searchHitItem(lang, h, now)
		it.Here = space.holds(h.BandID, h.CommunityID)
		k := h.Kind
		if k == "band" || k == "community" {
			k = "group"
		}
		byKind[k] = append(byKind[k], it)
	}
	var groups []searchGroup
	for _, k := range searchKinds {
		items := byKind[k.kind]
		if len(items) == 0 {
			continue
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].Here && !items[j].Here })
		groups = append(groups, searchGroup{Kind: k.kind, Label: i18n.T(lang, k.label), Items: items})
	}
	// The group the current space's finds lead with comes first.
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Items[0].Here && !groups[j].Items[0].Here })
	return groups, nil
}

// paletteCommands are the commands the person can run, matching q (all
// of them when q is empty), each with its shortcut (search.js runs the
// shortcuts).
func (s *Server) paletteCommands(ctx context.Context, lang string, person *store.Person, q string, space searchSpace) ([]searchItem, error) {
	cmds := []searchItem{
		{Title: i18n.T(lang, "Home"), URL: "/home", Keys: []string{"g", "h"}},
		{Title: i18n.T(lang, "Library"), URL: "/catalog", Keys: []string{"g", "l"}},
		{Title: i18n.T(lang, "Calendar"), URL: "/calendar", Keys: []string{"g", "a"}},
		{Title: i18n.T(lang, "Notifications"), URL: "/notifications", Keys: []string{"g", "n"}},
		{Title: i18n.T(lang, "Metronome"), URL: "/metronome", Keys: []string{"g", "b"}},
		{Title: i18n.T(lang, "Tuner"), URL: "/tuner", Keys: []string{"g", "t"}},
	}
	lessonURL, err := s.newLessonURL(ctx, person, space)
	if err != nil {
		return nil, err
	}
	if lessonURL != "" {
		cmds = append(cmds, searchItem{Title: i18n.T(lang, "New lesson"), URL: "/new/lesson", Keys: []string{"n", "l"}})
	}
	cmds = append(cmds,
		searchItem{Title: i18n.T(lang, "New set list"), URL: "/new/setlist", Keys: []string{"n", "s"}},
		// Done on the song page itself; search.js offers it only there.
		searchItem{Title: i18n.T(lang, "Stage mode"), Action: "stage", Keys: []string{"Shift+s"}},
	)
	fq := store.FoldName(strings.TrimSpace(q))
	out := []searchItem{}
	for _, c := range cmds {
		c.Kind = "command"
		if fq == "" || strings.Contains(store.FoldName(c.Title), fq) {
			out = append(out, c)
		}
	}
	return out, nil
}

// newLessonURL is where "New lesson" goes: the lessons' page of the
// coaching community the person is in, when they coach there, else of
// the first one they coach; "" when they coach none.
func (s *Server) newLessonURL(ctx context.Context, person *store.Person, space searchSpace) (string, error) {
	communities, err := s.store.ListCommunityCardsForPerson(ctx, person.ID)
	if err != nil {
		return "", err
	}
	first := ""
	for _, c := range communities {
		if !c.IsCoaching() {
			continue
		}
		coach, err := s.store.HasAnyCommunityManagePrivilege(ctx, c.ID, person.ID)
		if err != nil {
			return "", err
		}
		if !coach {
			continue
		}
		u := fmt.Sprintf("/communities/%d/lessons#new", c.ID)
		if space.kind == "community" && space.id == c.ID {
			return u, nil
		}
		if first == "" {
			first = u
		}
	}
	return first, nil
}

// searchSpaceOf is the space a request comes from: ?space=, else the
// page it was opened from (Referer), for the /new/… shortcuts.
func searchSpaceOf(r *http.Request) searchSpace {
	if sp := parseSearchSpace(r.URL.Query().Get("space")); sp.id != 0 {
		return sp
	}
	ref := r.Referer()
	for _, kind := range []string{"band", "community"} {
		marker := "/" + kind + "s/"
		if i := strings.Index(ref, marker); i >= 0 {
			rest := ref[i+len(marker):]
			if j := strings.IndexAny(rest, "/?#"); j >= 0 {
				rest = rest[:j]
			}
			if id, err := strconv.ParseInt(rest, 10, 64); err == nil {
				return searchSpace{kind, id}
			}
		}
	}
	return searchSpace{}
}

// handleSearchAPI is GET /api/search?q=…&space=band:17: the palette's
// commands and finds.
func (s *Server) handleSearchAPI(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	lang := s.langOfRequest(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 100 {
		q = q[:100]
	}
	space := searchSpaceOf(r)
	cmds, err := s.paletteCommands(r.Context(), lang, person, q, space)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	groups, err := s.searchResults(r.Context(), lang, person, q, space, 5)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if groups == nil {
		groups = []searchGroup{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"q": q, "commands": cmds, "groups": groups, "commandsLabel": i18n.T(lang, "Commands")})
}

// handleSearchPage is GET /search?q=…: the same, as a page.
func (s *Server) handleSearchPage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	lang := s.langOfRequest(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 100 {
		q = q[:100]
	}
	space := searchSpaceOf(r)
	groups, err := s.searchResults(r.Context(), lang, person, q, space, 20)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cmds, err := s.paletteCommands(r.Context(), lang, person, q, space)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var pageCmds []searchItem
	for _, c := range cmds {
		if c.URL != "" { // not the song page's own
			pageCmds = append(pageCmds, c)
		}
	}
	s.render(w, "search", map[string]any{"Me": person, "Q": q, "Groups": groups, "Commands": pageCmds})
}

// handleNewLesson is GET /new/lesson, "New lesson" (n l): the lessons'
// page of the coaching community the person is in, open on a new one.
func (s *Server) handleNewLesson(w http.ResponseWriter, r *http.Request) {
	u, err := s.newLessonURL(r.Context(), personFromContext(r), searchSpaceOf(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if u == "" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, u, http.StatusFound)
}

// handleNewSetlist is GET /new/setlist, "New set list" (n s): the set
// lists' page, open on a new one for the band the person is in (when
// they're one of its members), else a personal one.
func (s *Server) handleNewSetlist(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	u := "/setlists#new"
	if sp := searchSpaceOf(r); sp.kind == "band" {
		if ok, err := s.store.IsCurrentBandMember(r.Context(), sp.id, person.ID); err == nil && ok {
			u = fmt.Sprintf("/setlists#new-band-%d", sp.id)
		}
	}
	http.Redirect(w, r, u, http.StatusFound)
}
