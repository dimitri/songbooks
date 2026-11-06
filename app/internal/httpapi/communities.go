package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"app/internal/store"
	"app/internal/webassets"
)

// requireCommunityMember wraps requireAuth, then checks the logged-in
// person is a CURRENT member (either role) of the community identified by
// the {id} path param — the gate on "may see this community's material at
// all," same shape requireBandMember uses for bands. A non-member and a
// bad/missing community id both get the same 403.
func (s *Server) requireCommunityMember(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		communityID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid community id")
			return
		}
		person := personFromContext(r)
		if person.IsAdmin {
			next(w, r)
			return
		}
		isMember, err := s.store.IsCurrentCommunityMember(r.Context(), communityID, person.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !isMember {
			httpError(w, http.StatusForbidden, "must be a current member of this community")
			return
		}
		next(w, r)
	})
}

// requireCommunityStaff wraps requireAuth, then checks the logged-in
// person is a CURRENT INSTRUCTOR of the community identified by the {id}
// path param — the gate on every instructor-only action: membership
// management, catalog add/fork/remove/create, rename, and submission
// review. A current STUDENT gets the same 403 a non-member would (don't
// leak the distinction via status code).
func (s *Server) requireCommunityStaff(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		communityID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid community id")
			return
		}
		person := personFromContext(r)
		// Site admin bypass: an admin manages every community, not just
		// the ones they happen to already be staff of — without this, a
		// brand-new community has no staff at all until its FIRST member
		// joins through some other path, and an admin can't fix a
		// misconfigured community (wrong staff, stuck invite state)
		// without first adding themselves as staff through a workaround.
		if person.IsAdmin {
			next(w, r)
			return
		}
		isInstructor, err := s.store.IsCurrentCommunityStaff(r.Context(), communityID, person.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !isInstructor {
			httpError(w, http.StatusForbidden, "must be a current staff member of this community")
			return
		}
		next(w, r)
	})
}

// requireCommunityPrivilege wraps requireAuth, then checks the logged-in
// person's CURRENT role in the community identified by the {id} path
// param grants priv — the fine-grained replacement for
// requireCommunityStaff's old flat staff-only gate, letting a custom
// role (e.g. an "Events coordinator" granting only manage_events) reach
// exactly the actions its privileges cover, not an all-or-nothing tier.
func (s *Server) requireCommunityPrivilege(priv string, next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		communityID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid community id")
			return
		}
		person := personFromContext(r)
		if person.IsAdmin {
			next(w, r)
			return
		}
		has, err := s.store.HasCommunityPrivilege(r.Context(), communityID, person.ID, priv)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !has {
			httpError(w, http.StatusForbidden, "your role in this community doesn't grant this")
			return
		}
		next(w, r)
	})
}

// requireCommunityAnyManagePrivilege is requireCommunityPrivilege's
// "any of these" variant — gates a page (not a single action) that's
// useful to anyone holding at least one management privilege, even if
// a given viewer's own role can't act on every section shown (each
// section's own form submits through its own requireCommunityPrivilege
// route, which is the real enforcement boundary).
func (s *Server) requireCommunityAnyManagePrivilege(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		communityID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid community id")
			return
		}
		person := personFromContext(r)
		if person.IsAdmin {
			next(w, r)
			return
		}
		has, err := s.store.HasAnyCommunityManagePrivilege(r.Context(), communityID, person.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !has {
			httpError(w, http.StatusForbidden, "your role in this community doesn't grant any management privilege")
			return
		}
		next(w, r)
	})
}

// communityCardView is store.CommunityCardRow plus the next upcoming
// event's digest, same "what's coming up" cue bands.html's own card
// shows via HasNextGig/NextGigDate.
type communityCardView struct {
	store.CommunityCardRow
	HasNextEvent  bool
	NextEventID   int64
	NextEventName string
	NextEventDate time.Time
}

