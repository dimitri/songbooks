package httpapi

import (
	"strings"

	"app/internal/song"
)

// The lesson view (docs/lesson-page-review.md): a song whose content is
// all in its Guitar Pro file ("content: gp"), or whose chart was only
// read from that file (importedFromGP: a course's lessons, imported from
// their files, the old display's grid and sections extracted from them),
// is practised from the file and its video, not read as a chart. Its
// page has three views, not the chart's eight:
//
//   - Video: the lesson's video first, the course around it (its lesson
//     n of N, progress, files, done, the lessons either side), the score
//     under it;
//   - Score: the score alone;
//   - Mixer.
//
// The video is the lesson's own, else the song's reference (lessonVideo);
// with none, the views are Score and Mixer. A song with a chart of its
// own next to its file (written by hand, or with lyrics) keeps the
// chart's views, in a course or not.

// lessonViewData sets LessonView, and LessonVideoID with its title
// (LessonVideoTitle), on a song page's data; nothing for a song that
// isn't one.
func lessonViewData(data map[string]any, sg *song.Song) {
	if name, _ := data["GPFileName"].(string); name == "" {
		return
	}
	if !isGPOnly(sg) && !importedFromGP(sg) {
		return
	}
	panel, _ := data["LessonPanel"].(*lessonPanelData)
	data["LessonView"] = true
	video, title := youtubeID(sg.Get("reference")), orDefault(sg.Get("title"), "untitled")
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
