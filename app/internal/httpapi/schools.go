package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

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
			httpError(w, http.StatusBadRequest, "invalid school id")
			return
		}
		person := personFromContext(r)
		isMember, err := s.store.IsCurrentSchoolMember(r.Context(), schoolID, person.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !isMember {
			httpError(w, http.StatusForbidden, "must be a current member of this school")
			return
		}
		next(w, r)
	})
}

// requireSchoolInstructor wraps requireAuth, then checks the logged-in
// person is a CURRENT INSTRUCTOR of the school identified by the {id}
// path param — the gate on every instructor-only action: membership
// management, catalog add/fork/remove/create, rename, and submission
// review. A current STUDENT gets the same 403 a non-member would (don't
// leak the distinction via status code).
func (s *Server) requireSchoolInstructor(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		schoolID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid school id")
			return
		}
		person := personFromContext(r)
		isInstructor, err := s.store.IsCurrentSchoolInstructor(r.Context(), schoolID, person.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !isInstructor {
			httpError(w, http.StatusForbidden, "must be a current instructor of this school")
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
	isInstructor, err := s.store.IsCurrentSchoolInstructor(r.Context(), schoolID, person.ID)
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
		"Me": person, "School": school, "Entries": entries, "IsInstructor": isInstructor,
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
	s.render(w, "school_admin", map[string]any{
		"Me": person, "School": school, "Members": members,
		"PendingSubmissions": pending, "AllSubmissions": decided,
		"Entries": entries,
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
	if req.Role != "instructor" && req.Role != "student" {
		httpError(w, http.StatusBadRequest, `role must be "instructor" or "student"`)
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
// school role "student" (see store.grantGroupInviteMembershipTx);
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
	groupInviteID, token, err := s.store.CreateGroupInvite(r.Context(), "school", schoolID, actor.ID, slots)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": groupInviteID, "url": "/join/" + token})
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
// instructor-only (requireSchoolInstructor); store.AcceptSchoolSubmission
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
