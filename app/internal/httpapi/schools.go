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

// requireSchoolMember wraps requireAuth, then checks the logged-in
// person is a CURRENT member (either role) of the school identified by
// the {id} path param — the gate on "may see this school's material at
// all," same shape requireBandMember uses for bands. A non-member and a
// bad/missing school id both get the same 403.
func (s *Server) requireSchoolMember(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		schoolID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid community id")
			return
		}
		person := personFromContext(r)
		if person.IsAdmin {
			next(w, r)
			return
		}
		isMember, err := s.store.IsCurrentSchoolMember(r.Context(), schoolID, person.ID)
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

// requireSchoolStaff wraps requireAuth, then checks the logged-in
// person is a CURRENT INSTRUCTOR of the school identified by the {id}
// path param — the gate on every instructor-only action: membership
// management, catalog add/fork/remove/create, rename, and submission
// review. A current STUDENT gets the same 403 a non-member would (don't
// leak the distinction via status code).
func (s *Server) requireSchoolStaff(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		schoolID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
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
		isInstructor, err := s.store.IsCurrentSchoolStaff(r.Context(), schoolID, person.ID)
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

// handleSchoolsPage lists the logged-in person's own current schools
// (either role), with a "start a school" action.
func (s *Server) handleSchoolsPage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	schools, err := s.store.ListSchoolsForPerson(r.Context(), person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "schools", map[string]any{"Me": person, "Schools": schools})
}

