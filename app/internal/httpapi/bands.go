package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
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
	bands, err := s.store.ListBandsForPerson(r.Context(), person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "bands", map[string]any{"Me": person, "Bands": bands})
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
	members, err := s.store.ListBandMembers(r.Context(), bandID, true)
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

	s.render(w, "band", map[string]any{
		"Me": person, "Band": band, "Members": members, "Changes": changes, "HasLastSeen": since != nil,
	})
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
		} `json:"slots"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	slots := make([]store.GroupInviteSlotInput, 0, len(req.Slots))
	for _, sl := range req.Slots {
		slots = append(slots, store.GroupInviteSlotInput{DisplayName: sl.DisplayName, Instrument: sl.Instrument})
	}
	actor := personFromContext(r)
	groupInviteID, token, err := s.store.CreateGroupInvite(r.Context(), "band", bandID, actor.ID, slots)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": groupInviteID, "url": "/join/" + token})
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
	books, err := s.store.ListBooks()
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "band_catalog", map[string]any{"Me": person, "Band": band, "Entries": entries, "Books": books})
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
		Title   string `json:"title"`
		Artist  string `json:"artist"`
		Project string `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Project) == "" {
		httpError(w, http.StatusBadRequest, "title and project are required")
		return
	}
	projectID, err := s.store.ProjectID(r.Context(), req.Project)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such project: "+req.Project)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	actor := personFromContext(r)
	newID, err := s.store.CreateBandVariant(r.Context(), bandID, projectID, req.Title, req.Artist, actor.ID)
	if errors.Is(err, store.ErrForbidden) {
		httpError(w, http.StatusForbidden, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	v, err := s.store.GetVariant(r.Context(), newID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"variantId": newID, "book": v.Book, "stem": v.Stem,
	})
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
	v, err := s.store.GetVariant(r.Context(), newID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"variantId": newID, "book": v.Book, "stem": v.Stem,
	})
}
