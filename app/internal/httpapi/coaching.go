package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"app/internal/store"
)

// A coaching community's pages (docs/coaching.md): its students (each one's
// voice, songs in their key, lessons) and its lessons.

var noteNames = []string{"C", "C♯", "D", "E♭", "E", "F", "F♯", "G", "A♭", "A", "B♭", "B"}

// noteName is a MIDI note's name: 60 is C4.
func noteName(midi int) string { return fmt.Sprintf("%s%d", noteNames[midi%12], midi/12-1) }

type noteOption struct {
	Midi int
	Name string
}

// voiceNotes are the notes a range is picked from: C2 to C7.
func voiceNotes() []noteOption {
	var out []noteOption
	for m := 36; m <= 96; m++ {
		out = append(out, noteOption{m, noteName(m)})
	}
	return out
}

type rangeView struct {
	store.VoiceRange
	Label             string
	LowName, HighName string
	LowMidi, HighMidi int // 0 when not noted
	// Where it sits on the voice chart (C2 to C7), in %.
	Left, Width float64
}

func rangeViews(rs []store.VoiceRange) []rangeView {
	labels := map[string]string{"chest": "Chest voice", "mixed": "Mixed voice", "head": "Head voice"}
	out := make([]rangeView, len(rs))
	for i, r := range rs {
		out[i] = rangeView{VoiceRange: r, Label: labels[r.Register]}
		if r.Low != nil {
			out[i].LowName, out[i].LowMidi = noteName(*r.Low), *r.Low
		}
		if r.High != nil {
			out[i].HighName, out[i].HighMidi = noteName(*r.High), *r.High
		}
		if r.Low != nil && r.High != nil {
			out[i].Left = float64(*r.Low-36) * 100 / 60
			out[i].Width = float64(*r.High-*r.Low+1) * 100 / 61
		}
	}
	return out
}

// coachingContext loads a coaching community and says whether the viewer
// is its coach (its staff, or a site admin); nil and an error page when
// it isn't a coaching community.
func (s *Server) coachingContext(w http.ResponseWriter, r *http.Request) (*store.Community, bool, bool) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	community, err := s.store.GetCommunity(r.Context(), communityID)
	if err != nil || !community.IsCoaching() {
		http.NotFound(w, r)
		return nil, false, false
	}
	person := personFromContext(r)
	coach := person.IsAdmin
	if !coach {
		if coach, err = s.store.HasAnyCommunityManagePrivilege(r.Context(), communityID, person.ID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return nil, false, false
		}
	}
	return community, coach, true
}

type studentRow struct {
	PersonID   int64
	Name       string
	Username   string
	Ranges     []rangeView
	SongbookID int64
	SongCount  int
	NextLesson *store.Event
}

