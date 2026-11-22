package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"app/internal/store"
)

// A set list's readers over HTTP read its rows (setlist_items, migration
// 0115): the running order and its version picker, the API, the song
// page's previous/next and "2 of 2", the click track, the PDFs; the
// picker saves.
func TestSetlistReadersAndVersionPicker(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	book, err := st.GetDefaultSongbook(ctx, "band", f.Band)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddToSongbook(ctx, book, f.BandSong, f.Core); err != nil {
		t.Fatal(err)
	}
	psych, err := st.MakeVersion(ctx, f.Core, f.BandSong, store.BandOwner(f.Band), book, "psychedelic")
	if err != nil {
		t.Fatal(err)
	}
	l, meta, version, err := st.LoadSetlistByID(ctx, f.SetlistID)
	if err != nil {
		t.Fatal(err)
	}
	row := l.Sets[0].Entries[0].ID
	page := fmt.Sprintf("/bands/%d/setlists/%d", f.Band, f.SetlistID)

	g := as(t, st, f.Guest)
	html := g.page(page)
	picker := fmt.Sprintf(`<select class="setlist-version" data-item="%d"`, row)
	if !strings.Contains(html, picker) ||
		!strings.Contains(html, fmt.Sprintf(`<option value="%d" selected>Night Drive</option>`, f.BandSong)) ||
		!strings.Contains(html, fmt.Sprintf(`<option value="%d">Night Drive — psychedelic</option>`, psych)) {
		t.Fatal("the running order offers no version of Night Drive to pick")
	}
	if strings.Count(html, `class="setlist-version"`) != 1 {
		t.Error("a song with one version offers a pick")
	}

	// The API: each song with its row.
	w := g.do("GET", fmt.Sprintf("/api/setlists/%d", f.SetlistID), nil)
	var api struct {
		Setlist struct {
			Sets []struct {
				Label   string `json:"label"`
				Entries []struct {
					ID        int64 `json:"id"`
					VariantID int64 `json:"variant_id"`
				} `json:"entries"`
			} `json:"sets"`
		} `json:"setlist"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &api); err != nil || len(api.Setlist.Sets) != 1 || api.Setlist.Sets[0].Entries[0].ID != row {
		t.Fatalf("GET the set list: %s", w.Body.String())
	}

	// Pick: not someone outside the band, not another work's song.
	pick := func(c client, song int64, ver string) int {
		return c.do("PUT", fmt.Sprintf("/api/setlists/%d/items/%d", f.SetlistID, row), map[string]any{"variant_id": song, "version": ver}).Code
	}
	if code := pick(as(t, st, f.Outsider), psych, version); code != http.StatusForbidden {
		t.Errorf("an outsider's pick: %d", code)
	}
	if code := pick(g, f.CatalogSong, version); code != http.StatusBadRequest {
		t.Errorf("another work's song: %d", code)
	}
	if code := pick(g, psych, fmt.Sprint(meta.Version-1)); code != http.StatusConflict {
		t.Errorf("a stale pick: %d", code)
	}
	if code := pick(g, psych, version); code != http.StatusOK {
		t.Fatalf("the pick: %d", code)
	}
	html = g.page(page)
	if !strings.Contains(html, `<span class="song-version-name">— psychedelic</span>`) ||
		!strings.Contains(html, fmt.Sprintf(`href="/song/%d?setlist=%d"`, psych, f.SetlistID)) ||
		!strings.Contains(html, fmt.Sprintf(`<option value="%d" selected>Night Drive — psychedelic</option>`, psych)) {
		t.Error("the running order doesn't play the version picked")
	}

	// The song page in the set list: previous and next, its place.
	html = g.page(fmt.Sprintf("/song/%d?setlist=%d", f.CatalogSong, f.SetlistID))
	if !strings.Contains(html, fmt.Sprintf(`href="/song/%d?setlist=%d"`, psych, f.SetlistID)) || !strings.Contains(html, "2 of 2") {
		t.Error("the song page's previous isn't the version picked, or its place isn't 2 of 2")
	}

	// The click track: the version picked.
	if html := g.page(page + "/clicktrack"); !strings.Contains(html, fmt.Sprintf(`"variantId":%d`, psych)) || strings.Contains(html, fmt.Sprintf(`"variantId":%d,`, f.BandSong)) {
		t.Error("the click track doesn't play the version picked")
	}

	// The PDFs: the set list on a page, and its songbook.
	for _, path := range []string{"/export-setlist-page.pdf", "/export.pdf"} {
		w := g.do("GET", fmt.Sprintf("/setlists/%d%s", f.SetlistID, path), nil)
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "%PDF") {
			t.Errorf("GET %s: %d", path, w.Code)
		}
	}
}
