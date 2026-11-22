package store

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"app/internal/setlist"
)

// BandFixture: a band (creator core), a guest member, a former member, an
// outsider, the band's own song, a catalogue song in the band's songbook
// and in its set list.
type BandFixture struct {
	Band, Core, Guest, Former, Outsider int64
	BandSong, CatalogSong, SetlistID    int64
}

func NewBandFixture(t *testing.T, st *Store) BandFixture {
	t.Helper()
	ctx := context.Background()
	var f BandFixture
	person := func(name string) int64 {
		id, err := st.CreatePerson(ctx, name, strings.ToUpper(name[:1])+name[1:], name+"@example.com")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.Core, f.Guest, f.Former, f.Outsider = person("core"), person("guest"), person("former"), person("outsider")
	var err error
	if f.Band, err = st.CreateBand(ctx, "Other Lies", f.Core); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBandMember(ctx, f.Band, f.Guest, "guest", f.Core); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBandMember(ctx, f.Band, f.Former, "core", f.Core); err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveBandMember(ctx, f.Band, f.Former, f.Core); err != nil {
		t.Fatal(err)
	}
	if f.BandSong, err = st.CreateVariant(ctx, OwnerBand, f.Band, "Night Drive", "Other Lies", f.Core); err != nil {
		t.Fatal(err)
	}
	if f.CatalogSong, err = st.CreateVariant(ctx, OwnerCatalog, 0, "Le Vent", "Someone", f.Core); err != nil {
		t.Fatal(err)
	}
	if err := st.AddBandCatalogEntry(ctx, f.Band, f.CatalogSong, f.Core); err != nil {
		t.Fatal(err)
	}
	band := f.Band
	if f.SetlistID, err = st.CreateSetlist(ctx, "band", "Friday", "", nil, &band, nil, f.Core); err != nil {
		t.Fatal(err)
	}
	l := &setlist.Setlist{Sets: []setlist.Set{{Label: "Set 1", Entries: []setlist.Entry{{VariantID: f.BandSong}, {VariantID: f.CatalogSong}}}}}
	if _, err := st.SaveSetlistByID(ctx, f.SetlistID, l, "Friday", "", "", f.Core); err != nil {
		t.Fatal(err)
	}
	return f
}

func countSongs(t *testing.T, st *Store) int {
	t.Helper()
	var n int
	if err := st.db.QueryRow(context.Background(), `SELECT count(*) FROM songs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestBandMemberEditsTheBandsSong: a current member, core or guest, edits
// the band's song itself (same row, version+1, a revision kept, no new
// song); a former member and an outsider can't.
func TestBandMemberEditsTheBandsSong(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := NewBandFixture(t, st)

	for _, c := range []struct {
		name string
		who  int64
		want bool
	}{{"core", f.Core, true}, {"guest", f.Guest, true}, {"former", f.Former, false}, {"outsider", f.Outsider, false}} {
		ok, err := st.CanEditVariant(ctx, c.who, f.BandSong)
		if err != nil {
			t.Fatal(err)
		}
		if ok != c.want {
			t.Errorf("%s: CanEditVariant = %v, want %v", c.name, ok, c.want)
		}
	}

	songs := countSongs(t, st)
	_, version, err := st.LoadVariantRaw(f.BandSong)
	if err != nil {
		t.Fatal(err)
	}
	for _, who := range []int64{f.Core, f.Guest} {
		newVersion, err := st.SaveVariantRaw(f.BandSong, "title: Night Drive\nartist: Other Lies\n\n[Verse]\nC G\n", version, who)
		if err != nil {
			t.Fatal(err)
		}
		v, _ := strconv.Atoi(version)
		if newVersion != strconv.Itoa(v+1) {
			t.Errorf("version %s after a save from %s, want %d", newVersion, version, v+1)
		}
		version = newVersion
	}
	revs, err := st.ListRevisions(ctx, "variant", strconv.FormatInt(f.BandSong, 10))
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 {
		t.Errorf("%d revisions after two saves, want 2", len(revs))
	}
	if n := countSongs(t, st); n != songs {
		t.Errorf("%d songs after the members' saves, want %d (no copy)", n, songs)
	}
}

// TestBandVersionsOf: a member's own copy of the band's song points at the
// band's version — through forked_from_id, or the same work — and stops
// when they leave the band; someone else's song never does.
func TestBandVersionsOf(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := NewBandFixture(t, st)

	copyID, err := st.ForkVariant(ctx, f.BandSong, f.Guest, "user", 0)
	if err != nil {
		t.Fatal(err)
	}
	sameWork, err := st.CreateVariant(ctx, OwnerPerson, f.Guest, "Night Drive", "Other Lies", f.Guest)
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := st.CreateVariant(ctx, OwnerPerson, f.Guest, "Something Else", "", f.Guest)
	if err != nil {
		t.Fatal(err)
	}
	outsiders, err := st.CreateVariant(ctx, OwnerPerson, f.Outsider, "Night Drive", "Other Lies", f.Outsider)
	if err != nil {
		t.Fatal(err)
	}

	got, err := st.BandVersionsOf(ctx, f.Guest, []int64{copyID, sameWork, unrelated, f.BandSong})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{copyID, sameWork} {
		v := got[id]
		if v == nil || v.BandID != f.Band || v.BandVariantID != f.BandSong || v.BandName != "Other Lies" {
			t.Errorf("song %d: band version %+v, want band %d's song %d", id, v, f.Band, f.BandSong)
		}
	}
	if got[unrelated] != nil || got[f.BandSong] != nil {
		t.Errorf("unexpected band versions: %+v", got)
	}
	if v, err := st.BandVersionOf(ctx, f.Outsider, outsiders); err != nil || v != nil {
		t.Errorf("an outsider's song: %+v, %v; want none", v, err)
	}

	if err := st.RemoveBandMember(ctx, f.Band, f.Guest, f.Core); err != nil {
		t.Fatal(err)
	}
	if v, err := st.BandVersionOf(ctx, f.Guest, copyID); err != nil || v != nil {
		t.Errorf("after leaving the band: %+v, %v; want none", v, err)
	}
}

// TestAdoptIntoBand: the catalogue's song a band uses becomes the band's
// copy — songbook entry and set list item repointed, the original
// untouched — once; adopting again, or the band's own song, changes
// nothing.
func TestAdoptIntoBand(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := NewBandFixture(t, st)

	holds, err := st.BandsHolding(ctx, f.Guest, f.CatalogSong)
	if err != nil || len(holds) != 1 || holds[0].BandID != f.Band || holds[0].OwnSongID != 0 {
		t.Fatalf("BandsHolding before: %+v, %v", holds, err)
	}
	if holds, _ := st.BandsHolding(ctx, f.Outsider, f.CatalogSong); len(holds) != 0 {
		t.Errorf("an outsider's BandsHolding: %+v", holds)
	}
	if holds, _ := st.BandsHolding(ctx, f.Guest, f.BandSong); len(holds) != 0 {
		t.Errorf("the band's own song is held: %+v", holds)
	}
	plan, err := st.PlanBandAdoptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 1 || plan[0].VariantID != f.CatalogSong || plan[0].Action != "copy" || plan[0].Entries != 1 || plan[0].SetlistItems != 1 {
		t.Fatalf("plan before: %+v", plan)
	}

	_, origVersion, _ := st.LoadVariantRaw(f.CatalogSong)
	newID, created, err := st.AdoptIntoBand(ctx, f.Band, f.CatalogSong, f.Guest)
	if err != nil {
		t.Fatal(err)
	}
	if !created || newID == f.CatalogSong {
		t.Fatalf("AdoptIntoBand = %d, %v", newID, created)
	}
	v, err := st.GetVariant(ctx, newID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Visibility != "band" || v.OwnerBandID == nil || *v.OwnerBandID != f.Band || v.ForkedFromID == nil || *v.ForkedFromID != f.CatalogSong {
		t.Errorf("the band's copy: %+v", v)
	}
	if ok, _ := st.CanEditVariant(ctx, f.Guest, newID); !ok {
		t.Error("a guest member can't edit the band's copy")
	}
	entries, err := st.ListBandCatalog(ctx, f.Band)
	if err != nil {
		t.Fatal(err)
	}
	var inRep []int64
	for _, e := range entries {
		inRep = append(inRep, e.VariantID)
	}
	if !containsID(inRep, newID) || containsID(inRep, f.CatalogSong) {
		t.Errorf("repertoire %v: want the copy %d, not the catalogue's %d", inRep, newID, f.CatalogSong)
	}
	l, _, _, err := st.LoadSetlistByID(ctx, f.SetlistID)
	if err != nil {
		t.Fatal(err)
	}
	if got := SetlistVariantIDs(l); len(got) != 2 || got[0] != f.BandSong || got[1] != newID {
		t.Errorf("set list %v, want [%d %d]", got, f.BandSong, newID)
	}
	revs, _ := st.ListRevisions(ctx, "setlist", strconv.FormatInt(f.SetlistID, 10))
	if len(revs) != 3 { // created, saved, repointed
		t.Errorf("%d set list revisions, want 3", len(revs))
	}
	if _, version, _ := st.LoadVariantRaw(f.CatalogSong); version != origVersion {
		t.Errorf("the catalogue's song changed: version %s, was %s", version, origVersion)
	}
	if holds, _ := st.BandsHolding(ctx, f.Guest, f.CatalogSong); len(holds) != 0 {
		t.Errorf("BandsHolding after: %+v", holds)
	}
	if plan, _ := st.PlanBandAdoptions(ctx); len(plan) != 0 {
		t.Errorf("plan after: %+v", plan)
	}

	// Once: again reuses the copy, and the band's song is its own.
	songs := countSongs(t, st)
	if again, created, err := st.AdoptIntoBand(ctx, f.Band, f.CatalogSong, f.Guest); err != nil || created || again != newID {
		t.Errorf("adopting again: %d, %v, %v; want %d, false", again, created, err, newID)
	}
	if same, created, err := st.AdoptIntoBand(ctx, f.Band, f.BandSong, f.Guest); err != nil || created || same != f.BandSong {
		t.Errorf("adopting the band's song: %d, %v, %v", same, created, err)
	}
	if n := countSongs(t, st); n != songs {
		t.Errorf("%d songs, want %d", n, songs)
	}
}

// TestAnotherBandsSongIsRefused: a band never takes another band's song
// (an original band's songs stay theirs; covers come from the catalogue).
// The plan leaves it, reported, as it leaves a community's song;
// AdoptIntoBand refuses it, naming the band and the work's catalogue
// version when there is one; nothing changes.
func TestAnotherBandsSongIsRefused(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := NewBandFixture(t, st)
	other, err := st.CreateBand(ctx, "Mercy", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	// Mercy's original, and Mercy's copy of the catalogue's song.
	original, err := st.CreateVariant(ctx, OwnerBand, other, "Theirs", "Mercy", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	cover, _, err := st.AdoptIntoBand(ctx, other, f.CatalogSong, f.Core)
	if err != nil {
		t.Fatal(err)
	}
	community, err := st.CreateCommunity(ctx, "Jam Club", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	clubs, err := st.CreateCommunityVariant(ctx, community, "Club Song", "", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	l, _, version, err := st.LoadSetlistByID(ctx, f.SetlistID)
	if err != nil {
		t.Fatal(err)
	}
	l.Sets[0].Entries = append(l.Sets[0].Entries, setlist.Entry{VariantID: original}, setlist.Entry{VariantID: clubs})
	if _, err := st.SaveSetlistByID(ctx, f.SetlistID, l, "Friday", "", version, f.Core); err != nil {
		t.Fatal(err)
	}
	plan, err := st.PlanBandAdoptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[int64]string{}
	for _, a := range plan {
		if a.BandID == f.Band {
			actions[a.VariantID] = a.Action
		}
	}
	if len(actions) != 3 || actions[original] != "skip" || actions[clubs] != "skip" || actions[f.CatalogSong] != "copy" {
		t.Errorf("plan %v: want another band's and the community's songs left, the catalogue's copied", actions)
	}

	songs := CountSongs(t, st)
	before := SetlistVariantIDs(mustSetlist(t, st, f.SetlistID))
	for _, c := range []struct {
		id, catalog int64
	}{{original, 0}, {cover, f.CatalogSong}} {
		_, _, err := st.AdoptIntoBand(ctx, f.Band, c.id, f.Core)
		var refused *OtherBandSongError
		if !errors.As(err, &refused) || !errors.Is(err, ErrForbidden) || refused.BandName != "Mercy" || refused.CatalogVariantID != c.catalog {
			t.Errorf("adopting Mercy's song %d: %v (%+v), want refused, catalogue version %d", c.id, err, refused, c.catalog)
		}
		if err := st.BandMayTake(ctx, f.Band, c.id); !errors.As(err, &refused) {
			t.Errorf("BandMayTake(Mercy's song %d) = %v, want refused", c.id, err)
		}
	}
	if err := st.BandMayTake(ctx, other, original); err != nil {
		t.Errorf("Mercy taking its own song: %v", err)
	}
	if n := CountSongs(t, st); n != songs {
		t.Errorf("%d songs after the refusals, want %d", n, songs)
	}
	if got := SetlistVariantIDs(mustSetlist(t, st, f.SetlistID)); len(got) != len(before) {
		t.Errorf("set list changed: %v, was %v", got, before)
	}
}

func mustSetlist(t *testing.T, st *Store, id int64) *setlist.Setlist {
	t.Helper()
	l, _, _, err := st.LoadSetlistByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// TestVariantOwnerOf names whose song it is, for the "you can't change
// this song" dialog.
func TestVariantOwnerOf(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := NewBandFixture(t, st)
	mine, err := st.CreateVariant(ctx, OwnerPerson, f.Guest, "Mine", "", f.Guest)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		id         int64
		kind, name string
	}{{f.BandSong, "band", "Other Lies"}, {f.CatalogSong, "global", ""}, {mine, "user", "Guest"}} {
		o, err := st.VariantOwnerOf(ctx, c.id)
		if err != nil || o.Kind != c.kind || o.Name != c.name {
			t.Errorf("song %d: %+v, %v; want %s %q", c.id, o, err, c.kind, c.name)
		}
	}
}

// NewTestStore is newTestStore, for this directory's external tests
// (band_edit_http_test.go, package store_test, with NewBandFixture above):
// they drive the HTTP API against the same test database, so they live in
// this package's test binary and run with its other tests, never beside
// them (newTestStore empties the tables).
var NewTestStore = newTestStore

// CountSongs is countSongs, for the external tests.
var CountSongs = countSongs
