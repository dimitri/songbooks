package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"app/internal/httpapi"
	"app/internal/setlist"
	"app/internal/store"
)

// The band's song, edited by its members over HTTP: what the song page
// offers on Edit, and what the API saves. Here rather than in httpapi so
// that it shares the store tests' database, one package at a time (see
// NewTestStore).

type client struct {
	t  *testing.T
	st *store.Store
	h  http.Handler
}

// as is the app, signed in as personID.
func as(t *testing.T, st *store.Store, personID int64) client {
	t.Helper()
	p, err := st.GetPersonByID(context.Background(), personID)
	if err != nil {
		t.Fatal(err)
	}
	return client{t: t, st: st, h: httpapi.NewServer(st, nil, p, "", "")}
}

func (c client) do(method, path string, body any) *httptest.ResponseRecorder {
	c.t.Helper()
	var r *http.Request
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		r = httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	return w
}

// page is GET path's HTML, which must be a 200.
func (c client) page(path string) string {
	c.t.Helper()
	w := c.do("GET", path, nil)
	if w.Code != http.StatusOK {
		c.t.Fatalf("GET %s: %d", path, w.Code)
	}
	return w.Body.String()
}

// variantID is a JSON answer's "variantId", which must be a 200.
func (c client) variantID(w *httptest.ResponseRecorder) int64 {
	c.t.Helper()
	if w.Code != http.StatusOK {
		c.t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
	var out struct {
		VariantID int64 `json:"variantId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		c.t.Fatal(err)
	}
	return out.VariantID
}

// saveRaw is PUT /api/songs/{id}/raw at the song's current version.
func (c client) saveRaw(id int64, text string) *httptest.ResponseRecorder {
	c.t.Helper()
	_, version, err := c.st.LoadVariantRaw(id)
	if err != nil {
		c.t.Fatal(err)
	}
	return c.do("PUT", fmt.Sprintf("/api/songs/%d/raw", id), map[string]string{"version": version, "text": text})
}

func version(t *testing.T, st *store.Store, id int64) int {
	t.Helper()
	_, v, err := st.LoadVariantRaw(id)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(v)
	return n
}

func setlistSongs(t *testing.T, st *store.Store, id int64) []int64 {
	t.Helper()
	l, _, _, err := st.LoadSetlistByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return store.SetlistVariantIDs(l)
}

// TestMembersEditTheBandsSongInPlace: a current member, core or guest,
// opens the band's song with the editor (no offer to copy), and saving it
// saves that very song; a former member and an outsider get a 403. No
// copy is ever made.
func TestMembersEditTheBandsSongInPlace(t *testing.T) {
	st := store.NewTestStore(t)
	f := store.NewBandFixture(t, st)
	songs := store.CountSongs(t, st)
	text := "title: Night Drive\nartist: Other Lies\n\n[Verse]\nAm F C G\n"

	for _, who := range []int64{f.Core, f.Guest} {
		c := as(t, st, who)
		for _, path := range []string{
			fmt.Sprintf("/song/%d", f.BandSong),
			fmt.Sprintf("/song/%d?setlist=%d", f.BandSong, f.SetlistID),
			fmt.Sprintf("/song/%d?band=%d", f.BandSong, f.Band),
		} {
			html := c.page(path)
			if !strings.Contains(html, `data-can-edit="true"`) || strings.Contains(html, "data-edit-offer") || strings.Contains(html, "edit-offer-dialog") {
				t.Errorf("person %d, %s: not the editor on the band's song", who, path)
			}
		}
		before := version(t, st, f.BandSong)
		w := c.saveRaw(f.BandSong, text)
		if w.Code != http.StatusOK {
			t.Fatalf("person %d: PUT raw: %d %s", who, w.Code, w.Body.String())
		}
		if after := version(t, st, f.BandSong); after != before+1 {
			t.Errorf("person %d: version %d after a save from %d", who, after, before)
		}
	}
	revs, err := st.ListRevisions(context.Background(), "variant", strconv.FormatInt(f.BandSong, 10))
	if err != nil || len(revs) != 2 {
		t.Errorf("%d revisions (%v), want 2", len(revs), err)
	}
	for _, who := range []int64{f.Former, f.Outsider} {
		c := as(t, st, who)
		if w := c.saveRaw(f.BandSong, text); w.Code != http.StatusForbidden {
			t.Errorf("person %d: PUT raw: %d, want 403", who, w.Code)
		}
		if w := c.do("GET", fmt.Sprintf("/song/%d", f.BandSong), nil); w.Code != http.StatusForbidden {
			t.Errorf("person %d: GET the band's song: %d, want 403", who, w.Code)
		}
		if w := c.do("POST", fmt.Sprintf("/api/bands/%d/catalog/fork/%d", f.Band, f.CatalogSong), nil); w.Code != http.StatusForbidden {
			t.Errorf("person %d: making the band's version: %d, want 403", who, w.Code)
		}
	}
	if n := store.CountSongs(t, st); n != songs {
		t.Errorf("%d songs, want %d: a member's edit made a copy", n, songs)
	}
}

// TestEditFromTheBandsSetlist: the catalogue's song in the band's
// songbook and set list, opened from the set list by a member, doesn't
// open the editor on the catalogue's song: Edit offers the band's version.
// Making it repoints the set list and the songbook, and editing it
// through ?setlist= saves the band's song, the catalogue's untouched.
func TestEditFromTheBandsSetlist(t *testing.T) {
	st := store.NewTestStore(t)
	f := store.NewBandFixture(t, st)
	c := as(t, st, f.Guest)
	catalogVersion := version(t, st, f.CatalogSong)

	for _, path := range []string{
		fmt.Sprintf("/song/%d?setlist=%d", f.CatalogSong, f.SetlistID),
		fmt.Sprintf("/song/%d?band=%d", f.CatalogSong, f.Band),
		fmt.Sprintf("/song/%d", f.CatalogSong), // from home or a search: still the band's
	} {
		html := c.page(path)
		if !strings.Contains(html, `data-edit-offer="band"`) || !strings.Contains(html, fmt.Sprintf(`data-band-id="%d"`, f.Band)) || !strings.Contains(html, `data-can-edit="false"`) {
			t.Errorf("%s: no offer of the band's version", path)
		}
	}
	// Someone in no band that uses it: their own version, not the
	// catalogue's song (TestCatalogueSongs).
	if html := as(t, st, f.Outsider).page(fmt.Sprintf("/song/%d", f.CatalogSong)); !strings.Contains(html, `data-edit-offer="copy"`) {
		t.Error("an outsider: no offer of their own version of the catalogue's song")
	}

	bandSong := c.variantID(c.do("POST", fmt.Sprintf("/api/bands/%d/catalog/fork/%d", f.Band, f.CatalogSong), nil))
	if bandSong == f.CatalogSong || bandSong == 0 {
		t.Fatalf("the band's version: %d", bandSong)
	}
	if got := setlistSongs(t, st, f.SetlistID); len(got) != 2 || got[1] != bandSong {
		t.Errorf("set list %v: want its second item the band's song %d", got, bandSong)
	}
	html := c.page(fmt.Sprintf("/song/%d?setlist=%d", bandSong, f.SetlistID))
	if !strings.Contains(html, `data-can-edit="true"`) || strings.Contains(html, "data-edit-offer") {
		t.Error("the band's version from the set list: not the editor")
	}
	if w := c.saveRaw(bandSong, "title: Le Vent\n\n[Verse]\nD A\n"); w.Code != http.StatusOK {
		t.Fatalf("PUT raw: %d", w.Code)
	}
	if v := version(t, st, f.CatalogSong); v != catalogVersion {
		t.Errorf("the catalogue's song changed: version %d, was %d", v, catalogVersion)
	}
	// Making it again opens the same one.
	if again := c.variantID(c.do("POST", fmt.Sprintf("/api/bands/%d/catalog/fork/%d", f.Band, f.CatalogSong), nil)); again != bandSong {
		t.Errorf("again: %d, want %d", again, bandSong)
	}
}

// TestAddingToTheBandMakesTheBandsSong: every way of adding someone
// else's song to a band — its set list, its repertoire, its songbook —
// adds the band's own copy.
func TestAddingToTheBandMakesTheBandsSong(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	c := as(t, st, f.Guest)
	catalog := func(title string) int64 {
		id, err := st.CreateVariant(ctx, store.OwnerCatalog, 0, title, "", f.Core)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	isBands := func(how string, id int64) {
		t.Helper()
		v, err := st.GetVariant(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if v.Visibility != "band" || v.OwnerBandID == nil || *v.OwnerBandID != f.Band {
			t.Errorf("%s: song %d is %s, not the band's", how, id, v.Visibility)
		}
		entries, err := st.ListBandCatalog(ctx, f.Band)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.VariantID == id {
				return
			}
		}
		t.Errorf("%s: song %d isn't in the band's repertoire", how, id)
	}

	// The set list.
	added := catalog("Added To The Set List")
	l, meta, ver, err := st.LoadSetlistByID(ctx, f.SetlistID)
	if err != nil {
		t.Fatal(err)
	}
	l.Sets[0].Entries = append(l.Sets[0].Entries, setlist.Entry{VariantID: added})
	type jsonEntry struct {
		VariantID int64 `json:"variant_id"`
	}
	type jsonSet struct {
		Label   string      `json:"label"`
		Entries []jsonEntry `json:"entries"`
	}
	var sets []jsonSet
	for _, s := range l.Sets {
		js := jsonSet{Label: s.Label}
		for _, e := range s.Entries {
			js.Entries = append(js.Entries, jsonEntry{VariantID: e.VariantID})
		}
		sets = append(sets, js)
	}
	w := c.do("PUT", fmt.Sprintf("/api/setlists/%d", f.SetlistID), map[string]any{
		"version": ver, "title": meta.Title, "setlist": map[string]any{"sets": sets},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("PUT set list: %d %s", w.Code, w.Body.String())
	}
	got := setlistSongs(t, st, f.SetlistID)
	if len(got) != 3 || got[0] != f.BandSong || got[1] != f.CatalogSong || got[2] == added {
		t.Fatalf("set list %v: want the added song as the band's copy, the songs already there as they were", got)
	}
	isBands("set list", got[2])
	var answer struct {
		Version string `json:"version"`
	}
	json.Unmarshal(w.Body.Bytes(), &answer)
	if after, _ := st.GetSetlistMeta(ctx, f.SetlistID); answer.Version != strconv.Itoa(after.Version) {
		t.Errorf("PUT answered version %s, the set list is at %d", answer.Version, after.Version)
	}

	// The repertoire.
	rep := catalog("Added To The Repertoire")
	id := c.variantID(c.do("POST", fmt.Sprintf("/api/bands/%d/catalog", f.Band), map[string]int64{"variantId": rep}))
	if id == rep {
		t.Error("repertoire: the catalogue's song itself")
	}
	isBands("repertoire", id)

	// The band's songbook.
	sb, err := st.GetDefaultSongbook(ctx, "band", f.Band)
	if err != nil {
		t.Fatal(err)
	}
	book := catalog("Added To The Songbook")
	id = c.variantID(c.do("POST", fmt.Sprintf("/api/songbooks/%d/entries", sb), map[string]int64{"variantId": book}))
	if id == book {
		t.Error("songbook: the catalogue's song itself")
	}
	isBands("songbook", id)

	// A person's own songbook still takes the catalogue's song as it is.
	mine, err := st.GetDefaultSongbook(ctx, "person", f.Guest)
	if err != nil {
		t.Fatal(err)
	}
	if id := c.variantID(c.do("POST", fmt.Sprintf("/api/songbooks/%d/entries", mine), map[string]int64{"variantId": book})); id != book {
		t.Errorf("my songbook: %d, want the catalogue's %d", id, book)
	}
}

// TestNoSilentCopy: Edit on a song someone can't change offers an explicit
// copy, saying whose song it is; opening the page copies nothing. A copy
// of one's own of the band's song points at the band's version, on its
// page and in one's songbook.
func TestNoSilentCopy(t *testing.T) {
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
	theirs, err := st.CreateCommunityVariant(ctx, community, "Club Song", "", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	songs := store.CountSongs(t, st)
	c := as(t, st, f.Outsider)
	html := c.page(fmt.Sprintf("/song/%d?edit=1", theirs))
	if !strings.Contains(html, `data-edit-offer="copy"`) || !strings.Contains(html, `data-action="copy"`) || !strings.Contains(html, "Jam Club") {
		t.Error("a community guest: no copy offer naming the community")
	}
	if n := store.CountSongs(t, st); n != songs {
		t.Errorf("opening the page made a copy: %d songs, want %d", n, songs)
	}

	// The guest's own copy of the band's song, asked for.
	g := as(t, st, f.Guest)
	mine := g.variantID(g.do("POST", fmt.Sprintf("/api/variants/%d/fork", f.BandSong), nil))
	html = g.page(fmt.Sprintf("/song/%d", mine))
	if !strings.Contains(html, "band-version-notice") || !strings.Contains(html, fmt.Sprintf(`href="/song/%d?band=%d"`, f.BandSong, f.Band)) {
		t.Error("the copy's page: no notice of the band's version")
	}
	sb, err := st.GetDefaultSongbook(ctx, "person", f.Guest)
	if err != nil {
		t.Fatal(err)
	}
	if html := g.page(fmt.Sprintf("/songbooks/%d", sb)); !strings.Contains(html, "band-version-tag") {
		t.Error("my songbook: no tag on the copy")
	}
	if html := g.page(fmt.Sprintf("/song/%d", f.BandSong)); strings.Contains(html, "band-version-notice") {
		t.Error("the band's own song carries the notice")
	}
}

// TestSongbookKey: the same song in another key, kept in a songbook: it
// opens in that key from that songbook (and, for one's own songbook, from
// nowhere in particular), not from elsewhere; keeping it takes the right
// to change the songbook; the song itself never changes.
func TestSongbookKey(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	g := as(t, st, f.Guest)
	songVersion := version(t, st, f.BandSong)
	mine, err := st.GetDefaultSongbook(ctx, "person", f.Guest)
	if err != nil {
		t.Fatal(err)
	}
	bandBook, err := st.GetDefaultSongbook(ctx, "band", f.Band)
	if err != nil {
		t.Fatal(err)
	}
	keyed := func(html string) bool { return strings.Contains(html, `data-key-offset="-2"`) }

	// Not in my songbook yet: keeping the key there adds it.
	w := g.do("PUT", fmt.Sprintf("/api/songbooks/%d/entries/%d/key", mine, f.BandSong), map[string]int{"semitones": -2})
	if w.Code != http.StatusOK {
		t.Fatalf("PUT my key: %d %s", w.Code, w.Body.String())
	}
	if n, ok, err := st.MySongbookKey(ctx, f.Guest, f.BandSong); err != nil || !ok || n != -2 {
		t.Fatalf("my songbook's key: %d %v %v", n, ok, err)
	}
	for path, want := range map[string]bool{
		fmt.Sprintf("/song/%d?songbook=%d", f.BandSong, mine):              true,
		fmt.Sprintf("/song/%d", f.BandSong):                                true, // from home, a search
		fmt.Sprintf("/song/%d?songbook=%d", f.BandSong, bandBook):          false,
		fmt.Sprintf("/song/%d?setlist=%d", f.BandSong, f.SetlistID):        false,
		fmt.Sprintf("/song/%d?band=%d", f.BandSong, f.Band):                false,
		fmt.Sprintf("/song/%d?songbook=%d&key=original", f.BandSong, mine): false,
	} {
		html := g.page(path)
		if keyed(html) != want {
			t.Errorf("%s: in the songbook's key %v, want %v", path, keyed(html), want)
		}
		if want && (!strings.Contains(html, "songbook-key-line") || !strings.Contains(html, "edit-key-note") || !strings.Contains(html, "Other Lies’s version")) {
			t.Errorf("%s: no line saying whose version, in which key", path)
		}
		// Edit is still the band's song, in place.
		if !strings.Contains(html, `data-can-edit="true"`) || strings.Contains(html, "data-edit-offer") {
			t.Errorf("%s: Edit isn't the band's song", path)
		}
	}
	// Another member doesn't get my key.
	if keyed(as(t, st, f.Core).page(fmt.Sprintf("/song/%d", f.BandSong))) {
		t.Error("another member opens the song in my key")
	}

	// The band's songbook: a member keeps a key for the band.
	if w := g.do("PUT", fmt.Sprintf("/api/songbooks/%d/entries/%d/key", bandBook, f.BandSong), map[string]int{"semitones": -2}); w.Code != http.StatusOK {
		t.Fatalf("PUT the band's key: %d", w.Code)
	}
	if html := as(t, st, f.Core).page(fmt.Sprintf("/song/%d?songbook=%d", f.BandSong, bandBook)); !keyed(html) {
		t.Error("a member: the band's songbook's key not applied")
	}
	// Without the right to change the songbook: no.
	for _, c := range []struct {
		who, book int64
	}{{f.Outsider, mine}, {f.Former, bandBook}, {f.Core, mine}} {
		if w := as(t, st, c.who).do("PUT", fmt.Sprintf("/api/songbooks/%d/entries/%d/key", c.book, f.BandSong), map[string]int{"semitones": 3}); w.Code != http.StatusForbidden {
			t.Errorf("person %d, songbook %d: %d, want 403", c.who, c.book, w.Code)
		}
	}
	// A songbook without the song, not one's own: not added.
	other, err := st.CreateSongbook(ctx, "person", f.Guest, "Gigs", false)
	if err != nil {
		t.Fatal(err)
	}
	if w := g.do("PUT", fmt.Sprintf("/api/songbooks/%d/entries/%d/key", other, f.BandSong), map[string]int{"semitones": 1}); w.Code != http.StatusNotFound {
		t.Errorf("a songbook without the song: %d, want 404", w.Code)
	}
	// The keys the player offers to keep it in.
	var keys struct {
		Songbooks []struct {
			SongbookID int64 `json:"songbookId"`
			Mine       bool  `json:"mine"`
			Semitones  int   `json:"semitones"`
		} `json:"songbooks"`
	}
	w = g.do("GET", fmt.Sprintf("/api/songs/%d/songbook-keys", f.BandSong), nil)
	if err := json.Unmarshal(w.Body.Bytes(), &keys); err != nil || len(keys.Songbooks) != 2 || !keys.Songbooks[0].Mine || keys.Songbooks[0].Semitones != -2 || keys.Songbooks[1].SongbookID != bandBook {
		t.Errorf("songbook-keys: %s", w.Body.String())
	}

	if v := version(t, st, f.BandSong); v != songVersion {
		t.Errorf("the song changed: version %d, was %d", v, songVersion)
	}
}

// TestCatalogueSongs: the catalogue's song is everyone's and no one's to
// change in place but a site admin's. Edit offers one's own version (an
// explicit copy), which takes the song's place in one's songbook, its key
// there kept.
func TestCatalogueSongs(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	admin, err := st.CreatePerson(ctx, "admin", "Admin", "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAdmin(ctx, admin, true); err != nil {
		t.Fatal(err)
	}
	text := "title: Le Vent\nartist: Someone\n\n[Verse]\nC\n"
	for _, c := range []struct {
		who  int64
		want bool
	}{{f.Outsider, false}, {f.Core, false}, {admin, true}} {
		if ok, err := st.CanEditVariant(ctx, c.who, f.CatalogSong); err != nil || ok != c.want {
			t.Errorf("person %d: CanEditVariant = %v, %v; want %v", c.who, ok, err, c.want)
		}
		want := http.StatusForbidden
		if c.want {
			want = http.StatusOK
		}
		if w := as(t, st, c.who).saveRaw(f.CatalogSong, text); w.Code != want {
			t.Errorf("person %d: PUT raw on the catalogue's song: %d, want %d", c.who, w.Code, want)
		}
	}
	if html := as(t, st, admin).page(fmt.Sprintf("/song/%d", f.CatalogSong)); !strings.Contains(html, `data-can-edit="true"`) || strings.Contains(html, "data-edit-offer") {
		t.Error("an admin: not the editor on the catalogue's song")
	}

	// The outsider's songbook has it, two semitones down.
	o := as(t, st, f.Outsider)
	mine, err := st.GetDefaultSongbook(ctx, "person", f.Outsider)
	if err != nil {
		t.Fatal(err)
	}
	if w := o.do("PUT", fmt.Sprintf("/api/songbooks/%d/entries/%d/key", mine, f.CatalogSong), map[string]int{"semitones": -2}); w.Code != http.StatusOK {
		t.Fatalf("PUT key: %d", w.Code)
	}
	html := o.page(fmt.Sprintf("/song/%d?songbook=%d", f.CatalogSong, mine))
	if !strings.Contains(html, `data-edit-offer="copy"`) || !strings.Contains(html, "Make my own version") {
		t.Error("no offer of one's own version")
	}
	ownVersion := o.variantID(o.do("POST", fmt.Sprintf("/api/variants/%d/fork", f.CatalogSong), nil))
	if n, ok, err := st.SongbookEntryKey(ctx, mine, ownVersion); err != nil || !ok || n != -2 {
		t.Errorf("my version in my songbook: %d %v %v, want -2", n, ok, err)
	}
	if _, ok, _ := st.SongbookEntryKey(ctx, mine, f.CatalogSong); ok {
		t.Error("the catalogue's song still in my songbook beside my version")
	}
	if html := o.page(fmt.Sprintf("/song/%d", ownVersion)); !strings.Contains(html, `data-can-edit="true"`) {
		t.Error("my version: not the editor")
	}
}

// TestSearchShowsTheBandsVersion: a member searching the library finds
// their band's version of a song in place of the catalogue's, labelled;
// with two bands, each band's; someone in no band, the catalogue's. The
// band's song says what it's based on.
func TestSearchShowsTheBandsVersion(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	ours, _, err := st.AdoptIntoBand(ctx, f.Band, f.CatalogSong, f.Core)
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateBand(ctx, "Mercy", f.Guest)
	if err != nil {
		t.Fatal(err)
	}
	theirs, _, err := st.AdoptIntoBand(ctx, other, f.CatalogSong, f.Guest)
	if err != nil {
		t.Fatal(err)
	}
	type hit struct {
		VariantID int64  `json:"variantId"`
		Band      string `json:"band"`
	}
	search := func(who int64, path string) []hit {
		t.Helper()
		w := as(t, st, who).do("GET", path, nil)
		var hits []hit
		if err := json.Unmarshal(w.Body.Bytes(), &hits); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return hits
	}
	for _, path := range []string{"/api/catalog/search?q=vent", "/api/catalog/match?title=Le+Vent"} {
		if got := search(f.Core, path); len(got) != 1 || got[0].VariantID != ours || got[0].Band != "Other Lies" {
			t.Errorf("%s, a member of one band: %+v, want only its version %d", path, got, ours)
		}
		got := search(f.Guest, path)
		if len(got) != 2 || got[0].VariantID != theirs || got[0].Band != "Mercy" || got[1].VariantID != ours || got[1].Band != "Other Lies" {
			t.Errorf("%s, a member of two bands: %+v, want Mercy's %d and Other Lies's %d", path, got, theirs, ours)
		}
		if got := search(f.Outsider, path); len(got) != 1 || got[0].VariantID != f.CatalogSong || got[0].Band != "" {
			t.Errorf("%s, in no band: %+v, want the catalogue's %d", path, got, f.CatalogSong)
		}
	}
	if html := as(t, st, f.Core).page("/catalog?q=vent"); !strings.Contains(html, fmt.Sprintf(`href="/song/%d"`, ours)) || strings.Contains(html, fmt.Sprintf(`href="/song/%d"`, f.CatalogSong)) || !strings.Contains(html, "band-version-tag") {
		t.Error("the catalogue page: not the band's version, labelled")
	}
	html := as(t, st, f.Core).page(fmt.Sprintf("/song/%d", ours))
	if !strings.Contains(html, `class="based-on"`) || !strings.Contains(html, fmt.Sprintf(`href="/song/%d"`, f.CatalogSong)) {
		t.Error("the band's song: no \"Based on\" link to the catalogue's")
	}
}

// TestAddingAnotherBandsSongIsRefused: someone in two bands adding one
// band's song to the other's set list, repertoire or songbook is refused
// with a 409 that says whose song it is; for a cover, it offers the
// catalogue's version (catalogVariantId), which adds as this band's copy;
// for an original, it says it stays theirs. Nothing is added or copied.
// A band's add dialogs search (?band=) offers its own version or the
// catalogue's, never another band's.
func TestAddingAnotherBandsSongIsRefused(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	mercy, err := st.CreateBand(ctx, "Mercy", f.Core) // the core member is in both
	if err != nil {
		t.Fatal(err)
	}
	original, err := st.CreateVariant(ctx, store.OwnerBand, mercy, "Mercy Original", "Mercy", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	cover, _, err := st.AdoptIntoBand(ctx, mercy, f.CatalogSong, f.Core)
	if err != nil {
		t.Fatal(err)
	}
	c := as(t, st, f.Core)
	type refusal struct {
		Band             string `json:"band"`
		CatalogVariantID int64  `json:"catalogVariantId"`
		Message          string `json:"message"`
		Question         string `json:"question"`
	}
	refused := func(how string, w *httptest.ResponseRecorder, catalog int64) {
		t.Helper()
		var r refusal
		json.Unmarshal(w.Body.Bytes(), &r)
		if w.Code != http.StatusConflict || r.Band != "Mercy" || r.CatalogVariantID != catalog || r.Message == "" || (catalog != 0) != (r.Question != "") {
			t.Errorf("%s: %d %s, want a 409 naming Mercy, catalogue version %d", how, w.Code, w.Body.String(), catalog)
		}
	}
	songs := store.CountSongs(t, st)
	sb, err := st.GetDefaultSongbook(ctx, "band", f.Band)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		id, catalog int64
	}{{original, 0}, {cover, f.CatalogSong}} {
		refused("repertoire", c.do("POST", fmt.Sprintf("/api/bands/%d/catalog", f.Band), map[string]int64{"variantId": x.id}), x.catalog)
		refused("fork into the band", c.do("POST", fmt.Sprintf("/api/bands/%d/catalog/fork/%d", f.Band, x.id), nil), x.catalog)
		refused("band songbook", c.do("POST", fmt.Sprintf("/api/songbooks/%d/entries", sb), map[string]int64{"variantId": x.id}), x.catalog)

		l, meta, ver, err := st.LoadSetlistByID(ctx, f.SetlistID)
		if err != nil {
			t.Fatal(err)
		}
		var entries []map[string]any
		for _, e := range l.Sets[0].Entries {
			entries = append(entries, map[string]any{"variant_id": e.VariantID})
		}
		entries = append(entries, map[string]any{"variant_id": x.id})
		refused("set list", c.do("PUT", fmt.Sprintf("/api/setlists/%d", f.SetlistID), map[string]any{
			"version": ver, "title": meta.Title, "setlist": map[string]any{"sets": []map[string]any{{"label": "Set 1", "entries": entries}}},
		}), x.catalog)
	}
	if n := store.CountSongs(t, st); n != songs {
		t.Errorf("%d songs after the refusals, want %d", n, songs)
	}
	if got := setlistSongs(t, st, f.SetlistID); len(got) != 2 {
		t.Errorf("set list %v: changed by a refused save", got)
	}
	entries, err := st.ListBandCatalog(ctx, f.Band)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.VariantID == original || e.VariantID == cover {
			t.Errorf("Other Lies's songbook holds Mercy's song %d", e.VariantID)
		}
	}

	// The catalogue's version the refusal offers: added as Other Lies's
	// own copy.
	if id := c.variantID(c.do("POST", fmt.Sprintf("/api/bands/%d/catalog", f.Band), map[string]int64{"variantId": f.CatalogSong})); id == f.CatalogSong || id == cover {
		t.Errorf("the catalogue's version: %d, want Other Lies's copy", id)
	} else if v, _ := st.GetVariant(ctx, id); v == nil || *v.OwnerBandID != f.Band {
		t.Errorf("the catalogue's version added as %+v", v)
	}

	// A band's add dialog: its own version or the catalogue's, never
	// Mercy's; outside a band, each band's version.
	type hit struct {
		VariantID int64  `json:"variantId"`
		Band      string `json:"band"`
	}
	search := func(path string) []hit {
		var hits []hit
		json.Unmarshal(c.do("GET", path, nil).Body.Bytes(), &hits)
		return hits
	}
	for _, path := range []string{
		fmt.Sprintf("/api/catalog/search?q=vent&band=%d", f.Band),
		fmt.Sprintf("/api/catalog/match?title=Le+Vent&band=%d", f.Band),
	} {
		for _, h := range search(path) {
			if h.Band == "Mercy" || h.VariantID == cover {
				t.Errorf("%s offers Mercy's version: %+v", path, h)
			}
		}
	}
	if got := search("/api/catalog/search?q=vent"); len(got) != 2 {
		t.Errorf("search outside a band: %+v, want each band's version", got)
	}
}

// TestProposedSongIsTheBandsOwn: a band proposal of a catalogue song,
// once accepted, adds the band's own copy (not a pointer at the
// catalogue's song); proposing another band's song is refused like adding
// it.
func TestProposedSongIsTheBandsOwn(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	proposed, err := st.CreateVariant(ctx, store.OwnerCatalog, 0, "Proposed", "", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.CreateProposal(ctx, f.Band, f.Guest, "new_song", proposed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AcceptProposal(ctx, id, f.Core); err != nil {
		t.Fatal(err)
	}
	entries, err := st.ListBandCatalog(ctx, f.Band)
	if err != nil {
		t.Fatal(err)
	}
	var copied bool
	for _, e := range entries {
		if e.VariantID == proposed {
			t.Error("the repertoire points at the catalogue's song")
		}
		if e.ForkedFromID != nil && *e.ForkedFromID == proposed && e.Visibility == "band" {
			copied = true
		}
	}
	if !copied {
		t.Error("no band copy of the proposed song in the repertoire")
	}

	mercy, err := st.CreateBand(ctx, "Mercy", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := st.CreateVariant(ctx, store.OwnerBand, mercy, "Mercy Original", "", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	w := as(t, st, f.Core).do("POST", fmt.Sprintf("/api/bands/%d/proposals", f.Band), map[string]any{"kind": "new_song", "targetVariantId": theirs})
	if w.Code != http.StatusConflict {
		t.Errorf("proposing Mercy's song to Other Lies: %d %s, want 409", w.Code, w.Body.String())
	}
}
