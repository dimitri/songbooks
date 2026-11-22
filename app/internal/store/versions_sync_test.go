package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"app/internal/setlist"
	"app/internal/store"
)

// The ownership model, steps 6 and 7 (docs/ownership-model.md §5, §8):
// the original's changes offered to its versions, frozen copies when a
// song goes private or is deleted, the bands' public recordings.

const leVent = `title: Le Vent
artist: Someone
tempo: 96

[Verse]
grid: | Am | G | F | E7 |

[Chorus]
grid: | F | G | C | C |
`

// unread is personID's unread notifications of the version's change.
func unread(t *testing.T, st *store.Store, personID int64) []store.Notification {
	t.Helper()
	ns, err := st.ListNotifications(context.Background(), personID, true)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Notification
	for _, n := range ns {
		if n.Kind == store.NotificationVersionChanged {
			out = append(out, n)
		}
	}
	return out
}

func saveCatalogue(t *testing.T, st *store.Store, id int64, body string) {
	t.Helper()
	if _, err := st.SaveVariantRaw(id, body, "", 0); err != nil {
		t.Fatal(err)
	}
}

func originalVersion(t *testing.T, st *store.Store, id int64) int {
	t.Helper()
	_, v, err := st.LoadVariantRaw(id)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	fmt.Sscan(v, &n)
	return n
}

// TestOriginalChangeOfferedToVersion: the band's version of the
// catalogue's song is told when the catalogue's chart changes — not for
// whitespace or its title — once while it waits; on the version's page a
// member takes the change, the parts the band changed too staying the
// band's, or keeps theirs.
func TestOriginalChangeOfferedToVersion(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	saveCatalogue(t, st, f.CatalogSong, leVent)
	book, err := st.GetDefaultSongbook(ctx, "band", f.Band)
	if err != nil {
		t.Fatal(err)
	}
	g := as(t, st, f.Guest)
	version := g.variantID(g.do("POST", fmt.Sprintf("/api/bands/%d/catalog/fork/%d", f.Band, f.CatalogSong), map[string]int64{"songbookId": book}))
	if store.SongBase(t, st, version) != leVent {
		t.Fatal("the version doesn't record the chart it was made from")
	}

	// Whitespace, then the title alone: nobody is told.
	saveCatalogue(t, st, f.CatalogSong, strings.Replace(leVent, "\n\n[Chorus]", "\n\n\n[Chorus]", 1))
	titled := strings.Replace(leVent, "title: Le Vent", "title: Le Vent (live)", 1)
	saveCatalogue(t, st, f.CatalogSong, titled)
	if n := unread(t, st, f.Core); len(n) != 0 {
		t.Fatalf("told of a whitespace or title change: %+v", n)
	}
	if c, err := st.OriginalChangeFor(ctx, f.Core, version); err != nil || c != nil {
		t.Fatalf("a change offered for whitespace or the title: %+v, %v", c, err)
	}

	// A chord: every current member is told, once.
	v2 := strings.Replace(titled, "| Am | G | F | E7 |", "| Am | G7 | F | E7 |", 1)
	saveCatalogue(t, st, f.CatalogSong, v2)
	saveCatalogue(t, st, f.CatalogSong, strings.Replace(v2, "tempo: 96", "tempo: 100", 1))
	for _, p := range []int64{f.Core, f.Guest} {
		n := unread(t, st, p)
		if len(n) != 1 || n[0].Link != store.VersionChangedLink(version) || n[0].Body != "The shared version of Le Vent (live) changed" {
			t.Errorf("person %d told: %+v", p, n)
		}
	}
	for _, p := range []int64{f.Former, f.Outsider} {
		if n := unread(t, st, p); len(n) != 0 {
			t.Errorf("person %d, not a member, told: %+v", p, n)
		}
	}
	html := as(t, st, f.Core).page(fmt.Sprintf("/song/%d", version))
	if !strings.Contains(html, `id="original-change"`) || !strings.Contains(html, "Take this change") || !strings.Contains(html, "tempo 96 → 100") {
		t.Error("the version's page shows no change of the original")
	}
	if strings.Contains(as(t, st, f.Core).page(fmt.Sprintf("/song/%d", f.CatalogSong)), `id="original-change"`) {
		t.Error("the original's own page offers a change")
	}

	// The band changes its chorus; the catalogue changes its chorus too.
	verBody, _, _ := st.LoadVariantRaw(version)
	if w := g.saveRaw(version, strings.Replace(verBody, "| F | G | C | C |", "| F | G | C | Am |", 1)); w.Code != http.StatusOK {
		t.Fatalf("saving the version: %d %s", w.Code, w.Body.String())
	}
	theirs := strings.NewReplacer("tempo: 96", "tempo: 100", "| F | G | C | C |", "| F | G | C7 | C |").Replace(v2)
	saveCatalogue(t, st, f.CatalogSong, theirs)
	c, err := st.OriginalChangeFor(ctx, f.Guest, version)
	if err != nil || c == nil {
		t.Fatalf("no change offered: %v", err)
	}
	parts := map[string]bool{}
	for _, p := range c.Parts {
		parts[p.Key] = p.Conflict
	}
	if len(parts) != 3 || parts["h:tempo"] || parts["s:verse#1"] || !parts["s:chorus#1"] {
		t.Fatalf("parts: %+v", c.Parts)
	}
	if c, _ := st.OriginalChangeFor(ctx, f.Outsider, version); c != nil {
		t.Error("an outsider is offered the band's version's change")
	}

	path := fmt.Sprintf("/api/variants/%d/original-change", version)
	take := map[string]any{"action": "take", "parts": []string{"h:tempo", "s:verse#1", "s:chorus#1"}, "originalVersion": c.OriginalVersion - 1}
	if w := g.do("POST", path, take); w.Code != http.StatusConflict {
		t.Errorf("taking a change the original made again since: %d", w.Code)
	}
	take["originalVersion"] = c.OriginalVersion
	if w := as(t, st, f.Outsider).do("POST", path, take); w.Code != http.StatusForbidden {
		t.Errorf("an outsider taking the change: %d", w.Code)
	}
	if w := g.do("POST", path, take); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"changed":true`) {
		t.Fatalf("taking the change: %d %s", w.Code, w.Body.String())
	}
	got, _, _ := st.LoadVariantRaw(version)
	for _, s := range []string{"tempo: 100", "| Am | G7 | F | E7 |", "| F | G | C | Am |", "title: Le Vent\n"} {
		if !strings.Contains(got, s) {
			t.Errorf("the version after taking the change lacks %q:\n%s", s, got)
		}
	}
	if strings.Contains(got, "C7") {
		t.Errorf("the band's own chorus was overwritten:\n%s", got)
	}
	if store.SongBase(t, st, version) != theirs {
		t.Error("the version isn't synced with the original")
	}
	if n := unread(t, st, f.Core); len(n) != 0 {
		t.Errorf("still told after the change was taken: %+v", n)
	}
	if c, _ := st.OriginalChangeFor(ctx, f.Core, version); c != nil {
		t.Errorf("still offered after taking it: %+v", c.Parts)
	}

	// Keep ours: the version stays as it is, synced.
	again := strings.Replace(theirs, "tempo: 100", "tempo: 108", 1)
	saveCatalogue(t, st, f.CatalogSong, again)
	if w := g.do("POST", path, map[string]any{"action": "keep", "originalVersion": originalVersion(t, st, f.CatalogSong)}); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"changed":false`) {
		t.Fatalf("keeping ours: %d %s", w.Code, w.Body.String())
	}
	if after, _, _ := st.LoadVariantRaw(version); after != got {
		t.Error("keeping ours changed the version")
	}
	if store.SongBase(t, st, version) != again {
		t.Error("keeping ours didn't sync the version")
	}
}