// handleCoachingStudentsPage is GET /communities/{id}/students: the coach's
// students; a student goes to their own page.
func (s *Server) handleCoachingStudentsPage(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	person := personFromContext(r)
	if !coach {
		http.Redirect(w, r, fmt.Sprintf("/communities/%d/students/%d", community.ID, person.ID), http.StatusFound)
		return
	}
	members, err := s.store.ListCommunityMembers(r.Context(), community.ID, false)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	lessons, err := s.store.ListCoachingLessons(r.Context(), community.ID, time.Now(), 0)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var rows []studentRow
	for _, m := range members {
		if m.IsStaff() {
			continue
		}
		row := studentRow{PersonID: m.PersonID, Name: m.DisplayName, Username: m.Username}
		rs, err := s.store.StudentRanges(r.Context(), community.ID, m.PersonID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		row.Ranges = rangeViews(rs)
		if row.SongbookID, err = s.store.FindStudentSongbook(r.Context(), community.ID, m.PersonID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if row.SongbookID != 0 {
			songs, _ := s.store.ListStudentSongs(r.Context(), row.SongbookID)
			row.SongCount = len(songs)
		}
		for _, l := range lessons {
			for _, st := range l.Students {
				if st.PersonID == m.PersonID && row.NextLesson == nil {
					ev := l.Event
					row.NextLesson = &ev
				}
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	data := s.coachingPageData(r, community, coach, "students")
	data["Students"] = rows
	s.render(w, "coaching_students", data)
}

func (s *Server) coachingPageData(r *http.Request, community *store.Community, coach bool, tab string) map[string]any {
	return map[string]any{
		"Person":    personFromContext(r),
		"Community": community,
		"IsStaff":   coach,
		"IsCoach":   coach,
		"Tab":       tab,
	}
}

// handleCoachingStudentPage is GET /communities/{id}/students/{personID}: a
// student's voice, songs (in their key) and lessons — the coach's to
// change, the student's to read.
func (s *Server) handleCoachingStudentPage(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	studentID, err := pathID(r, "personID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	person := personFromContext(r)
	if !coach && studentID != person.ID {
		http.NotFound(w, r)
		return
	}
	member, ok := s.currentStudent(r, community.ID, studentID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	rs, err := s.store.StudentRanges(r.Context(), community.ID, studentID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	notes, err := s.store.StudentNotes(r.Context(), community.ID, studentID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sbID, err := s.store.FindStudentSongbook(r.Context(), community.ID, studentID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var songs []store.StudentSong
	if sbID != 0 {
		if songs, err = s.store.ListStudentSongs(r.Context(), sbID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	lessons, err := s.store.ListCoachingLessons(r.Context(), community.ID, time.Now().Add(-24*time.Hour), studentID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data := s.coachingPageData(r, community, coach, "students")
	if !coach {
		data["Tab"] = "voice"
	}
	data["Student"] = member
	data["Ranges"] = rangeViews(rs)
	data["Notes"] = notes
	data["SongbookID"] = sbID
	data["Songs"] = songs
	data["Lessons"] = lessons
	data["NoteOptions"] = voiceNotes()
	if coach {
		// Songs to add: the community's other songbooks', not already there.
		have := map[int64]bool{}
		for _, s := range songs {
			have[s.VariantID] = true
		}
		var pick []store.StudentSong
		sbs, err := s.store.ListSongbooks(r.Context(), "community", community.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, sb := range sbs {
			if sb.IsStudent() {
				continue
			}
			entries, err := s.store.ListSongbookEntries(r.Context(), sb.ID)
			if err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			for _, e := range entries {
				if !have[e.VariantID] {
					have[e.VariantID] = true
					pick = append(pick, store.StudentSong{VariantID: e.VariantID, Title: e.Title, Artist: e.Artist})
				}
			}
		}
		sort.Slice(pick, func(i, j int) bool { return pick[i].Title < pick[j].Title })
		data["AddSongs"] = pick
	}
	s.render(w, "coaching_student", data)
}

// currentStudent is a current member of the community who isn't staff.
func (s *Server) currentStudent(r *http.Request, communityID, personID int64) (*store.CommunityMember, bool) {
	members, err := s.store.ListCommunityMembers(r.Context(), communityID, false)
	if err != nil {
		return nil, false
	}
	for _, m := range members {
		if m.PersonID == personID && !m.IsStaff() {
			return &m, true
		}
	}
	return nil, false
}

// coachRequest checks a coaching API call: the community is a coaching
// one, the viewer its coach, and {personID} one of its students.
func (s *Server) coachRequest(w http.ResponseWriter, r *http.Request) (*store.Community, *store.CommunityMember, bool) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return nil, nil, false
	}
	if !coach {
		httpError(w, http.StatusForbidden, "only the coach can do that")
		return nil, nil, false
	}
	studentID, err := pathID(r, "personID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid student")
		return nil, nil, false
	}
	member, ok := s.currentStudent(r, community.ID, studentID)
	if !ok {
		httpError(w, http.StatusNotFound, "no such student")
		return nil, nil, false
	}
	return community, member, true
}

// handlePutStudentRange is PUT /api/communities/{id}/students/{personID}/ranges/{register}.
func (s *Server) handlePutStudentRange(w http.ResponseWriter, r *http.Request) {
	community, student, ok := s.coachRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		Low  *int   `json:"low"`
		High *int   `json:"high"`
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	rg := store.VoiceRange{Register: r.PathValue("register"), Low: req.Low, High: req.High, Note: req.Note}
	if err := s.store.SetStudentRange(r.Context(), community.ID, student.PersonID, rg, personFromContext(r).ID); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePutStudentNotes is PUT /api/communities/{id}/students/{personID}/notes.
func (s *Server) handlePutStudentNotes(w http.ResponseWriter, r *http.Request) {
	community, student, ok := s.coachRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		Notes string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetStudentNotes(r.Context(), community.ID, student.PersonID, req.Notes, personFromContext(r).ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePostStudentSong is POST /api/communities/{id}/students/{personID}/songs:
// a song of the community in the student's songbook (made the first time).
func (s *Server) handlePostStudentSong(w http.ResponseWriter, r *http.Request) {
	community, student, ok := s.coachRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		VariantID int64 `json:"variantId"`
		Semitones int   `json:"semitones"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	actor := personFromContext(r).ID
	sbID, err := s.store.StudentSongbook(r.Context(), community.ID, student.PersonID, student.DisplayName+"’s songs", actor)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.AddToSongbook(r.Context(), sbID, req.VariantID, actor); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Semitones != 0 {
		if err := s.store.SetEntrySemitones(r.Context(), sbID, req.VariantID, req.Semitones); err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]int64{"songbookId": sbID})
}

// studentSongbook is the student's songbook of a coaching API call.
func (s *Server) studentSongbook(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	community, student, ok := s.coachRequest(w, r)
	if !ok {
		return 0, 0, false
	}
	sbID, err := s.store.FindStudentSongbook(r.Context(), community.ID, student.PersonID)
	if err != nil || sbID == 0 {
		httpError(w, http.StatusNotFound, "no songbook for this student yet")
		return 0, 0, false
	}
	variantID, err := pathID(r, "variantID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid song")
		return 0, 0, false
	}
	return sbID, variantID, true
}

// handlePutStudentSongKey is PUT /api/communities/{id}/students/{personID}/songs/{variantID}/key.
func (s *Server) handlePutStudentSongKey(w http.ResponseWriter, r *http.Request) {
	sbID, variantID, ok := s.studentSongbook(w, r)
	if !ok {
		return
	}
	var req struct {
		Semitones int `json:"semitones"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetEntrySemitones(r.Context(), sbID, variantID, req.Semitones); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, "not in the student's songbook")
			return
		}
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"semitones": req.Semitones})
}

// handleDeleteStudentSong is DELETE /api/communities/{id}/students/{personID}/songs/{variantID}.
func (s *Server) handleDeleteStudentSong(w http.ResponseWriter, r *http.Request) {
	sbID, variantID, ok := s.studentSongbook(w, r)
	if !ok {
		return
	}
	if err := s.store.RemoveFromSongbook(r.Context(), sbID, variantID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Lessons -----------------------------------------------------------------

type lessonDay struct {
	Day     time.Time
	Lessons []store.CoachingLesson
}

// handleCoachingLessonsPage is GET /communities/{id}/lessons: the coach's
// calendar of lessons and their students; a student's own lessons.
func (s *Server) handleCoachingLessonsPage(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	person := personFromContext(r)
	only := person.ID
	if coach {
		only = 0
	}
	lessons, err := s.store.ListCoachingLessons(r.Context(), community.ID, time.Now().Add(-6*time.Hour), only)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var days []lessonDay
	for _, l := range lessons {
		d := l.Event.StartsAt.In(time.Local)
		day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.Local)
		if len(days) == 0 || !days[len(days)-1].Day.Equal(day) {
			days = append(days, lessonDay{Day: day})
		}
		days[len(days)-1].Lessons = append(days[len(days)-1].Lessons, l)
	}
	data := s.coachingPageData(r, community, coach, "lessons")
	data["Days"] = days
	if coach {
		members, err := s.store.ListCommunityMembers(r.Context(), community.ID, false)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var students []store.CommunityMember
		for _, m := range members {
			if !m.IsStaff() {
				students = append(students, m)
			}
		}
		data["Students"] = students
		data["DefaultStart"] = nextHour().Format("2006-01-02T15:04")
	}
	s.render(w, "coaching_lessons", data)
}

func nextHour() time.Time {
	t := time.Now().In(time.Local).Add(24 * time.Hour)
	return time.Date(t.Year(), t.Month(), t.Day(), 18, 0, 0, 0, time.Local)
}

type coachingLessonRequest struct {
	StartsAt string  `json:"startsAt"` // 2006-01-02T15:04, local time
	Minutes  int     `json:"minutes"`
	Location string  `json:"location"`
	Name     string  `json:"name"`
	Students []int64 `json:"students"`
}

// handlePostCoachingLesson is POST /api/communities/{id}/lessons: a lesson
// on the calendar, its students registered by the coach.
func (s *Server) handlePostCoachingLesson(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	if !coach {
		httpError(w, http.StatusForbidden, "only the coach can do that")
		return
	}
	var req coachingLessonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	start, err := time.ParseInLocation("2006-01-02T15:04", req.StartsAt, time.Local)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid start time")
		return
	}
	if req.Minutes <= 0 || req.Minutes > 480 {
		req.Minutes = 60
	}
	end := start.Add(time.Duration(req.Minutes) * time.Minute)
	name := req.Name
	if name == "" {
		name = "Lesson"
	}
	actor := personFromContext(r).ID
	id, err := s.store.CreateEvent(r.Context(), "community", community.ID, "lesson", name, "", req.Location, start, &end, "members", actor)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.setLessonStudents(r, community.ID, id, req.Students); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// setLessonStudents makes students the lesson's registrations: only
// current students of the community.
func (s *Server) setLessonStudents(r *http.Request, communityID, eventID int64, students []int64) error {
	want := map[int64]bool{}
	for _, id := range students {
		if _, ok := s.currentStudent(r, communityID, id); !ok {
			return fmt.Errorf("person %d isn't a student here", id)
		}
		want[id] = true
	}
	regs, err := s.store.ListEventRegistrations(r.Context(), eventID)
	if err != nil {
		return err
	}
	for _, reg := range regs {
		if !want[reg.PersonID] {
			if err := s.store.UnregisterFromEvent(r.Context(), eventID, reg.PersonID); err != nil {
				return err
			}
		}
		delete(want, reg.PersonID)
	}
	for id := range want {
		if err := s.store.RegisterForEvent(r.Context(), eventID, id, 0); err != nil {
			return err
		}
	}
	return nil
}

// coachLesson checks a lesson API call: the coach, a lesson of this community.
func (s *Server) coachLesson(w http.ResponseWriter, r *http.Request) (*store.Community, int64, bool) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return nil, 0, false
	}
	if !coach {
		httpError(w, http.StatusForbidden, "only the coach can do that")
		return nil, 0, false
	}
	eventID, err := pathID(r, "eventID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid lesson")
		return nil, 0, false
	}
	ev, err := s.store.GetEvent(r.Context(), eventID)
	if err != nil || ev.OwnerKind != "community" || ev.OwnerCommunityID != community.ID || ev.EventType != "lesson" {
		httpError(w, http.StatusNotFound, "no such lesson")
		return nil, 0, false
	}
	return community, eventID, true
}

// handlePutCoachingLessonStudents is PUT /api/communities/{id}/lessons/{eventID}/students.
func (s *Server) handlePutCoachingLessonStudents(w http.ResponseWriter, r *http.Request) {
	community, eventID, ok := s.coachLesson(w, r)
	if !ok {
		return
	}
	var req struct {
		Students []int64 `json:"students"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.setLessonStudents(r, community.ID, eventID, req.Students); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteCoachingLesson is DELETE /api/communities/{id}/lessons/{eventID}.
func (s *Server) handleDeleteCoachingLesson(w http.ResponseWriter, r *http.Request) {
	_, eventID, ok := s.coachLesson(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteEvent(r.Context(), eventID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSetCommunityKind is POST /api/communities/{id}/kind: a coaching
// community, or a plain one.
func (s *Server) handleSetCommunityKind(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetCommunityKind(r.Context(), communityID, req.Kind); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"kind": req.Kind})
}
