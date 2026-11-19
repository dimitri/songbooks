package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"app/internal/store"
)

// A community's Members page (/communities/{id}/members): everyone in it,
// one card each with their picture or initials, role, instruments and
// when they joined. Its admins always see it; its other members only when
// the community says so (store.CommunityMembersVisibility, set on the
// Admin page). What the members see is what they'd learn from meeting
// each other: no username, no "hasn't joined yet", no past members, and
// never anything a coach keeps about a student (their voice, notes, keys,
// assignments, lessons — those stay on the student's own page). Changing
// a role or removing someone renders only for a role granting
// manage_members, and the APIs behind them check that privilege
// themselves (requireCommunityPrivilege in router.go).

// communityMembersAccess is what one person may do with a community's
// Members page: See it, see it as an admin (Manage: usernames, past
// members, who hasn't joined yet), change roles or remove people
// (ManageMembers) and change who sees it (ManageSettings).
type communityMembersAccess struct {
	See, Manage, ManageMembers, ManageSettings bool
}

func (s *Server) communityMembersAccess(ctx context.Context, p *store.Person, communityID int64) (communityMembersAccess, error) {
	if p == nil {
		return communityMembersAccess{}, nil
	}
	if p.IsAdmin {
		return communityMembersAccess{true, true, true, true}, nil
	}
	var a communityMembersAccess
	var err error
	if a.Manage, err = s.store.HasAnyCommunityManagePrivilege(ctx, communityID, p.ID); err != nil {
		return a, err
	}
	if a.ManageMembers, err = s.store.HasCommunityPrivilege(ctx, communityID, p.ID, store.PrivManageMembers); err != nil {
		return a, err
	}
	if a.ManageSettings, err = s.store.HasCommunityPrivilege(ctx, communityID, p.ID, store.PrivManageSettings); err != nil {
		return a, err
	}
	a.See = a.Manage
	if !a.See {
		if a.See, err = s.store.CanSeeCommunityMembers(ctx, communityID, p.ID); err != nil {
			return a, err
		}
	}
	return a, nil
}

// communityMemberCard is one person on the Members page.
type communityMemberCard struct {
	store.CommunityRosterEntry
	Initials  string
	RoleLabel string // see communityRoleLabel
	// StudentPage: the coach's link to this student's page (coaching.go).
	StudentPage bool
}

// communityRoleChoice is one role, in the page's role filter or a role
// select: its name (what the API takes) and its label.
type communityRoleChoice struct {
	Name, Label string
}

// communityRoleLabel is what a role is called on the Members page, for
// the template to translate: the built-in ones by what they are (a
// coaching community's staff are its coaches and its members its
// students), a custom role by the name its community gave it.
func communityRoleLabel(coaching bool, key, name string) string {
	switch {
	case key == "staff" && coaching:
		return "Coach"
	case key == "staff":
		return "role|Staff"
	case key == "member" && coaching:
		return "Student"
	case key == "member":
		return "role|Member"
	case key == "guest":
		return "role|Guest"
	}
	return name
}

// personInitials is a name's first letters, two at most: "Lucas Mercier" → "LM".
func personInitials(name string) string {
	var out []rune
	for _, word := range strings.Fields(name) {
		for _, r := range word {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				out = append(out, unicode.ToUpper(r))
				break
			}
		}
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

// handleCommunityMembersPage is GET /communities/{id}/members, behind
// requireCommunityMember: a member the community doesn't show its list to
// gets a 404, as if the page weren't there.
func (s *Server) handleCommunityMembersPage(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	person := personFromContext(r)
	community, err := s.store.GetCommunity(r.Context(), communityID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	access, err := s.communityMembersAccess(r.Context(), person, communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !access.See {
		http.NotFound(w, r)
		return
	}
	roster, err := s.store.ListCommunityRoster(r.Context(), communityID, access.Manage)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var current, past []communityMemberCard
	var roleChoices []communityRoleChoice
	seenRole := map[string]bool{}
	for _, e := range roster {
		if !access.Manage {
			// Not theirs to know: how someone logs in, or that they
			// haven't made their account yet.
			e.Username, e.IsShell = "", false
		}
		card := communityMemberCard{
			CommunityRosterEntry: e,
			Initials:             personInitials(e.DisplayName),
			RoleLabel:            communityRoleLabel(community.IsCoaching(), e.RoleKey, e.Role),
			StudentPage:          access.Manage && community.IsCoaching() && e.RoleKey != "staff" && e.IsCurrent(),
		}
		if !e.IsCurrent() {
			past = append(past, card)
			continue
		}
		current = append(current, card)
		if !seenRole[e.Role] {
			seenRole[e.Role] = true
			roleChoices = append(roleChoices, communityRoleChoice{Name: e.Role, Label: card.RoleLabel})
		}
	}
	var roles []communityRoleChoice
	if access.ManageMembers {
		all, err := s.store.ListCommunityRoles(r.Context(), communityID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, role := range all {
			roles = append(roles, communityRoleChoice{Name: role.Name, Label: communityRoleLabel(community.IsCoaching(), role.BuiltinKey, role.Name)})
		}
	}
	visibility, err := s.store.CommunityMembersVisibility(r.Context(), communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "community_members", map[string]any{
		"Me": person, "Person": person, "Community": community, "Tab": "members",
		"IsStaff": access.Manage, "CanSeeMembers": true, "CanManageMembers": access.ManageMembers,
		"CanManageSettings": access.ManageSettings, "Visibility": visibility,
		"Members": current, "PastMembers": past, "RoleChoices": roleChoices, "Roles": roles,
	})
}

// handlePutCommunityMembersVisibility is PUT
// /api/communities/{id}/members-visibility {"visibility": "admins" |
// "members"}, behind requireCommunityPrivilege(manage_settings).
func (s *Server) handlePutCommunityMembersVisibility(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Visibility string `json:"visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	err := s.store.SetCommunityMembersVisibility(r.Context(), communityID, req.Visibility)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such community")
		return
	}
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"visibility": req.Visibility})
}

// addMembersTab tells a community page's tab bar whether to show the
// Members tab (the "community-tabs" and "coaching-tab-links" partials
// read .CanSeeMembers): render calls it for every page, so the pages
// with a tab bar needn't each work it out. A page that sets
// CanSeeMembers itself keeps its value.
func (s *Server) addMembersTab(w http.ResponseWriter, data any) {
	m, ok := data.(map[string]any)
	if !ok {
		return
	}
	if _, set := m["CanSeeMembers"]; set {
		return
	}
	var communityID int64
	switch c := m["Community"].(type) {
	case *store.Community:
		if c != nil {
			communityID = c.ID
		}
	case store.Community:
		communityID = c.ID
	}
	if communityID == 0 {
		return
	}
	lw, ok := w.(*langWriter)
	if !ok || lw.person == nil {
		return
	}
	access, err := s.communityMembersAccess(lw.r.Context(), lw.person, communityID)
	m["CanSeeMembers"] = err == nil && access.See
}
