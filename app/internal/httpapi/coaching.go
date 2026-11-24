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
	// A pop coach's words too: the chest voice where the belt is, the
	// head voice where the falsetto is.
	labels := map[string]string{"chest": "Chest / belt", "mixed": "voice|Mix", "head": "Head / falsetto"}
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

// coachingContext loads a coaching community and says whether its pages
// are the coach's for this viewer (coachingViewerOf: the staff; a site
// admin unless they're a student there; an admin's "See as…"); nil and
// an error page when it isn't a coaching community. A change asked for
// during a student's or a member's preview is refused: the admin would
// be acting for someone who didn't.
func (s *Server) coachingContext(w http.ResponseWriter, r *http.Request) (*store.Community, bool, bool) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	community, err := s.store.GetCommunity(r.Context(), communityID)
	if err != nil || !community.IsCoaching() {
		http.NotFound(w, r)
		return nil, false, false
	}
	v, err := s.coachingViewerOf(r, communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return nil, false, false
	}
	if v.ReadOnly && r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpError(w, http.StatusConflict, i18n.T(s.langOfRequest(r), "This is a preview: leave it to make a change."))
		return nil, false, false
	}
	return community, v.Coach, true
}

// viewedPersonID is whose student pages a coaching community's page shows:
// the viewer's own, or the student a site admin looks as (0 for a member
// with nothing yet).
func (s *Server) viewedPersonID(r *http.Request, communityID int64) int64 {
	v, err := s.coachingViewerOf(r, communityID)
	if err != nil {
		return personFromContext(r).ID
	}
	if v.Coach {
		return personFromContext(r).ID
	}
	return v.PersonID
}

type studentRow struct {
	PersonID   int64
	Name       string
	Ranges     []rangeView
	SongbookID int64
	SongCount  int
	NextLesson *store.Event
	// The table view's: the whole range (lowest to highest noted), the
	// keys of their songs (how many moved, down and up, the furthest
	// each way), their lessons so far by kind, the last one and the
	// last note, and their parts in the events' programs to come.
	LowName, HighName string
	LowMidi, HighMidi int
	KeysDown, KeysUp  int
	KeyMin, KeyMax    int
	Lessons           int
	LessonsAlone      int
	LessonsGroup      int
	LessonsOnline     int
	LessonsInRoom     int
	LessonsToCome     int
	MoreToCome        int // after the next one
	LastLesson        *store.Event
	LastNote          *time.Time
	Leads, Backings   int
	Practice          store.PracticeSummary // the last seven days
}

