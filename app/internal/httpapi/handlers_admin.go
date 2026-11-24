package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"app/internal/store"
)

// handleAdminPage is the site-wide admin panel: every person (name,
// email, created date, is_admin), and the accounts that can't log in
// any more (a password but no email: store.ListPasswordNoEmail; an email
// but no password nor passkey while no login email is sent:
// store.ListEmailNoPassword). Gated by requireAdmin (router.go) — this is
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
	communities, err := s.store.ListAllCommunitiesWithCounts(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	groupInvites, err := s.store.ListAllGroupInvites(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	plainInvites, err := s.store.ListPlainInvites(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	shellPeople, err := s.store.ListShellPeople(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	noEmail, err := s.store.ListPasswordNoEmail(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Without a mailer that delivers, an account with only an email (no
	// password, no passkey) can't log back in either.
	var noPassword []store.PersonAdminRow
	if !s.loginEmails() {
		if noPassword, err = s.store.ListEmailNoPassword(r.Context()); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.render(w, "admin", map[string]any{
		"Me": personFromContext(r), "People": people, "LockedOut": noEmail, "NoPassword": noPassword,
		"AdminCount": adminCount, "Communities": communities,
		"GroupInvites": groupInvites, "PlainInvites": plainInvites,
		"ShellPeople": shellPeople,
		"Demo":        s.demoStatus(r.Context()),
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
	// Back where the switch was: the person's own page, or the list.
	if r.FormValue("back") == "person" {
		http.Redirect(w, r, "/admin/people/"+strconv.FormatInt(personID, 10), http.StatusFound)
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

// handleAdminCloseGroupInvite closes any group invite (band or community
// roster link) by id — the admin panel's own revoke action, for a link
// that was sent to the wrong person, posted somewhere it shouldn't have
// been, or is just done being needed. No band/community scoping check: an
// admin may not be a member of the band/community the invite belongs to,
// which is exactly why this exists instead of the member-only
// handleCloseBandGroupInvite/its community counterpart.
func (s *Server) handleAdminCloseGroupInvite(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid invite id")
		return
	}
	if err := s.store.CloseGroupInviteAdmin(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/admin#invites", http.StatusFound)
}

// adminMembershipMutationRequest is the shared body shape for the three
// membership-mutation endpoints below — one small JSON struct instead
// of three near-identical ones, since every caller sends a subset of
// these same three fields (kind always; targetID to add; role to add
// or change).
type adminMembershipMutationRequest struct {
	Kind     string `json:"kind"` // "band" or "community"
	TargetID int64  `json:"targetID"`
	Role     string `json:"role"` // band_members.status ("core"/"guest") or a community role's name
	// RoleID is one of the band's roles, its own included
	// (docs/band-roles.md): what the person page sends for a band; Role
	// ("core"/"guest") is still taken without it.
	RoleID int64 `json:"roleId"`
}

// handleAdminAddMembership adds personID to a band or community at the
// given role, as the admin panel's own action — bypassing the "must
// already be a current member to invite someone else" rule every other
// add-member path enforces, the same site-wide-admin exception
// handleAdminCloseGroupInvite already carries.
func (s *Server) handleAdminAddMembership(w http.ResponseWriter, r *http.Request) {
	personID, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	var req adminMembershipMutationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TargetID == 0 {
		httpError(w, http.StatusBadRequest, "kind and targetID are required")
		return
	}
	actor := personFromContext(r)
	switch req.Kind {
	case "band":
		if req.Role == "" {
			req.Role = "core"
		}
		roleID, err := s.store.ResolveBandInviteRole(r.Context(), req.TargetID, req.RoleID, req.Role)
		if err != nil {
			roleError(w, err)
			return
		}
		if _, err := s.store.AddBandMemberWithRole(r.Context(), req.TargetID, personID, roleID, actor.ID); err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
	case "community":
		if req.Role == "" {
			req.Role = "member"
		}
		if _, err := s.store.AddCommunityMember(r.Context(), req.TargetID, personID, req.Role, actor.ID); err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
	default:
		httpError(w, http.StatusBadRequest, "kind must be \"band\" or \"community\"")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleAdminSetMembershipRole promotes/demotes personID's current
// membership in the given band or community.
func (s *Server) handleAdminSetMembershipRole(w http.ResponseWriter, r *http.Request) {
	personID, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	targetID, err := strconv.ParseInt(r.PathValue("targetID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid target id")
		return
	}
	var req adminMembershipMutationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Role == "" && req.RoleID == 0) {
		httpError(w, http.StatusBadRequest, "role is required")
		return
	}
	actor := personFromContext(r)
	switch r.PathValue("kind") {
	case "band":
		var err error
		if req.RoleID != 0 {
			err = s.store.SetBandMemberRole(r.Context(), targetID, personID, req.RoleID, actor.ID)
		} else {
			err = s.store.SetBandMemberStatus(r.Context(), targetID, personID, req.Role, actor.ID)
		}
		if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) {
			roleError(w, err)
			return
		}
		if err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
	case "community":
		err := s.store.SetCommunityMemberRole(r.Context(), targetID, personID, req.Role)
		if errors.Is(err, store.ErrForbidden) {
			roleError(w, err)
			return
		}
		if err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
	default:
		httpError(w, http.StatusBadRequest, "kind must be \"band\" or \"community\"")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleAdminRemoveMembership ends personID's current membership in the
// given band or community (soft removal — see RemoveBandMember/
// RemoveCommunityMember's own doc comments).
func (s *Server) handleAdminRemoveMembership(w http.ResponseWriter, r *http.Request) {
	personID, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	targetID, err := strconv.ParseInt(r.PathValue("targetID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid target id")
		return
	}
	actor := personFromContext(r)
	switch r.PathValue("kind") {
	case "band":
		if err := s.store.RemoveBandMember(r.Context(), targetID, personID, actor.ID); err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
	case "community":
		if err := s.store.RemoveCommunityMember(r.Context(), targetID, personID); err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
	default:
		httpError(w, http.StatusBadRequest, "kind must be \"band\" or \"community\"")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
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
