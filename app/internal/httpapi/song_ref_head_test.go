package httpapi

import (
	"strings"
	"testing"

	"app/internal/webassets"
)

func TestSongRefHeadFor(t *testing.T) {
	for _, c := range []struct {
		ref        string
		band, edit bool
		kind, id   string
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", true, false, "youtube", "dQw4w9WgXcQ"},
		{"https://youtu.be/dQw4w9WgXcQ?t=3", false, false, "youtube", "dQw4w9WgXcQ"},
		{"https://www.youtube.com/watch?v=../../x", false, false, "link", ""},
		{"https://open.spotify.com/track/1", false, true, "link", ""},
		{"", true, false, "band", ""},
		{"  ", false, true, "", ""},
	} {
		h := songRefHeadFor(c.ref, c.band, c.edit)
		if h.Kind != c.kind || h.YouTubeID != c.id || h.CanEdit != c.edit {
			t.Errorf("songRefHeadFor(%q, %v, %v) = %+v", c.ref, c.band, c.edit, h)
		}
	}
}

// The header's reference: a thumbnail for a YouTube link, an icon for
// anything else, "+ Reference" for an editor of a song without one, and
// nothing for a reader of it.
func TestSongRefHeadTemplate(t *testing.T) {
	render := func(lang string, h songRefHead) string {
		var b strings.Builder
		if err := webassets.TemplatesFor(lang).ExecuteTemplate(&b, "song-ref-head", h); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	yt := render("en", songRefHeadFor("https://www.youtube.com/watch?v=dQw4w9WgXcQ", false, false))
	for _, want := range []string{
		`data-kind="youtube"`, `<img src="https://i.ytimg.com/vi/dQw4w9WgXcQ/mqdefault.jpg" alt=""`,
		`loading="lazy"`, `referrerpolicy="no-referrer"`, `aria-label="Open the song&#39;s reference"`, ">Reference<",
	} {
		if !strings.Contains(yt, want) {
			t.Errorf("YouTube: no %q in\n%s", want, yt)
		}
	}
	if strings.Contains(yt, " hidden") || strings.Contains(yt, "data-can-edit") {
		t.Errorf("YouTube, a reader:\n%s", yt)
	}

	link := render("fr", songRefHeadFor("https://example.com/song", false, false))
	if !strings.Contains(link, `data-kind="link"`) || strings.Contains(link, "<img") || !strings.Contains(link, ">Référence<") {
		t.Errorf("a link, in French:\n%s", link)
	}

	band := render("en", songRefHeadFor("", true, false))
	if !strings.Contains(band, `data-kind="band"`) || strings.Contains(band, "<img") || strings.Contains(band, " hidden") {
		t.Errorf("a band recording:\n%s", band)
	}

	add := render("fr", songRefHeadFor("", false, true))
	if !strings.Contains(add, `data-kind=""`) || strings.Contains(add, " hidden") || !strings.Contains(add, `aria-label="Ajouter une référence"`) {
		t.Errorf("none, an editor:\n%s", add)
	}

	none := render("en", songRefHeadFor("", false, false))
	if !strings.Contains(none, `data-kind="" hidden`) {
		t.Errorf("none, a reader:\n%s", none)
	}
}
