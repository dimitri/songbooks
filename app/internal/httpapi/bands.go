package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"app/internal/store"
)

// requireBandMember wraps requireAuth, then checks the logged-in person
// is a CURRENT member of the band identified by the {id} path param —
// the gate on every band-scoped page/mutation in this phase. No band
// "owner"/"admin" role exists (see internal/store/bands.go's Band doc
// comment), so this one check also covers add/remove member, promote/
// demote, band-catalog add/fork/remove, and deleting the whole band —
// any current member (core or guest alike) may do all of those.
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
	bandCards, _, err := s.buildBandCards(r.Context(), person.ID, bands)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "bands", map[string]any{"Me": person, "Bands": bandCards, "Tab": tab, "ShowAllTab": person.IsAdmin})
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
	changes, err := s.store.ListBandChanges(r.Context(), bandID, since)
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
	openProposals, err := s.store.ListProposals(r.Context(), bandID, store.ProposalFilter{Status: "open"})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// "Listening party" has no event log of its own — it's the SAME
	// proposal/vote queue, pre-filtered to open 'new_song' proposals
	// (see handleListeningPartyPage's own doc comment: "reuse the
	// proposal+vote mechanism rather than building a third parallel
	// system"). So a proposal's own Kind IS its listening-party-ness;
	// no separate query, just a per-item label below in the template.

	var repertoireCount int
	var repertoirePreview []store.CatalogVariant
	repertoire, err := s.store.ListBandCatalog(r.Context(), bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	repertoireCount = len(repertoire)
	for _, e := range repertoire {
		repertoirePreview = append(repertoirePreview, e.CatalogVariant)
	}
	if len(repertoirePreview) > 5 {
		repertoirePreview = repertoirePreview[:5]
	}

	// Same per-album grouping the Repertoire tab uses (handleBandCatalogPage),
	// applied to the already-trimmed preview slice — an "original" act's
	// Overview card should read as album groups too, not a flat 5-song
	// list that happens to mix records together.
	var repertoirePreviewAlbums []albumGroup
	if band.MusicType == "original" {
		covers, err := s.store.GetAlbumCovers(r.Context(), bandID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		previewEntries := make([]store.BandCatalogEntry, len(repertoirePreview))
		for i, v := range repertoirePreview {
			previewEntries[i] = store.BandCatalogEntry{CatalogVariant: v}
		}
		repertoirePreviewAlbums = groupByAlbum(previewEntries, covers)
	}

	setlists, err := s.store.ListSetlistsForBand(r.Context(), bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	gigs, err := s.store.ListUpcomingGigsForPerson(r.Context(), person.ID, 200)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var nextGig *store.UpcomingGig
	for i := range gigs {
		if gigs[i].BandID != nil && *gigs[i].BandID == bandID {
			nextGig = &gigs[i]
			break // ListUpcomingGigsForPerson is already soonest-first
		}
	}

	s.render(w, "band", map[string]any{
		"Me": person, "Band": band, "Changes": changes, "HasLastSeen": since != nil,
		"OpenProposals": openProposals,
		"Repertoire":    repertoirePreview, "RepertoireCount": repertoireCount,
		"RepertoirePreviewAlbums": repertoirePreviewAlbums,
		"Setlists":                setlists, "NextGig": nextGig,
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

	// Phase 27: show a complete roster picture — current members (with
	// their instrument) AND anyone named on a still-open group-invite
	// roster who hasn't claimed their slot yet ("Marc hasn't joined
	// yet"), plus any already-created, still-shareable invite link, so
	// the page never again shows a link only once right after creating
	// it (see ListGroupInvitesForTarget/ListOpenBandInvites doc comments).
	groupInvites, err := s.store.ListGroupInvitesForTarget(r.Context(), "band", bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	openInvites, err := s.store.ListOpenBandInvites(r.Context(), bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Whether to show the pending-slots column header (Name/Instrument/
	// Role) at all — computed here rather than in the template, since
	// "any unclaimed slot across any open group invite" needs a flag a
	// nested range can't cheaply produce on its own.
	hasPendingSlots := false
	for _, gi := range groupInvites {
		if gi.ClosedAt != nil {
			continue
		}
		for _, sl := range gi.Slots {
			if sl.ClaimedByPersonID == nil {
				hasPendingSlots = true
			}
		}
	}

	s.render(w, "band_members", map[string]any{
		"Me": person, "Band": band, "Members": members,
		"GroupInvites": groupInvites, "OpenInvites": openInvites,
		"HasPendingSlots": hasPendingSlots,
	})
}

// handleBandSetlistsPage is the Setlists tab: every setlist owned by this
// band, linking to practice/listening/clicktrack for each — a per-band
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

// handleAddBandMember adds an existing person (by username — this is a
// local single-instance app, so a direct add action stands in for an
// email-invite flow) to a band as core or guest. Any current band member
// may do this.
func (s *Server) handleAddBandMember(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Username string `json:"username"`
		Status   string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" {
		httpError(w, http.StatusBadRequest, "missing username")
		return
	}
	if req.Status != "core" && req.Status != "guest" {
		httpError(w, http.StatusBadRequest, "status must be \"core\" or \"guest\"")
		return
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
	memberID, err := s.store.AddBandMember(r.Context(), bandID, target.ID, req.Status, actor.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"memberId": memberID})
}

// handleRemoveBandMember ends personID's current membership in the band
// (sets left_at — history is kept, see migration 0010). Any current
// member may remove any other member, or themselves.
func (s *Server) handleRemoveBandMember(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	personID, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	actor := personFromContext(r)
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
// core and guest. Any current member may change any other member's
// status (or their own) — no reduced tier exists between core and guest
// to gate this on, consistent with "guests get identical edit rights to
// core members" elsewhere in this phase.
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
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleDeleteBand permanently deletes a band and everything under it.
// No "owner"/"admin" role exists for bands in this phase (see
// requireBandMember's doc comment), so — per the phase-2 brief's explicit
// instruction to flag rather than silently restrict a case like this —
// ANY current member may delete the whole band, not just whoever created
// it. There's no narrower role available to gate this on instead.
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

// handleCreateBandInvite makes a single-use band invite link, valid for 7
// days — the band equivalent of handleCreateInvite (members.go). Any
// current band member may generate one, same as any other band-scoped
// mutation.
func (s *Server) handleCreateBandInvite(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	status := r.FormValue("status")
	if status != "core" && status != "guest" {
		httpError(w, http.StatusBadRequest, "status must be \"core\" or \"guest\"")
		return
	}
	actor := personFromContext(r)
	token, err := s.store.CreateBandInvite(r.Context(), bandID, status, actor.ID, 7*24*time.Hour)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": "/invite/" + token})
}

// handleCreateBandGroupInvite makes a brand-new, reusable group invite
// for this band — the "type a roster, get one shareable link" action
// (see internal/store/group_invites.go). Any current band member may
// create one, same as any other band-scoped mutation; every claimed
// slot grants the claiming person band status "guest" (see
// store.grantGroupInviteMembershipTx's doc comment for why guest, not
// core, is the default here).
func (s *Server) handleCreateBandGroupInvite(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Slots []struct {
			DisplayName string `json:"displayName"`
			Instrument  string `json:"instrument"`
			Status      string `json:"status"`
		} `json:"slots"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	slots := make([]store.GroupInviteSlotInput, 0, len(req.Slots))
	for _, sl := range req.Slots {
		slots = append(slots, store.GroupInviteSlotInput{DisplayName: sl.DisplayName, Instrument: sl.Instrument, Status: sl.Status})
	}
	actor := personFromContext(r)
	groupInviteID, token, err := s.store.CreateGroupInvite(r.Context(), "band", bandID, actor.ID, slots, "")
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": groupInviteID, "url": "/join/" + token})
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	slotID, err := s.store.AddGroupInviteSlot(r.Context(), groupInviteID, "band", bandID, store.GroupInviteSlotInput{
		DisplayName: req.DisplayName, Instrument: req.Instrument, Status: req.Status,
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.UpdateGroupInviteSlot(r.Context(), groupInviteID, slotID, bandID, req.DisplayName, req.Instrument, req.Status); err != nil {
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
// auto-closes one, only this action does). Any current band member may
// close it, same "no hierarchy" rule as every other band-scoped action.
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
		albumGroups = groupByAlbum(entries, covers)
	}

	s.render(w, "band_catalog", map[string]any{
		"Me": person, "Band": band, "Entries": entries,
		"AlbumGroups": albumGroups,
	})
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
	Entries     []store.BandCatalogEntry
}

func groupByAlbum(entries []store.BandCatalogEntry, covers map[string]store.AlbumCover) []albumGroup {
	byAlbum := map[string][]store.BandCatalogEntry{}
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

// bulkLineRE splits a pasted "Title - Artist" line into its two halves —
// the last " - "/" – "/" — " occurrence, not the first, so a title that
// itself contains a hyphen ("Mr. Brightside - The Killers" is fine, but
// so is "Self-Control - Laura Branigan": only the LAST separator is
// treated as the title/artist boundary). No separator at all is a
// title-only line (artist left blank, same as the single-song form's own
// optional Artist field).
var bulkLineRE = regexp.MustCompile(`\s+[-\x{2013}\x{2014}]\s+`)

func splitTitleArtist(line string) (title, artist string) {
	loc := bulkLineRE.FindAllStringIndex(line, -1)
	if len(loc) == 0 {
		return strings.TrimSpace(line), ""
	}
	last := loc[len(loc)-1]
	return strings.TrimSpace(line[:last[0]]), strings.TrimSpace(line[last[1]:])
}

// bulkCreateResult is one line's outcome — created (Book/Stem set) or
// skipped (Error set, e.g. a blank line or a store-level failure) —
// reported per-line rather than failing the whole paste on one bad row,
// since a 20-song paste with one typo'd line shouldn't lose the other 19.
type bulkCreateResult struct {
	Title     string `json:"title"`
	Artist    string `json:"artist,omitempty"`
	VariantID int64  `json:"variantId,omitempty"`
	Error     string `json:"error,omitempty"`
}

// handleCreateBandVariantsBulk is the "fill in several songs at once"
// counterpart to handleCreateBandVariant: one placeholder (empty-body,
// title/artist only) per non-blank input line, each immediately editable
// — the repertoire page's bulk-add form links each result straight into
// that song's own edit view (?edit=1, not the Play page) via the
// returned Book/Stem, same "then click a song to fill it in" flow the
// single-song form's own "+ create" now also uses (see that handler).
func (s *Server) handleCreateBandVariantsBulk(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Lines string `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	actor := personFromContext(r)

	var results []bulkCreateResult
	for _, raw := range strings.Split(req.Lines, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		title, artist := splitTitleArtist(line)
		if title == "" {
			results = append(results, bulkCreateResult{Title: line, Error: "couldn't read a title from this line"})
			continue
		}
		newID, err := s.store.CreateBandVariant(r.Context(), bandID, title, artist, actor.ID)
		if err != nil {
			results = append(results, bulkCreateResult{Title: title, Artist: artist, Error: err.Error()})
			continue
		}
		results = append(results, bulkCreateResult{Title: title, Artist: artist, VariantID: newID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// handleAddBandCatalogEntry points the band's repertoire at an existing
// (global, or anyone's visible) variant, by reference — "add this song to
// the band's set," not a copy.
func (s *Server) handleAddBandCatalogEntry(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		VariantID int64 `json:"variantId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VariantID == 0 {
		httpError(w, http.StatusBadRequest, "missing variantId")
		return
	}
	actor := personFromContext(r)
	if err := s.store.AddBandCatalogEntry(r.Context(), bandID, req.VariantID, actor.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"added": true})
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

// handleForkVariantIntoBand forks an existing (global, or anyone's
// visible) variant into the band's own repertoire — the new variant row
// gets visibility='band', owner_band_id = this band, and every current
// band member can then edit it in place (see CanEditVariant).
func (s *Server) handleForkVariantIntoBand(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	variantID, err := strconv.ParseInt(r.PathValue("variantID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	actor := personFromContext(r)
	newID, err := s.store.ForkVariant(r.Context(), variantID, actor.ID, "band", bandID)
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
