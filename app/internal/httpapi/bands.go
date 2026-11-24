package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"app/internal/i18n"
	"app/internal/store"
)

// requireBandMember wraps requireAuth, then checks the logged-in person
// is a CURRENT member of the band identified by the {id} path param (or
// a site admin) — the gate on every band page and on what's a member's
// own (rating a recording, their own proposals' drafts). What a member
// may CHANGE is their role's: requireBandPrivilege.
//
// A non-member and a bad/missing band id both get the same 403, same
// "don't leak whether it exists" reasoning requireRole uses for project
// membership.
func (s *Server) requireBandMember(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		bandID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid band id")
			return
		}
		person := personFromContext(r)
		if person.IsAdmin {
			next(w, r)
			return
		}
		isMember, err := s.store.IsCurrentBandMember(r.Context(), bandID, person.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !isMember {
			httpError(w, http.StatusForbidden, "must be a current member of this band")
			return
		}
		next(w, r)
	})
}

// requireBandPrivilege wraps requireAuth, then checks the logged-in
// person's current role in the band identified by the {id} path param
// grants priv (store.HasBandPrivilege; a site admin always passes) — the
// gate on every band action a role may or may not allow
// (docs/band-roles.md). A non-member gets the same 403.
func (s *Server) requireBandPrivilege(priv string, next http.HandlerFunc) http.HandlerFunc {
	return s.requireBandPrivilegeSaying(priv, "your role in this band doesn't allow this", next)
}

// requireBandInviter is requireBandPrivilege(manage_members) for every
// way of inviting someone to a band — the band link, a one-off link,
// adding an account by email or by name, a personal invite, a roster link and
// its names — with a 403 that says so.
func (s *Server) requireBandInviter(next http.HandlerFunc) http.HandlerFunc {
	return s.requireBandPrivilegeSaying(store.BandPrivManageMembers, "your role in this band doesn't let you invite people (by default, only core members do)", next)
}

func (s *Server) requireBandPrivilegeSaying(priv, forbidden string, next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		bandID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid band id")
			return
		}
		ok, err := s.bandPrivilege(r, bandID, priv)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok {
			httpError(w, http.StatusForbidden, forbidden)
			return
		}
		next(w, r)
	})
}

// bandPrivilege is store.HasBandPrivilege for the signed-in person, a
// site admin let through (as requireBandPrivilege) — but for
// rename_archive, an archived band is read only for admins too: the
// check a band handler makes for an action its route doesn't gate.
func (s *Server) bandPrivilege(r *http.Request, bandID int64, priv string) (bool, error) {
	person := personFromContext(r)
	if person == nil {
		return false, nil
	}
	if person.IsAdmin {
		if priv == store.BandPrivRenameArchive {
			return true, nil
		}
		archived, err := s.store.BandArchived(r.Context(), bandID)
		return !archived, err
	}
	return s.store.HasBandPrivilege(r.Context(), bandID, person.ID, priv)
}

// bandPrivs is what the signed-in person's role in bandID grants, every
// privilege for a site admin: for a page to show what its viewer may do
// ({{if .Privs.Can "edit_look"}}).
func (s *Server) bandPrivs(r *http.Request, bandID int64) (store.BandPrivs, error) {
	person := personFromContext(r)
	if person == nil {
		return store.BandPrivs{}, nil
	}
	if person.IsAdmin {
		archived, err := s.store.BandArchived(r.Context(), bandID)
		return store.AllBandPrivs(archived), err
	}
	return s.store.BandPrivileges(r.Context(), bandID, person.ID)
}

// handleBandsPage lists the logged-in person's own current bands, with a
// "create a band" action.
func (s *Server) handleBandsPage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)

	// "All bands" is an admin-only second tab — browsing every band on
	// the instance regardless of membership, not just the ones you
	// belong to. Gated on person.IsAdmin (same flag /admin already
	// gates on); anyone else only ever sees their own "My bands" tab.
	tab := "mine"
	var bands []store.Band
	var err error
	if r.URL.Query().Get("tab") == "all" && person.IsAdmin {
		tab = "all"
		bands, err = s.store.ListAllBands(r.Context())
	} else {
		bands, err = s.store.ListBandsForPerson(r.Context(), person.ID)
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Reuses the exact same per-band card data (member count, since-last-
	// visit digest, open-vote count) the home page shows — Phase 27, so
	// this listing isn't just band names any more, consistent with the
	// home page's activity-first framing. For the admin "all bands" tab
	// this still works for bands the admin doesn't belong to: every
	// query inside buildBandCards is keyed by band id, not membership —
	// GetBandLastSeen just comes back nil ("you haven't visited yet"),
	// which is the honest answer for a band you're not in.
	bandCards, _, err := s.buildBandCards(r.Context(), s.langOf(w), person.ID, bands)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// My bands' own cards: each in its band's look, with its faces, its
	// songbook's size, its next date and its last activity (list_cards.go).
	list, fonts, err := s.bandListCards(r.Context(), s.langOf(w), person.ID, bandCards)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "bands", map[string]any{"Me": person, "Bands": list, "BrandFonts": fonts, "Tab": tab, "ShowAllTab": person.IsAdmin})
}