// TestFrozenCopies: Other Lies's public song, linked by Mercy (its
// songbook in its own key, its set list), and by a person, goes private:
// the confirmation says who uses it; each keeps a frozen copy in its
// place, a band member keeps the song itself. Deleted, the same, the
// copies following nothing.
func TestFrozenCopies(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	core := as(t, st, f.Core)
	if w := core.do("POST", fmt.Sprintf("/api/variants/%d/public", f.BandSong), map[string]bool{"public": true}); w.Code != http.StatusOK {
		t.Fatalf("publishing: %d", w.Code)
	}
	mercy, err := st.CreateBand(ctx, "Mercy", f.Outsider)
	if err != nil {
		t.Fatal(err)
	}
	o := as(t, st, f.Outsider)
	link := func(songID int64) {
		t.Helper()
		if id := o.variantID(o.do("POST", fmt.Sprintf("/api/bands/%d/catalog", mercy), map[string]int64{"variantId": songID})); id != songID {
			t.Fatalf("Mercy linking song %d: %d", songID, id)
		}
	}
	link(f.BandSong)
	mercyBook, _ := st.GetDefaultSongbook(ctx, "band", mercy)
	if err := st.SetEntrySemitones(ctx, mercyBook, f.BandSong, -2); err != nil {
		t.Fatal(err)
	}
	gig, err := st.CreateSetlist(ctx, "band", "Gig", "", nil, &mercy, nil, f.Outsider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveSetlistByID(ctx, gig, &setlist.Setlist{Sets: []setlist.Set{{Label: "Set 1", Entries: []setlist.Entry{{VariantID: f.BandSong}}}}}, "Gig", "", "", f.Outsider); err != nil {
		t.Fatal(err)
	}
	outBook, _ := st.GetDefaultSongbook(ctx, "person", f.Outsider)
	guestBook, _ := st.GetDefaultSongbook(ctx, "person", f.Guest)
	for _, b := range []struct{ book, who int64 }{{outBook, f.Outsider}, {guestBook, f.Guest}} {
		if err := st.AddToSongbook(ctx, b.book, f.BandSong, b.who); err != nil {
			t.Fatal(err)
		}
	}
	fan, err := st.CreatePerson(ctx, "fan", "Fan", "fan@example.com")
	if err != nil {
		t.Fatal(err)
	}
	fanVersion := as(t, st, fan).variantID(as(t, st, fan).do("POST", fmt.Sprintf("/api/variants/%d/fork", f.BandSong), nil))

	// The confirmation.
	w := core.do("GET", fmt.Sprintf("/api/variants/%d/uses?for=private", f.BandSong), nil)
	var uses struct {
		Message                       string
		Songbooks, Setlists, Versions int
	}
	if err := json.Unmarshal(w.Body.Bytes(), &uses); err != nil || w.Code != http.StatusOK {
		t.Fatalf("uses: %d %s", w.Code, w.Body.String())
	}
	want := "Make “Night Drive” private? 2 songbooks link to it, 1 set list lists it and 1 person has their own version. Their songbooks keep a frozen copy"
	if uses.Songbooks != 2 || uses.Setlists != 1 || uses.Versions != 1 || !strings.HasPrefix(uses.Message, want) {
		t.Errorf("uses: %+v", uses)
	}
	if w := as(t, st, f.Guest).do("GET", fmt.Sprintf("/api/variants/%d/uses?for=private", f.BandSong), nil); w.Code != http.StatusOK {
		t.Errorf("a member asking: %d", w.Code)
	}

	if w := core.do("POST", fmt.Sprintf("/api/variants/%d/public", f.BandSong), map[string]bool{"public": false}); w.Code != http.StatusOK {
		t.Fatalf("making it private: %d %s", w.Code, w.Body.String())
	}
	l, _, _, err := st.LoadSetlistByID(ctx, gig)
	if err != nil {
		t.Fatal(err)
	}
	frozen := l.Sets[0].Entries[0].VariantID
	if frozen == f.BandSong {
		t.Fatal("Mercy's set list still lists the private song")
	}
	if owner, from := store.SongOwnerAndOrigin(t, st, frozen); owner != store.BandOwner(mercy) || from != f.BandSong {
		t.Errorf("the frozen copy: owner %+v, from %d", owner, from)
	}
	if n, in, _ := st.SongbookEntryKey(ctx, mercyBook, frozen); !in || n != -2 {
		t.Errorf("the frozen copy in Mercy's songbook: %v, key %d", in, n)
	}
	if fz, err := st.FrozenOf(ctx, frozen); err != nil || fz == nil || fz.From != "Night Drive — Other Lies" {
		t.Errorf("frozen from: %+v %v", fz, err)
	}
	if ids := store.SongbookSongIDs(t, st, outBook); len(ids) != 1 || ids[0] == f.BandSong || ids[0] == frozen {
		t.Errorf("the outsider's songbook: %v", ids)
	} else if owner, _ := store.SongOwnerAndOrigin(t, st, ids[0]); owner != store.PersonOwner(f.Outsider) {
		t.Errorf("the outsider's copy is %+v's", owner)
	}
	if _, in, _ := st.SongbookEntryKey(ctx, guestBook, f.BandSong); !in {
		t.Error("a member's songbook lost the band's song")
	}
	if _, from := store.SongOwnerAndOrigin(t, st, fanVersion); from != f.BandSong {
		t.Error("the fan's version lost its original")
	}
	if html := o.page(fmt.Sprintf("/song/%d", frozen)); !strings.Contains(html, "Frozen from") {
		t.Error("the frozen copy's page doesn't say so")
	}

	// Deleted: the same, the copies following nothing.
	gin, err := st.CreateVariant(ctx, store.OwnerBand, f.Band, "Gin Lane", "Other Lies", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	if w := core.do("POST", fmt.Sprintf("/api/variants/%d/public", gin), map[string]bool{"public": true}); w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	link(gin)
	ginVersion := as(t, st, fan).variantID(as(t, st, fan).do("POST", fmt.Sprintf("/api/variants/%d/fork", gin), nil))
	w = core.do("GET", fmt.Sprintf("/api/variants/%d/uses?for=delete", gin), nil)
	if !strings.Contains(w.Body.String(), "Delete “Gin Lane”? This can’t be undone. 1 songbook links to it and 1 person has their own version.") {
		t.Errorf("uses for delete: %s", w.Body.String())
	}
	if w := as(t, st, f.Outsider).do("DELETE", fmt.Sprintf("/api/songs/%d", gin), nil); w.Code != http.StatusForbidden {
		t.Errorf("Mercy deleting Other Lies's song: %d", w.Code)
	}
	if w := core.do("DELETE", fmt.Sprintf("/api/songs/%d", gin), nil); w.Code != http.StatusOK {
		t.Fatalf("deleting: %d %s", w.Code, w.Body.String())
	}
	var ginCopy int64
	for _, id := range store.SongbookSongIDs(t, st, mercyBook) {
		if fz, _ := st.FrozenOf(ctx, id); fz != nil && fz.From == "Gin Lane — Other Lies" {
			ginCopy = id
		}
	}
	if ginCopy == 0 {
		t.Fatal("Mercy has no frozen copy of the deleted song")
	}
	if _, from := store.SongOwnerAndOrigin(t, st, ginCopy); from != 0 {
		t.Errorf("the copy of a deleted song follows %d", from)
	}
	if _, from := store.SongOwnerAndOrigin(t, st, ginVersion); from != 0 {
		t.Errorf("the fan's version of a deleted song follows %d", from)
	}
}

// TestPublicBandRecordings: a band's recordings of its public song are
// everyone's to hear; on a public song it links, its core members make
// one recording public; a private song's never.
func TestPublicBandRecordings(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	store.AddTestRecording(t, st, f.Band, f.BandSong, false)
	store.AddTestRecording(t, st, f.Band, f.BandSong, false)
	if by, _ := st.PlayedByFor(ctx, []int64{f.BandSong}); len(by[f.BandSong]) != 0 {
		t.Errorf("a private song's recordings are public: %+v", by)
	}
	if w := as(t, st, f.Outsider).do("GET", fmt.Sprintf("/song/%d/recordings", f.BandSong), nil); w.Code != http.StatusForbidden {
		t.Errorf("an outsider opening a private song's recordings: %d", w.Code)
	}
	if err := st.SetSongPublic(ctx, f.Core, f.BandSong, true); err != nil {
		t.Fatal(err)
	}
	by, _ := st.PlayedByFor(ctx, []int64{f.BandSong})
	if len(by[f.BandSong]) != 1 || by[f.BandSong][0].BandName != "Other Lies" || by[f.BandSong][0].Recordings != 2 {
		t.Errorf("played by: %+v", by)
	}
	if html := as(t, st, f.Outsider).page(fmt.Sprintf("/song/%d", f.BandSong)); !strings.Contains(html, "played by:") || !strings.Contains(html, "2 recordings") {
		t.Error("the public song's page doesn't say who plays it")
	}

	// The catalogue's song, which the band links: one recording made
	// public by a core member, not a guest.
	rec := store.AddTestRecording(t, st, f.Band, f.CatalogSong, false)
	path := fmt.Sprintf("/api/bands/%d/recordings/%d/public", f.Band, rec)
	if w := as(t, st, f.Guest).do("POST", path, map[string]bool{"public": true}); w.Code != http.StatusForbidden {
		t.Errorf("a guest making a recording public: %d", w.Code)
	}
	if w := as(t, st, f.Core).do("POST", path, map[string]bool{"public": true}); w.Code != http.StatusOK {
		t.Fatalf("a core member making a recording public: %d %s", w.Code, w.Body.String())
	}
	html := as(t, st, f.Outsider).page(fmt.Sprintf("/song/%d/recordings", f.CatalogSong))
	if !strings.Contains(html, "Other Lies") || !strings.Contains(html, fmt.Sprintf(`data-rp-src="/recordings/%d/audio"`, rec)) || strings.Contains(html, "thecode") {
		t.Error("the song's public recordings page lacks the band's recording")
	}
	if html := as(t, st, f.Outsider).page("/catalog"); !strings.Contains(html, "played by:") {
		t.Error("The Big Songbook doesn't say who plays its song")
	}
	if html := as(t, st, f.Core).page(fmt.Sprintf("/bands/%d/recordings/songs/%d", f.Band, f.CatalogSong)); !strings.Contains(html, "rec-public-box") {
		t.Error("no public switch on the band's recordings of a song it links")
	}
	// The band's own song: public with it, not one by one.
	own := store.AddTestRecording(t, st, f.Band, f.BandSong, false)
	if w := as(t, st, f.Core).do("POST", fmt.Sprintf("/api/bands/%d/recordings/%d/public", f.Band, own), map[string]bool{"public": true}); w.Code != http.StatusBadRequest {
		t.Errorf("making the own song's recording public one by one: %d", w.Code)
	}
}
