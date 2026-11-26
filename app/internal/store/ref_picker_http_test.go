package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/internal/httpapi"
	"app/internal/reftitle"
	"app/internal/store"
	"app/internal/ytaudio"
)

// The YouTube picker (httpapi/yt_picker.go, static/js/yt-picker.js): one
// API for every place that picks a video — the Reference view, the
// Import wizard, Add / Create — and the reference it sets saved as any
// reference is (PUT meta), with the rights and The Big Songbook's rules
// as they were.

// pickerFakes: YouTube's search and oEmbed, offline.
func pickerFakes(t *testing.T) {
	t.Helper()
	var asked []string
	httpapi.YouTubeSearch = func(_ context.Context, title, artist string) ([]ytaudio.Candidate, error) {
		asked = append(asked, title+"|"+artist)
		var out []ytaudio.Candidate
		for i := 0; i < 10; i++ {
			id := fmt.Sprintf("VIDEO%06d", i)
			out = append(out, ytaudio.Candidate{ID: id, URL: "https://www.youtube.com/watch?v=" + id, Title: title + " " + fmt.Sprint(i), Channel: artist, Duration: 200})
		}
		return out, nil
	}
	oembed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		titles := map[string][2]string{
			"AAAAAAAAAAA": {"Someone - Le Vent (Official Video)", "Someone"},
			"BBBBBBBBBBB": {"Someone - Le Vent (Live at the Olympia, 1994)", "Someone"},
			"CCCCCCCCCCC": {"Le Vent (Acoustic)", "Someone - Topic"},
		}
		for id, x := range titles {
			if strings.Contains(r.URL.Query().Get("url"), id) {
				json.NewEncoder(w).Encode(map[string]string{"title": x[0], "author_name": x[1]})
				return
			}
		}
		http.NotFound(w, r)
	}))
	p := oembed.URL + "/oembed?format=json"
	httpapi.RefTitles = &reftitle.Fetcher{Client: oembed.Client(), Providers: map[string]string{"youtube.com": p, "youtu.be": p}}
	t.Cleanup(func() {
		oembed.Close()
		httpapi.YouTubeSearch = ytaudio.Search
		httpapi.RefTitles = nil
	})
}

type pickAnswer struct {
	Candidates []struct {
		ID, URL, Title, Channel, Thumb string
		Duration                       float64
	} `json:"candidates"`
}

func (c client) find(query string) (int, pickAnswer) {
	c.t.Helper()
	w := c.do("GET", "/api/youtube/find?"+query, nil)
	var a pickAnswer
	_ = json.Unmarshal(w.Body.Bytes(), &a)
	return w.Code, a
}

// putRef is the picker's save: the song's reference, with a catalogue
// choice's headers.
func (c client) putRef(song int64, link string, headers map[string]string) *httptest.ResponseRecorder {
	c.t.Helper()
	raw, _ := json.Marshal(map[string]string{"key": "reference", "value": link})
	r := httptest.NewRequest("PUT", fmt.Sprintf("/api/songs/%d/meta", song), strings.NewReader(string(raw)))
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	return w
}

