package httpapi

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"app/internal/song"
	"app/internal/store"
)

// TestLessonViewData: a song all in its Guitar Pro file, or a course's
// lesson with one, gets the lesson view; its video is the lesson's, else
// the song's reference; a chart next to its file, outside a course, and a
// song without a file don't.
func TestLessonViewData(t *testing.T) {
	gpOnly := song.Parse("title: Shuffle\ncontent: gp\nreference: https://youtu.be/abcdefghijk\n\n[Song]\ngp[#1]: 1-12\n")
	chart := song.Parse("title: Shuffle\nreference: https://youtu.be/abcdefghijk\n\n[Song]\ngrid: | A7 | D7 | A7 | E7 |\n")
	lesson := &lessonPanelData{Lesson: store.Lesson{Title: "Cours 5"}, VideoID: "lessonvideo"}
	cases := []struct {
		name      string
		sg        *song.Song
		data      map[string]any
		view      bool
		video, by string
	}{
		{"content: gp", gpOnly, map[string]any{"GPFileName": "a.gp"}, true, "abcdefghijk", "Shuffle"},
		{"content: gp, no file", gpOnly, map[string]any{}, false, "", ""},
		{"chart and file", chart, map[string]any{"GPFileName": "a.gp"}, false, "", ""},
		{"a lesson", chart, map[string]any{"GPFileName": "a.gp", "LessonPanel": lesson}, true, "lessonvideo", "Cours 5"},
		{"a lesson without a video", chart, map[string]any{"GPFileName": "a.gp", "LessonPanel": &lessonPanelData{Lesson: store.Lesson{Title: "Cours 6"}}}, true, "abcdefghijk", "Cours 6"},
		{"a lesson without a file", chart, map[string]any{"LessonPanel": lesson}, false, "", ""},
	}
	for _, c := range cases {
		lessonViewData(c.data, c.sg)
		view, _ := c.data["LessonView"].(bool)
		video, _ := c.data["LessonVideoID"].(string)
		by, _ := c.data["LessonVideoTitle"].(string)
		if view != c.view || video != c.video || by != c.by {
			t.Errorf("%s: view %v video %q (%q), want %v %q (%q)", c.name, view, video, by, c.view, c.video, c.by)
		}
	}
}

// TestLessonViewTabs renders the song page: a "content: gp" song shows
// the lesson's tabs, Video, Score and Mixer, and nothing else to pick a
// view by (the chart's switcher, hidden, keeps only its engine role); its
// video block is there when it has a reference, not without; a chart's
// page has no lesson tabs.
func TestLessonViewTabs(t *testing.T) {
	render := func(text string) string {
		sg := song.Parse(text)
		data := map[string]any{
			"VariantID": int64(7), "Title": sg.Get("title"), "Text": text, "Version": "1",
			"Sections": buildSections(sg), "GPFileName": "a.gp", "GPOnly": isGPOnly(sg), "HasGPView": isGPOnly(sg),
			"Tempo": "120", "Time": "4/4", "TimeNum": "4", "TimeDen": "4", "BrandKind": "", "BrandID": int64(0),
		}
		lessonViewData(data, sg)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/song/7?lang=en", nil)
		(&Server{}).render(&langWriter{ResponseWriter: w, r: r}, "song", data)
		return w.Body.String()
	}
	tabsOf := func(body string) []string {
		m := regexp.MustCompile(`(?s)<div class="view-tabs lesson-tabs" id="lesson-tabs".*?</div>`).FindString(body)
		var out []string
		for _, x := range regexp.MustCompile(`data-lesson-tab="(\w+)"`).FindAllStringSubmatch(m, -1) {
			out = append(out, x[1])
		}
		return out
	}

	body := render("title: Shuffle\ncontent: gp\nreference: https://youtu.be/abcdefghijk\n\n[Song]\ngp[#1]: 1-12\n")
	if !strings.Contains(body, "</html>") {
		t.Fatalf("incomplete page")
	}
	if got := strings.Join(tabsOf(body), ","); got != "video,score,mixer" {
		t.Errorf("tabs: %q, want video,score,mixer", got)
	}
	if !strings.Contains(body, `id="lesson-stage-video" data-video="abcdefghijk"`) {
		t.Errorf("no video block for the song's reference")
	}
	if !strings.Contains(body, `<span class="lesson-stage-video-title">Shuffle</span>`) {
		t.Errorf("the video's title is missing")
	}

	body = render("title: Shuffle\ncontent: gp\n\n[Song]\ngp[#1]: 1-12\n")
	if got := strings.Join(tabsOf(body), ","); got != "score,mixer" {
		t.Errorf("tabs without a video: %q, want score,mixer", got)
	}
	if strings.Contains(body, `id="lesson-stage-video"`) {
		t.Errorf("a video block without a video")
	}

	body = render("title: Shuffle\nreference: https://youtu.be/abcdefghijk\n\n[Song]\ngrid: | A7 | D7 | A7 | E7 |\n")
	if strings.Contains(body, `id="lesson-tabs"`) || strings.Contains(body, `id="lesson-stage"`) {
		t.Errorf("a chart next to its file got the lesson view")
	}
	if !strings.Contains(body, `data-value="jazz"`) {
		t.Errorf("a chart lost its views")
	}
}
