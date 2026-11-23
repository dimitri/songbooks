package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"app/internal/httpapi"
	"app/internal/reftitle"
	"app/internal/store"
)

// fakeVideos points httpapi.RefTitles at an oEmbed endpoint answering
// videos' titles by their YouTube id, until the test ends.
func fakeVideos(t *testing.T, videos map[string]string) {
	t.Helper()
	oembed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		title, ok := videos[store.YouTubeID(r.URL.Query().Get("url"))]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"title":%q}`, title)
	}))
	httpapi.RefTitles = &reftitle.Fetcher{Client: oembed.Client(), Providers: map[string]string{
		"youtube.com": oembed.URL + "/oembed", "youtu.be": oembed.URL + "/oembed"}}
	t.Cleanup(func() { httpapi.RefTitles = nil; oembed.Close() })
}

// TestReferenceLabelFromTitle: a catalogue song's empty reference label
// is filled from its recording's title, cleaned, at a new reference (a
// fix of the link) or a new catalogue song; never over a typed label;
// none when the title only repeats the song. The "another recording"
// dialog's suggestion is the same cleaned label.
func TestReferenceLabelFromTitle(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newCatalogueFixture(t, st)
	fakeVideos(t, map[string]string{
		"AAAAAAAAAAA": "Someone - Le Vent (Official Video)",
		"BBBBBBBBBBB": "Someone - Le Vent (Live at the Olympia, 1994) [HD]",
		"CCCCCCCCCCC": "Someone - Le Vent (Acoustic)",
		"DDDDDDDDDDD": "Someone - La Mer (Demo) | Lyrics",
	})
	label := func(id int64) string {
		t.Helper()
		v, err := st.GetVariant(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return v.VersionName
	}
	start := "title: Le Vent\nartist: Someone\nreference: https://www.youtube.com/watch?v=AAAAAAAAAAA\n\n[Verse]\nLe vent\n"
	setChart(t, st, f.CatalogSong, start)
	store.ExecForTest(t, st, `UPDATE songs SET reference_url = 'https://www.youtube.com/watch?v=AAAAAAAAAAA' WHERE id = $1`, f.CatalogSong)
	ed := as(t, st, f.Editor)

	// An edit that keeps the reference: the empty label stays empty (the
	// songs there before are cmd/reflabels').
	if w := ed.saveRaw(f.CatalogSong, strings.Replace(start, "Le vent\n", "Le vent souffle\n", 1)); w.Code != http.StatusOK {
		t.Fatalf("an edit: %d %s", w.Code, w.Body.String())
	}
	if l := label(f.CatalogSong); l != "" {
		t.Errorf("labelled without a new reference: %q", l)
	}
	// The link fixed: the label from its title.
	cur := body(t, st, f.CatalogSong)
	if w := ed.saveRawAs(f.CatalogSong, strings.Replace(cur, "AAAAAAAAAAA", "BBBBBBBBBBB", 1), map[string]string{"X-Edit-Kind": "fix"}); w.Code != http.StatusOK {
		t.Fatalf("the link fixed: %d %s", w.Code, w.Body.String())
	}
	if l := label(f.CatalogSong); l != "Live at the Olympia, 1994" {
		t.Errorf("the label from the new link's title: %q", l)
	}
	if rt, _ := st.ReferenceTitle(ctx, f.CatalogSong); rt.For("https://www.youtube.com/watch?v=BBBBBBBBBBB") != "Someone - Le Vent (Live at the Olympia, 1994) [HD]" {
		t.Errorf("the title isn't kept: %+v", rt)
	}
	// Fixed again: the label isn't empty any more, it stays.
	cur = body(t, st, f.CatalogSong)
	if w := ed.saveRawAs(f.CatalogSong, strings.Replace(cur, "BBBBBBBBBBB", "CCCCCCCCCCC", 1), map[string]string{"X-Edit-Kind": "fix"}); w.Code != http.StatusOK {
		t.Fatalf("the link fixed again: %d %s", w.Code, w.Body.String())
	}
	if l := label(f.CatalogSong); l != "Live at the Olympia, 1994" {
		t.Errorf("a label overwritten: %q", l)
	}

	// A new catalogue song (its staff): labelled from its link's title;
	// one whose title only repeats it: none.
	create := func(title, ref string) int64 {
		t.Helper()
		w := as(t, st, f.Admin).do("POST", "/api/catalog/songs", map[string]string{"title": title, "artist": "Someone", "referenceUrl": ref})
		if w.Code != http.StatusOK {
			t.Fatalf("a new catalogue song: %d %s", w.Code, w.Body.String())
		}
		var out struct{ VariantID int64 }
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out.VariantID
	}
	if id := create("La Mer", "https://youtu.be/DDDDDDDDDDD"); label(id) != "Demo" {
		t.Errorf("a new song's label: %q", label(id))
	}
	if id := create("Le Vent", "https://youtu.be/AAAAAAAAAAA"); label(id) != "" {
		t.Errorf("a new song whose title only repeats it: %q", label(id))
	}

	// The "another recording" dialog's suggestion.
	w := ed.do("GET", "/api/reference-title?"+url.Values{"url": {"https://youtu.be/BBBBBBBBBBB"}, "title": {"Le Vent"}, "artist": {"Someone"}}.Encode(), nil)
	var sug struct{ Title, Label string }
	_ = json.Unmarshal(w.Body.Bytes(), &sug)
	if sug.Title != "Someone - Le Vent (Live at the Olympia, 1994) [HD]" || sug.Label != "Live at the Olympia, 1994" {
		t.Errorf("the suggestion: %+v", sug)
	}
}

// TestHomeSongNewsSeen: the home page lists the changes in the person's
// songs; "Mark as seen" (up to the newest shown) empties it until the
// next change; FR strings.
func TestHomeSongNewsSeen(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newCatalogueFixture(t, st)
	setChart(t, st, f.CatalogSong, "title: Le Vent\nartist: Someone\n\n[Verse]\ngrid: | Am | G |\n")
	ed, core := as(t, st, f.Editor), as(t, st, f.Core)
	if w := ed.saveRawSaying(f.CatalogSong, "title: Le Vent\nartist: Someone\n\n[Verse]\ngrid: | Am | G7 |\n", "the G is a G7"); w.Code != http.StatusOK {
		t.Fatalf("an edit: %d %s", w.Code, w.Body.String())
	}
	home := core.page("/home")
	for _, want := range []string{`id="song-news"`, "Changes in your songs", "“the G is a G7”", "by Editor", "Other Lies", fmt.Sprintf(`href="/variants/%d/history#rev-`, f.CatalogSong)} {
		if !strings.Contains(home, want) {
			t.Errorf("the home page lacks %q", want)
		}
	}
	if strings.Contains(ed.page("/home"), `id="song-news"`) {
		t.Error("the editor is told of their own change")
	}
	news, err := st.SongNewsFor(ctx, f.Core, 0)
	if err != nil || len(news) != 1 {
		t.Fatalf("news: %+v %v", news, err)
	}
	upTo := news[0].At.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	if w := core.do("POST", "/api/song-news/seen", map[string]string{"upTo": upTo}); w.Code != http.StatusOK {
		t.Fatalf("seen: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(core.page("/home"), `id="song-news"`) {
		t.Error("the list after Mark as seen")
	}
	if w := ed.saveRawSaying(f.CatalogSong, "title: Le Vent\nartist: Someone\n\n[Verse]\ngrid: | Am7 | G7 |\n", "an Am7"); w.Code != http.StatusOK {
		t.Fatalf("another edit: %d", w.Code)
	}
	if home := core.page("/home"); !strings.Contains(home, "“an Am7”") || strings.Contains(home, "the G is a G7") {
		t.Error("the next change isn't listed alone")
	}
	store.ExecForTest(t, st, `UPDATE people SET locale = 'fr' WHERE id = $1`, f.Core)
	if home := core.page("/home"); !strings.Contains(home, "Changements dans vos morceaux") || !strings.Contains(home, "Marquer comme vu") {
		t.Error("no French on the French home page")
	}
}