// TestYouTubePickerAPI: the search's results (title and artist apart, or
// what was typed), at most 8, each with its thumbnail; a pasted link as
// that one video, by its canonical address, its title and channel read
// with oEmbed; the wizard's old address the same.
func TestYouTubePickerAPI(t *testing.T) {
	st := store.NewTestStore(t)
	pickerFakes(t)
	f := store.NewBandFixture(t, st)
	c := as(t, st, f.Core)

	code, a := c.find("title=Le+Vent&artist=Someone")
	if code != http.StatusOK || len(a.Candidates) != 8 {
		t.Fatalf("search: %d, %d results", code, len(a.Candidates))
	}
	if x := a.Candidates[0]; x.ID != "VIDEO000000" || x.Thumb != "https://i.ytimg.com/vi/VIDEO000000/mqdefault.jpg" || x.Channel != "Someone" || x.Duration != 200 {
		t.Errorf("a result: %+v", x)
	}
	// Typed: "Title — Artist" apart, words as the title.
	if _, a := c.find("q=" + strings.ReplaceAll("Le Vent — Someone", " ", "+")); len(a.Candidates) == 0 || a.Candidates[0].Title != "Le Vent 0" || a.Candidates[0].Channel != "Someone" {
		t.Errorf("typed title — artist: %+v", a.Candidates)
	}
	if _, a := c.find("q=le+vent+someone"); len(a.Candidates) == 0 || a.Candidates[0].Title != "le vent someone 0" {
		t.Errorf("typed words: %+v", a.Candidates)
	}
	// The wizard's old address: the same answer.
	if w := c.do("GET", "/api/import/youtube/find?title=Le+Vent&artist=Someone", nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"thumb":"https://i.ytimg.com/vi/VIDEO000000/mqdefault.jpg"`) {
		t.Errorf("the old address: %d %s", w.Code, w.Body.String())
	}
	// A pasted link.
	code, a = c.find("url=" + strings.ReplaceAll("youtu.be/AAAAAAAAAAA?si=x", "?", "%3F"))
	if code != http.StatusOK || len(a.Candidates) != 1 {
		t.Fatalf("a link: %d %+v", code, a)
	}
	if x := a.Candidates[0]; x.URL != "https://www.youtube.com/watch?v=AAAAAAAAAAA" || x.ID != "AAAAAAAAAAA" || x.Title != "Someone - Le Vent (Official Video)" || x.Channel != "Someone" || x.Thumb == "" {
		t.Errorf("a link's video: %+v", x)
	}
	// Another site's page: as it is, no video.
	if _, a := c.find("url=https://example.invalid/song"); len(a.Candidates) != 1 || a.Candidates[0].ID != "" || a.Candidates[0].URL != "https://example.invalid/song" {
		t.Errorf("a page: %+v", a.Candidates)
	}
	for _, q := range []string{"", "url=ftp://example.com/x", "q=+"} {
		if code, _ := c.find(q); code != http.StatusBadRequest {
			t.Errorf("%q: %d", q, code)
		}
	}
}

// TestReferenceFromPicker: a video picked sets the song's reference, as
// the link field did — the owner's own song, a band's song by its
// members, nobody else; on The Big Songbook's song a new video asks "a
// fix, or another recording": a fix in place, its label filled from the
// video's title while it has none (and said back, with the title);
// another recording a new catalogue song, labelled, the song unchanged.
func TestReferenceFromPicker(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	pickerFakes(t)
	f := newCatalogueFixture(t, st)
	own, err := st.CreateVariant(ctx, store.OwnerPerson, f.Outsider, "Mon Morceau", "Moi", f.Outsider)
	if err != nil {
		t.Fatal(err)
	}
	me, core, guest := as(t, st, f.Outsider), as(t, st, f.Core), as(t, st, f.Guest)
	refOf := func(id int64) string {
		t.Helper()
		c, err := st.SongChartOf(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return c.ReferenceURL
	}

	// One's own song: set, changed, removed; nobody else's to change.
	link := "https://www.youtube.com/watch?v=AAAAAAAAAAA"
	if w := me.putRef(own, link, nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"referenceTitle":"Someone - Le Vent (Official Video)"`) {
		t.Fatalf("own song: %d %s", w.Code, w.Body.String())
	}
	if refOf(own) != link || !strings.Contains(body(t, st, own), "reference: "+link) {
		t.Errorf("own song's reference: %q", refOf(own))
	}
	if w := core.putRef(own, "https://www.youtube.com/watch?v=BBBBBBBBBBB", nil); w.Code != http.StatusForbidden || refOf(own) != link {
		t.Errorf("someone else's song: %d", w.Code)
	}
	if w := me.putRef(own, "https://www.youtube.com/watch?v=CCCCCCCCCCC", nil); w.Code != http.StatusOK || refOf(own) != "https://www.youtube.com/watch?v=CCCCCCCCCCC" {
		t.Errorf("changed: %d %q", w.Code, refOf(own))
	}
	if w := me.putRef(own, "", nil); w.Code != http.StatusOK || refOf(own) != "" {
		t.Errorf("removed: %d %q", w.Code, refOf(own))
	}

	// A band's song: its members change it, as before; an outsider not.
	if w := core.putRef(f.BandSong, link, nil); w.Code != http.StatusOK || refOf(f.BandSong) != link {
		t.Errorf("band song, a core member: %d", w.Code)
	}
	guestRights, _ := st.SongRightsFor(ctx, f.Guest, f.BandSong)
	if w := guest.putRef(f.BandSong, "https://www.youtube.com/watch?v=CCCCCCCCCCC", nil); (w.Code == http.StatusOK) != (guestRights != nil && (guestRights.Edit || guestRights.Contribute)) {
		t.Errorf("band song, a guest: %d, rights %+v", w.Code, guestRights)
	}
	if w := me.putRef(f.BandSong, "https://www.youtube.com/watch?v=BBBBBBBBBBB", nil); w.Code != http.StatusForbidden {
		t.Errorf("band song, an outsider: %d", w.Code)
	}

	// The Big Songbook's song, its reference set: a new video asks first.
	setChart(t, st, f.CatalogSong, "title: Le Vent\nartist: Someone\nreference: "+link+"\n\n[Verse]\nLe vent\n")
	store.ExecForTest(t, st, `UPDATE songs SET reference_url = $2, version_name = '' WHERE id = $1`, f.CatalogSong, link)
	ed := as(t, st, f.Editor)
	live := "https://www.youtube.com/watch?v=BBBBBBBBBBB"
	if w := ed.putRef(f.CatalogSong, live, nil); w.Code != http.StatusPreconditionRequired || !strings.Contains(w.Body.String(), `"referenceChanged":true`) || refOf(f.CatalogSong) != link {
		t.Fatalf("asked first: %d %s", w.Code, w.Body.String())
	}
	// Another recording: a new song, labelled from its video's title; this one as it was.
	w := ed.putRef(f.CatalogSong, live, map[string]string{"X-Edit-Kind": "recording"})
	nv := newSongID(t, w)
	if refOf(f.CatalogSong) != link {
		t.Error("the song changed with another recording")
	}
	if v, _ := st.GetVariant(ctx, nv); v == nil || v.VersionName != "Live at the Olympia, 1994" || refOf(nv) != live {
		t.Errorf("another recording's song: %+v", v)
	}
	// A fix: in place, its empty label filled from the new video's title,
	// said back with the title.
	fixed := "https://youtu.be/CCCCCCCCCCC"
	w = ed.putRef(f.CatalogSong, fixed, map[string]string{"X-Edit-Kind": "fix"})
	var out map[string]any
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("a fix: %d %s", w.Code, w.Body.String())
	}
	if refOf(f.CatalogSong) != fixed || out["referenceTitle"] != "Le Vent (Acoustic)" || out["referenceLabel"] != "Acoustic" {
		t.Errorf("a fix: %q %v", refOf(f.CatalogSong), out)
	}
	// Never over a label set: the next fix keeps it.
	w = ed.putRef(f.CatalogSong, link, map[string]string{"X-Edit-Kind": "fix"})
	if v, _ := st.GetVariant(ctx, f.CatalogSong); w.Code != http.StatusOK || v.VersionName != "Acoustic" || strings.Contains(w.Body.String(), "referenceLabel") {
		t.Errorf("a label kept: %d %s %q", w.Code, w.Body.String(), v.VersionName)
	}
	// A newcomer's fix: live too (every edit is), checked afterwards.
	if w := me.putRef(f.CatalogSong, live, map[string]string{"X-Edit-Kind": "fix"}); w.Code != http.StatusOK {
		t.Errorf("a newcomer's fix: %d %s", w.Code, w.Body.String())
	}
}

