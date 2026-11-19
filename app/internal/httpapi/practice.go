package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"app/internal/store"
)

// Practice (docs/coaching.md, "Practice"): the coach gives a student or a
// group something to work on between lessons, a song of theirs or
// something written, so many days a week, until a date; the student's
// practice is logged, the time a song of theirs plays counted by the
// song page, and what they tell besides.

type assignmentRequest struct {
	PersonID  int64  `json:"personId"`
	GroupID   int64  `json:"groupId"`
	VariantID int64  `json:"variantId"`
	Title     string `json:"title"`
	Notes     string `json:"notes"`
	PerWeek   int    `json:"perWeek"`
	DueOn     string `json:"dueOn"` // 2006-01-02, or ""
	// A goal on a song: a level, and the % of notes in tune and on time.
	Level        string `json:"level"`
	PitchTarget  int    `json:"pitchTarget"`
	RhythmTarget int    `json:"rhythmTarget"`
}

// handlePostAssignment is POST /api/communities/{id}/assignments: the
// coach gives a student or a group something to practise.
func (s *Server) handlePostAssignment(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	if !coach {
		httpError(w, http.StatusForbidden, "only the coach can do that")
		return
	}
	var req assignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	ctx := r.Context()
	switch {
	case req.PersonID != 0 && req.GroupID == 0:
		if _, ok := s.currentStudent(r, community.ID, req.PersonID); !ok {
			httpError(w, http.StatusBadRequest, "not a student here")
			return
		}
	case req.GroupID != 0 && req.PersonID == 0:
		if g, err := s.store.GetCoachingGroup(ctx, req.GroupID); err != nil || g.CommunityID != community.ID {
			httpError(w, http.StatusBadRequest, "no such group")
			return
		}
	default:
		httpError(w, http.StatusBadRequest, "a student or a group")
		return
	}
	if req.VariantID != 0 {
		if in, err := s.store.CommunityHasSong(ctx, community.ID, req.VariantID); err != nil || !in {
			httpError(w, http.StatusBadRequest, "not a song of this community")
			return
		}
	}
	var due *time.Time
	if req.DueOn != "" {
		d, err := time.ParseInLocation("2006-01-02", req.DueOn, time.Local)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid date")
			return
		}
		due = &d
	}
	id, err := s.store.CreatePracticeAssignment(ctx, store.NewPracticeAssignment{
		CommunityID: community.ID, PersonID: req.PersonID, GroupID: req.GroupID, VariantID: req.VariantID,
		Title: strings.TrimSpace(req.Title), Notes: strings.TrimSpace(req.Notes), PerWeek: req.PerWeek, DueOn: due,
		By: personFromContext(r).ID, Level: req.Level, PitchTarget: req.PitchTarget, RhythmTarget: req.RhythmTarget,
	})
	if errors.Is(err, store.ErrBadAssignment) {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// handlePutAssignment is PUT /api/communities/{id}/assignments/{assignmentID}
// {"done": bool}.
func (s *Server) handlePutAssignment(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	if !coach {
		httpError(w, http.StatusForbidden, "only the coach can do that")
		return
	}
	id, err := pathID(r, "assignmentID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid assignment")
		return
	}
	var req struct {
		Done bool `json:"done"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetPracticeAssignmentDone(r.Context(), community.ID, id, req.Done); errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such assignment")
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteAssignment is DELETE /api/communities/{id}/assignments/{assignmentID}.
func (s *Server) handleDeleteAssignment(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	if !coach {
		httpError(w, http.StatusForbidden, "only the coach can do that")
		return
	}
	id, err := pathID(r, "assignmentID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid assignment")
		return
	}
	if err := s.store.DeletePracticeAssignment(r.Context(), community.ID, id); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePostPractice is POST /api/communities/{id}/practice: a student
// says what they practised, so many minutes, on an assignment of theirs
// or not, today or a day before.
func (s *Server) handlePostPractice(w http.ResponseWriter, r *http.Request) {
	community, _, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	me := personFromContext(r)
	if _, ok := s.currentStudent(r, community.ID, me.ID); !ok {
		httpError(w, http.StatusForbidden, "only a student logs practice")
		return
	}
	var req struct {
		AssignmentID int64  `json:"assignmentId"`
		Minutes      int    `json:"minutes"`
		Note         string `json:"note"`
		Day          string `json:"day"` // 2006-01-02, today if ""
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Minutes < 1 || req.Minutes > 600 {
		httpError(w, http.StatusBadRequest, "minutes: 1 to 600")
		return
	}
	ctx := r.Context()
	if req.AssignmentID != 0 {
		mine, err := s.store.ListStudentAssignments(ctx, community.ID, me.ID, true)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		found := false
		for _, a := range mine {
			found = found || a.ID == req.AssignmentID
		}
		if !found {
			httpError(w, http.StatusBadRequest, "not an assignment of yours")
			return
		}
	}
	day := time.Now()
	if req.Day != "" {
		d, err := time.ParseInLocation("2006-01-02", req.Day, time.Local)
		if err != nil || d.After(day) || d.Before(day.AddDate(0, 0, -30)) {
			httpError(w, http.StatusBadRequest, "a day of the last 30")
			return
		}
		day = d
	}
	id, err := s.store.LogPractice(ctx, community.ID, me.ID, req.AssignmentID, day, req.Minutes*60, strings.TrimSpace(req.Note))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// handleDeletePractice is DELETE /api/communities/{id}/practice/{logID}:
// a student takes back what they told.
func (s *Server) handleDeletePractice(w http.ResponseWriter, r *http.Request) {
	community, _, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	id, err := pathID(r, "logID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid log")
		return
	}
	if err := s.store.DeletePracticeLog(r.Context(), community.ID, personFromContext(r).ID, id); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePostPlayed is POST /api/practice/played {"songbookId", "variantId",
// "seconds"}: the song page's count of the time a song played, opened
// from a songbook (sent with navigator.sendBeacon, so any content type).
// Logged only for a coaching community's student playing their own or
// their group's songbook; anyone else, quietly nothing.
func (s *Server) handlePostPlayed(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		SongbookID int64 `json:"songbookId"`
		VariantID  int64 `json:"variantId"`
		Seconds    int   `json:"seconds"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if _, err := s.store.LogPlayed(r.Context(), personFromContext(r).ID, req.SongbookID, req.VariantID, req.Seconds); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePostPitchTake is POST /api/practice/pitch: a take checked by
// pitch-check.js ({"songbookId", "variantId", "level", "pitch", "rhythm",
// "cents", "ms", "notes", "hits", "seconds"}), kept for a coaching student
// singing a song of their songbook; the song's progress back (204 for
// anyone else, nothing kept).
func (s *Server) handlePostPitchTake(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SongbookID int64  `json:"songbookId"`
		VariantID  int64  `json:"variantId"`
		Level      string `json:"level"`
		Pitch      int    `json:"pitch"`
		Rhythm     int    `json:"rhythm"`
		Cents      int    `json:"cents"`
		Ms         int    `json:"ms"`
		Notes      int    `json:"notes"`
		Hits       int    `json:"hits"`
		Seconds    int    `json:"seconds"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	me := personFromContext(r).ID
	kept, err := s.store.RecordTake(r.Context(), me, req.SongbookID, req.VariantID, store.Take{
		Level: req.Level, Pitch: req.Pitch, Rhythm: req.Rhythm, Cents: req.Cents, Ms: req.Ms, Notes: req.Notes, Hits: req.Hits, Seconds: req.Seconds,
	})
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !kept {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.writeSongProgress(w, r, me, req.SongbookID, req.VariantID)
}

// handleGetPitchProgress is GET /api/practice/pitch?songbookId=&variantId=:
// the coach's goal on the song, the best and last takes, the streak (204
// when it isn't a coaching student's song of theirs).
func (s *Server) handleGetPitchProgress(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sb, err1 := strconv.ParseInt(q.Get("songbookId"), 10, 64)
	v, err2 := strconv.ParseInt(q.Get("variantId"), 10, 64)
	if err1 != nil || err2 != nil {
		httpError(w, http.StatusBadRequest, "songbookId and variantId")
		return
	}
	s.writeSongProgress(w, r, personFromContext(r).ID, sb, v)
}

func (s *Server) writeSongProgress(w http.ResponseWriter, r *http.Request, personID, songbookID, variantID int64) {
	p, err := s.store.SongProgressOf(r.Context(), personID, songbookID, variantID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