// handleCreateBand makes a brand-new band, with the creator as its first
// (core) member. Any logged-in person may create one — bands aren't a
// scarce/gated resource.
func (s *Server) handleCreateBand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		httpError(w, http.StatusBadRequest, "missing name")
		return
	}
	person := personFromContext(r)
	id, err := s.store.CreateBand(r.Context(), req.Name, person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// handleBandPage is one band's detail page: current + past members (the
// member list has a "show past members" toggle driven client-side off
// IncludePast), a link to the band's repertoire, and a "changes since
// your last visit" digest (phase 7).
//
// The digest is computed against the OLD last-seen timestamp, then
// MarkBandSeen updates it to now — in that order, deliberately, so the
// digest reflects "since you last looked," not "since right now" (a
// digest computed after the update would always be empty). A person who
// has never viewed this band's page before gets since=nil, i.e. "every
// change ever."
func (s *Server) handleBandPage(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	person := personFromContext(r)
	band, err := s.store.GetBand(r.Context(), bandID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	since, err := s.store.GetBandLastSeen(r.Context(), bandID, person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	changes, err := s.store.ListBandChanges(r.Context(), s.langOf(w), bandID, since)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.MarkBandSeen(r.Context(), bandID, person.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Phase 39: the band page becomes a real home page — repertoire/
	// setlist preview plus a merged activity feed (proposals, votes
	// awaiting, listening-party picks, score edits) and shortcuts to
	// create more of it, not just a roster page with a sub-nav out to
	// everything else.
	openProposals, err := s.store.ListProposals(r.Context(), bandID, store.ProposalFilter{Status: "open", Viewer: person.ID})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	repertoire, err := s.store.ListBandCatalog(r.Context(), bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	repertoireCount := len(repertoire)

	setlists, err := s.store.ListSetlistsForBand(r.Context(), bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	members, err := s.store.ListBandMembers(r.Context(), bandID, false)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	nextUp, err := s.bandNextUp(r.Context(), person.ID, bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// The sections' rows on a phone (space-hub): where each one stands.
	lang := s.langOf(w)
	hints := map[string]spaceHint{
		"catalog":    {Text: i18n.N(lang, repertoireCount, "%d song", "%d songs")},
		"setlists":   {Text: strconv.Itoa(len(setlists))},
		"events":     {Text: i18n.T(lang, "plan a rehearsal")},
		"recordings": {Text: i18n.T(lang, "rehearsals, concerts")},
		"members":    {Text: strconv.Itoa(len(members))},
		"settings":   {Text: i18n.T(lang, "name, look")},
	}
	if nextUp != nil {
		hints["events"] = spaceHint{Text: dayHint(lang, nextUp.Starts)}
	}
	// The votes waiting are the Repertoire's: its To vote view.
	if n := len(openProposals); n > 0 {
		hints["catalog"] = spaceHint{Text: i18n.T(lang, "%d to vote on", n), Due: true}
	}
	s.render(w, "band", map[string]any{
		"Me": person, "SpaceHints": hints, "Band": band, "Changes": changes, "HasLastSeen": since != nil,
		"OpenProposals": openProposals, "RepertoireCount": repertoireCount,
		"Setlists": setlists, "MemberCount": len(members), "NextUp": nextUp,
	})
}

// handleSetBandName renames a band — any current member may do it, same
// no-owner-role model as handleSetBandMusicType just below.
func (s *Server) handleSetBandName(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		httpError(w, http.StatusBadRequest, "name cannot be empty")
		return
	}
	if err := s.store.SetBandName(r.Context(), bandID, name); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

// handleSetBandMusicType sets whether a band plays covers or original
// music — any current member may set it (requireBandMember), same as
// every other band-admin action in this app's no-owner-role model.
func (s *Server) handleSetBandMusicType(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		MusicType string `json:"musicType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetBandMusicType(r.Context(), bandID, strings.TrimSpace(req.MusicType)); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"musicType": req.MusicType})
}

// handleBandMembersPage is the Members tab: full current roster (with
// instrument), unclaimed group-invite slots ("hasn't joined yet"), past
// members, and the add-person/invite-link/group-invite-link/delete-band
// forms — split out of handleBandPage (Phase 43 tabs) so the Overview tab
// stays focused on activity, not roster administration.
func (s *Server) handleBandMembersPage(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	person := personFromContext(r)
	band, err := s.store.GetBand(r.Context(), bandID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	members, err := s.store.ListBandMembers(r.Context(), bandID, true)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Placeholders' personal links, to show on their own roster row.
	shellPeople, err := s.store.ListShellPeopleForBand(r.Context(), bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	links := make(map[int64]store.ShellPersonRow, len(shellPeople))
	for _, sp := range shellPeople {
		links[sp.PersonID] = sp
	}

	// What the viewer's role lets them do here (docs/band-roles.md).
	privs, err := s.bandPrivs(r, bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	roles, err := s.store.ListBandRoles(r.Context(), bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	roleByID := make(map[int64]store.BandRole, len(roles))
	for _, role := range roles {
		roleByID[role.ID] = role
	}

	// One row per person: a placeholder's invite link sits on the same
	// row as their name, instruments and role.
	rows := make([]bandRosterRow, 0, len(members))
	for _, m := range members {
		row := bandRosterRow{
			BandMember:         m,
			Role:               roleByID[m.RoleID],
			CanEditInstruments: m.IsCurrent() && store.MayEditMemberInstruments(privs.Can(store.BandPrivManageMembers), m.PersonID == person.ID, m.IsShell),
		}
		if m.IsShell {
			row.InviteToken = links[m.PersonID].OpenInviteToken
			row.InviteUntil = links[m.PersonID].OpenInviteUntil
		}
		rows = append(rows, row)
	}

	// The band's open links ("Band link or QR code"): group invites
	// with no names on them.
	groupInvites, err := s.store.ListGroupInvitesForTarget(r.Context(), "band", bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	linkRows := []bandLinkRow{}
	for _, gi := range bandLinks(groupInvites) {
		linkRows = append(linkRows, bandLinkRow{GroupInviteView: gi, Role: inviteRoleOf(roles, gi.RoleID, gi.DefaultStatus)})
	}

	// The people the viewer already plays with, to add by name.
	var known []store.KnownPerson
	if privs.Can(store.BandPrivManageMembers) {
		current := map[int64]bool{}
		for _, m := range members {
			if m.IsCurrent() {
				current[m.PersonID] = true
			}
		}
		if known, err = s.knownPeopleExcept(r.Context(), person.ID, current); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	s.render(w, "band_members", map[string]any{
		"Me": person, "Band": band, "Members": rows, "KnownPeople": known,
		// IsCore: may invite people (manage_members), as before roles.
		"IsCore":    privs.Can(store.BandPrivManageMembers),
		"Privs":     privs,
		"Roles":     roles,
		"BandLinks": linkRows,
	})
}

// bandLinkRow is one of the band's open links (QR codes) on its Members
// page, with the role it gives.
type bandLinkRow struct {
	store.GroupInviteView
	Role store.BandRole
}

// inviteRoleOf is the role an invite gives, among the band's roles: its
// own (roleID), else the built-in of its status ("core"; anything else
// guest) — an invite made before roles, or one whose role was deleted.
func inviteRoleOf(roles []store.BandRole, roleID int64, status string) store.BandRole {
	key := "guest"
	if status == "core" {
		key = "core"
	}
	var builtin store.BandRole
	for _, r := range roles {
		if roleID != 0 && r.ID == roleID {
			return r
		}
		if r.BuiltinKey == key {
			builtin = r
		}
	}
	return builtin
}

// inviteRole is the role an invite request gives in bandID: roleId, one
// of the band's roles (a band's own included), else status, "core" or
// "guest" (an older client's), else def ("" for none). It answers 400
// and false when that names none of the band's roles.
func (s *Server) inviteRole(w http.ResponseWriter, r *http.Request, bandID, roleID int64, status, def string) (int64, bool) {
	if roleID == 0 && status == "" {
		status = def
	}
	id, err := s.store.ResolveBandInviteRole(r.Context(), bandID, roleID, status)
	switch {
	case errors.Is(err, store.ErrInvalid):
		httpError(w, http.StatusBadRequest, "roleId must be one of the band's roles (or status \"core\" or \"guest\")")
		return 0, false
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
		return 0, false
	}
	return id, true
}

// bandRosterRow is one person's line on the band's Members page: their
// membership, plus (for a placeholder) their personal invite link, and
// whether the viewer may edit their instruments.
type bandRosterRow struct {
	store.BandMember
	Role               store.BandRole // the member's role (Name "core"/"guest" for a built-in)
	InviteToken        string    // "" when a placeholder has no live link
	InviteUntil        time.Time // InviteToken's expiry
	CanEditInstruments bool
}

// InstrumentsText is the row's instruments as the edit field shows them.
func (r bandRosterRow) InstrumentsText() string { return strings.Join(r.Instruments, ", ") }

// handleSetBandMemberInstruments is PUT /api/bands/{id}/members/
// {personID}/instruments, body {"instruments": ["bass", "vocals"]}
// (entries may be comma separated): replaces that member's instruments,
// as store.MayEditMemberInstruments allows — any member for a
// placeholder, core members (and the person) for a real account.
func (s *Server) handleSetBandMemberInstruments(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	personID, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	var req struct {
		Instruments []string `json:"instruments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	actor := personFromContext(r)
	saved, err := s.store.SetBandMemberInstruments(r.Context(), bandID, actor.ID, personID, actor.IsAdmin, req.Instruments)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "not a current member of this band")
		return
	case errors.Is(err, store.ErrForbidden):
		httpError(w, http.StatusForbidden, "only a core member can change the instruments of someone who has joined")
		return
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instruments": saved})
}

// handleNewBandMemberInviteLink is POST /api/bands/{id}/members/
// {personID}/invite-link: a fresh 30-day personal link for a member who
// hasn't joined yet, replacing their old one whether or not it expired
// (store.CreatePersonalInvite). Any band member may; 404 if the person
// isn't a current member, 409 if they've joined already.
func (s *Server) handleNewBandMemberInviteLink(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	personID, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	isMember, err := s.store.IsCurrentBandMember(r.Context(), bandID, personID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !isMember {
		httpError(w, http.StatusNotFound, "not a current member of this band")
		return
	}
	const ttl = 30 * 24 * time.Hour
	token, err := s.store.CreatePersonalInvite(r.Context(), personID, personFromContext(r).ID, ttl)
	if errors.Is(err, store.ErrNotShell) {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The row shows the new expiry as the page does (date, in the page's language).
	writeJSON(w, http.StatusOK, map[string]any{
		"url":     "/invite/" + token,
		"expires": i18n.Date(s.langOf(w), time.Now().Add(ttl), inviteUntilLayout),
	})
}

// inviteUntilLayout is how a roster row shows when a personal link expires.
const inviteUntilLayout = "2 January 2006"

// handleBandSetlistsPage is the Setlists tab: every setlist owned by this
// band, linking to its running order and click track — a per-band
// view over the same data /setlists shows globally (ListSetlistsForBand).
func (s *Server) handleBandSetlistsPage(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	person := personFromContext(r)
	band, err := s.store.GetBand(r.Context(), bandID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setlists, err := s.store.ListSetlistsForBand(r.Context(), bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "band_setlists", map[string]any{"Me": person, "Band": band, "Setlists": setlists})
}

// handleAddBandMember adds an existing account to a band with one of
// its roles ({roleId}, or {status}: core or guest): {personId}, someone
// the adder already plays with (see addTarget; anyone else is added by
// name, handleCreateBandPersonalInvite). manage_members, like every
// other way of inviting someone.
func (s *Server) handleAddBandMember(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		PersonID int64  `json:"personId"`
		Status   string `json:"status"`
		RoleID   int64  `json:"roleId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	roleID, ok := s.inviteRole(w, r, bandID, req.RoleID, req.Status, "")
	if !ok {
		return
	}
	target := s.addTarget(w, r, req.PersonID)
	if target == nil {
		return
	}
	actor := personFromContext(r)
	memberID, err := s.store.AddBandMemberWithRole(r.Context(), bandID, target.ID, roleID, actor.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"memberId": memberID})
}

// handleRemoveBandMember ends personID's current membership in the band
// (closes the validity range — history is kept, see migrations 0010 and
// 0059). Someone else: a member whose role grants remove_members (by
// default, core). Oneself — leaving the band —: any member, as long as
// the band isn't archived (read only: no one leaves it either).
func (s *Server) handleRemoveBandMember(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	personID, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	actor := personFromContext(r)
	may, err := s.bandPrivilege(r, bandID, store.BandPrivRemoveMembers)
	if err == nil && !may && personID == actor.ID {
		var archived bool
		archived, err = s.store.BandArchived(r.Context(), bandID)
		may = !archived
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !may {
		httpError(w, http.StatusForbidden, "your role in this band doesn't let you remove people")
		return
	}
	if err := s.store.RemoveBandMember(r.Context(), bandID, personID, actor.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}

// handleSetBandMemberStatus promotes/demotes a current member between
// core and guest: the built-in roles (handleSetBandMemberRole for any
// role). manage_roles-gated; 409 if no one would be left to manage the
// roles.
func (s *Server) handleSetBandMemberStatus(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	personID, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	actor := personFromContext(r)
	if err := s.store.SetBandMemberStatus(r.Context(), bandID, personID, req.Status, actor.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
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

// handleDeleteBand permanently deletes a band and everything under it:
// site admins only, for now (the user's decision, docs/band-roles.md); a
// band's members archive it instead (handleSetBandArchived).
func (s *Server) handleDeleteBand(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.store.DeleteBand(r.Context(), bandID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// handleSetBandArchived is POST /api/bands/{id}/archive {"archived":
// true|false}: archives the band (read only for everyone, out of the
// menus and the home page, its members still reach it from "All my
// groups") or unarchives it — rename_archive (by default, core).
func (s *Server) handleSetBandArchived(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Archived bool `json:"archived"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetBandArchived(r.Context(), bandID, req.Archived); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"archived": req.Archived})
}

// handleCreateBandGroupInvite makes a brand-new, reusable group invite
// for this band — the "type a roster, get one shareable link" action
// (see internal/store/group_invites.go). Core members only
// (requireBandInviter); every claimed
// slot grants the claiming person band status "guest" (see
// store.grantGroupInviteMembershipTx's doc comment for why guest, not
// core, is the default here).
// handleCreateBandPersonalInvite replaces the old group-invite ("pick
// your name from a shared roster link") flow with a personal one: a
// shell person is created with their membership (band_id/status)
// already attached, then one single-use invite token is minted for
// THAT person — see store.CreateShellPerson/CreatePersonalInvite. The
// roster shows this person immediately (as "hasn't joined yet"),
// exactly as a quick-add did before, but now with a real invite link
// attached to them specifically rather than an anonymous shared one.
func (s *Server) handleCreateBandPersonalInvite(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		DisplayName string `json:"displayName"`
		Instrument  string `json:"instrument"`
		Status      string `json:"status"`
		RoleID      int64  `json:"roleId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DisplayName == "" {
		httpError(w, http.StatusBadRequest, "displayName is required")
		return
	}
	// One of the band's roles; an older client's status, guest when it
	// names neither.
	if req.RoleID == 0 && req.Status != "core" {
		req.Status = "guest"
	}
	roleID, ok := s.inviteRole(w, r, bandID, req.RoleID, req.Status, "guest")
	if !ok {
		return
	}
	actor := personFromContext(r)

	personID, err := s.store.CreateShellPerson(r.Context(), req.DisplayName)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Instrument != "" {
		// On the new person's profile, as collected from this band.
		if err := s.store.CollectInstruments(r.Context(), personID, []string{req.Instrument}, store.InstrumentSource{Kind: store.InstrumentFromBand, ID: bandID}); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if _, err := s.store.AddBandMemberWithRole(r.Context(), bandID, personID, roleID, actor.ID); err != nil {
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

// handleCreatePersonalInviteForPerson (re)generates a personal invite
// link for an existing shell person — their memberships don't change,
// only a fresh token is minted (the "Get invite link" / "resend"
// action for a shell added before this existed, or whose link
// expired). Allowed when the caller may invite people where the target
// is waiting: core in one of their bands, or holding manage_members in
// one of their communities (store.CanInvitePerson) — the same rules as
// the band and community routes that made the invite — or a site admin.
func (s *Server) handleCreatePersonalInviteForPerson(w http.ResponseWriter, r *http.Request) {
	personID, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	actor := personFromContext(r)
	allowed := actor.IsAdmin
	if !allowed {
		allowed, err = s.store.CanInvitePerson(r.Context(), actor.ID, personID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if !allowed {
		httpError(w, http.StatusForbidden, "only a band's core members, or a community's staff who manage members, can make a new invite link for this person")
		return
	}
	token, err := s.store.CreatePersonalInvite(r.Context(), personID, actor.ID, 30*24*time.Hour)
	if errors.Is(err, store.ErrNotShell) {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": "/invite/" + token})
}

func (s *Server) handleCreateBandGroupInvite(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Slots []struct {
			DisplayName string `json:"displayName"`
			Instrument  string `json:"instrument"`
			Status      string `json:"status"`
			RoleID      int64  `json:"roleId"`
		} `json:"slots"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	slots := make([]store.GroupInviteSlotInput, 0, len(req.Slots))
	for _, sl := range req.Slots {
		slots = append(slots, store.GroupInviteSlotInput{DisplayName: sl.DisplayName, Instrument: sl.Instrument, Status: sl.Status, RoleID: sl.RoleID})
	}
	actor := personFromContext(r)
	groupInviteID, token, err := s.store.CreateGroupInvite(r.Context(), "band", bandID, actor.ID, slots, "")
	if errors.Is(err, store.ErrInvalid) {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": groupInviteID, "url": "/join/" + token})
}

// handleCreateBandLink returns the band's open link for one of its roles
// ({roleId}, or {status}: core or guest) — the "Band link or QR code"
// card on the members page, for a rehearsal or a gig where handing out
// personal links one by one isn't practical. No names: anyone who
// follows it creates an account (or logs in) and joins straight away
// with the link's role (see store.grantGroupInviteMembershipTx).
// Internally a GroupInvite with no slots and its role set, as a
// community link (handleCreateCommunityLink). One link per role: an open
// one is handed back rather than minting another, each closable; closing
// one and asking again makes a fresh link. manage_members, like every
// other band invite.
func (s *Server) handleCreateBandLink(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Status string `json:"status"`
		RoleID int64  `json:"roleId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	roleID, ok := s.inviteRole(w, r, bandID, req.RoleID, req.Status, "")
	if !ok {
		return
	}
	invites, err := s.store.ListGroupInvitesForTarget(r.Context(), "band", bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	roles, err := s.store.ListBandRoles(r.Context(), bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, gi := range bandLinks(invites) {
		if role := inviteRoleOf(roles, gi.RoleID, gi.DefaultStatus); role.ID == roleID {
			writeJSON(w, http.StatusOK, map[string]any{"id": gi.ID, "url": "/join/" + gi.Token, "roleId": roleID, "status": gi.DefaultStatus})
			return
		}
	}
	actor := personFromContext(r)
	id, token, err := s.store.CreateBandLink(r.Context(), bandID, actor.ID, roleID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status := inviteRoleOf(roles, roleID, "").BuiltinKey
	if status != "core" {
		status = "guest"
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "url": "/join/" + token, "roleId": roleID, "status": status})
}

// bandLinks keeps the band's still-open open links (handleCreateBandLink)
// out of all its group invites: open, with a role or a status on the
// link, and no named slot left to pick — what the members page lists
// under "Band link or QR code". A slot on such a link is someone who
// joined through it (ClaimGroupInviteUnlisted records one per join).
func bandLinks(invites []store.GroupInviteView) []store.GroupInviteView {
	var out []store.GroupInviteView
	for _, gi := range invites {
		if gi.ClosedAt != nil || (gi.RoleID == 0 && gi.DefaultStatus != "core" && gi.DefaultStatus != "guest") {
			continue
		}
		if !hasWaitingSlot(gi) {
			out = append(out, gi)
		}
	}
	return out
}

// hasWaitingSlot reports whether gi still has a name nobody has claimed
// yet: a link with names, not an open link (whose slots are only the
// people who joined through it).
func hasWaitingSlot(gi store.GroupInviteView) bool {
	for _, sl := range gi.Slots {
		if sl.ClaimedByPersonID == nil {
			return true
		}
	}
	return false
}

// handleAddGroupInviteSlot appends one more named roster slot to an
// already-open band group invite — "add names, even for members that
// have not joined yet" from the roster itself, not just at link-creation
// time.
func (s *Server) handleAddGroupInviteSlot(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	groupInviteID, err := strconv.ParseInt(r.PathValue("groupInviteID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid group invite id")
		return
	}
	var req struct {
		DisplayName string `json:"displayName"`
		Instrument  string `json:"instrument"`
		Status      string `json:"status"`
		RoleID      int64  `json:"roleId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	slotID, err := s.store.AddGroupInviteSlot(r.Context(), groupInviteID, "band", bandID, store.GroupInviteSlotInput{
		DisplayName: req.DisplayName, Instrument: req.Instrument, Status: req.Status, RoleID: req.RoleID,
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"slotId": slotID})
}

// handleUpdateGroupInviteSlot fixes a pending roster slot's name,
// instrument, or role before that person has joined.
func (s *Server) handleUpdateGroupInviteSlot(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	groupInviteID, err := strconv.ParseInt(r.PathValue("groupInviteID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid group invite id")
		return
	}
	slotID, err := strconv.ParseInt(r.PathValue("slotID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid slot id")
		return
	}
	var req struct {
		DisplayName string `json:"displayName"`
		Instrument  string `json:"instrument"`
		Status      string `json:"status"`
		RoleID      int64  `json:"roleId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.UpdateGroupInviteSlot(r.Context(), groupInviteID, slotID, bandID, req.DisplayName, req.Instrument, req.Status, req.RoleID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleRemoveGroupInviteSlot deletes a pending (unclaimed) roster slot.
func (s *Server) handleRemoveGroupInviteSlot(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	groupInviteID, err := strconv.ParseInt(r.PathValue("groupInviteID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid group invite id")
		return
	}
	slotID, err := strconv.ParseInt(r.PathValue("slotID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid slot id")
		return
	}
	if err := s.store.RemoveGroupInviteSlot(r.Context(), groupInviteID, slotID, bandID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}

// handleCloseBandGroupInvite explicitly closes a still-open band group
// invite (see this phase's lifecycle note: slot-exhaustion alone never
// auto-closes one, only this action does). Core members only
// (requireBandInviter), like making one.
func (s *Server) handleCloseBandGroupInvite(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	groupInviteID, err := strconv.ParseInt(r.PathValue("groupInviteID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid group invite id")
		return
	}
	if err := s.store.CloseGroupInvite(r.Context(), groupInviteID, "band", bandID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"closed": true})
}

// handleBandCatalogPage is a band's repertoire page — same lineage
// display (forked from X, revision history link) as the personal catalog
// view, reusing the band_catalog template.
func (s *Server) handleBandCatalogPage(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	person := personFromContext(r)
	band, err := s.store.GetBand(r.Context(), bandID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	entries, err := s.store.ListBandCatalog(r.Context(), bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Key/tempo badges — the same "what to glance at before playing"
	// cue the setlist's gig-row already shows (see gigRow in
	// handlers_setlist.go) — so the repertoire list reads the same way
	// a setlist does, per the redesign. Best-effort: a variant that
	// fails to load just shows no badges, never breaks the page.
	rows := make([]bandCatalogRow, len(entries))
	for i, e := range entries {
		rows[i] = bandCatalogRow{BandCatalogEntry: e}
		if sg, _, err := s.store.LoadVariant(e.VariantID); err == nil {
			rows[i].Key = sg.Get("key")
			rows[i].Tempo = sg.Get("tempo")
		}
	}
	// The melody's range on each row (row_voice.go).
	rowIDs := make([]int64, len(rows))
	for i := range rows {
		rowIDs[i] = rows[i].VariantID
	}
	ranges := s.melodyRanges(r.Context(), rowIDs)
	for i := range rows {
		rows[i].Voice = ranges.voice(s.langOfRequest(r), rows[i].VariantID, rows[i].Key, 0)
	}

	// An "original" act's repertoire reads naturally grouped by the
	// record a song is from; a covers band (or a band that hasn't said
	// which it is) keeps the flat list. No per-song track-order or
	// release-year signal is stored, so songs within an album and
	// albums themselves fall back to alphabetical — an honest
	// simplification, not a claim of real track/release order.
	var albumGroups []albumGroup
	if band.MusicType == "original" {
		covers, err := s.store.GetAlbumCovers(r.Context(), bandID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		albumGroups = groupByAlbum(rows, covers)
	}

	ids := make([]int64, len(rows))
	for i, row := range rows {
		ids[i] = row.VariantID
	}
	// The band's songbook: the Add / Create dialog adds to it.
	sbID, err := s.store.GetDefaultSongbook(r.Context(), "band", bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "band_catalog", map[string]any{
		"Me": person, "Band": band, "Entries": rows, "Badges": s.songBadgesFor(r.Context(), ids),
		"AlbumGroups": albumGroups, "SongbookID": sbID,
		"Export":      bandCatalogExport(r.Context(), s, bandID),
	})
}

// bandCatalogExport is the Songbook's PDF button (space-export) and its
// Playlist button (space-playlist): the songbook as a PDF, and the
// playlist of its songs' recordings, in the head of its list.
func bandCatalogExport(ctx context.Context, s *Server, bandID int64) spaceExport {
	e := spaceExport{PDF: fmt.Sprintf("/bands/%d/export-songbook.pdf", bandID)}
	if sbID, err := s.store.GetDefaultSongbook(ctx, "band", bandID); err == nil {
		e.Playlist = fmt.Sprintf("/songbooks/%d/playlist", sbID)
	}
	return e
}

// bandCatalogRow is BandCatalogEntry plus the key/tempo badges the
// Repertoire list shows next to each title — the same at-a-glance cue
// the setlist's gig-row already uses (see gigRow in handlers_setlist.go).
type bandCatalogRow struct {
	store.BandCatalogEntry
	Key, Tempo string
	// Voice: the song's melody range (row_voice.go).
	Voice rowVoice
}

// albumGroup is one album's songs on a band's Repertoire tab (or the
// Overview preview), for an "original"-type band — cover art fields are
// zero-valued when no project_albums row exists yet for that album
// (migration 0034), which the template treats as "no cover, just show
// the name".
type albumGroup struct {
	Album       string
	CoverURL    string
	ReleaseYear int
	Credit      string
	Entries     []bandCatalogRow
}

func groupByAlbum(entries []bandCatalogRow, covers map[string]store.AlbumCover) []albumGroup {
	byAlbum := map[string][]bandCatalogRow{}
	for _, e := range entries {
		album := e.Album
		if album == "" {
			album = "(no album set)"
		}
		byAlbum[album] = append(byAlbum[album], e)
	}
	names := make([]string, 0, len(byAlbum))
	for name := range byAlbum {
		names = append(names, name)
	}
	sort.Strings(names)
	groups := make([]albumGroup, 0, len(names))
	for _, name := range names {
		songs := byAlbum[name]
		sort.Slice(songs, func(i, j int) bool { return songs[i].Title < songs[j].Title })
		g := albumGroup{Album: name, Entries: songs}
		if c, ok := covers[name]; ok {
			g.CoverURL, g.ReleaseYear, g.Credit = c.CoverURL, c.ReleaseYear, c.Credit
		}
		groups = append(groups, g)
	}
	return groups
}

// handleCreateBandVariant creates a brand-new, from-scratch variant
// owned by this band (visibility='band') via store.CreateBandVariant —
// the band repertoire page's "new original song" form, for a band that
// wants a private composition it never had to fork from anything else.
// Gated by requireBandMember, same as every other band-repertoire
// mutation; store.CreateBandVariant itself double-checks current
// membership too (it also accepts a direct API caller, same defense in
// depth as ForkVariant's scope="band" branch).
func (s *Server) handleCreateBandVariant(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Title  string `json:"title"`
		Artist string `json:"artist"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" {
		httpError(w, http.StatusBadRequest, "title is required")
		return
	}
	actor := personFromContext(r)
	newID, err := s.store.CreateBandVariant(r.Context(), bandID, req.Title, req.Artist, actor.ID)
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

// handleAddBandCatalogEntry adds a song to the band's songbook as a link
// (docs/ownership-model.md §4): a public song, or one of the band's own;
// another band's private song is refused (store.LinkCheck), with the
// catalogue's song of the same work to link instead when there is one.
// Nothing is copied: Edit on the link makes the band's version. Answers
// the song's id.
func (s *Server) handleAddBandCatalogEntry(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		VariantID int64 `json:"variantId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VariantID == 0 {
		httpError(w, http.StatusBadRequest, "missing variantId")
		return
	}
	ctx := r.Context()
	actor := personFromContext(r)
	sbID, err := s.store.GetDefaultSongbook(ctx, "band", bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sb, err := s.store.GetSongbook(ctx, sbID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !s.writeLinkErr(w, r, s.store.LinkCheck(ctx, actor.ID, req.VariantID, sb)) {
		return
	}
	if err := s.store.AddBandCatalogEntry(ctx, bandID, req.VariantID, actor.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": true, "variantId": req.VariantID})
}

// handleRemoveBandCatalogEntry drops the band's pointer to one variant.
func (s *Server) handleRemoveBandCatalogEntry(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	variantID, err := strconv.ParseInt(r.PathValue("variantID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	actor := personFromContext(r)
	if err := s.store.RemoveBandCatalogEntry(r.Context(), bandID, variantID, actor.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}

// handleForkVariantIntoBand is "Make Mercy's version" (the song page's
// Edit on a song the band links, docs/ownership-model.md §5): the band's
// own version of the song (store.MakeVersion), which takes the link's
// place in the band's songbook ({"songbookId"}, else every songbook of the
// band's that links it) and set lists, its position and key kept. A song
// that is the band's already is answered as it is: Edit edits it. Answers
// the version's id.
func (s *Server) handleForkVariantIntoBand(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	variantID, err := strconv.ParseInt(r.PathValue("variantID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	var req struct {
		SongbookID int64 `json:"songbookId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // an empty body: every songbook
	ctx := r.Context()
	if own, err := s.store.IsBandSong(ctx, bandID, variantID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	} else if own {
		writeJSON(w, http.StatusOK, map[string]any{"variantId": variantID})
		return
	}
	newID, err := s.store.MakeVersion(ctx, personFromContext(r).ID, variantID, store.BandOwner(bandID), req.SongbookID, "")
	if !s.writeLinkErr(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"variantId": newID})
}

// writeLinkErr answers a refused link or version (store.LinkRefusal), or
// any other error, and says whether there was none. A refusal is a 409
// that says why: a closed songbook's song goes nowhere else; another
// band's private song stays theirs, with the catalogue's version of the
// work offered when there is one (catalogVariantId, question).
func (s *Server) writeLinkErr(w http.ResponseWriter, r *http.Request, err error) bool {
	var refused *store.LinkRefusal
	switch {
	case err == nil:
		return true
	case errors.As(err, &refused) && refused.Reason != "unseen":
		lang := s.langOfRequest(r)
		out := map[string]any{"error": refused.Reason, "owner": refused.OwnerName}
		switch {
		case refused.Reason == "closed":
			out["message"] = i18n.T(lang, "This song is in a closed songbook: it can’t be added anywhere else.")
		case refused.CatalogueSongID != 0:
			out["catalogVariantId"] = refused.CatalogueSongID
			out["message"] = i18n.T(lang, "This is %s’s private song: only its own songbooks hold it.", refused.OwnerName)
			out["question"] = i18n.T(lang, "This is %s’s private song. Add the catalogue’s song instead?", refused.OwnerName)
		default:
			out["message"] = i18n.T(lang, "This is %s’s private song: only its own songbooks hold it.", refused.OwnerName)
		}
		writeJSON(w, http.StatusConflict, out)
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrForbidden):
		httpError(w, http.StatusForbidden, "you can't add this song here")
	case errors.Is(err, store.ErrInvalid):
		httpError(w, http.StatusBadRequest, err.Error())
	default:
		httpError(w, http.StatusInternalServerError, err.Error())
	}
	return false
}

// handleBandSettingsPage is the band's Settings section: its name, what
// it plays, its look (core members) and deleting it.
func (s *Server) handleBandSettingsPage(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	person := personFromContext(r)
	band, err := s.store.GetBand(r.Context(), bandID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The roles editor (docs/band-roles.md) and what the viewer may
	// change; isCore: the band's look and language (edit_look).
	roles, err := s.bandRolesEditor(r, bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	isCore := roles.Privs.Can(store.BandPrivEditLook)
	locale, err := s.store.BandLocale(r.Context(), bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "band_settings", map[string]any{
		"BandRoles": roles,
		"Me": person, "Band": band, "IsCore": isCore || person.IsAdmin,
		"Locale": locale,
	})
}
