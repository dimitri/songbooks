package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"app/internal/song"
	"app/internal/store"
)

// The lesson view (docs/lesson-page-review.md). A song is a lesson when it
// has its Guitar Pro file and either is one of a course's lessons (a
// songbook of kind "course", placed in one of its lessons:
// store.CourseLessonsOfSong, whichever way it's opened), or is all in its
// file ("content: gp"), or has a chart only read from that file
// (importedFromGP). That one property (LessonView) makes its page a
// lesson's: the video beside the score, Score and Mixer, the one player
// (Watch the lesson / Play along), a phone's two screens
// (lesson-view.js); and no transposing (a lesson is played in its file's
// key: no Key in the player, the Mixer or the header, no Transpose in the
// editor — its chart is the file's, for its editors too — and no
// student's or songbook's key).
//
// A lesson opened from anywhere (a search, the library, home's "recently",
// the history, a notification, a shared /song/N link) is opened in its
// course, exactly as from its songbook (withCourseContext): its card
// (progress, files, done, the lessons either side), the way back to it,
// the previous and next lessons, its video. Only a course the viewer may
// open (store.CanAccessSongbook, or a site admin): someone else sees the
// lesson's page, not its course's card, its other lessons' titles or its
// video. A song in two courses' lessons: the first the viewer may open,
// the lowest id first. A song opened from somewhere in particular (a set
// list, a songbook — a student's, a band's —, an event, a band, a
// community, a playlist beside it) keeps that place.
//
// The video is the lesson's own (the course's lesson, else the song's
// reference); with none, there's no switch, the player plays along. A
// song with a chart of its own next to its file (written by hand, or
// with lyrics), outside a course, keeps the chart's views.

// songLesson is the course lesson a song is placed in, for its page.
type songLesson struct {
	Title, VideoID string
}

// lessonViewData sets LessonView, and LessonVideoID with its title
// (LessonVideoTitle), on a song page's data, and takes its keys away;
// nothing for a song that isn't a lesson. course is the song's course
// lesson, nil for none.
func lessonViewData(data map[string]any, sg *song.Song, course *songLesson) {
	if name, _ := data["GPFileName"].(string); name == "" {
		return
	}
	if course == nil && !isGPOnly(sg) && !importedFromGP(sg) {
		return
	}
	panel, _ := data["LessonPanel"].(*lessonPanelData)
	data["LessonView"] = true
	video, title := youtubeID(sg.Get("reference")), orDefault(sg.Get("title"), "untitled")
	if course != nil {
		if course.VideoID != "" {
			video = course.VideoID
		}
		if course.Title != "" {
			title = course.Title
		}
	}
	if panel != nil {
		if panel.VideoID != "" {
			video = panel.VideoID
		}
		title = panel.Lesson.Title
	}
	if video != "" {
		data["LessonVideoID"] = video
		data["LessonVideoTitle"] = title
	}
	// Played in its file's key: no student's, songbook's or coach's key.
	for _, k := range []string{"StudentKey", "StudentKeySet", "StudentKeyName", "KeyContext", "KeyMine", "SongbookKey", "CoachStudentKeys"} {
		delete(data, k)
	}
}

// importedFromGP reports whether a song's chart is only what its Guitar
// Pro import read from the file: it names the file it came from
// ("gp_source:"), every section with content of its own plays bars of the
// file ("gp[...]:"), and there are no lyrics. A chart written by hand, or
// a song sung, isn't one.
func importedFromGP(sg *song.Song) bool {
	if strings.TrimSpace(sg.Get("gp_source")) == "" {
		return false
	}
	any := false
	for _, sec := range sg.Sections {
		if len(sec.Lyrics) > 0 {
			return false
		}
		if len(sec.Measures) == 0 && len(sec.Scores) == 0 && len(sec.GP) == 0 {
			continue // a bare repeat of an earlier section
		}
		if len(sec.GP) == 0 {
			return false
		}
		any = true
	}
	return any
}

// songLessonFor is the song's course lesson for its page: a lesson (its
// file, in a course's lesson) whose course the viewer may open gives its
// title and video; one whose course they may not is still a lesson,
// without them; nil for a song in no course's lesson.
func (s *Server) songLessonFor(r *http.Request, variantID int64, data map[string]any) *songLesson {
	if data["GPFileName"] == nil {
		return nil
	}
	refs, err := s.store.CourseLessonsOfSong(r.Context(), variantID)
	if err != nil || len(refs) == 0 {
		return nil
	}
	out := &songLesson{}
	if c := s.openableCourse(r, refs); c != nil {
		if l, err := s.store.GetLesson(r.Context(), c.LessonID); err == nil {
			out.Title, out.VideoID = l.Title, s.lessonVideo(*l)
		}
	}
	return out
}

// openableCourse is the first of a song's course lessons whose course the
// viewer may open, nil for none.
func (s *Server) openableCourse(r *http.Request, refs []store.CourseLessonRef) *store.CourseLessonRef {
	for i := range refs {
		if s.mayOpenSongbook(r, refs[i].SongbookID) {
			return &refs[i]
		}
	}
	return nil
}

// mayOpenSongbook: the viewer may open the songbook (its owner's member,
// with its grant when it's gated; a public one), or is a site admin.
func (s *Server) mayOpenSongbook(r *http.Request, songbookID int64) bool {
	return s.mayOpenSongbookAs(r.Context(), personFromContext(r), songbookID)
}

// songContextParams are the places a song can be opened from, in its
// address: with any of them, the song keeps it.
var songContextParams = []string{"setlist", "songbook", "event", "band", "community", "embed"}

// withCourseContext opens a lesson opened from no particular place in its
// course, as ?songbook=<course> would: the first of its course lessons
// the viewer may open. r as it is otherwise.
func (s *Server) withCourseContext(r *http.Request, variantID int64) *http.Request {
	q := r.URL.Query()
	for _, k := range songContextParams {
		if q.Get(k) != "" {
			return r
		}
	}
	refs, err := s.store.CourseLessonsOfSong(r.Context(), variantID)
	if err != nil || len(refs) == 0 {
		return r
	}
	c := s.openableCourse(r, refs)
	if c == nil {
		return r
	}
	q.Set("songbook", strconv.FormatInt(c.SongbookID, 10))
	r2 := r.Clone(r.Context())
	r2.URL.RawQuery = q.Encode()
	return r2
}
