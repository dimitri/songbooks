package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"app/internal/store"
)

// The space switcher (menu PR 7): the bands and communities one belongs
// to, for the desktop sidebar's "My groups" list, the switcher's sheet
// (the "space-switcher" template) and the phone's Groups tab, which
// reopens the space used last.

// sidebarSpaces is how many spaces the sidebar lists before "All my
// groups".
const sidebarSpaces = 6

// navSpace is one space in the switcher.
type navSpace struct {
	Kind     string // "band" or "community"
	ID       int64
	Name     string
	Initials string // its avatar's letters, as on its card: "CA" for Couleur Acoustique
	Tone     int    // its avatar's colour, 0–7
	URL      string // its home page
	// Sub says what it is: "Band", "Community" or "Coaching" (translated
	// where it's shown).
	Sub     string
	Pinned  bool
	Current bool // the page is in this space
}

// navSpaces is the switcher's part of a page's navState.
type navSpaces struct {
	All     []navSpace // every space, pinned first, then the last used
	Shown   []navSpace // the sidebar's: the first sidebarSpaces of All
	Current *navSpace  // the space the page is in, nil outside one
	// LastURL is the home of the space used last, for the phone's Groups
	// tab; "" when one never used any (the tab opens "My groups").
	LastURL string
}

// More is how many spaces the sidebar leaves to "All my groups".
func (n navSpaces) More() int { return len(n.All) - len(n.Shown) }

// pageSpace is the band or community a page's data shows, if any: its
// kind and id.
func pageSpace(data map[string]any) (string, int64) {
	switch b := data["Band"].(type) {
	case *store.Band:
		if b != nil {
			return "band", b.ID
		}
	case store.Band:
		return "band", b.ID
	}
	switch c := data["Community"].(type) {
	case *store.Community:
		if c != nil {
			return "community", c.ID
		}
	case store.Community:
		return "community", c.ID
	}
	return "", 0
}

// buildNavSpaces turns the store's list into the switcher's, marking the
// space the page is in (kind, id).
func buildNavSpaces(spaces []store.Space, kind string, id int64) navSpaces {
	var n navSpaces
	for i, sp := range spaces {
		v := navSpace{
			Kind: sp.Kind, ID: sp.ID, Name: sp.Name, Initials: initials(sp.Name),
			Tone: int((sp.ID*7 + int64(len(sp.Kind))) % 8), Pinned: sp.Pinned,
			Current: sp.Kind == kind && sp.ID == id,
		}
		switch {
		case sp.Kind == "band":
			v.URL, v.Sub = fmt.Sprintf("/bands/%d", sp.ID), "Band"
		case sp.CommunityKind == "coaching":
			v.URL, v.Sub = fmt.Sprintf("/communities/%d", sp.ID), "Coaching"
		default:
			v.URL, v.Sub = fmt.Sprintf("/communities/%d", sp.ID), "Community"
		}
		n.All = append(n.All, v)
		if v.Current {
			n.Current = &n.All[i]
		}
	}
	n.Shown = n.All[:min(len(n.All), sidebarSpaces)]
	// The last used: the most recent use, pinned or not.
	var last *store.Space
	for i := range spaces {
		if u := spaces[i].UsedAt; u != nil && (last == nil || u.After(*last.UsedAt)) {
			last = &spaces[i]
		}
	}
	if last != nil {
		for _, v := range n.All {
			if v.Kind == last.Kind && v.ID == last.ID {
				n.LastURL = v.URL
			}
		}
	}
	return n
}

// spacesFor is the switcher for person on a page showing data: it
// records the use of the space the page is in first, so that space leads
// the recent ones. A failure costs the page nothing but the list.
func (s *Server) spacesFor(ctx context.Context, person *store.Person, data map[string]any) navSpaces {
	kind, id := pageSpace(data)
	if kind != "" {
		_ = s.store.TouchSpace(ctx, person.ID, kind, id)
	}
	spaces, err := s.store.ListSpaces(ctx, person.ID)
	if err != nil {
		return navSpaces{}
	}
	return buildNavSpaces(spaces, kind, id)
}

// handlePinSpace is PUT /api/me/spaces/{kind}/{id}/pin, body {"pinned":
// true|false}: pins a band or a community at the top of one's switcher,
// or unpins it.
func (s *Server) handlePinSpace(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	id, err := pathID(r, "id")
	if err != nil || (kind != "band" && kind != "community") {
		httpError(w, http.StatusBadRequest, "not a band or a community")
		return
	}
	var req struct {
		Pinned bool `json:"pinned"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.PinSpace(r.Context(), personFromContext(r).ID, kind, id, req.Pinned); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
