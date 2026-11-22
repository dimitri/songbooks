package httpapi

import "app/internal/song"

// The lesson view (docs/lesson-page-review.md): a song whose content is
// all in its Guitar Pro file ("content: gp"), or a course's lesson played
// from its file, is practised from the file and the lesson's video, not
// read as a chart. Its page has three views, not the chart's eight:
//
//   - Video: the lesson's video first, the course around it (its lesson
//     n of N, progress, files, done, the lessons either side), the score
//     under it;
//   - Score: the score alone;
//   - Mixer.
//
// The video is the lesson's own, else the song's reference (lessonVideo);
// with none, the views are Score and Mixer. A song with a chart next to
// its file, outside a course, keeps the chart's views.

// lessonViewData sets LessonView, and LessonVideoID with its title
// (LessonVideoTitle), on a song page's data; nothing for a song that
// isn't one.
func lessonViewData(data map[string]any, sg *song.Song) {
	if name, _ := data["GPFileName"].(string); name == "" {
		return
	}
	panel, _ := data["LessonPanel"].(*lessonPanelData)
	if panel == nil && !isGPOnly(sg) {
		return
	}
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
