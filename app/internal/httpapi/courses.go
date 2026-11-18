package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"app/internal/store"
)

// A course is a songbook whose songs are in lessons (store/lessons.go,
// docs/courses.md). Its page lists the lessons, with where the viewer is
// in each; a lesson's page has its text, its songs and its files. Those
// who may change the songbook (CanEditSongbook) organize the lessons.

// courseLesson is a lesson as the course page lists it.
type courseLesson struct {
	store.Lesson
	Href     string
	Progress store.LessonProgress
	Files    int
}

// lessonFile is a file of a lesson, with where it's downloaded from.
type lessonFile struct {
	store.SongbookFile
	Href string
}

func lessonHref(sbID, lessonID int64) string {
	return fmt.Sprintf("/songbooks/%d/lessons/%d", sbID, lessonID)
}

// lessonVideo is a lesson's YouTube video id: its own video, else the
// first its songs link to; "" when there's none.
func (s *Server) lessonVideo(l store.Lesson) string {
	if id := youtubeID(l.VideoURL); id != "" {
		return id
	}
	for _, song := range l.Songs {
		if sg, _, err := s.store.LoadVariant(song.VariantID); err == nil {
			if id := youtubeID(sg.Get("reference")); id != "" {
				return id
			}
		}
	}
	return ""
}

// courseFiles is a course's files a viewer may download, by lesson; none
// outside a community, or for a role that doesn't download.
func (s *Server) courseFiles(r *http.Request, sb *store.Songbook) (map[int64][]lessonFile, error) {
	out := map[int64][]lessonFile{}
	if sb.OwnerKind != "community" {
		return out, nil
	}
	if ok, err := s.canDownload(r, sb.OwnerCommunityID); err != nil || !ok {
		return out, err
	}
	files, err := s.store.ListSongbookFiles(r.Context(), []int64{sb.ID})
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if f.LessonID == nil {
			continue
		}
		href := fmt.Sprintf("/communities/%d/downloads/%d/%s", sb.OwnerCommunityID, f.ID, url.PathEscape(f.Filename))
		out[*f.LessonID] = append(out[*f.LessonID], lessonFile{SongbookFile: f, Href: href})
	}
	// In the lesson's song order, each song's score then its audio; the
	// files with no song after.
	lessons, _, err := s.store.ListLessons(r.Context(), sb.ID)
	if err != nil {
		return nil, err
	}
	at := map[int64]int{}
	for _, l := range lessons {
		for i, song := range l.Songs {
			at[song.VariantID] = i
		}
	}
	place := func(f lessonFile) int {
		if f.VariantID == nil {
			return len(at)
		}
		return at[*f.VariantID]
	}
	for _, fs := range out {
		sort.SliceStable(fs, func(i, j int) bool {
			if a, b := place(fs[i]), place(fs[j]); a != b {
				return a < b
			}
			return kindOrder[fs[i].Kind] < kindOrder[fs[j].Kind]
		})
	}
	return out, nil
}

// courseBase is what every course page shows: the songbook, its community
// (when it has one: back link and look), and whether the viewer may
// organize it.
func (s *Server) courseBase(r *http.Request, sb *store.Songbook) (map[string]any, error) {
	person := personFromContext(r)
	canEdit, err := s.store.CanEditSongbook(r.Context(), person.ID, sb.ID)
	if err != nil {
		return nil, err
	}
	canEdit = canEdit || person.IsAdmin
	// Who may edit sees the course as a student does until they turn
	// editing on.
	data := map[string]any{"Me": person, "Songbook": sb, "CanEdit": canEdit, "Editing": canEdit && editMode(r)}
	if sb.OwnerKind == "community" {
		c, err := s.store.GetCommunity(r.Context(), sb.OwnerCommunityID)
		if err != nil {
			return nil, err
		}
		data["Community"] = c
	}
	return data, nil
}

