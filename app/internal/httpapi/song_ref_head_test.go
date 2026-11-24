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

// The reference's name: the app keeps no video title, so its site.
func TestRefLinkName(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ": "YouTube",
		"https://youtu.be/dQw4w9WgXcQ":                "YouTube",
		"https://m.youtube.com/watch?v=dQw4w9WgXcQ":   "YouTube",
		"https://open.spotify.com/track/1":            "open.spotify.com",
		"https://www.Example.com/a":                   "example.com",
		"not a link":                                  "not a link",
	} {
		if got := refLinkName(in); got != want {
			t.Errorf("refLinkName(%q) = %q, want %q", in, got, want)
		}
	}
	var b strings.Builder
	if err := webassets.TemplatesFor("fr").ExecuteTemplate(&b, "song-ref-head", songRefHeadFor("https://youtu.be/dQw4w9WgXcQ", false, false)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `id="topbar-ref-name" data-title="">YouTube<`) || !strings.Contains(b.String(), `width="96" height="54"`) {
		t.Errorf("YouTube's name and thumbnail:\n%s", b.String())
	}
	// An uploaded recording: the band's in a band's place, else one's own.
	b.Reset()
	band := songRefHeadFor("", true, false)
	band.Band = true
	if err := webassets.TemplatesFor("fr").ExecuteTemplate(&b, "song-ref-head", band); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `id="topbar-ref-name" data-title="">Enregistrement du groupe<`) {
		t.Errorf("a band recording's name:\n%s", b.String())
	}
	b.Reset()
	if err := webassets.TemplatesFor("fr").ExecuteTemplate(&b, "song-ref-head", songRefHeadFor("", true, false)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `id="topbar-ref-name" data-title="">Votre enregistrement<`) || strings.Contains(b.String(), "groupe") {
		t.Errorf("one's own recording's name:\n%s", b.String())
	}
}
