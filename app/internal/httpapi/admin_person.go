package httpapi

import (
	"errors"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"time"

	"app/internal/i18n"
	"app/internal/store"
)

// The site admin's page for one person, /admin/people/{id}: who they are
// and where their account stands (a chip: active, hasn't joined yet,
// can't log in), their way in (a personal invite link while they haven't
// joined, a login link once they have), every band and community they're
// on with their role there, and the site-admin switch. Admin only
// (requireAdmin on every route, router.go); no band or community
// membership asked of the admin, as everywhere on /admin.

// personalInviteTTL is how long a personal link made here works, as on
// the band and community pages.
const personalInviteTTL = 30 * 24 * time.Hour

// adminPersonState is the chip under the name: "pending" (hasn't joined
// yet: a placeholder, or an account nobody ever got into), "no-email"
// (can't log in: the email is the account), "no-password" (an email but
// neither a password nor a passkey, while no login email is sent), or
// "active".
func (s *Server) adminPersonState(a *store.PersonAccess) string {
	switch {
	case a.HasntJoined():
		return "pending"
	case !a.HasEmail:
		return "no-email"
	case !a.HasPassword && !a.HasPasskey && !s.loginEmails():
		return "no-password"
	}
	return "active"
}

// adminRoleChoice is one option of a role select: the value the server
// takes back (a band role's id, a community role's name) and its label.
type adminRoleChoice struct {
	Value    string `json:"value"`
	Label    string `json:"label"`
	Selected bool   `json:"selected,omitempty"`
}

// adminMembershipRow is one band or community the person is on.
type adminMembershipRow struct {
	store.PersonalInviteMembership
	Href      string
	KindLabel string
	Roles     []adminRoleChoice
}

// adminAddTarget is one band or community the person isn't on yet, for
// the add form: its roles, the default one selected.
type adminAddTarget struct {
	Key   string            `json:"key"` // "band:12", "community:4"
	Name  string            `json:"name"`
	Roles []adminRoleChoice `json:"roles"`
}

// bandRoleChoices is a band's roles for a select, selected the one with
// id selected (0: the built-in key def's).
func bandRoleChoices(lang string, roles []store.BandRole, selected int64, def string) []adminRoleChoice {
	out := make([]adminRoleChoice, 0, len(roles))
	for _, r := range roles {
		label := r.Name
		switch r.BuiltinKey {
		case "core":
			label = i18n.T(lang, "role|Core")
		case "guest":
			label = i18n.T(lang, "role|Guest")
		}
		sel := r.ID == selected || (selected == 0 && r.BuiltinKey == def)
		out = append(out, adminRoleChoice{Value: strconv.FormatInt(r.ID, 10), Label: label, Selected: sel})
	}
	return out
}

// communityRoleChoices is a community's roles for a select (by name, as
// SetCommunityMemberRole takes them), selected the one named selected.
func communityRoleChoices(lang string, coaching bool, roles []store.CommunityRole, selected string) []adminRoleChoice {
	out := make([]adminRoleChoice, 0, len(roles))
	for _, r := range roles {
		label := communityRoleLabel(coaching, r.BuiltinKey, r.Name)
		if r.BuiltinKey != "" {
			label = i18n.T(lang, label)
		}
		out = append(out, adminRoleChoice{Value: r.Name, Label: label, Selected: r.Name == selected})
	}
	return out
}

// communityKindLabel is what a membership row says the group is.
func communityKindLabel(kind string) string {
	switch kind {
	case "coaching":
		return "Coaching"
	case "global":
		return "Catalogue"
	}
	return "Community"
}

