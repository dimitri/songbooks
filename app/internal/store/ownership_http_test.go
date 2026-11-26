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

// The ownership model over HTTP (docs/ownership-model.md §2, §4, §5):
// versions made from the song page, publishing, closed songbooks.

// TestMakeTheBandsVersionFromItsSongbook: a member opens the catalogue's
// song from the band's songbook: Edit offers "Make Other Lies's version",
// which takes the link's place there (its key kept); on the band's
// version, Edit edits it and "New arrangement from this" makes a named one
// beside it.
func TestMakeTheBandsVersionFromItsSongbook(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	f.GuestsEditTheSongbook(t, st) // the guest: another member who edits
	book, err := st.GetDefaultSongbook(ctx, "band", f.Band)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetEntrySemitones(ctx, book, f.CatalogSong, -3); err != nil {
		t.Fatal(err)
	}
	g := as(t, st, f.Guest)
	html := g.page(fmt.Sprintf("/song/%d?songbook=%d", f.CatalogSong, book))
	if strings.Contains(html, "data-edit-offer") || !strings.Contains(html, "Make Other Lies’s arrangement…") || !strings.Contains(html, fmt.Sprintf(`data-band-id="%d" data-songbook-id="%d"`, f.Band, book)) {
		t.Fatal("no \"Make Other Lies's version\" offer from the band's songbook")
	}
	if strings.Contains(html, "new-version-open") || strings.Contains(html, "song-publish-toggle") {
		t.Error("a band member is offered a new version, or publishing, of the catalogue's song")
	}
	version := g.variantID(g.do("POST", fmt.Sprintf("/api/bands/%d/catalog/fork/%d", f.Band, f.CatalogSong), map[string]int64{"songbookId": book}))
	if n, in, _ := st.SongbookEntryKey(ctx, book, version); !in || n != -3 {
		t.Errorf("the band's version in its songbook: %v, key %d (want -3)", in, n)
	}
	if _, in, _ := st.SongbookEntryKey(ctx, book, f.CatalogSong); in {
		t.Error("the link stayed beside the version")
	}

	html = g.page(fmt.Sprintf("/song/%d?songbook=%d", version, book))
	if !strings.Contains(html, `data-can-edit="true"`) || strings.Contains(html, "data-edit-offer") || !strings.Contains(html, "new-version-open") {
		t.Error("the band's version: not the editor, or no \"New version from this\"")
	}
	if strings.Contains(html, "song-publish-toggle") {
		t.Error("a guest member may publish the band's song")
	}
	if !strings.Contains(as(t, st, f.Core).page(fmt.Sprintf("/song/%d", version)), "song-publish-toggle") {
		t.Error("a core member isn't offered to publish the band's song")
	}
	if w := g.do("POST", fmt.Sprintf("/api/variants/%d/versions", version), map[string]any{"name": " ", "songbookId": book}); w.Code != http.StatusBadRequest {
		t.Errorf("a new version without a name: %d", w.Code)
	}
	if w := as(t, st, f.Outsider).do("POST", fmt.Sprintf("/api/variants/%d/versions", version), map[string]any{"name": "mine"}); w.Code != http.StatusForbidden {
		t.Errorf("an outsider's new version of the band's song: %d", w.Code)
	}
	psych := g.variantID(g.do("POST", fmt.Sprintf("/api/variants/%d/versions", version), map[string]any{"name": "psychedelic", "songbookId": book}))
	for _, id := range []int64{version, psych} {
		if _, in, _ := st.SongbookEntryKey(ctx, book, id); !in {
			t.Errorf("song %d isn't a row of the band's songbook", id)
		}
	}
	if v, _ := st.GetVariant(ctx, psych); v == nil || v.VersionName != "psychedelic" || *v.OwnerBandID != f.Band || v.Public {
		t.Errorf("the new version: %+v", v)
	}
}

