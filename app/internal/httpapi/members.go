package httpapi

import (
	"net/http"
	"strconv"
	"time"
)

// handleMembersPage lists a project's members and lets an owner invite new
// ones or change/revoke an existing member's role. Owner-gated by the
// requireRole("owner", ...) wrapper in router.go.
func (s *Server) handleMembersPage(w http.ResponseWriter, r *http.Request) {
	book := r.PathValue("book")
	members, err := s.store.ListMembers(r.Context(), book)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "members", map[string]any{"Book": book, "Members": members})
}

// handleCreateInvite makes a new single-use invite link, valid for 7 days.
func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	book := r.PathValue("book")
	role := r.FormValue("role")
	if role != "owner" && role != "editor" && role != "viewer" {
		httpError(w, http.StatusBadRequest, "invalid role")
		return
	}
	token, err := s.store.CreateInvite(r.Context(), book, role, 7*24*time.Hour)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": "/invite/" + token})
}

func (s *Server) handleSetMemberRole(w http.ResponseWriter, r *http.Request) {
	book := r.PathValue("book")
	personID, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	role := r.FormValue("role")
	if role != "owner" && role != "editor" && role != "viewer" {
		httpError(w, http.StatusBadRequest, "invalid role")
		return
	}
	if err := s.store.SetMemberRole(r.Context(), book, personID, role); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	book := r.PathValue("book")
	personID, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	if err := s.store.RemoveMember(r.Context(), book, personID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}