// handleCommunitiesPage lists the logged-in person's own current communities
// (either role), with a "start a community" action.
func (s *Server) handleCommunitiesPage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	rows, err := s.store.ListCommunityCardsForPerson(r.Context(), person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cards := make([]communityCardView, len(rows))
	for i, row := range rows {
		cards[i] = communityCardView{CommunityCardRow: row}
		events, err := s.store.ListEventsForCommunity(r.Context(), row.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		now := time.Now()
		for _, e := range events {
			if e.IsPast(now) {
				continue
			}
			if !cards[i].HasNextEvent || e.StartsAt.Before(cards[i].NextEventDate) {
				cards[i].HasNextEvent = true
				cards[i].NextEventID = e.ID
				cards[i].NextEventName = e.Name
				cards[i].NextEventDate = e.StartsAt
			}
		}
	}
	s.render(w, "communities", map[string]any{"Me": person, "Communities": cards})
}

// handleCreateCommunity makes a brand-new community, with the creator as its
// first member — AS AN INSTRUCTOR (see store.CreateCommunity). Any
// logged-in person may create one, same as bands.
func (s *Server) handleCreateCommunity(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		httpError(w, http.StatusBadRequest, "missing name")
		return
	}
	person := personFromContext(r)
	id, err := s.store.CreateCommunity(r.Context(), req.Name, person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// communityCatalogRow is store.CommunityCatalogEntry plus the band
// repertoire page's own key/tempo badges (see bandCatalogRow).
type communityCatalogRow struct {
	store.CommunityCatalogEntry
	Key, Tempo string
}

// handleCommunityPage is the member-facing community page: the shared
// repertoire, member-editable (add/fork/remove) the same way a band's
// own repertoire page is — no staff review gate. Membership and the
// legacy submissions review queue live on the separate instructor-only
// /communities/{id}/admin page (handleCommunityAdminPage).
// handleCommunityPage is the community's real home page: who runs it,
// how many members, a songbooks summary (this community can have more
// than one — see handleCommunitySongbooksPage), and an upcoming-events
// teaser. The actual repertoire listing/editing UI lives at its own
// /communities/{id}/repertoire tab (handleCommunityRepertoirePage).
func (s *Server) handleCommunityPage(w http.ResponseWriter, r *http.Request) {
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
	members, err := s.store.ListCommunityMembers(r.Context(), communityID, false)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var staffNames []string
	for _, m := range members {
		if m.IsStaff() {
			staffNames = append(staffNames, m.DisplayName)
		}
	}
	songbooks, err := s.store.ListSongbooks(r.Context(), "community", communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	songbookRows := make([]communitySongbookRow, len(songbooks))
	for i, sb := range songbooks {
		entries, err := s.store.ListSongbookEntries(r.Context(), sb.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		songbookRows[i] = communitySongbookRow{Songbook: sb, SongCount: len(entries)}
	}
	events, err := s.store.ListEventsForCommunity(r.Context(), communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	var upcoming []store.Event
	for _, ev := range events {
		if !ev.IsPast(now) {
			upcoming = append(upcoming, ev)
		}
	}
	sort.Slice(upcoming, func(i, j int) bool { return upcoming[i].StartsAt.Before(upcoming[j].StartsAt) })
	if len(upcoming) > 3 {
		upcoming = upcoming[:3]
	}
	canManage, err := s.store.HasAnyCommunityManagePrivilege(r.Context(), communityID, person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "community", map[string]any{
		"Me": person, "Community": community, "Members": members, "StaffNames": staffNames,
		"Songbooks": songbookRows, "UpcomingEvents": upcoming,
		"IsStaff": canManage || person.IsAdmin,
	})
}

// communitySongbookRow is a store.Songbook plus its song count, for the
// overview and songbooks-tab listings.
type communitySongbookRow struct {
	store.Songbook
	SongCount int
}

// handleCommunitySongbooksPage lists every songbook this community has
// (its default repertoire plus any additional named ones — event-
// specific prep books, a "Christmas set," whatever a community wants),
// with a create-another-one action. "More than one songbook per
// community" is the user's own explicit requirement; the backend
// (store.CreateSongbook/ListSongbooks) was already fully generic across
// person/band/community, this page is what actually exposes it.
func (s *Server) handleCommunitySongbooksPage(w http.ResponseWriter, r *http.Request) {
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
	songbooks, err := s.store.ListSongbooks(r.Context(), "community", communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows := make([]communitySongbookRow, len(songbooks))
	for i, sb := range songbooks {
		entries, err := s.store.ListSongbookEntries(r.Context(), sb.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rows[i] = communitySongbookRow{Songbook: sb, SongCount: len(entries)}
	}
	canEdit, err := s.store.HasCommunityPrivilege(r.Context(), communityID, person.ID, store.PrivEditRepertoire)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "community_songbooks", map[string]any{
		"Me": person, "Community": community, "Songbooks": rows, "CanEdit": canEdit || person.IsAdmin,
	})
}

// handleCommunityRepertoirePage is the community's default songbook,
// with the richer add/fork/remove dialog UI (the content that used to
// live at /communities/{id} itself before it became the real home page
// — see handleCommunityPage).
func (s *Server) handleCommunityRepertoirePage(w http.ResponseWriter, r *http.Request) {
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
	catalog, err := s.store.ListCommunityCatalog(r.Context(), communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Key/tempo badges, same "what to glance at before playing" cue the
	// band repertoire page's gig-row shows (see bandCatalogRow) — best
	// effort, a variant that fails to load just shows no badges.
	entries := make([]communityCatalogRow, len(catalog))
	for i, e := range catalog {
		entries[i] = communityCatalogRow{CommunityCatalogEntry: e}
		if sg, _, err := s.store.LoadVariant(e.VariantID); err == nil {
			entries[i].Key = sg.Get("key")
			entries[i].Tempo = sg.Get("tempo")
		}
	}
	canManage, err := s.store.HasAnyCommunityManagePrivilege(r.Context(), communityID, person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	canEdit, err := s.store.HasCommunityPrivilege(r.Context(), communityID, person.ID, store.PrivEditRepertoire)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "community_repertoire", map[string]any{
		"Me": person, "Community": community, "Entries": entries,
		"IsStaff": canManage || person.IsAdmin, "CanEdit": canEdit || person.IsAdmin,
		"ContextualGroup": webassets.ContextualGroup{
			Label: "This community", PDFExportPath: fmt.Sprintf("/communities/%d/export-songbook.pdf", communityID),
		},
	})
}

// handleCommunityAdminPage is the instructor-only management page:
// current+past members, the pending submissions queue, and the
// community's catalog — everything an instructor needs to run the community.
func (s *Server) handleCommunityAdminPage(w http.ResponseWriter, r *http.Request) {
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
	members, err := s.store.ListCommunityMembers(r.Context(), communityID, true)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	pending, err := s.store.ListCommunitySubmissions(r.Context(), communityID, store.SubmissionFilter{Status: "pending"})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	decided, err := s.store.ListCommunitySubmissions(r.Context(), communityID, store.SubmissionFilter{})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	entries, err := s.store.ListCommunityCatalog(r.Context(), communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	shellPeople, err := s.store.ListShellPeopleForCommunity(r.Context(), communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	events, err := s.store.ListEventsForCommunity(r.Context(), communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	eventManagers := map[int64][]store.EventManager{}
	for _, ev := range events {
		mgrs, err := s.store.ListEventManagers(r.Context(), ev.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		eventManagers[ev.ID] = mgrs
	}
	roles, err := s.store.ListCommunityRoles(r.Context(), communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "community_admin", map[string]any{
		"Me": person, "Community": community, "Members": members,
		"PendingSubmissions": pending, "AllSubmissions": decided,
		"Entries": entries, "ShellPeople": shellPeople,
		"Events": events, "EventManagers": eventManagers,
		"Roles": roles, "AllPrivileges": store.AllCommunityPrivileges,
	})
}

// handleRenameCommunity changes a community's name — the admin page's "basic
// settings" action.
func (s *Server) handleRenameCommunity(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, "missing name")
		return
	}
	if err := s.store.RenameCommunity(r.Context(), communityID, req.Name); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleSetCommunityDefaultJoinRole sets the role granted to someone who
// joins this community automatically via event registration — "apply
// community edited defaults."
func (s *Server) handleSetCommunityDefaultJoinRole(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetCommunityDefaultJoinRole(r.Context(), communityID, req.Role); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleAddCommunityMember adds an existing person (by username, same
// direct-add-stands-in-for-invite convention as handleAddBandMember) to
// a community as instructor or student. Instructor-only.
func (s *Server) handleAddCommunityMember(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" {
		httpError(w, http.StatusBadRequest, "missing username")
		return
	}
	if req.Role == "" {
		req.Role = "member"
	}
	target, err := s.store.GetPersonByUsername(r.Context(), req.Username)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such person: "+req.Username)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	actor := personFromContext(r)
	memberID, err := s.store.AddCommunityMember(r.Context(), communityID, target.ID, req.Role, actor.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"memberId": memberID})
}

// handleCreateCommunityPersonalInvite is handleCreateBandPersonalInvite's
// community counterpart: creates a shell person (see store.
// CreateShellPerson's doc comment), adds them to this community at role,
// and mints a personal invite token for them — one action, so the
// person is on the roster immediately and the invite link is just how
// they finish their own account, not a prerequisite for being added.
func (s *Server) handleCreateCommunityPersonalInvite(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		DisplayName string `json:"displayName"`
		Role        string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DisplayName == "" {
		httpError(w, http.StatusBadRequest, "displayName is required")
		return
	}
	role := req.Role
	if role == "" {
		role = "member"
	}
	actor := personFromContext(r)

	personID, err := s.store.CreateShellPerson(r.Context(), req.DisplayName)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.store.AddCommunityMember(r.Context(), communityID, personID, role, actor.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	token, err := s.store.CreatePersonalInvite(r.Context(), personID, actor.ID, 30*24*time.Hour)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"personId": personID, "url": "/invite/" + token})
}

// handleRemoveCommunityMember ends personID's current membership in the
// community. Instructor-only; refused (via store.ErrForbidden) if it would
// leave the community with no current instructor.
func (s *Server) handleRemoveCommunityMember(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	personID, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	if err := s.store.RemoveCommunityMember(r.Context(), communityID, personID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, store.ErrForbidden) {
			httpError(w, http.StatusForbidden, err.Error())
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}

// handleSetCommunityMemberRole promotes/demotes a current member between
// instructor and student. Instructor-only; refused if demoting would
// leave the community with no current instructor.
func (s *Server) handleSetCommunityMemberRole(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	personID, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetCommunityMemberRole(r.Context(), communityID, personID, req.Role); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, store.ErrForbidden) {
			httpError(w, http.StatusForbidden, err.Error())
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleCreateCommunityGroupInvite makes a brand-new, reusable group
// invite for this community — the community equivalent of
// handleCreateBandGroupInvite. manage_members-gated. Every claimed slot
// joins at req.Role (any of this community's own roles — falls back to
// "member" if unset, see store.grantGroupInviteMembershipTx); inviting
// someone directly AS staff stays the deliberate direct-add flow
// (handleAddCommunityMember), not a casual roster link.
func (s *Server) handleCreateCommunityGroupInvite(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Slots []struct {
			DisplayName string `json:"displayName"`
		} `json:"slots"`
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	slots := make([]store.GroupInviteSlotInput, 0, len(req.Slots))
	for _, sl := range req.Slots {
		slots = append(slots, store.GroupInviteSlotInput{DisplayName: sl.DisplayName})
	}
	actor := personFromContext(r)
	groupInviteID, token, err := s.store.CreateGroupInvite(r.Context(), "community", communityID, actor.ID, slots, req.Role)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": groupInviteID, "url": "/join/" + token})
}

// handleCreateCommunityLink mints a single, reusable, open-join link for
// this community (community) — no pre-typed roster, unlike
// handleCreateCommunityGroupInvite's slots-based one: anyone who follows it
// creates an account (or logs into an existing one) and immediately gets
// the role fixed on the link itself at creation time, "staff" or
// "member" — the user's own explicit design choice for Community over
// the slots-per-person model bands/communities already have. Internally this
// is still a GroupInvite with zero slots (see ClaimGroupInviteUnlisted),
// just created without ever offering a roster form.
func (s *Server) handleCreateCommunityLink(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Role string `json:"role"` // any of this community's own role names — built-in or custom
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	role := req.Role
	if role == "" {
		role = "member"
	}
	actor := personFromContext(r)
	_, token, err := s.store.CreateGroupInvite(r.Context(), "community", communityID, actor.ID, nil, role)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": "/join/" + token, "role": role})
}

// handleCloseCommunityGroupInvite explicitly closes a still-open community
// group invite. Instructor-only, same hierarchy reasoning as creation.
func (s *Server) handleCloseCommunityGroupInvite(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	groupInviteID, err := strconv.ParseInt(r.PathValue("groupInviteID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid group invite id")
		return
	}
	if err := s.store.CloseGroupInvite(r.Context(), groupInviteID, "community", communityID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"closed": true})
}

// handleAddCommunityCatalogEntry points the community's repertoire at an
// existing (global, or anyone's visible) variant, by reference. Any
// current member, same as a band's own catalog — no staff review gate.
func (s *Server) handleAddCommunityCatalogEntry(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		VariantID int64 `json:"variantId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VariantID == 0 {
		httpError(w, http.StatusBadRequest, "missing variantId")
		return
	}
	actor := personFromContext(r)
	if err := s.store.AddCommunityCatalogEntry(r.Context(), communityID, req.VariantID, actor.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"added": true})
}

// handleRemoveCommunityCatalogEntry drops the community's pointer to one
// variant. Any current member, same as a band's own catalog.
func (s *Server) handleRemoveCommunityCatalogEntry(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	variantID, err := strconv.ParseInt(r.PathValue("variantID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	if err := s.store.RemoveCommunityCatalogEntry(r.Context(), communityID, variantID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}

// handleForkVariantIntoCommunity forks an existing (global, or anyone's
// visible) variant into the community's own repertoire — visibility=
// 'community', owner_community_id = this community. Any current member.
func (s *Server) handleForkVariantIntoCommunity(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	variantID, err := strconv.ParseInt(r.PathValue("variantID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	actor := personFromContext(r)
	newID, err := s.store.ForkVariant(r.Context(), variantID, actor.ID, "community", communityID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, store.ErrForbidden) {
		httpError(w, http.StatusForbidden, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"variantId": newID})
}

// handleCreateCommunityVariant creates a brand-new, from-scratch variant
// owned by this community (visibility='community') via
// store.CreateCommunityVariant. Any current member.
func (s *Server) handleCreateCommunityVariant(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Title  string `json:"title"`
		Artist string `json:"artist"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" {
		httpError(w, http.StatusBadRequest, "title is required")
		return
	}
	actor := personFromContext(r)
	newID, err := s.store.CreateCommunityVariant(r.Context(), communityID, req.Title, req.Artist, actor.ID)
	if errors.Is(err, store.ErrForbidden) {
		httpError(w, http.StatusForbidden, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"variantId": newID})
}

// handleCreateCommunitySubmission is a student submitting their own
// already-created/forked personal variant for the community's
// consideration — POST /api/communities/{id}/submissions. kind is
// "new_song" or "edit"; "edit" requires sourceVariantId (the community/
// global variant being edited). Any current community member may call
// this (instructors aren't barred from submitting either, though the
// brief's primary scenario is students) — store.CreateCommunitySubmission
// itself enforces that submittedVariantId is owned by the caller.
func (s *Server) handleCreateCommunitySubmission(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Kind               string `json:"kind"`
		SourceVariantID    int64  `json:"sourceVariantId"`
		SubmittedVariantID int64  `json:"submittedVariantId"`
		Note               string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SubmittedVariantID == 0 {
		httpError(w, http.StatusBadRequest, "missing submittedVariantId")
		return
	}
	var sourceVariantID *int64
	if req.SourceVariantID != 0 {
		sourceVariantID = &req.SourceVariantID
	}
	actor := personFromContext(r)
	id, err := s.store.CreateCommunitySubmission(r.Context(), communityID, actor.ID, req.Kind, sourceVariantID, req.SubmittedVariantID, req.Note)
	if errors.Is(err, store.ErrForbidden) {
		httpError(w, http.StatusForbidden, err.Error())
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// handleAcceptCommunitySubmission accepts a pending submission —
// instructor-only (requireCommunityStaff); store.AcceptCommunitySubmission
// double-checks instructor status against the submission's OWN
// community_id too, same defense-in-depth every other accept/reject path
// in this codebase uses.
func (s *Server) handleAcceptCommunitySubmission(w http.ResponseWriter, r *http.Request) {
	submissionID, err := strconv.ParseInt(r.PathValue("submissionID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid submission id")
		return
	}
	actor := personFromContext(r)
	if err := s.store.AcceptCommunitySubmission(r.Context(), submissionID, actor.ID); err != nil {
		writeCommunitySubmissionDecisionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

// handleRejectCommunitySubmission rejects a pending submission —
// instructor-only.
func (s *Server) handleRejectCommunitySubmission(w http.ResponseWriter, r *http.Request) {
	submissionID, err := strconv.ParseInt(r.PathValue("submissionID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid submission id")
		return
	}
	actor := personFromContext(r)
	if err := s.store.RejectCommunitySubmission(r.Context(), submissionID, actor.ID); err != nil {
		writeCommunitySubmissionDecisionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"rejected": true})
}

func writeCommunitySubmissionDecisionError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, store.ErrForbidden) {
		httpError(w, http.StatusForbidden, err.Error())
		return
	}
	if errors.Is(err, store.ErrAlreadyDecided) {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	httpError(w, http.StatusInternalServerError, err.Error())
}

// handleListCommunityRoles is GET /api/communities/{id}/roles — any
// current member may see the role list (needed to populate an invite's
// target-role picker, for instance), not just those who can edit it.
func (s *Server) handleListCommunityRoles(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	roles, err := s.store.ListCommunityRoles(r.Context(), communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, roles)
}

// handleCreateCommunityRole makes a new custom role.
func (s *Server) handleCreateCommunityRole(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Name       string   `json:"name"`
		Privileges []string `json:"privileges"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, "name is required")
		return
	}
	roleID, err := s.store.CreateCommunityRole(r.Context(), communityID, req.Name, req.Privileges)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			httpError(w, http.StatusConflict, err.Error())
			return
		}
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"roleId": roleID})
}

// handleSetCommunityRolePrivileges replaces a role's entire privilege
// set — works for a built-in role too (a community may re-tune what
// "member" or "staff" itself grants).
func (s *Server) handleSetCommunityRolePrivileges(w http.ResponseWriter, r *http.Request) {
	roleID, err := strconv.ParseInt(r.PathValue("roleID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid role id")
		return
	}
	var req struct {
		Privileges []string `json:"privileges"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetCommunityRolePrivileges(r.Context(), roleID, req.Privileges); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleRenameCommunityRole renames a CUSTOM role (refused for a built-in).
func (s *Server) handleRenameCommunityRole(w http.ResponseWriter, r *http.Request) {
	roleID, err := strconv.ParseInt(r.PathValue("roleID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid role id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := s.store.RenameCommunityRole(r.Context(), roleID, req.Name); err != nil {
		if errors.Is(err, store.ErrForbidden) {
			httpError(w, http.StatusForbidden, err.Error())
			return
		}
		if errors.Is(err, store.ErrConflict) {
			httpError(w, http.StatusConflict, err.Error())
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleDeleteCommunityRole deletes a CUSTOM role not currently held by
// any member (refused for a built-in, or one still in use).
func (s *Server) handleDeleteCommunityRole(w http.ResponseWriter, r *http.Request) {
	roleID, err := strconv.ParseInt(r.PathValue("roleID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid role id")
		return
	}
	if err := s.store.DeleteCommunityRole(r.Context(), roleID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, store.ErrForbidden) || errors.Is(err, store.ErrConflict) {
			httpError(w, http.StatusConflict, err.Error())
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