// handleAdminPersonPage is GET /admin/people/{personID}.
func (s *Server) handleAdminPersonPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := s.langOf(w)
	personID, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	person, err := s.store.GetPersonByID(ctx, personID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, "no such person")
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	access, err := s.store.GetPersonAccess(ctx, personID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	memberships, err := s.store.GetPersonMemberships(ctx, personID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	rows := make([]adminMembershipRow, 0, len(memberships))
	already := map[string]bool{}
	for _, m := range memberships {
		row := adminMembershipRow{PersonalInviteMembership: m}
		already[m.Kind+":"+strconv.FormatInt(m.ID, 10)] = true
		if m.Kind == "band" {
			row.Href = "/bands/" + strconv.FormatInt(m.ID, 10)
			row.KindLabel = "Band"
			roles, err := s.store.ListBandRoles(ctx, m.ID)
			if err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			row.Roles = bandRoleChoices(lang, roles, m.RoleID, "")
		} else {
			row.Href = "/communities/" + strconv.FormatInt(m.ID, 10)
			row.KindLabel = communityKindLabel(m.GroupKind)
			roles, err := s.store.ListCommunityRoles(ctx, m.ID)
			if err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			row.Roles = communityRoleChoices(lang, m.GroupKind == "coaching", roles, m.Status)
		}
		rows = append(rows, row)
	}

	// The add form offers the bands and communities they aren't on, each
	// with its own roles (a band's own roles included: docs/band-roles.md).
	allBands, err := s.store.ListAllBands(ctx)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	allCommunities, err := s.store.ListAllCommunitiesWithCounts(ctx)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var addBands, addCommunities []adminAddTarget
	for _, b := range allBands {
		key := "band:" + strconv.FormatInt(b.ID, 10)
		if already[key] {
			continue
		}
		roles, err := s.store.ListBandRoles(ctx, b.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		addBands = append(addBands, adminAddTarget{Key: key, Name: b.Name, Roles: bandRoleChoices(lang, roles, 0, "core")})
	}
	for _, c := range allCommunities {
		key := "community:" + strconv.FormatInt(c.ID, 10)
		if already[key] {
			continue
		}
		roles, err := s.store.ListCommunityRoles(ctx, c.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		addCommunities = append(addCommunities, adminAddTarget{Key: key, Name: c.Name, Roles: communityRoleChoices(lang, c.Kind == "coaching", roles, "member")})
	}
	addRoles := map[string][]adminRoleChoice{}
	for _, t := range append(append([]adminAddTarget{}, addBands...), addCommunities...) {
		addRoles[t.Key] = t.Roles
	}

	data := map[string]any{
		"Me": personFromContext(r), "Person": person, "Access": access,
		"State": s.adminPersonState(access), "Memberships": rows,
		"AddBands": addBands, "AddCommunities": addCommunities, "AddRoles": addRoles,
		"LoginEmails": s.loginEmails(),
	}
	if access.HasntJoined() {
		invite, err := s.store.GetPersonalInviteFor(ctx, personID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if invite != nil {
			data["Invite"] = invite
			if !invite.Expired() {
				link := feedBase(r) + "/invite/" + invite.Token
				data["InviteURL"] = link
				// Drawn here, not fetched from /api/qr?data=…: the token
				// stays out of URLs (as the login link's QR code).
				if svg, err := qrSVG(link); err == nil {
					data["InviteQR"] = template.HTML(svg)
				}
			}
		}
	}
	s.render(w, "admin_person", data)
}

// adminInviteTarget resolves {personID} for the invite routes below: 403
// cross-origin, 400 on a bad id, 404 when nobody has it. It writes the
// error response itself and returns 0 then.
func (s *Server) adminInviteTarget(w http.ResponseWriter, r *http.Request) (int64, *store.PersonAccess) {
	if !sameOrigin(r) {
		httpError(w, http.StatusForbidden, "cross-origin request")
		return 0, nil
	}
	id, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return 0, nil
	}
	access, err := s.store.GetPersonAccess(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such person")
		return 0, nil
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return 0, nil
	}
	return id, access
}

// handleAdminNewPersonalInvite is POST /api/admin/people/{personID}/invite:
// a fresh personal link for someone who hasn't joined (a placeholder, or
// an account nobody ever got into, which becomes a placeholder:
// store.CreatePersonalInvite), replacing the one they had. 409 for
// someone with an account: their way in is a login link.
func (s *Server) handleAdminNewPersonalInvite(w http.ResponseWriter, r *http.Request) {
	id, access := s.adminInviteTarget(w, r)
	if access == nil {
		return
	}
	if !access.HasntJoined() {
		httpError(w, http.StatusConflict, store.ErrNotShell.Error())
		return
	}
	admin := personFromContext(r)
	token, err := s.store.CreatePersonalInvite(r.Context(), id, admin.ID, personalInviteTTL)
	if errors.Is(err, store.ErrNotShell) {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Printf("personal invite: admin %q (id %d) made a new link for person %d", admin.DisplayName, admin.ID, id)
	writeJSON(w, http.StatusOK, map[string]any{"url": feedBase(r) + "/invite/" + token})
}

// handleAdminRevokePersonalInvite is DELETE /api/admin/people/{personID}/invite:
// their personal link stops working (store.RevokePersonalInvites). 404
// when they had none.
func (s *Server) handleAdminRevokePersonalInvite(w http.ResponseWriter, r *http.Request) {
	id, access := s.adminInviteTarget(w, r)
	if access == nil {
		return
	}
	had, err := s.store.RevokePersonalInvites(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !had {
		httpError(w, http.StatusNotFound, "no invite link to revoke")
		return
	}
	admin := personFromContext(r)
	log.Printf("personal invite: admin %q (id %d) revoked the link of person %d", admin.DisplayName, admin.ID, id)
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}