// handleCoachingStudentsPage is GET /communities/{id}/students: the coach's
// students; a student goes to their own page.
func (s *Server) handleCoachingStudentsPage(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	if !coach {
		if id := s.viewedPersonID(r, community.ID); id != 0 {
			http.Redirect(w, r, fmt.Sprintf("/communities/%d/students/%d", community.ID, id), http.StatusFound)
		} else {
			http.Redirect(w, r, fmt.Sprintf("/communities/%d/songbooks", community.ID), http.StatusFound)
		}
		return
	}
	members, err := s.store.ListCommunityMembers(r.Context(), community.ID, false)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	practice, err := s.store.CommunityPracticeSummaries(r.Context(), community.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	lessons, err := s.store.ListCoachingLessons(r.Context(), community.ID, time.Time{}, 0)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	parts, err := s.store.ProgramParts(r.Context(), community.ID, now)
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
		row := studentRow{PersonID: m.PersonID, Name: m.DisplayName}
		rs, err := s.store.StudentRanges(r.Context(), community.ID, m.PersonID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		row.Ranges = rangeViews(rs, lang)
		for _, rv := range row.Ranges {
			if rv.LowMidi != 0 && (row.LowMidi == 0 || rv.LowMidi < row.LowMidi) {
				row.LowMidi, row.LowName = rv.LowMidi, rv.LowName
			}
			if rv.HighMidi > row.HighMidi {
				row.HighMidi, row.HighName = rv.HighMidi, rv.HighName
			}
		}
		if row.SongbookID, err = s.store.FindStudentSongbook(r.Context(), community.ID, m.PersonID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if row.SongbookID != 0 {
			songs, _ := s.store.ListStudentSongs(r.Context(), row.SongbookID)
			row.SongCount = len(songs)
			for _, sg := range songs {
				switch {
				case sg.Semitones < 0:
					row.KeysDown++
				case sg.Semitones > 0:
					row.KeysUp++
				}
				row.KeyMin, row.KeyMax = min(row.KeyMin, sg.Semitones), max(row.KeyMax, sg.Semitones)
			}
		}
		for _, l := range lessons {
			mine := false
			for _, st := range l.Students {
				mine = mine || st.PersonID == m.PersonID
			}
			if !mine {
				continue
			}
			ev := l.Event
			if !ev.IsPast(now) {
				row.LessonsToCome++
				if row.NextLesson == nil {
					row.NextLesson = &ev
				}
				continue
			}
			row.Lessons++
			row.LastLesson = &ev
			if ev.CoachingGroupID != nil {
				row.LessonsGroup++
			} else {
				row.LessonsAlone++
			}
			if ev.LessonMode == "online" {
				row.LessonsOnline++
			} else {
				row.LessonsInRoom++
			}
		}
		if notes, err := s.store.ListTeachingNotes(r.Context(), community.ID, m.PersonID); err == nil && len(notes) > 0 {
			at := notes[0].CreatedAt
			for _, n := range notes {
				if n.CreatedAt.After(at) {
					at = n.CreatedAt
				}
			}
			row.LastNote = &at
		}
		row.MoreToCome = max(0, row.LessonsToCome-1)
		p := parts[m.PersonID]
		row.Leads, row.Backings = p[0], p[1]
		row.Practice = practice[m.PersonID]
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return store.FoldName(rows[i].Name) < store.FoldName(rows[j].Name) })
	data := s.coachingPageData(r, community, coach, "students")
	data["Students"] = rows
	// Cards or the table, the Students section's first two views.
	data["StudentsView"] = ""
	if v := r.URL.Query().Get("view"); v == "cards" || v == "table" {
		data["StudentsView"] = v
	}
	// The open link that makes students (role member, no names): the
	// newest one still open, if the coach made one.
	invites, err := s.store.ListGroupInvitesForTarget(r.Context(), "community", community.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, gi := range invites {
		if gi.ClosedAt == nil && len(gi.Slots) == 0 && (gi.DefaultStatus == "member" || gi.DefaultStatus == "") {
			data["StudentLink"] = "/join/" + gi.Token
			break
		}
	}
	s.render(w, "coaching_students", data)
}

func (s *Server) coachingPageData(r *http.Request, community *store.Community, coach bool, tab string) map[string]any {
	// Me: the sidebar's account chip and Admin, as on every other page.
	return map[string]any{
		"Me":        personFromContext(r),
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
	if !coach && studentID != s.viewedPersonID(r, community.ID) {
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
		sbID, err = s.store.StudentSongbook(ctx, community.ID, studentID, i18n.T(s.langOfRequest(r), "%s’s songs", member.DisplayName), person.ID)
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
	if err := s.voiceData(r, community.ID, studentID, coach, data["Ranges"].([]rangeView), data); err != nil {
		fail(err)
		return
	}
	data["SongbookID"] = sbID
	data["Songs"] = s.withKeys(r.Context(), songs)
	data["Groups"] = inGroups
	data["Lessons"] = lessons
	data["NoteOptions"] = voiceNotes(s.langOfRequest(r))
	data["Now"] = time.Now()
	if err := s.checkLevelsData(r, community.ID, data); err != nil {
		fail(err)
		return
	}
	if data["Exercises"], err = s.store.ListExercises(r.Context(), community.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if data["Assignments"], err = s.store.ListStudentAssignments(ctx, community.ID, studentID, false); err != nil {
		fail(err)
		return
	}
	if data["PracticeLogs"], err = s.store.ListPracticeLogs(ctx, community.ID, studentID, time.Now().AddDate(0, 0, -28)); err != nil {
		fail(err)
		return
	}
	// The notes the student reads (the coach sees them all, below); the
	// student opening their own page has read what told them so.
	data["CoachFirstName"] = firstWord(s.store.CoachName(ctx, community.ID))
	if !coach {
		if data["SharedNotes"], err = s.store.ListSharedTeachingNotes(ctx, community.ID, studentID); err != nil {
			fail(err)
			return
		}
		if studentID == person.ID {
			_ = s.store.MarkNotificationsReadByLink(ctx, "teaching_note_shared", sharedNotesLink(community.ID, studentID))
		}
	}
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
	// The community's songbook links public songs and its own
	// (store.LinkCheck).
	if !s.writeLinkErr(w, r, s.store.LinkCheck(r.Context(), personFromContext(r).ID, req.VariantID, sb)) {
		return
	}
	req.VariantID = s.coachingSongFor(r.Context(), sb, req.VariantID)
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
		Body      string `json:"body"`
		EventID   int64  `json:"eventId"`
		VariantID int64  `json:"variantId"`
		// Shared: the student reads it; else it's private, the coach's
		// and the staff's only.
		Shared bool `json:"shared"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Body) == "" {
		httpError(w, http.StatusBadRequest, "a note needs a text")
		return
	}
	ctx := r.Context()
	var eventID, variantID *int64
	if req.EventID != 0 {
		ev, err := s.store.GetEvent(ctx, req.EventID)
		if err != nil || ev.OwnerKind != "community" || ev.OwnerCommunityID != community.ID {
			httpError(w, http.StatusBadRequest, "not a lesson of this community")
			return
		}
		eventID = &req.EventID
	}
	if req.VariantID != 0 {
		if ok, err := s.store.CommunityHasSong(ctx, community.ID, req.VariantID); err != nil || !ok {
			httpError(w, http.StatusBadRequest, "not a song of this community")
			return
		}
		variantID = &req.VariantID
	}
	me := personFromContext(r)
	id, err := s.store.AddStudentNote(ctx, community.ID, student.PersonID, eventID, variantID, strings.TrimSpace(req.Body), req.Shared, me.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Shared {
		if err := s.notifySharedNote(r, community, student.PersonID, me); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "shared": req.Shared})
}

// handlePostShareTeachingNote is POST
// /api/communities/{id}/students/{personID}/teaching-notes/{noteID}/share:
// a private note shared with the student, who is told. One way only: a
// shared note can be rewritten or deleted, never made private again (the
// student may have read it).
func (s *Server) handlePostShareTeachingNote(w http.ResponseWriter, r *http.Request) {
	community, student, ok := s.coachRequest(w, r)
	if !ok {
		return
	}
	noteID, err := pathID(r, "noteID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid note")
		return
	}
	if err := s.store.ShareTeachingNote(r.Context(), community.ID, student.PersonID, noteID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, "no such private note")
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.notifySharedNote(r, community, student.PersonID, personFromContext(r)); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"shared": true})
}

// sharedNotesLink is where a student reads their shared notes.
func sharedNotesLink(communityID, studentID int64) string {
	return fmt.Sprintf("/communities/%d/students/%d#notes", communityID, studentID)
}

// notifySharedNote tells the student the coach shared a note with them,
// in their language ("New note from Chloé").
func (s *Server) notifySharedNote(r *http.Request, community *store.Community, studentID int64, author *store.Person) error {
	lang := s.recipientLang(r, studentID, community.ID)
	body := i18n.T(lang, "New note from %s", i18n.Of(lang, firstWord(author.DisplayName)))
	_, err := s.store.CreateNotification(r.Context(), studentID, "teaching_note_shared", body, sharedNotesLink(community.ID, studentID))
	return err
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
	only := s.viewedPersonID(r, community.ID)
	if coach {
		only = 0
	} else if only == 0 {
		only = -1 // a member with nothing yet: no lessons
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
	data["Now"] = time.Now()
	for _, l := range lessons {
		if l.Event.LessonMode == "online" && !l.Event.IsPast(time.Now()) {
			data["HasOnline"] = true
		}
	}
	if data["HasOnline"] != nil {
		if data["Headphones"], err = s.store.LiveHeadphones(r.Context(), person.ID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
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

// handlePutCoachingLessonMode is PUT /api/communities/{id}/lessons/{eventID}/mode
// {"mode": "in_person" | "online"}: a lesson moved online, or back in
// the room.
func (s *Server) handlePutCoachingLessonMode(w http.ResponseWriter, r *http.Request) {
	_, eventID, ok := s.coachLesson(w, r)
	if !ok {
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetLessonMode(r.Context(), eventID, req.Mode); err != nil {
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
