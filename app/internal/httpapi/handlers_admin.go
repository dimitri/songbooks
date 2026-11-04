package httpapi

import (
	"net/http"
	"strconv"
	"time"
)

// handleAdminPage is the site-wide admin panel: every person (username,
// created date, is_admin). Gated by requireAdmin (router.go) — this is
// instance-wide, not scoped to any one songbook/band.
func (s *Server) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	people, err := s.store.ListAllPeople(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	adminCount := 0
	for _, p := range people {
		if p.IsAdmin {
			adminCount++
		}
	}
	s.render(w, "admin", map[string]any{
		"Me": personFromContext(r), "People": people,
		"AdminCount": adminCount,
	})
}

// handleSetAdmin toggles a person's site-wide is_admin flag. A person
// can't revoke their own admin status through this form — a simple
// guard against an admin locking themselves out with no other admin left
// to undo it, since there's no recovery path for "the only admin account
// removed its own admin bit" yet.
func (s *Server) handleSetAdmin(w http.ResponseWriter, r *http.Request) {
	personID, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	me := personFromContext(r)
	if me != nil && me.ID == personID {
		httpError(w, http.StatusBadRequest, "can't change your own admin status here")
		return
	}
	isAdmin := r.FormValue("is_admin") == "1"
	if err := s.store.SetAdmin(r.Context(), personID, isAdmin); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/admin", http.StatusFound)
}

// handleCreatePlainInvite makes a single-use, project/band-less invite
// link — a "just create an account" invite, valid for 7 days. Its natural
// home is the admin panel: it isn't scoped to any project or band, and
// there is otherwise no self-serve registration path into this instance.
func (s *Server) handleCreatePlainInvite(w http.ResponseWriter, r *http.Request) {
	actor := personFromContext(r)
	token, err := s.store.CreatePlainInvite(r.Context(), actor.ID, 7*24*time.Hour)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": "/invite/" + token})
}

// handleAdminRemovePerson is a deliberate stub: removing a person touches
// every project they own/edit/view and isn't safe to do blindly (what
// happens to a project they solely own?) without a real policy decision —
// out of scope to invent here. Listing people is fully functional;
// removal returns 501 rather than silently doing something half-right.
func (s *Server) handleAdminRemovePerson(w http.ResponseWriter, r *http.Request) {
	httpError(w, http.StatusNotImplemented, "removing a person isn't implemented yet — "+
		"deciding what happens to projects they solely own needs a real policy, not a blind delete")
}
