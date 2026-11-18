package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"app/internal/i18n"
	"app/internal/store"
)

// A coaching community's pages (docs/coaching.md): its students (each one's
// voice, songs in their key, lessons) and its lessons.

type noteOption struct {
	Midi int
	Name string
}

// voiceNotes are the notes a range is picked from: C2 to C7, named in
// lang (a singer's way: do3 for middle C in French, i18n.NoteName).
func voiceNotes(lang string) []noteOption {
	var out []noteOption
	for m := 36; m <= 96; m++ {
		out = append(out, noteOption{m, i18n.NoteName(lang, m)})
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

// rangeViews are a student's ranges to show, the notes named in lang;
// the labels stay English, translated where printed ({{t .Label}}).
func rangeViews(rs []store.VoiceRange, lang string) []rangeView {
	labels := map[string]string{"chest": "Chest voice", "mixed": "Mixed voice", "head": "Head voice"}
	out := make([]rangeView, len(rs))
	for i, r := range rs {
		out[i] = rangeView{VoiceRange: r, Label: labels[r.Register]}
		if r.Low != nil {
			out[i].LowName, out[i].LowMidi = i18n.NoteName(lang, *r.Low), *r.Low
		}
		if r.High != nil {
			out[i].HighName, out[i].HighMidi = i18n.NoteName(lang, *r.High), *r.High
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
	lang := s.langOfRequest(r)
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
		row.Ranges = rangeViews(rs, lang)
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
// student's voice, teaching notes, songs (in their key), groups and
// lessons — the coach's to change, the student's to read.
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
	ctx := r.Context()
	fail := func(err error) { httpError(w, http.StatusInternalServerError, err.Error()) }
	rs, err := s.store.StudentRanges(ctx, community.ID, studentID)
	if err != nil {
		fail(err)
		return
	}
	// The coach's songbook for the student, there from the first visit.
	var sbID int64
	if coach {
		sbID, err = s.store.StudentSongbook(ctx, community.ID, studentID, member.DisplayName+"’s songs", person.ID)
	} else {
		sbID, err = s.store.FindStudentSongbook(ctx, community.ID, studentID)
	}
	if err != nil {
		fail(err)
		return
	}
	var songs []store.StudentSong
	if sbID != 0 {
		if songs, err = s.store.ListStudentSongs(ctx, sbID); err != nil {
			fail(err)
			return
		}
	}
	groups, err := s.store.ListCoachingGroups(ctx, community.ID, studentID)
	if err != nil {
		fail(err)
		return
	}
	var inGroups []groupSongs
	for _, g := range groups {
		gs, err := s.store.ListStudentSongs(ctx, g.SongbookID)
		if err != nil {
			fail(err)
			return
		}
		inGroups = append(inGroups, groupSongs{Group: g, Songs: gs})
	}
	lessons, err := s.store.ListCoachingLessons(ctx, community.ID, time.Now().Add(-24*time.Hour), studentID)
	if err != nil {
		fail(err)
		return
	}
	data := s.coachingPageData(r, community, coach, "students")
	if !coach {
		data["Tab"] = "voice"
	}
	data["Student"] = member
	data["Ranges"] = rangeViews(rs, s.langOfRequest(r))
	data["SongbookID"] = sbID
	data["Songs"] = s.withKeys(r.Context(), songs)
	data["Groups"] = inGroups
	data["Lessons"] = lessons
	data["NoteOptions"] = voiceNotes(s.langOfRequest(r))
	if coach {
		if data["Notes"], err = s.store.StudentNotes(ctx, community.ID, studentID); err != nil {
			fail(err)
			return
		}
		if data["TeachingNotes"], err = s.store.ListTeachingNotes(ctx, community.ID, studentID); err != nil {
			fail(err)
			return
		}
		past, err := s.store.ListCoachingLessons(ctx, community.ID, time.Now().AddDate(0, -12, 0), studentID)
		if err != nil {
			fail(err)
			return
		}
		data["NoteLessons"] = past
		if data["AddSongs"], err = s.songsToAdd(r, community.ID, songs); err != nil {
			fail(err)
			return
		}
	}
	s.render(w, "coaching_student", data)
}

// groupSongs is a group and its songs, in its key.
type groupSongs struct {
	Group store.CoachingGroup
	Songs []store.StudentSong
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

// coachSongbook checks a call on a songbook's songs: the coach, and a
// student's or a group's songbook of this community ({sbID}).
func (s *Server) coachSongbook(w http.ResponseWriter, r *http.Request) (*store.Songbook, bool) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return nil, false
	}
	if !coach {
		httpError(w, http.StatusForbidden, "only the coach can do that")
		return nil, false
	}
	sbID, err := pathID(r, "sbID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook")
		return nil, false
	}
	sb, err := s.store.GetSongbook(r.Context(), sbID)
	if err != nil || sb.OwnerCommunityID != community.ID || (sb.Kind != store.SongbookKindStudent && sb.Kind != store.SongbookKindGroup) {
		httpError(w, http.StatusNotFound, "no such songbook")
		return nil, false
	}
	return sb, true
}

// handlePostCoachingSong is POST /api/communities/{id}/songbooks/{sbID}/songs:
// a song of the community for a student or a group, in a key.
func (s *Server) handlePostCoachingSong(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.coachSongbook(w, r)
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
	if err := s.store.AddToSongbook(r.Context(), sb.ID, req.VariantID, personFromContext(r).ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Semitones != 0 {
		if err := s.store.SetEntrySemitones(r.Context(), sb.ID, req.VariantID, req.Semitones); err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePutCoachingSongKey is PUT /api/communities/{id}/songbooks/{sbID}/songs/{variantID}/key.
func (s *Server) handlePutCoachingSongKey(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.coachSongbook(w, r)
	if !ok {
		return
	}
	variantID, err := pathID(r, "variantID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid song")
		return
	}
	var req struct {
		Semitones int `json:"semitones"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetEntrySemitones(r.Context(), sb.ID, variantID, req.Semitones); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, "not in that songbook")
			return
		}
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"semitones": req.Semitones})
}

// handleDeleteCoachingSong is DELETE /api/communities/{id}/songbooks/{sbID}/songs/{variantID}.
func (s *Server) handleDeleteCoachingSong(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.coachSongbook(w, r)
	if !ok {
		return
	}
	variantID, err := pathID(r, "variantID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid song")
		return
	}
	if err := s.store.RemoveFromSongbook(r.Context(), sb.ID, variantID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// songsToAdd is the community's songs (its songbooks other than students'
// and groups'), less those already in a songbook.
func (s *Server) songsToAdd(r *http.Request, communityID int64, have []store.StudentSong) ([]store.StudentSong, error) {
	in := map[int64]bool{}
	for _, s := range have {
		in[s.VariantID] = true
	}
	var pick []store.StudentSong
	sbs, err := s.store.ListSongbooks(r.Context(), "community", communityID)
	if err != nil {
		return nil, err
	}
	for _, sb := range sbs {
		if sb.IsStudent() || sb.Kind == store.SongbookKindGroup {
			continue
		}
		entries, err := s.store.ListSongbookEntries(r.Context(), sb.ID)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !in[e.VariantID] {
				in[e.VariantID] = true
				pick = append(pick, store.StudentSong{VariantID: e.VariantID, Title: e.Title, Artist: e.Artist})
			}
		}
	}
	sort.Slice(pick, func(i, j int) bool { return pick[i].Title < pick[j].Title })
	return pick, nil
}

// Teaching notes -----------------------------------------------------------

// handlePostTeachingNote is POST /api/communities/{id}/students/{personID}/teaching-notes.
func (s *Server) handlePostTeachingNote(w http.ResponseWriter, r *http.Request) {
	community, student, ok := s.coachRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		Body    string `json:"body"`
		EventID int64  `json:"eventId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
		httpError(w, http.StatusBadRequest, "a note needs a text")
		return
	}
	var eventID *int64
	if req.EventID != 0 {
		eventID = &req.EventID
	}
	id, err := s.store.AddTeachingNote(r.Context(), community.ID, student.PersonID, eventID, req.Body, personFromContext(r).ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// handlePutTeachingNote is PUT /api/communities/{id}/students/{personID}/teaching-notes/{noteID}.
func (s *Server) handlePutTeachingNote(w http.ResponseWriter, r *http.Request) {
	community, student, ok := s.coachRequest(w, r)
	if !ok {
		return
	}
	noteID, err := pathID(r, "noteID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid note")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Body == "" {
		err = s.store.DeleteTeachingNote(r.Context(), community.ID, student.PersonID, noteID)
	} else {
		err = s.store.UpdateTeachingNote(r.Context(), community.ID, student.PersonID, noteID, req.Body)
	}
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
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
		if data["Groups"], err = s.store.ListCoachingGroups(r.Context(), community.ID, 0); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
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
	Mode     string  `json:"mode"`    // "in_person" or "online"
	GroupID  int64   `json:"groupId"` // a group's lesson: its members come
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
	var group *store.CoachingGroup
	if req.GroupID != 0 {
		g, err := s.store.GetCoachingGroup(r.Context(), req.GroupID)
		if err != nil || g.CommunityID != community.ID {
			httpError(w, http.StatusBadRequest, "no such group")
			return
		}
		group = g
		for _, m := range g.Members {
			req.Students = append(req.Students, m.PersonID)
		}
		if name == "" {
			name = g.Name
		}
	}
	if name == "" {
		name = i18n.T(s.langOfRequest(r), "Lesson")
	}
	if req.Mode == "" {
		req.Mode = "in_person"
	}
	actor := personFromContext(r).ID
	id, err := s.store.CreateEvent(r.Context(), "community", community.ID, "lesson", name, "", req.Location, start, &end, "members", actor)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var groupID *int64
	if group != nil {
		groupID = &group.ID
	}
	if err := s.store.SetLessonDetails(r.Context(), id, req.Mode, groupID); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	seen := map[int64]bool{}
	var students []int64
	for _, p := range req.Students {
		if !seen[p] {
			seen[p] = true
			students = append(students, p)
		}
	}
	if err := s.setLessonStudents(r, community.ID, id, students); err != nil {
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
