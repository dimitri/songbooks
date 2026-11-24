package httpapi

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"app/internal/song"
	"app/internal/store"
)

// TestLessonViewData: a song with its Guitar Pro file is a lesson when it's
// one of a course's lessons (whichever way it's opened), all in its file,
// or its chart was only read from it (gp_source, every section's bars from
// the file, no lyrics); its video is the lesson's (the course's lesson's,
// the panel's), else the song's reference; its keys are taken away. A
// chart of its own (written by hand, sung, a section added by hand)
// outside a course, and a song without a file, aren't lessons.
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
	course := &songLesson{Title: "Cours 5", VideoID: "coursevideo"}
	cases := []struct {
		name      string
		sg        *song.Song
		data      map[string]any
		course    *songLesson
		view      bool
		video, by string
	}{
		{"content: gp", gpOnly, file(nil), nil, true, "abcdefghijk", "Shuffle"},
		{"content: gp, no file", gpOnly, map[string]any{}, nil, false, "", ""},
		{"imported from its file", imported, file(nil), nil, true, "abcdefghijk", "Shuffle"},
		{"imported, opened in its course", imported, file(lesson("lessonvideo")), course, true, "lessonvideo", "Cours 5"},
		{"imported, a lesson without a video", imported, file(lesson("")), &songLesson{Title: "Cours 5"}, true, "abcdefghijk", "Cours 5"},
		{"imported, sung", sung, file(nil), nil, false, "", ""},
		{"imported, a section added by hand", added, file(nil), nil, false, "", ""},
		{"a chart by hand, a course's lesson, opened from elsewhere", chart, file(nil), course, true, "coursevideo", "Cours 5"},
		{"a section added by hand, a course's lesson", added, file(nil), course, true, "coursevideo", "Cours 5"},
		{"a chart by hand", chart, file(nil), nil, false, "", ""},
		{"a course's lesson without its file", chart, map[string]any{}, course, false, "", ""},
	}
	for _, c := range cases {
		lessonViewData(c.data, c.sg, c.course)
		view, _ := c.data["LessonView"].(bool)
		video, _ := c.data["LessonVideoID"].(string)
		by, _ := c.data["LessonVideoTitle"].(string)
		if view != c.view || video != c.video || by != c.by {
			t.Errorf("%s: view %v video %q (%q), want %v %q (%q)", c.name, view, video, by, c.view, c.video, c.by)
		}
	}
}

// A lesson is played in its file's key: a student's, a songbook's or a
// coach's key is taken away; a chart keeps them.
func TestLessonViewKeys(t *testing.T) {
	keys := func() map[string]any {
		return map[string]any{"GPFileName": "a.gp", "StudentKey": -2, "StudentKeySet": true, "StudentKeyName": "D", "KeyContext": "Ada", "KeyMine": true, "SongbookKey": struct{}{}, "CoachStudentKeys": int64(3)}
	}
	d := keys()
	lessonViewData(d, song.Parse("title: Shuffle\ncontent: gp\n\n[Song]\ngp[#1]: 1-12\n"), nil)
	for _, k := range []string{"StudentKey", "StudentKeySet", "StudentKeyName", "KeyContext", "KeyMine", "SongbookKey", "CoachStudentKeys"} {
		if _, ok := d[k]; ok {
			t.Errorf("a lesson keeps %s", k)
		}
	}
	d = keys()
	lessonViewData(d, song.Parse("title: Shuffle\n\n[Song]\ngrid: | A7 | D7 |\n"), nil)
	if d["StudentKeySet"] != true || d["SongbookKey"] == nil {
		t.Errorf("a chart lost its keys: %v", d)
	}
}

// TestLessonViewTabs renders the song page: a "content: gp" song shows
// the lesson's views, Score and Mixer, and nothing else to pick a view by
// (the chart's switcher, hidden, keeps only its engine role); with a
// video (its reference), the video, the one player's switch (Watch the
// lesson / Play along) and its controls, the phone's Practise and ←
// Lesson; without, no video and no switch; a chart's page has none of it.
func TestLessonViewTabs(t *testing.T) {
	render := func(text string) string {
		sg := song.Parse(text)
		data := map[string]any{
			"VariantID": int64(7), "Title": sg.Get("title"), "Text": text, "Version": "1",
			"Sections": buildSections(sg), "GPFileName": "a.gp", "GPOnly": isGPOnly(sg), "HasGPView": isGPOnly(sg),
			"Tempo": "120", "Time": "4/4", "TimeNum": "4", "TimeDen": "4", "BrandKind": "", "BrandID": int64(0),
		}
		lessonViewData(data, sg, nil)
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
	if got := strings.Join(tabsOf(body), ","); got != "score,mixer" {
		t.Errorf("tabs: %q, want score,mixer", got)
	}
	for _, want := range []string{`data-mode="watch"`, `data-mode="play"`, `id="lesson-watch"`, `class="lesson-practise primary" data-screen="practice"`, `class="lesson-back" data-screen="lesson"`, `data-has-video="1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
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
	if strings.Contains(body, `id="lesson-stage-video"`) || strings.Contains(body, `id="lesson-mode"`) || strings.Contains(body, `data-has-video`) {
		t.Errorf("a video, or its switch, without a video")
	}

	body = render("title: Shuffle\nreference: https://youtu.be/abcdefghijk\n\n[Song]\ngrid: | A7 | D7 | A7 | E7 |\n")
	if strings.Contains(body, `id="lesson-tabs"`) || strings.Contains(body, `id="lesson-stage"`) {
		t.Errorf("a chart next to its file got the lesson view")
	}
	if !strings.Contains(body, `data-value="jazz"`) {
		t.Errorf("a chart lost its views")
	}
}
