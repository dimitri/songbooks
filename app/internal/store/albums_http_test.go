package store_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/internal/setlist"
	"app/internal/store"
)

// upload is a multipart PUT of data as the field "file".
func (c client) upload(path, name string, data []byte) *httptest.ResponseRecorder {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		c.t.Fatal(err)
	}
	fw.Write(data)
	mw.Close()
	r := httptest.NewRequest("PUT", path, &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	return w
}

// TestAlbumPagesAndArtwork: the album's page for its band (the editor
// for who edits the songbook), not for others until it's public; its
// artwork uploaded, cropped square and resized, served by its address;
// the songbook by album with the Live chip; the song page's album line
// and Album | Live switch.
func TestAlbumPagesAndArtwork(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	second, err := st.CreateVariant(ctx, store.OwnerBand, f.Band, "Paper Moon", "Other Lies", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.CreateAlbum(ctx, f.Core, f.Band, store.AlbumInput{Title: "Ultrablanc", ReleasedOn: "2026-04-10",
		Tracks: []store.AlbumTrackInput{{SongID: second, Title: "Paper Moons"}, {SongID: f.BandSong}}})
	if err != nil {
		t.Fatal(err)
	}
	core, guest, outsider := as(t, st, f.Core), as(t, st, f.Guest), as(t, st, f.Outsider)
	page := fmt.Sprintf("/bands/%d/albums/%d", f.Band, id)
	html := core.page(page)
	for _, want := range []string{"Ultrablanc", "Paper Moons", "the song: Paper Moon", "Play the album", "album-edit-dialog", "album-public"} {
		if !strings.Contains(html, want) {
			t.Fatalf("album page lacks %q", want)
		}
	}
	if html := guest.page(page); strings.Contains(html, "album-edit-dialog") || strings.Contains(html, `id="album-public"`) {
		t.Fatal("a guest gets the editor")
	}
	if w := outsider.do("GET", page, nil); w.Code != http.StatusNotFound {
		t.Fatalf("outsider: %d", w.Code)
	}
	if w := core.do("GET", page+"/play", nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Ultrablanc") {
		t.Fatalf("play: %d", w.Code)
	}

	// The artwork: a 600×400 picture becomes a 400×400 JPEG.
	img := image.NewRGBA(image.Rect(0, 0, 600, 400))
	for x := 0; x < 600; x++ {
		for y := 0; y < 400; y++ {
			img.Set(x, y, color.RGBA{uint8(x / 3), 40, uint8(y / 2), 255})
		}
	}
	var pngBuf bytes.Buffer
	png.Encode(&pngBuf, img)
	api := fmt.Sprintf("/api/bands/%d/albums/%d/cover", f.Band, id)
	if w := guest.upload(api, "a.png", pngBuf.Bytes()); w.Code != http.StatusForbidden {
		t.Fatalf("a guest uploads: %d", w.Code)
	}
	if w := core.upload(api, "a.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)); w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("an SVG: %d", w.Code)
	}
	w := core.upload(api, "a.png", pngBuf.Bytes())
	if w.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	var path string
	if _, err := fmt.Sscanf(strings.TrimSpace(w.Body.String()), `{"cover":%q}`, &path); err != nil || !strings.HasPrefix(path, fmt.Sprintf("/album-covers/%d/", id)) {
		t.Fatalf("cover answer %s (%v)", w.Body, err)
	}
	got := outsider.do("GET", path, nil)
	if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("cover: %d %s", got.Code, got.Header().Get("Content-Type"))
	}
	if cfg, err := jpeg.DecodeConfig(got.Body); err != nil || cfg.Width != 400 || cfg.Height != 400 {
		t.Fatalf("cover size: %+v %v", cfg, err)
	}
	if w := outsider.do("GET", fmt.Sprintf("/album-covers/%d/0000000000000000.jpg", id), nil); w.Code != http.StatusNotFound {
		t.Fatalf("a wrong address: %d", w.Code)
	}

	// Public: everyone signed in sees the page (no band sections).
	if w := guest.do("POST", fmt.Sprintf("/api/bands/%d/albums/%d/public", f.Band, id), map[string]bool{"public": true}); w.Code != http.StatusForbidden {
		t.Fatalf("a guest publishes: %d", w.Code)
	}
	if w := core.do("POST", fmt.Sprintf("/api/bands/%d/albums/%d/public", f.Band, id), map[string]bool{"public": true}); w.Code != http.StatusOK {
		t.Fatalf("publish: %d", w.Code)
	}
	if html := outsider.page(page); !strings.Contains(html, "Ultrablanc") || strings.Contains(html, "space-header") {
		t.Fatal("outsider on the public album")
	}

	// The live version, made over HTTP; the songbook by album folds it.
	if w := guest.do("POST", fmt.Sprintf("/api/songs/%d/live-version", f.BandSong), nil); w.Code != http.StatusForbidden {
		t.Fatalf("a guest makes the live version: %d", w.Code)
	}
	live := core.variantID(core.do("POST", fmt.Sprintf("/api/songs/%d/live-version", f.BandSong), nil))
	book := core.page(fmt.Sprintf("/bands/%d/catalog", f.Band))
	for _, want := range []string{"By album", `data-album-group="album-`, "Not on an album", fmt.Sprintf(`href="/song/%d?band=%d" title=`, live, f.Band), "“Paper Moons”"} {
		if !strings.Contains(book, want) {
			t.Fatalf("songbook lacks %q", want)
		}
	}
	if strings.Contains(book, fmt.Sprintf(`data-variant-id="%d"`, live)) {
		t.Fatal("the live version has a row of its own")
	}
	if az := core.page(fmt.Sprintf("/bands/%d/catalog?view=az", f.Band)); strings.Contains(az, `data-album-group="album-`) {
		t.Fatal("A–Z is grouped")
	}

	// The song page: where it comes from, Album | Live.
	songPage := core.page(fmt.Sprintf("/song/%d", f.BandSong))
	for _, want := range []string{"song-album-line", "Ultrablanc", "track 2", "album-live-switch", fmt.Sprintf(`href="/song/%d"`, live)} {
		if !strings.Contains(songPage, want) {
			t.Fatalf("song page lacks %q", want)
		}
	}
	if !strings.Contains(core.page(fmt.Sprintf("/song/%d", live)), "album-live-switch") {
		t.Fatal("the live version has no switch")
	}
}