// handleCoursePage is a course's page (/songbooks/{id} for a course):
// its lessons, where the viewer is in each, and the next one to do.
func (s *Server) handleCoursePage(w http.ResponseWriter, r *http.Request, sb *store.Songbook) {
	person := personFromContext(r)
	data, err := s.courseBase(r, sb)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	lessons, loose, err := s.store.ListLessons(r.Context(), sb.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	progress, err := s.store.CourseProgress(r.Context(), sb.ID, person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	files, err := s.courseFiles(r, sb)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var rows []courseLesson
	var next *courseLesson
	done := 0
	for _, l := range lessons {
		row := courseLesson{Lesson: l, Href: lessonHref(sb.ID, l.ID), Progress: progress[l.ID], Files: len(files[l.ID])}
		if row.Progress.Done {
			done++
		}
		rows = append(rows, row)
	}
	// Next: the first lesson not done after the last one opened, else the
	// first not done.
	last := -1
	for i, l := range rows {
		if l.Progress.Last != nil && (last < 0 || l.Progress.Last.After(*rows[last].Progress.Last)) {
			last = i
		}
	}
	for _, from := range []int{last, 0} {
		for i := max(from, 0); i < len(rows) && next == nil; i++ {
			if !rows[i].Progress.Done {
				next = &rows[i]
			}
		}
	}
	data["Lessons"], data["Loose"], data["Next"] = rows, loose, next
	data["Done"], data["Total"] = done, len(rows)
	if len(rows) > 0 {
		data["Percent"] = done * 100 / len(rows)
	}
	if sb.OwnerKind == "community" {
		data["HasFiles"] = len(files) > 0
	}
	s.render(w, "course", data)
}

// courseLessonFor is the lesson of the request's {lessonID}, in the
// songbook of its {id}, which the viewer can open.
func (s *Server) courseLessonFor(w http.ResponseWriter, r *http.Request) (*store.Songbook, *store.Lesson, bool) {
	sbID, err1 := pathID(r, "id")
	lessonID, err2 := pathID(r, "lessonID")
	if err1 != nil || err2 != nil {
		http.NotFound(w, r)
		return nil, nil, false
	}
	if ok, err := s.canSeeSongbook(r, sbID); err != nil || !ok {
		http.NotFound(w, r)
		return nil, nil, false
	}
	sb, err := s.store.GetSongbook(r.Context(), sbID)
	if err != nil {
		http.NotFound(w, r)
		return nil, nil, false
	}
	l, err := s.store.GetLesson(r.Context(), lessonID)
	if err != nil || l.SongbookID != sbID {
		http.NotFound(w, r)
		return nil, nil, false
	}
	return sb, l, true
}

// handleLessonPage is GET /songbooks/{id}/lessons/{lessonID}: the lesson's
// text, its songs, its files, and the lessons either side.
func (s *Server) handleLessonPage(w http.ResponseWriter, r *http.Request) {
	sb, lesson, ok := s.courseLessonFor(w, r)
	if !ok {
		return
	}
	person := personFromContext(r)
	data, err := s.courseBase(r, sb)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	lessons, loose, err := s.store.ListLessons(r.Context(), sb.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i, l := range lessons {
		if l.ID != lesson.ID {
			continue
		}
		if i > 0 {
			data["Prev"] = courseLesson{Lesson: lessons[i-1], Href: lessonHref(sb.ID, lessons[i-1].ID)}
		}
		if i < len(lessons)-1 {
			data["Next"] = courseLesson{Lesson: lessons[i+1], Href: lessonHref(sb.ID, lessons[i+1].ID)}
		}
	}
	progress, err := s.store.CourseProgress(r.Context(), sb.ID, person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	files, err := s.courseFiles(r, sb)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A lesson with a song is practised on the song's page, which shows
	// the lesson too (lessonPanel): its score, its playback, its audio.
	// Its own page stays for its editors, and on ?view=lesson.
	if len(lesson.Songs) > 0 && data["Editing"] != true && r.URL.Query().Get("view") != "lesson" {
		http.Redirect(w, r, fmt.Sprintf("/song/%d?songbook=%d", lesson.Songs[0].VariantID, sb.ID), http.StatusFound)
		return
	}
	data["Lesson"], data["Progress"], data["Files"] = lesson, progress[lesson.ID], files[lesson.ID]
	data["VideoID"] = s.lessonVideo(*lesson)
	if data["Editing"] == true {
		// Songs that can join this lesson: those in no lesson, then the
		// other lessons'.
		var others []store.LessonSong
		others = append(others, loose...)
		lang := s.langOf(w)
		for _, l := range lessons {
			if l.ID != lesson.ID {
				for _, s := range l.Songs {
					s.Title = fmt.Sprintf("%s (%s)", s.Title, l.Label(lang))
					others = append(others, s)
				}
			}
		}
		data["OtherSongs"] = others
	}
	s.render(w, "lesson", data)
}

// courseEditFor is the songbook of the request's {id}, which the viewer
// may change; it answers the request itself when not.
func (s *Server) courseEditFor(w http.ResponseWriter, r *http.Request) (*store.Songbook, bool) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return nil, false
	}
	p := personFromContext(r)
	ok, err := s.store.CanEditSongbook(r.Context(), p.ID, id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if !ok && !p.IsAdmin {
		httpError(w, http.StatusForbidden, "you can't change this songbook")
		return nil, false
	}
	sb, err := s.store.GetSongbook(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such songbook")
		return nil, false
	}
	return sb, true
}

// lessonOf is the request's {lessonID}, which must be one of sb's.
func (s *Server) lessonOf(w http.ResponseWriter, r *http.Request, sb *store.Songbook) (*store.Lesson, bool) {
	id, err := pathID(r, "lessonID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid lesson id")
		return nil, false
	}
	l, err := s.store.GetLesson(r.Context(), id)
	if err != nil || l.SongbookID != sb.ID {
		httpError(w, http.StatusNotFound, "no such lesson")
		return nil, false
	}
	return l, true
}

// lessonRequest is a lesson as a form sends it.
type lessonRequest struct {
	Number string `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Video  string `json:"video"`
}

func (q lessonRequest) number() (*int, error) {
	n := strings.TrimSpace(q.Number)
	if n == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(n)
	if err != nil || v < 0 {
		return nil, errors.New("a lesson's number is a whole number")
	}
	return &v, nil
}

// handleSetSongbookKind is POST /api/songbooks/{id}/kind: {"kind": "course"}
// or "songbook".
func (s *Server) handleSetSongbookKind(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.courseEditFor(w, r)
	if !ok {
		return
	}
	var req struct {
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetSongbookKind(r.Context(), sb.ID, req.Kind); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"kind": req.Kind})
}

// handleCreateLesson is POST /api/songbooks/{id}/lessons.
func (s *Server) handleCreateLesson(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.courseEditFor(w, r)
	if !ok {
		return
	}
	var req lessonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	n, err := req.number()
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.store.CreateLesson(r.Context(), sb.ID, n, req.Title)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "href": lessonHref(sb.ID, id)})
}

// handleUpdateLesson is PUT /api/songbooks/{id}/lessons/{lessonID}.
func (s *Server) handleUpdateLesson(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.courseEditFor(w, r)
	if !ok {
		return
	}
	l, ok := s.lessonOf(w, r, sb)
	if !ok {
		return
	}
	var req lessonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	n, err := req.number()
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Video != "" && youtubeID(req.Video) == "" {
		httpError(w, http.StatusBadRequest, "the video is a YouTube link")
		return
	}
	if err := s.store.UpdateLesson(r.Context(), l.ID, n, req.Title, req.Body, req.Video); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleDeleteLesson is DELETE /api/songbooks/{id}/lessons/{lessonID}: its
// songs and files stay in the course.
func (s *Server) handleDeleteLesson(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.courseEditFor(w, r)
	if !ok {
		return
	}
	l, ok := s.lessonOf(w, r, sb)
	if !ok {
		return
	}
	if err := s.store.DeleteLesson(r.Context(), l.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// handleLessonsFromFiles is POST /api/songbooks/{id}/lessons/from-files.
func (s *Server) handleLessonsFromFiles(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.courseEditFor(w, r)
	if !ok {
		return
	}
	created, placed, err := s.store.LessonsFromFiles(r.Context(), sb.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	videos, err := s.store.LessonVideosFromBooks(r.Context(), sb.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"created": created, "placed": placed, "videos": videos})
}

// handleSetSongLesson is PUT /api/songbooks/{id}/entries/{variantID}/lesson:
// {"lesson": id} puts the song last in that lesson, {"lesson": null} in
// none.
func (s *Server) handleSetSongLesson(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.courseEditFor(w, r)
	if !ok {
		return
	}
	variantID, err := pathID(r, "variantID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid song id")
		return
	}
	var req struct {
		Lesson *int64 `json:"lesson"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetSongLesson(r.Context(), sb.ID, variantID, req.Lesson); errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleMoveLessonSong is POST /api/songbooks/{id}/lessons/{lessonID}/songs/{variantID}/move:
// {"by": -1} one place up, {"by": 1} down.
func (s *Server) handleMoveLessonSong(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.courseEditFor(w, r)
	if !ok {
		return
	}
	l, ok := s.lessonOf(w, r, sb)
	if !ok {
		return
	}
	variantID, err := pathID(r, "variantID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid song id")
		return
	}
	var req struct {
		By int `json:"by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.By != -1 && req.By != 1) {
		httpError(w, http.StatusBadRequest, "by is -1 or 1")
		return
	}
	if err := s.store.MoveLessonSong(r.Context(), l.ID, variantID, req.By); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleSetFileLesson is PUT /api/songbooks/{id}/files/{fileID}/lesson:
// {"lesson": id}, or null for its song's lesson.
func (s *Server) handleSetFileLesson(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.courseEditFor(w, r)
	if !ok {
		return
	}
	fileID, err := pathID(r, "fileID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid file id")
		return
	}
	f, err := s.store.GetSongbookFile(r.Context(), fileID)
	if err != nil || f.SongbookID != sb.ID {
		httpError(w, http.StatusNotFound, "no such file")
		return
	}
	var req struct {
		Lesson *int64 `json:"lesson"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetFileLesson(r.Context(), fileID, req.Lesson); errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleSetLessonDone is PUT /api/songbooks/{id}/lessons/{lessonID}/done:
// {"done": true} — the viewer's own progress.
func (s *Server) handleSetLessonDone(w http.ResponseWriter, r *http.Request) {
	_, l, ok := s.courseLessonFor(w, r)
	if !ok {
		return
	}
	var req struct {
		Done bool `json:"done"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetLessonDone(r.Context(), l.ID, personFromContext(r).ID, req.Done); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"done": req.Done})
}

// lessonPanelData is what a song's page shows of the lesson it's in, in
// a course: its name and page, its video, whether the viewer is done
// with it, its songs (when more than one) and its files.
type lessonPanelData struct {
	Lesson     store.Lesson
	SongbookID int64
	Course     string // the course's name
	Of         int    // how many lessons it has
	Href       string // its own page
	VideoID    string
	Done       bool
	DoneCount  int // how many of its lessons the viewer is done with
	Percent    int // that, of all its lessons
	Files      []lessonFile
	Prev, Next *lessonLink // the lessons before and after it
}

// lessonLink is a link to another lesson: its page, which opens on its
// song when it has one.
type lessonLink struct {
	Href, Label string
}

// lessonPanel is the lessonPanelData of lesson, in course sb, for the
// request's viewer.
func (s *Server) lessonPanel(r *http.Request, sb *store.Songbook, lesson *store.Lesson) (*lessonPanelData, error) {
	progress, err := s.store.CourseProgress(r.Context(), sb.ID, personFromContext(r).ID)
	if err != nil {
		return nil, err
	}
	files, err := s.courseFiles(r, sb)
	if err != nil {
		return nil, err
	}
	lessons, _, err := s.store.ListLessons(r.Context(), sb.ID)
	if err != nil {
		return nil, err
	}
	panel := &lessonPanelData{
		Lesson: *lesson, SongbookID: sb.ID, Course: sb.Name, Of: len(lessons), Href: lessonHref(sb.ID, lesson.ID) + "?view=lesson",
		VideoID: s.lessonVideo(*lesson), Done: progress[lesson.ID].Done, Files: files[lesson.ID],
	}
	lang := s.langOfRequest(r)
	link := func(l store.Lesson) *lessonLink {
		return &lessonLink{Href: lessonHref(sb.ID, l.ID), Label: l.Label(lang)}
	}
	for i, l := range lessons {
		if progress[l.ID].Done {
			panel.DoneCount++
		}
		if l.ID == lesson.ID {
			if i > 0 {
				panel.Prev = link(lessons[i-1])
			}
			if i+1 < len(lessons) {
				panel.Next = link(lessons[i+1])
			}
		}
	}
	if len(lessons) > 0 {
		panel.Percent = panel.DoneCount * 100 / len(lessons)
	}
	return panel, nil
}
