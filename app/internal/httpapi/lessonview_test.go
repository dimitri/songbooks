package httpapi

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"app/internal/song"
	"app/internal/store"
)

// TestLessonViewData: a song all in its Guitar Pro file, or whose chart
// was only read from it (gp_source, every section's bars from the file, no
// lyrics), gets the lesson view, in a course or not; its video is the
// lesson's, else the song's reference. A chart of its own (written by
// hand, sung, a section added by hand) and a song without a file don't.
func TestLessonViewData(t *testing.T) {
	const ref = "reference: https://youtu.be/abcdefghijk\n"
	gpOnly := song.Parse("title: Shuffle\ncontent: gp\n" + ref + "\n[Song]\ngp[#1]: 1-12\n")
	imported := song.Parse("title: Shuffle\ngp_source: Shuffle.gp\n" + ref + "\n[Intro]\ngp[#1]: 1-4\ngrid: | A7 | D7 | A7 | E7 |\n\n[Solo]\ngp[#1]: 5-8\ngrid: | A7 | D7 | A7 | E7 |\n\n[Intro]\n")
	sung := song.Parse("title: Shuffle\ngp_source: Shuffle.gp\n\n[Verse]\ngp[#1]: 1-4\ngrid: | A7 | D7 | A7 | E7 |\nla la la, la la\n")
	added := song.Parse("title: Shuffle\ngp_source: Shuffle.gp\n\n[Intro]\ngp[#1]: 1-4\ngrid: | A7 | D7 | A7 | E7 |\n\n[Coda]\ngrid: | A7 | E7 |\n")
	chart := song.Parse("title: Shuffle\n" + ref + "\n[Song]\ngrid: | A7 | D7 | A7 | E7 |\n")
	lesson := func(video string) *lessonPanelData {
		return &lessonPanelData{Lesson: store.Lesson{Title: "Cours 5"}, VideoID: video}
	}
	file := func(panel *lessonPanelData) map[string]any {
		d := map[string]any{"GPFileName": "a.gp"}
		if panel != nil {
			d["LessonPanel"] = panel
		}
		return d
	}
	cases := []struct {
		name      string
		sg        *song.Song
		data      map[string]any
		view      bool
		video, by string
	}{
		{"content: gp", gpOnly, file(nil), true, "abcdefghijk", "Shuffle"},
		{"content: gp, no file", gpOnly, map[string]any{}, false, "", ""},
		{"imported from its file", imported, file(nil), true, "abcdefghijk", "Shuffle"},
		{"imported, a lesson", imported, file(lesson("lessonvideo")), true, "lessonvideo", "Cours 5"},
		{"imported, a lesson without a video", imported, file(lesson("")), true, "abcdefghijk", "Cours 5"},
		{"imported, sung", sung, file(nil), false, "", ""},
		{"imported, a section added by hand", added, file(nil), false, "", ""},
		{"a chart by hand, in a course", chart, file(lesson("lessonvideo")), false, "", ""},
		{"a chart by hand", chart, file(nil), false, "", ""},
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