// handleCreateSchool makes a brand-new school, with the creator as its
// first member — AS AN INSTRUCTOR (see store.CreateSchool). Any
// logged-in person may create one, same as bands.
func (s *Server) handleCreateSchool(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		httpError(w, http.StatusBadRequest, "missing name")
		return
	}
	person := personFromContext(r)
	id, err := s.store.CreateSchool(r.Context(), req.Name, person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// handleSchoolPage is the student-facing (but instructor-reachable too)
// school page: the school's read-only repertoire, a "submit your work"
// entry point, and the viewer's own submission history/status. Catalog
// management, membership, and the submissions review queue live on the
// separate instructor-only /schools/{id}/admin page (handleSchoolAdminPage).
func (s *Server) handleSchoolPage(w http.ResponseWriter, r *http.Request) {
	schoolID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	person := personFromContext(r)
	school, err := s.store.GetSchool(r.Context(), schoolID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	entries, err := s.store.ListSchoolCatalog(r.Context(), schoolID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	isInstructor, err := s.store.IsCurrentSchoolStaff(r.Context(), schoolID, person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	mySubmissions, err := s.store.ListSchoolSubmissionsForStudent(r.Context(), schoolID, person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	mySongbookID, err := s.store.GetDefaultSongbook(r.Context(), "person", person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	myCatalog, err := s.store.ListSongbookEntries(r.Context(), mySongbookID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "school", map[string]any{
		"Me": person, "School": school, "Entries": entries, "IsStaff": isInstructor,
		"MySubmissions": mySubmissions, "MyCatalog": myCatalog,
	})
}

// handleSchoolAdminPage is the instructor-only management page:
// current+past members, the pending submissions queue, and the
// school's catalog — everything an instructor needs to run the school.
func (s *Server) handleSchoolAdminPage(w http.ResponseWriter, r *http.Request) {
	schoolID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	person := personFromContext(r)
	school, err := s.store.GetSchool(r.Context(), schoolID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	members, err := s.store.ListSchoolMembers(r.Context(), schoolID, true)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	pending, err := s.store.ListSchoolSubmissions(r.Context(), schoolID, store.SubmissionFilter{Status: "pending"})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	decided, err := s.store.ListSchoolSubmissions(r.Context(), schoolID, store.SubmissionFilter{})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	entries, err := s.store.ListSchoolCatalog(r.Context(), schoolID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	shellPeople, err := s.store.ListShellPeopleForSchool(r.Context(), schoolID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	events, err := s.store.ListEventsForSchool(r.Context(), schoolID)
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
	s.render(w, "school_admin", map[string]any{
		"Me": person, "School": school, "Members": members,
		"PendingSubmissions": pending, "AllSubmissions": decided,
		"Entries": entries, "ShellPeople": shellPeople,
		"Events": events, "EventManagers": eventManagers,
	})
}

// handleRenameSchool changes a school's name — the admin page's "basic
// settings" action.
func (s *Server) handleRenameSchool(w http.ResponseWriter, r *http.Request) {
	schoolID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, "missing name")
		return
	}
	if err := s.store.RenameSchool(r.Context(), schoolID, req.Name); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleSetSchoolDefaultJoinRole sets the role granted to someone who
// joins this community automatically via event registration — "apply
// community edited defaults."
func (s *Server) handleSetSchoolDefaultJoinRole(w http.ResponseWriter, r *http.Request) {
	schoolID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetSchoolDefaultJoinRole(r.Context(), schoolID, req.Role); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleAddSchoolMember adds an existing person (by username, same
// direct-add-stands-in-for-invite convention as handleAddBandMember) to
// a school as instructor or student. Instructor-only.
func (s *Server) handleAddSchoolMember(w http.ResponseWriter, r *http.Request) {
	schoolID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" {
		httpError(w, http.StatusBadRequest, "missing username")
		return
	}
	if req.Role != "staff" && req.Role != "member" {
		httpError(w, http.StatusBadRequest, `role must be "staff" or "member"`)
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
	memberID, err := s.store.AddSchoolMember(r.Context(), schoolID, target.ID, req.Role, actor.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"memberId": memberID})
}

// handleCreateSchoolPersonalInvite is handleCreateBandPersonalInvite's
// community counterpart: creates a shell person (see store.
// CreateShellPerson's doc comment), adds them to this school at role,
// and mints a personal invite token for them — one action, so the
// person is on the roster immediately and the invite link is just how
// they finish their own account, not a prerequisite for being added.
func (s *Server) handleCreateSchoolPersonalInvite(w http.ResponseWriter, r *http.Request) {
	schoolID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		DisplayName string `json:"displayName"`
		Role        string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DisplayName == "" {
		httpError(w, http.StatusBadRequest, "displayName is required")
		return
	}
	role := req.Role
	if role != "staff" && role != "member" {
		role = "member"
	}
	actor := personFromContext(r)

	personID, err := s.store.CreateShellPerson(r.Context(), req.DisplayName)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.store.AddSchoolMember(r.Context(), schoolID, personID, role, actor.ID); err != nil {
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

// handleRemoveSchoolMember ends personID's current membership in the
// school. Instructor-only; refused (via store.ErrForbidden) if it would
// leave the school with no current instructor.
func (s *Server) handleRemoveSchoolMember(w http.ResponseWriter, r *http.Request) {
	schoolID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	personID, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	if err := s.store.RemoveSchoolMember(r.Context(), schoolID, personID); err != nil {
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

// handleSetSchoolMemberRole promotes/demotes a current member between
// instructor and student. Instructor-only; refused if demoting would
// leave the school with no current instructor.
func (s *Server) handleSetSchoolMemberRole(w http.ResponseWriter, r *http.Request) {
	schoolID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
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
	if err := s.store.SetSchoolMemberRole(r.Context(), schoolID, personID, req.Role); err != nil {
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

// handleCreateSchoolGroupInvite makes a brand-new, reusable group
// invite for this school — the school equivalent of
// handleCreateBandGroupInvite. Instructor-only (unlike bands' "any
// current member," schools have a real instructor/student hierarchy —
// see this phase's lifecycle note). Every claimed slot defaults to
// school role "member" (see store.grantGroupInviteMembershipTx);
// inviting someone AS an instructor stays the deliberate direct-add
// flow (handleAddSchoolMember), not a casual roster link.
func (s *Server) handleCreateSchoolGroupInvite(w http.ResponseWriter, r *http.Request) {
	schoolID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Slots []struct {
			DisplayName string `json:"displayName"`
		} `json:"slots"`
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
	groupInviteID, token, err := s.store.CreateGroupInvite(r.Context(), "school", schoolID, actor.ID, slots, "")
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": groupInviteID, "url": "/join/" + token})
}

// handleCreateCommunityLink mints a single, reusable, open-join link for
// this community (school) — no pre-typed roster, unlike
// handleCreateSchoolGroupInvite's slots-based one: anyone who follows it
// creates an account (or logs into an existing one) and immediately gets
// the role fixed on the link itself at creation time, "staff" or
// "member" — the user's own explicit design choice for Community over
// the slots-per-person model bands/schools already have. Internally this
// is still a GroupInvite with zero slots (see ClaimGroupInviteUnlisted),
// just created without ever offering a roster form.
func (s *Server) handleCreateCommunityLink(w http.ResponseWriter, r *http.Request) {
	schoolID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Role string `json:"role"` // "staff" or "member"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	role := "member"
	if req.Role == "staff" {
		role = "staff"
	}
	// (role already matches the DB's own 'staff'/'member' enum values
	// 1:1 — no translation layer needed since migration 0044.)
	actor := personFromContext(r)
	isStaff, err := s.store.IsCurrentSchoolStaff(r.Context(), schoolID, actor.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !isStaff {
		httpError(w, http.StatusForbidden, "must be staff of this community")
		return
	}
	_, token, err := s.store.CreateGroupInvite(r.Context(), "school", schoolID, actor.ID, nil, role)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": "/join/" + token, "role": req.Role})
}

// handleCloseSchoolGroupInvite explicitly closes a still-open school
// group invite. Instructor-only, same hierarchy reasoning as creation.
func (s *Server) handleCloseSchoolGroupInvite(w http.ResponseWriter, r *http.Request) {
	schoolID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	groupInviteID, err := strconv.ParseInt(r.PathValue("groupInviteID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid group invite id")
		return
	}
	if err := s.store.CloseGroupInvite(r.Context(), groupInviteID, "school", schoolID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"closed": true})
}

// handleAddSchoolCatalogEntry points the school's repertoire at an
// existing (global, or anyone's visible) variant, by reference.
// Instructor-only.
func (s *Server) handleAddSchoolCatalogEntry(w http.ResponseWriter, r *http.Request) {
	schoolID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		VariantID int64 `json:"variantId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VariantID == 0 {
		httpError(w, http.StatusBadRequest, "missing variantId")
		return
	}
	actor := personFromContext(r)
	if err := s.store.AddSchoolCatalogEntry(r.Context(), schoolID, req.VariantID, actor.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"added": true})
}

// handleRemoveSchoolCatalogEntry drops the school's pointer to one
// variant. Instructor-only.
func (s *Server) handleRemoveSchoolCatalogEntry(w http.ResponseWriter, r *http.Request) {
	schoolID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	variantID, err := strconv.ParseInt(r.PathValue("variantID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	if err := s.store.RemoveSchoolCatalogEntry(r.Context(), schoolID, variantID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}

// handleForkVariantIntoSchool forks an existing (global, or anyone's
// visible) variant into the school's own repertoire — visibility=
// 'school', owner_school_id = this school. Instructor-only.
func (s *Server) handleForkVariantIntoSchool(w http.ResponseWriter, r *http.Request) {
	schoolID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	variantID, err := strconv.ParseInt(r.PathValue("variantID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	actor := personFromContext(r)
	newID, err := s.store.ForkVariant(r.Context(), variantID, actor.ID, "school", schoolID)
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

// handleCreateSchoolVariant creates a brand-new, from-scratch variant
// owned by this school (visibility='school') via
// store.CreateSchoolVariant. Instructor-only.
func (s *Server) handleCreateSchoolVariant(w http.ResponseWriter, r *http.Request) {
	schoolID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Title  string `json:"title"`
		Artist string `json:"artist"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" {
		httpError(w, http.StatusBadRequest, "title is required")
		return
	}
	actor := personFromContext(r)
	newID, err := s.store.CreateSchoolVariant(r.Context(), schoolID, req.Title, req.Artist, actor.ID)
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

// handleCreateSchoolSubmission is a student submitting their own
// already-created/forked personal variant for the school's
// consideration — POST /api/schools/{id}/submissions. kind is
// "new_song" or "edit"; "edit" requires sourceVariantId (the school/
// global variant being edited). Any current school member may call
// this (instructors aren't barred from submitting either, though the
// brief's primary scenario is students) — store.CreateSchoolSubmission
// itself enforces that submittedVariantId is owned by the caller.
func (s *Server) handleCreateSchoolSubmission(w http.ResponseWriter, r *http.Request) {
	schoolID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
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
	id, err := s.store.CreateSchoolSubmission(r.Context(), schoolID, actor.ID, req.Kind, sourceVariantID, req.SubmittedVariantID, req.Note)
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

// handleAcceptSchoolSubmission accepts a pending submission —
// instructor-only (requireSchoolStaff); store.AcceptSchoolSubmission
// double-checks instructor status against the submission's OWN
// school_id too, same defense-in-depth every other accept/reject path
// in this codebase uses.
func (s *Server) handleAcceptSchoolSubmission(w http.ResponseWriter, r *http.Request) {
	submissionID, err := strconv.ParseInt(r.PathValue("submissionID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid submission id")
		return
	}
	actor := personFromContext(r)
	if err := s.store.AcceptSchoolSubmission(r.Context(), submissionID, actor.ID); err != nil {
		writeSchoolSubmissionDecisionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

// handleRejectSchoolSubmission rejects a pending submission —
// instructor-only.
func (s *Server) handleRejectSchoolSubmission(w http.ResponseWriter, r *http.Request) {
	submissionID, err := strconv.ParseInt(r.PathValue("submissionID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid submission id")
		return
	}
	actor := personFromContext(r)
	if err := s.store.RejectSchoolSubmission(r.Context(), submissionID, actor.ID); err != nil {
		writeSchoolSubmissionDecisionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"rejected": true})
}

func writeSchoolSubmissionDecisionError(w http.ResponseWriter, err error) {
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
