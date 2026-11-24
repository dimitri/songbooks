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

// GuestsEditTheSongbook gives the band's guest role edit_songbook — a
// band that lets its guests edit its songs, as every band did before
// migration 0132 — for a test whose guest stands for "another member
// who edits".
func (f BandFixture) GuestsEditTheSongbook(t *testing.T, st *Store) {
	t.Helper()
	if _, err := st.db.Exec(context.Background(), `
		INSERT INTO band_role_privileges (role_id, privilege)
		SELECT id, 'edit_songbook' FROM band_roles WHERE band_id = $1 AND builtin_key = 'guest'
		ON CONFLICT DO NOTHING`, f.Band); err != nil {
		t.Fatal(err)
	}
}

func countSongs(t *testing.T, st *Store) int {
	t.Helper()
	var n int
	if err := st.db.QueryRow(context.Background(), `SELECT count(*) FROM songs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestBandMemberEditsTheBandsSong: a current member whose role grants
// edit_songbook — core; a guest only once the band gives guests the
// songbook (no longer by default: migration 0132) — edits the band's song
// itself (same row, version+1, a revision kept, no new song); a former
// member and an outsider can't.
func TestBandMemberEditsTheBandsSong(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := NewBandFixture(t, st)

	if ok, err := st.CanEditVariant(ctx, f.Guest, f.BandSong); err != nil || ok {
		t.Errorf("a guest edits the band's song by default: %v %v", ok, err)
	}
	f.GuestsEditTheSongbook(t, st)
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

// TestMakeBandVersion: Edit on a song the band links makes the band's
// version (docs/ownership-model.md §5): a new song, the band's, private,
// forked_from_id the original, in the link's place in the band's
// songbook (its key kept) and set list (a revision recorded), the
// original untouched; New version from this on the band's own song adds a
// named one beside it, nothing replaced.
func TestMakeBandVersion(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := NewBandFixture(t, st)
	f.GuestsEditTheSongbook(t, st) // the guest: another member who edits
	sb, err := st.GetDefaultSongbook(ctx, "band", f.Band)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetEntrySemitones(ctx, sb, f.CatalogSong, 3); err != nil {
		t.Fatal(err)
	}

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
	if _, err := st.MakeVersion(ctx, f.Outsider, f.CatalogSong, BandOwner(f.Band), 0, ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("an outsider making the band's version: %v", err)
	}

	_, origVersion, _ := st.LoadVariantRaw(f.CatalogSong)
	newID, err := st.MakeVersion(ctx, f.Guest, f.CatalogSong, BandOwner(f.Band), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	v, err := st.GetVariant(ctx, newID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Visibility != "band" || v.Public || v.OwnerBandID == nil || *v.OwnerBandID != f.Band || v.ForkedFromID == nil || *v.ForkedFromID != f.CatalogSong {
		t.Errorf("the band's version: %+v", v)
	}
	if ok, _ := st.CanEditVariant(ctx, f.Guest, newID); !ok {
		t.Error("a guest member can't edit the band's version")
	}
	if semis, in, _ := st.SongbookEntryKey(ctx, sb, newID); !in || semis != 3 {
		t.Errorf("the version in the songbook: %v, key %d (want 3)", in, semis)
	}
	if _, in, _ := st.SongbookEntryKey(ctx, sb, f.CatalogSong); in {
		t.Error("the link is still in the songbook beside the version")
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

	// New version from this: a second, named, beside it.
	songs := countSongs(t, st)
	psych, err := st.MakeVersion(ctx, f.Guest, newID, BandOwner(f.Band), sb, "psychedelic")
	if err != nil {
		t.Fatal(err)
	}
	pv, _ := st.GetVariant(ctx, psych)
	if pv == nil || pv.VersionName != "psychedelic" || pv.ForkedFromID == nil || *pv.ForkedFromID != newID {
		t.Errorf("the new version: %+v", pv)
	}
	for _, id := range []int64{newID, psych} {
		if _, in, _ := st.SongbookEntryKey(ctx, sb, id); !in {
			t.Errorf("song %d isn't in the band's songbook: one row per version", id)
		}
	}
	if n := countSongs(t, st); n != songs+1 {
		t.Errorf("%d songs, want %d", n, songs+1)
	}
	if _, err := st.MakeVersion(ctx, f.Outsider, newID, BandOwner(f.Band), sb, "mine"); !errors.Is(err, ErrForbidden) {
		t.Errorf("an outsider's new version of the band's song: %v", err)
	}
}

// TestAnotherBandsPrivateSongIsRefused: a band links public songs and its
// own (docs/ownership-model.md §4): another band's private song is
// refused — naming the band and the catalogue's song of the same work
// when there is one — as a link and as a version; once that band makes it
// public, it's a link like any public song. A community's private song
// too; a person links what they see.
func TestAnotherBandsPrivateSongIsRefused(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := NewBandFixture(t, st)
	other, err := st.CreateBand(ctx, "Mercy", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	original, err := st.CreateVariant(ctx, OwnerBand, other, "Theirs", "Mercy", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	cover, err := st.MakeVersion(ctx, f.Core, f.CatalogSong, BandOwner(other), 0, "")
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

	songs := CountSongs(t, st)
	for _, c := range []struct {
		id, catalog int64
		owner       string
	}{{original, 0, "Mercy"}, {cover, f.CatalogSong, "Mercy"}, {clubs, 0, "Jam Club"}} {
		err := st.LinkCheckFor(ctx, f.Core, c.id, BandOwner(f.Band))
		var refused *LinkRefusal
		if !errors.As(err, &refused) || !errors.Is(err, ErrForbidden) || refused.Reason != "private" || refused.OwnerName != c.owner || refused.CatalogueSongID != c.catalog {
			t.Errorf("linking %s's song %d: %v (%+v), want refused, catalogue song %d", c.owner, c.id, err, refused, c.catalog)
		}
		if _, err := st.MakeVersion(ctx, f.Core, c.id, BandOwner(f.Band), 0, ""); !errors.As(err, &refused) {
			t.Errorf("the band's version of %s's song %d: %v, want refused", c.owner, c.id, err)
		}
		// A person links what they see.
		if err := st.LinkCheckFor(ctx, f.Core, c.id, PersonOwner(f.Core)); err != nil {
			t.Errorf("Core linking song %d in their own songbook: %v", c.id, err)
		}
	}
	if err := st.LinkCheckFor(ctx, f.Core, original, BandOwner(other)); err != nil {
		t.Errorf("Mercy linking its own song: %v", err)
	}
	if n := CountSongs(t, st); n != songs {
		t.Errorf("%d songs after the refusals, want %d", n, songs)
	}

	// Public, it's a link like any.
	if err := st.SetSongPublic(ctx, f.Core, original, true); err != nil {
		t.Fatal(err)
	}
	if err := st.LinkCheckFor(ctx, f.Guest, original, BandOwner(f.Band)); err != nil {
		t.Errorf("linking Mercy's public song: %v", err)
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
