package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"app/internal/store"
)

// Warm-ups (docs/coaching.md, "Warm-ups"; migration 0087): a coaching
// community's exercises, written once by the coach as a pattern, played
// by the browser in every key of each student's path (warmup.js); the
// coach's recording of it in its written key, to listen to; sessions.

// handleWarmupsPage is GET /communities/{id}/warmups: the exercises and
// the sessions.
func (s *Server) handleWarmupsPage(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	exercises, err := s.store.ListExercises(ctx, community.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sessions, err := s.store.ListExerciseSessions(ctx, community.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data := s.coachingPageData(r, community, coach, "warmups")
	data["Exercises"] = exercises
	data["Sessions"] = sessions
	byID := map[int64]string{}
	for _, e := range exercises {
		byID[e.ID] = e.Name
	}
	data["ExerciseNames"] = byID
	s.render(w, "coaching_warmups", data)
}

// handleExerciseEditPage is GET /communities/{id}/warmups/new and
// /communities/{id}/warmups/{exerciseID}/edit: the coach's pattern form.
func (s *Server) handleExerciseEditPage(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	if !coach {
		http.NotFound(w, r)
		return
	}
	data := s.coachingPageData(r, community, coach, "warmups")
	if idStr := r.PathValue("exerciseID"); idStr != "" {
		id, _ := strconv.ParseInt(idStr, 10, 64)
		e, err := s.store.GetExercise(r.Context(), community.ID, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		data["Exercise"] = e
		data["ExerciseJSON"] = e
	}
	if err := s.checkLevelsData(r, community.ID, data); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	set, err := s.store.GetCheckSettings(r.Context(), community.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data["Messages"] = s.checkMessageViews(r, set)
	s.render(w, "coaching_exercise_edit", data)
}

// exercisePageData is what the practice page reads (warmup-page.js).
type exercisePageData struct {
	Exercise  *store.Exercise     `json:"exercise"`
	Waypoints []int               `json:"waypoints"` // the student's, nil: the app's
	Low       int                 `json:"low"`       // their range, MIDI, 0: not noted
	High      int                 `json:"high"`
	StudentID int64               `json:"studentId"` // whose path, 0: the written key
	IsCoach   bool                `json:"isCoach"`
	Settings  store.CheckSettings `json:"settings"`
	Name      string              `json:"name"`
	Coach     string              `json:"coach"`
	Session   *sessionView        `json:"session,omitempty"`
}

// sessionView is a session the exercise is played in: its exercises in
// order and their names.
type sessionView struct {
	ID          int64            `json:"id"`
	Name        string           `json:"name"`
	ExerciseIDs []int64          `json:"exerciseIds"`
	Names       map[int64]string `json:"names"`
}

// handleExercisePage is GET /communities/{id}/warmups/{exerciseID}: the
// exercise, in a student's keys (the student's own; the coach picks one
// with ?student=, else the written key).
func (s *Server) handleExercisePage(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	id, err := pathID(r, "exerciseID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	e, err := s.store.GetExercise(ctx, community.ID, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	pd := exercisePageData{Exercise: e, IsCoach: coach}
	studentID := s.viewedPersonID(r, community.ID)
	if coach {
		studentID, _ = strconv.ParseInt(r.URL.Query().Get("student"), 10, 64)
	}
	if studentID != 0 {
		member, ok := s.currentStudent(r, community.ID, studentID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		pd.StudentID = studentID
		pd.Name = firstWord(member.DisplayName)
		if pd.Waypoints, err = s.store.ExercisePath(ctx, e.ID, studentID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if pd.Low, pd.High, err = s.store.StudentRangeSpan(ctx, community.ID, studentID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if pd.Settings, err = s.store.GetCheckSettings(ctx, community.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// the exercise's own level and words over the community's
	for k, v := range e.Messages {
		if v != "" {
			pd.Settings.Messages[k] = v
		}
	}
	pd.Coach = firstWord(s.store.CoachName(ctx, community.ID))
	if sid, _ := strconv.ParseInt(r.URL.Query().Get("session"), 10, 64); sid != 0 {
		if sess, err := s.store.GetExerciseSession(ctx, community.ID, sid); err == nil {
			sv := &sessionView{ID: sess.ID, Name: sess.Name, ExerciseIDs: sess.ExerciseIDs, Names: map[int64]string{}}
			if all, err := s.store.ListExercises(ctx, community.ID); err == nil {
				for _, x := range all {
					sv.Names[x.ID] = x.Name
				}
			}
			pd.Session = sv
		}
	}
	data := s.coachingPageData(r, community, coach, "warmups")
	data["Exercise"] = e
	data["PageJSON"] = pd
	if coach {
		if data["Students"], err = s.communityStudents(r, community.ID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data["StudentID"] = studentID
	}
	s.render(w, "coaching_exercise", data)
}

func firstWord(s string) string {
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[:i]
	}
	return s
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// exerciseRequest checks a coach's call on an exercise of the community.
func (s *Server) coachExercise(w http.ResponseWriter, r *http.Request) (*store.Community, int64, bool) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return nil, 0, false
	}
	if !coach {
		httpError(w, http.StatusForbidden, "only the coach can do that")
		return nil, 0, false
	}
	id, _ := strconv.ParseInt(r.PathValue("exerciseID"), 10, 64)
	return community, id, true
}

// handlePostExercise is POST /api/communities/{id}/exercises.
func (s *Server) handlePostExercise(w http.ResponseWriter, r *http.Request) {
	community, _, ok := s.coachExercise(w, r)
	if !ok {
		return
	}
	var in store.ExerciseInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	id, err := s.store.CreateExercise(r.Context(), community.ID, in, personFromContext(r).ID)
	if errors.Is(err, store.ErrBadExercise) {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// handlePutExercise is PUT /api/communities/{id}/exercises/{exerciseID}.
func (s *Server) handlePutExercise(w http.ResponseWriter, r *http.Request) {
	community, id, ok := s.coachExercise(w, r)
	if !ok {
		return
	}
	var in store.ExerciseInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	err := s.store.UpdateExercise(r.Context(), community.ID, id, in)
	switch {
	case errors.Is(err, store.ErrBadExercise):
		httpError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "no such exercise")
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleDeleteExercise is DELETE /api/communities/{id}/exercises/{exerciseID}.
func (s *Server) handleDeleteExercise(w http.ResponseWriter, r *http.Request) {
	community, id, ok := s.coachExercise(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteExercise(r.Context(), community.ID, id); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePutExerciseDemo is PUT /api/communities/{id}/exercises/{exerciseID}/demo:
// the coach's recording (the body, an audio file up to 8 MB).
func (s *Server) handlePutExerciseDemo(w http.ResponseWriter, r *http.Request) {
	community, id, ok := s.coachExercise(w, r)
	if !ok {
		return
	}
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "audio/") {
		httpError(w, http.StatusBadRequest, "an audio recording")
		return
	}
	audio, err := io.ReadAll(io.LimitReader(r.Body, 8<<20+1))
	if err != nil || len(audio) == 0 || len(audio) > 8<<20 {
		httpError(w, http.StatusBadRequest, "a recording up to 8 MB")
		return
	}
	if err := s.store.SetExerciseDemo(r.Context(), community.ID, id, audio, ct); errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such exercise")
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteExerciseDemo is DELETE /api/communities/{id}/exercises/{exerciseID}/demo.
func (s *Server) handleDeleteExerciseDemo(w http.ResponseWriter, r *http.Request) {
	community, id, ok := s.coachExercise(w, r)
	if !ok {
		return
	}
	if err := s.store.SetExerciseDemo(r.Context(), community.ID, id, nil, ""); err != nil && !errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetExerciseDemo is GET /communities/{id}/warmups/{exerciseID}/demo:
// the coach's recording, for the community's members.
func (s *Server) handleGetExerciseDemo(w http.ResponseWriter, r *http.Request) {
	community, _, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("exerciseID"), 10, 64)
	audio, ct, err := s.store.ExerciseDemo(r.Context(), community.ID, id)
	if err != nil || audio == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.Write(audio)
}

// handlePutExercisePath is PUT /api/communities/{id}/exercises/{exerciseID}/path
// {"personId", "waypoints"}: a student's keys (none: the app's).
func (s *Server) handlePutExercisePath(w http.ResponseWriter, r *http.Request) {
	community, id, ok := s.coachExercise(w, r)
	if !ok {
		return
	}
	var req struct {
		PersonID  int64 `json:"personId"`
		Waypoints []int `json:"waypoints"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if _, ok := s.currentStudent(r, community.ID, req.PersonID); !ok {
		httpError(w, http.StatusBadRequest, "not a student here")
		return
	}
	if _, err := s.store.GetExercise(r.Context(), community.ID, id); err != nil {
		httpError(w, http.StatusNotFound, "no such exercise")
		return
	}
	if err := s.store.SetExercisePath(r.Context(), id, req.PersonID, req.Waypoints); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSaveExerciseSession is POST /api/communities/{id}/exercise-sessions
// and PUT …/{sessionID} {"name", "exerciseIds"}.
func (s *Server) handleSaveExerciseSession(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	if !coach {
		httpError(w, http.StatusForbidden, "only the coach can do that")
		return
	}
	var req struct {
		Name        string  `json:"name"`
		ExerciseIDs []int64 `json:"exerciseIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	sid, _ := strconv.ParseInt(r.PathValue("sessionID"), 10, 64)
	id, err := s.store.SaveExerciseSession(r.Context(), community.ID, sid, strings.TrimSpace(req.Name), req.ExerciseIDs)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such session")
		return
	}
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// handleDeleteExerciseSession is DELETE /api/communities/{id}/exercise-sessions/{sessionID}.
func (s *Server) handleDeleteExerciseSession(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	if !coach {
		httpError(w, http.StatusForbidden, "only the coach can do that")
		return
	}
	sid, _ := strconv.ParseInt(r.PathValue("sessionID"), 10, 64)
	if err := s.store.DeleteExerciseSession(r.Context(), community.ID, sid); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