// TestMakeTheCommunitysVersion: from a community's songbook that links a
// public song, who edits its repertoire is offered "Make the community's
// version", which takes the link's place; a guest isn't.
func TestMakeTheCommunitysVersion(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	community, err := st.CreateCommunity(ctx, "Jam Club", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddCommunityMember(ctx, community, f.Outsider, "guest", f.Core); err != nil {
		t.Fatal(err)
	}
	book, err := st.GetDefaultSongbook(ctx, "community", community)
	if err != nil {
		t.Fatal(err)
	}
	c := as(t, st, f.Core)
	if w := c.do("POST", fmt.Sprintf("/api/communities/%d/catalog", community), map[string]int64{"variantId": f.CatalogSong}); w.Code != http.StatusOK {
		t.Fatalf("linking the catalogue's song: %d %s", w.Code, w.Body.String())
	}
	path := fmt.Sprintf("/song/%d?songbook=%d", f.CatalogSong, book)
	if html := c.page(path); strings.Contains(html, "data-edit-offer") || !strings.Contains(html, `data-action="community"`) || !strings.Contains(html, "Make Jam Club’s arrangement…") {
		t.Fatal("no \"Make the community's version\" offer")
	}
	if html := as(t, st, f.Outsider).page(path); strings.Contains(html, `data-action="community"`) {
		t.Error("a guest is offered the community's version")
	}
	id := c.variantID(c.do("POST", fmt.Sprintf("/api/communities/%d/catalog/fork/%d", community, f.CatalogSong), map[string]int64{"songbookId": book}))
	if v, _ := st.GetVariant(ctx, id); v == nil || v.OwnerCommunityID == nil || *v.OwnerCommunityID != community || v.Public {
		t.Errorf("the community's version: %+v", v)
	}
	if _, in, _ := st.SongbookEntryKey(ctx, book, f.CatalogSong); in {
		t.Error("the link stayed beside the community's version")
	}
}

// TestPublishOverHTTP: a core member publishes the band's song (a guest
// member can't); then an outsider opens it, and another band links it.
func TestPublishOverHTTP(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	publish := func(who int64, public bool) int {
		return as(t, st, who).do("POST", fmt.Sprintf("/api/variants/%d/public", f.BandSong), map[string]bool{"public": public}).Code
	}
	if w := as(t, st, f.Outsider).do("GET", fmt.Sprintf("/song/%d", f.BandSong), nil); w.Code != http.StatusForbidden {
		t.Errorf("an outsider opening the band's private song: %d", w.Code)
	}
	if code := publish(f.Guest, true); code != http.StatusForbidden {
		t.Errorf("a guest member publishing: %d", code)
	}
	if code := publish(f.Core, true); code != http.StatusOK {
		t.Fatalf("a core member publishing: %d", code)
	}
	if w := as(t, st, f.Outsider).do("GET", fmt.Sprintf("/song/%d", f.BandSong), nil); w.Code != http.StatusOK {
		t.Errorf("an outsider opening the band's public song: %d", w.Code)
	}
	other, err := st.CreateBand(ctx, "Mercy", f.Outsider)
	if err != nil {
		t.Fatal(err)
	}
	o := as(t, st, f.Outsider)
	if id := o.variantID(o.do("POST", fmt.Sprintf("/api/bands/%d/catalog", other), map[string]int64{"variantId": f.BandSong})); id != f.BandSong {
		t.Errorf("Mercy linking Other Lies's public song: %d", id)
	}
	if code := publish(f.Core, false); code != http.StatusOK {
		t.Errorf("making it private again: %d", code)
	}
}

// TestClosedSongbookOverHTTP: a student of a community opens its closed
// songbook's song, keeps their own key for it in their songbook, and
// can't link it from their own songbook, make their version of it, or
// get an Edit offer; staff can't publish it.
func TestClosedSongbookOverHTTP(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	staff := f.Core
	student := f.Outsider
	community, err := st.CreateCommunity(ctx, "Florent", staff)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddCommunityMember(ctx, community, student, "guest", staff); err != nil {
		t.Fatal(err)
	}
	book, err := st.CreateSongbook(ctx, "community", community, "La méthode", false)
	if err != nil {
		t.Fatal(err)
	}
	lesson, err := st.CreateVariant(ctx, store.OwnerCommunity, community, "Leçon 1", "", staff)
	if err != nil {
		t.Fatal(err)
	}
	def, err := st.GetDefaultSongbook(ctx, "community", community)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveFromSongbook(ctx, def, lesson); err != nil {
		t.Fatal(err)
	}
	if err := st.AddToSongbook(ctx, book, lesson, staff); err != nil {
		t.Fatal(err)
	}
	if w := as(t, st, staff).do("POST", fmt.Sprintf("/api/communities/%d/songbooks/%d/closed", community, book), map[string]bool{"closed": true}); w.Code != http.StatusOK {
		t.Fatalf("closing the songbook: %d %s", w.Code, w.Body.String())
	}

	s := as(t, st, student)
	html := s.page(fmt.Sprintf("/song/%d?songbook=%d", lesson, book))
	if strings.Contains(html, "data-edit-offer") || strings.Contains(html, "song-publish-toggle") || strings.Contains(html, "new-version-open") {
		t.Error("a closed songbook's song offers a version, or publishing, to a student")
	}
	mine, err := st.GetDefaultSongbook(ctx, "person", student)
	if err != nil {
		t.Fatal(err)
	}
	var refusal struct {
		Error string `json:"error"`
	}
	w := s.do("POST", fmt.Sprintf("/api/songbooks/%d/entries", mine), map[string]int64{"variantId": lesson})
	json.Unmarshal(w.Body.Bytes(), &refusal)
	if w.Code != http.StatusConflict || refusal.Error != "closed" {
		t.Errorf("linking the closed songbook's song from my songbook: %d %s", w.Code, w.Body.String())
	}
	if w := s.do("POST", fmt.Sprintf("/api/variants/%d/fork", lesson), nil); w.Code != http.StatusConflict {
		t.Errorf("making my version of the closed songbook's song: %d %s", w.Code, w.Body.String())
	}
	if w := s.do("PUT", fmt.Sprintf("/api/songbooks/%d/entries/%d/key", mine, lesson), map[string]int{"semitones": 2}); w.Code != http.StatusOK {
		t.Errorf("keeping my key for it: %d %s", w.Code, w.Body.String())
	}
	if n, ok, err := st.MySongbookKey(ctx, student, lesson); err != nil || !ok || n != 2 {
		t.Errorf("my key: %d %v %v", n, ok, err)
	}
	if w := as(t, st, staff).do("POST", fmt.Sprintf("/api/variants/%d/public", lesson), map[string]bool{"public": true}); w.Code != http.StatusForbidden {
		t.Errorf("publishing the closed songbook's song: %d", w.Code)
	}
	if w := as(t, st, staff).do("POST", fmt.Sprintf("/api/songbooks/%d/public", book), map[string]bool{"public": true}); w.Code != http.StatusForbidden {
		t.Errorf("publishing the closed songbook: %d", w.Code)
	}
}
