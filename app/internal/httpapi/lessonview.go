package httpapi

import (
	"strings"

	"app/internal/song"
)

// The lesson view (docs/lesson-page-review.md). A song is a lesson when it
// has its Guitar Pro file and either is one of a course's lessons (a
// songbook of kind "course", placed in one of its lessons:
// store.CourseLessonOfSong, whichever way it's opened), or is all in its
// file ("content: gp"), or has a chart only read from that file
// (importedFromGP). That one property (LessonView) makes its page a
// lesson's: the video beside the score, Score and Mixer, the one player
// (Watch the lesson / Play along), a phone's two screens
// (lesson-view.js); and no transposing (a lesson is played in its file's
// key: no Key in the player, the Mixer or the header, no Transpose in the
// editor — its chart is the file's, for its editors too — and no
// student's or songbook's key).
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
		title = course.Title
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