// TestSetlistsUseLiveVersions: the band's setting (edit_band); with it on,
// a line added to a set list plays the song's live version, the album
// song is listed once to add, the page shows Live | Album; Use the live
// versions here moves a set list made before.
func TestSetlistsUseLiveVersions(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	second, err := st.CreateVariant(ctx, store.OwnerBand, f.Band, "Paper Moon", "Other Lies", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAlbum(ctx, f.Core, f.Band, store.AlbumInput{Title: "Ultrablanc", Tracks: []store.AlbumTrackInput{{SongID: second}, {SongID: f.BandSong}}}); err != nil {
		t.Fatal(err)
	}
	live, err := st.MakeLiveVersion(ctx, f.Core, second, store.LiveVersionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	core, guest := as(t, st, f.Core), as(t, st, f.Guest)
	setting := fmt.Sprintf("/api/bands/%d/setlist-versions", f.Band)
	if w := guest.do("POST", setting, map[string]string{"value": "live"}); w.Code != http.StatusForbidden {
		t.Fatalf("a guest sets it: %d", w.Code)
	}
	if w := core.do("POST", setting, map[string]string{"value": "live"}); w.Code != http.StatusOK {
		t.Fatalf("setting: %d %s", w.Code, w.Body)
	}
	if !strings.Contains(core.page(fmt.Sprintf("/bands/%d/settings", f.Band)), `name="setlistVersions" value="live" checked`) {
		t.Fatal("settings page")
	}
	// Add the album song: the line plays its live version.
	l := setlistOf(t, st, f.SetlistID)
	l.Sets[0].Entries = append(l.Sets[0].Entries, setlist.Entry{VariantID: second, Note: "segue"})
	if w := core.do("PUT", fmt.Sprintf("/api/setlists/%d", f.SetlistID), map[string]any{"version": "", "title": "Friday", "setlist": jsonSetlistOf(l)}); w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	ids := setlistSongs(t, st, f.SetlistID)
	if ids[len(ids)-1] != live {
		t.Fatalf("the new line plays %d, want the live version %d", ids[len(ids)-1], live)
	}
	html := core.page(fmt.Sprintf("/bands/%d/setlists/%d", f.Band, f.SetlistID))
	if !strings.Contains(html, "live-chip") {
		t.Fatal("no Live chip on the set list")
	}

	// The setting off: a set list made before keeps the album song, then
	// moves on request.
	if w := core.do("POST", setting, map[string]string{"value": "song"}); w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	band := f.Band
	other, err := st.CreateSetlist(ctx, "band", "Saturday", "", nil, &band, nil, f.Core)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveSetlistByID(ctx, other, &setlist.Setlist{Sets: []setlist.Set{{Label: "Set 1", Entries: []setlist.Entry{{VariantID: second}}}}}, "Saturday", "", "", f.Core); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(core.page(fmt.Sprintf("/bands/%d/setlists/%d", f.Band, other)), "use-live-versions") {
		t.Fatal("no Use the live versions here")
	}
	if w := as(t, st, f.Outsider).do("POST", fmt.Sprintf("/api/setlists/%d/use-live-versions", other), nil); w.Code != http.StatusForbidden && w.Code != http.StatusNotFound {
		t.Fatalf("an outsider moves the set list: %d", w.Code)
	}
	if w := core.do("POST", fmt.Sprintf("/api/setlists/%d/use-live-versions", other), nil); w.Code != http.StatusOK {
		t.Fatalf("use live: %d %s", w.Code, w.Body)
	}
	if ids := setlistSongs(t, st, other); len(ids) != 1 || ids[0] != live {
		t.Fatalf("moved: %v", ids)
	}
}

func setlistOf(t *testing.T, st *store.Store, id int64) *setlist.Setlist {
	t.Helper()
	l, _, _, err := st.LoadSetlistByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// jsonSetlistOf is l as PUT /api/setlists/{id} takes it.
func jsonSetlistOf(l *setlist.Setlist) map[string]any {
	var sets []map[string]any
	for _, s := range l.Sets {
		var entries []map[string]any
		for _, e := range s.Entries {
			entries = append(entries, map[string]any{"variant_id": e.VariantID, "note": e.Note})
		}
		sets = append(sets, map[string]any{"label": s.Label, "entries": entries})
	}
	return map[string]any{"sets": sets}
}