// TestReferenceView: the Reference view's picker, its search on the
// song's title and artist, for who changes the reference (none of the
// old link field's pencil); the words fit the place — a band's song says
// the band, one's own plain; an uploaded recording is the band's only in
// a band's place.
func TestReferenceView(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	own, err := st.CreateVariant(ctx, store.OwnerPerson, f.Outsider, "Mon Morceau", "Moi", f.Outsider)
	if err != nil {
		t.Fatal(err)
	}
	me, core := as(t, st, f.Outsider), as(t, st, f.Core)

	page := me.page(fmt.Sprintf("/song/%d", own))
	for _, want := range []string{
		`id="reference-picker" data-yt-picker data-title="Mon Morceau" data-artist="Moi"`,
		`data-use-label="Use this video"`,
		`/static/js/yt-picker.js`,
		"The recording you learn the song from",
		">Your recording</h3>",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("own song's Reference view lacks %q", want)
		}
	}
	for _, not := range []string{"reference-edit-btn", "reference-url-input", "whole band", "Save link", ">Band recording<"} {
		if strings.Contains(page, not) {
			t.Errorf("own song's Reference view has %q", not)
		}
	}

	band := core.page(fmt.Sprintf("/song/%d", f.BandSong))
	for _, want := range []string{`id="reference-picker"`, "The recording the whole band listens to", ">Band recording</h3>", `data-band-label="Band recording"`} {
		if !strings.Contains(band, want) {
			t.Errorf("band song's Reference view lacks %q", want)
		}
	}

	// With a reference: the video, "Change the video", the picker behind it.
	link := "https://www.youtube.com/watch?v=AAAAAAAAAAA"
	if w := me.putRef(own, link, nil); w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	page = me.page(fmt.Sprintf("/song/%d", own))
	for _, want := range []string{`id="reference-change-btn"`, `id="reference-picker-box" hidden`, `data-current="` + link + `"`, `id="reference-remove-btn"`} {
		if !strings.Contains(page, want) {
			t.Errorf("with a reference, the view lacks %q", want)
		}
	}

	// Who can't change it: no picker, no Change.
	store.ExecForTest(t, st, `UPDATE songs SET public = true WHERE id = $1`, own)
	other := core.page(fmt.Sprintf("/song/%d", own))
	if strings.Contains(other, `id="reference-picker"`) || strings.Contains(other, `id="reference-change-btn"`) {
		t.Error("someone else's song: a picker")
	}
}
