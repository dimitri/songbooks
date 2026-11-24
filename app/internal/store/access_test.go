package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// accessFixture: people in every role, and a song of every owner kind.
type accessFixture struct {
	admin, alice, bob, core, guest, former, staff, member, cguest, student, outsider int64
	band, otherBand, community                                                       int64
	aliceSong, bandSong, communitySong, closedSong, catalogueSong                    int64
	closedBook, openBook                                                             int64
}

func newAccessFixture(t *testing.T, st *Store) accessFixture {
	t.Helper()
	ctx := context.Background()
	var f accessFixture
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	id := func(v int64, err error) int64 {
		t.Helper()
		must(err)
		return v
	}
	person := func(name string) int64 {
		return id(st.CreatePerson(ctx, strings.ToUpper(name[:1])+name[1:], name+"@example.com"))
	}
	f.admin, f.alice, f.bob = person("admin"), person("alice"), person("bob")
	f.core, f.guest, f.former = person("core"), person("guest"), person("former")
	f.staff, f.member, f.cguest, f.student, f.outsider = person("staff"), person("member"), person("cguest"), person("student"), person("outsider")
	must(st.SetAdmin(ctx, f.admin, true))

	f.band = id(st.CreateBand(ctx, "Mercy", f.core))
	_, err := st.AddBandMember(ctx, f.band, f.guest, "guest", f.core)
	must(err)
	_, err = st.AddBandMember(ctx, f.band, f.former, "core", f.core)
	must(err)
	must(st.RemoveBandMember(ctx, f.band, f.former, f.core))
	f.otherBand = id(st.CreateBand(ctx, "Other Lies", f.bob))

	f.community = id(st.CreateCommunity(ctx, "Florent", f.staff))
	for who, role := range map[int64]string{f.member: "member", f.cguest: "guest", f.student: "member"} {
		_, err := st.AddCommunityMember(ctx, f.community, who, role, f.staff)
		must(err)
	}

	f.aliceSong = id(st.CreateVariant(ctx, OwnerPerson, f.alice, "Alice's", "", f.alice))
	f.bandSong = id(st.CreateVariant(ctx, OwnerBand, f.band, "Bad Spell", "Mercy", f.core))
	f.communitySong = id(st.CreateVariant(ctx, OwnerCommunity, f.community, "Jam 1", "", f.staff))
	f.closedSong = id(st.CreateVariant(ctx, OwnerCommunity, f.community, "Leçon 1", "", f.staff))
	f.catalogueSong = id(st.CreateVariant(ctx, OwnerCatalog, 0, "Le Vent", "Someone", f.admin))
	f.openBook = id(st.GetDefaultSongbook(ctx, "community", f.community))
	f.closedBook = id(st.CreateSongbook(ctx, "community", f.community, "La méthode", false))
	must(st.RemoveFromSongbook(ctx, f.openBook, f.closedSong))
	must(st.AddToSongbook(ctx, f.closedBook, f.closedSong, f.staff))
	must(st.SetSongbookClosed(ctx, f.closedBook, true))
	return f
}

// TestSongRightsMatrix: the one rule (docs/ownership-model.md §2, §4,
// §5), person × owner kind × public or private × closed × role: see,
// edit, publish, link from a person's / a band's / a community's
// songbook, make a version.
func TestSongRightsMatrix(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := newAccessFixture(t, st)

	type want struct {
		see, edit, publish                  bool
		linkPerson, linkBand, linkCommunity bool // from who's own songbook, the other band's, another community's
		version                             bool // their own version (a person's)
	}
	other, err := st.CreateCommunity(ctx, "Elsewhere", f.bob)
	if err != nil {
		t.Fatal(err)
	}
	check := func(label string, song, who int64, w want) {
		t.Helper()
		r, err := st.SongRightsFor(ctx, who, song)
		if err != nil {
			t.Fatal(err)
		}
		got := want{
			see: r.See, edit: r.Edit, publish: r.Publish,
			linkPerson: r.CanLinkFrom(PersonOwner(who)), linkBand: r.CanLinkFrom(BandOwner(f.otherBand)),
			linkCommunity: r.CanLinkFrom(CommunityOwner(other)), version: r.CanVersionFor(PersonOwner(who)),
		}
		if got != w {
			t.Errorf("%s: got %+v, want %+v", label, got, w)
		}
	}
	none := want{}
	seeOnly := want{see: true, linkPerson: true, version: true}
	publicOnly := want{see: true, linkPerson: true, linkBand: true, linkCommunity: true, version: true}

	for _, public := range []bool{false, true} {
		if public {
			for _, s := range []struct{ who, song int64 }{{f.alice, f.aliceSong}, {f.core, f.bandSong}, {f.staff, f.communitySong}} {
				if err := st.SetSongPublic(ctx, s.who, s.song, true); err != nil {
					t.Fatal(err)
				}
			}
		}
		pp := fmt.Sprintf("public %v", public)
		others := none
		if public {
			others = publicOnly
		}
		// A person's song.
		owner := want{see: true, edit: true, publish: true, linkPerson: true, version: true, linkBand: public, linkCommunity: public}
		check(pp+", Alice's own song, Alice", f.aliceSong, f.alice, owner)
		check(pp+", Alice's own song, Bob", f.aliceSong, f.bob, others)
		check(pp+", Alice's own song, an admin", f.aliceSong, f.admin, others)
		check(pp+", Alice's own song, no one signed in", f.aliceSong, 0, none)

		// A band's song: core edits and publishes; a guest sees it, no
		// longer edits it (migration 0132).
		core := want{see: true, edit: true, publish: true, linkPerson: true, version: true, linkBand: public, linkCommunity: public}
		guest := core
		guest.publish, guest.edit = false, false
		check(pp+", the band's song, a core member", f.bandSong, f.core, core)
		check(pp+", the band's song, a guest member", f.bandSong, f.guest, guest)
		check(pp+", the band's song, a former member", f.bandSong, f.former, others)
		check(pp+", the band's song, an outsider", f.bandSong, f.outsider, others)

		// A community's song: who has edit_repertoire edits, staff
		// publish, a guest sees.
		staff := want{see: true, edit: true, publish: true, linkPerson: true, version: true, linkBand: public, linkCommunity: public}
		member := staff
		member.publish = false
		cguest := member
		cguest.edit = false
		check(pp+", the community's song, staff", f.communitySong, f.staff, staff)
		check(pp+", the community's song, a member", f.communitySong, f.member, member)
		check(pp+", the community's song, a guest", f.communitySong, f.cguest, cguest)
		check(pp+", the community's song, an outsider", f.communitySong, f.outsider, others)
	}

	// The catalogue (The Big Songbook's, public): everyone sees it, links
	// it, makes their own version; the site's admins edit and publish.
	check("the catalogue's song, an admin", f.catalogueSong, f.admin, want{see: true, edit: true, publish: true, linkPerson: true, linkBand: true, linkCommunity: true, version: true})
	check("the catalogue's song, anyone", f.catalogueSong, f.outsider, publicOnly)
	check("the catalogue's song, no one signed in", f.catalogueSong, 0, none)

	// A closed songbook's song: opened by the community's members, edited
	// by who edits its repertoire; never published, linked or made a
	// version of anywhere else.
	closedStaff := want{see: true, edit: true}
	check("closed, staff", f.closedSong, f.staff, closedStaff)
	check("closed, a member", f.closedSong, f.member, closedStaff)
	check("closed, a guest", f.closedSong, f.cguest, want{see: true})
	check("closed, an outsider", f.closedSong, f.outsider, none)
	r, err := st.SongRightsFor(ctx, f.staff, f.closedSong)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Closed || !r.CanLinkFrom(CommunityOwner(f.community)) {
		t.Errorf("closed: %+v — the community's own songbooks still hold it", r)
	}
	if err := st.SetSongPublic(ctx, f.staff, f.closedSong, true); !errors.Is(err, ErrForbidden) {
		t.Errorf("publishing a closed songbook's song: %v", err)
	}
	if ok, _ := st.CanViewVariant(ctx, f.student, f.closedSong); !ok {
		t.Error("a student can't open the closed songbook's song")
	}
	_ = seeOnly
}

// TestSongVisibleMatchesTheRule: song_visible (the lists' and search's
// "see") says what SongRightsFor says, for every song and person.
func TestSongVisibleMatchesTheRule(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := newAccessFixture(t, st)
	if err := st.SetSongPublic(ctx, f.core, f.bandSong, true); err != nil {
		t.Fatal(err)
	}
	for _, song := range []int64{f.aliceSong, f.bandSong, f.communitySong, f.closedSong, f.catalogueSong} {
		for _, who := range []int64{0, f.admin, f.alice, f.bob, f.core, f.guest, f.former, f.staff, f.member, f.cguest, f.student, f.outsider} {
			var visible bool
			if err := st.db.QueryRow(ctx, `SELECT song_visible($1, $2)`, song, who).Scan(&visible); err != nil {
				t.Fatal(err)
			}
			r, err := st.SongRightsFor(ctx, who, song)
			if err != nil {
				t.Fatal(err)
			}
			if visible != r.See {
				t.Errorf("song %d, person %d: song_visible %v, SongRightsFor %v", song, who, visible, r.See)
			}
		}
	}
}

// TestPublishing: who makes a song or a songbook public (§2), and what
// goes with a songbook: its owner's songs, not the songs it links; a
// public songbook opens for everyone signed in; a closed one is never
// public.
func TestPublishing(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := newAccessFixture(t, st)

	for _, c := range []struct {
		label     string
		who, song int64
		ok        bool
	}{
		{"Alice, her song", f.alice, f.aliceSong, true},
		{"Bob, Alice's song", f.bob, f.aliceSong, false},
		{"a core member, the band's song", f.core, f.bandSong, true},
		{"a guest member, the band's song", f.guest, f.bandSong, false},
		{"staff, the community's song", f.staff, f.communitySong, true},
		{"a member with edit_repertoire, the community's song", f.member, f.communitySong, false},
		{"an admin, the catalogue's song", f.admin, f.catalogueSong, true},
		{"anyone, the catalogue's song", f.outsider, f.catalogueSong, false},
		{"staff, the closed songbook's song", f.staff, f.closedSong, false},
	} {
		err := st.SetSongPublic(ctx, c.who, c.song, false)
		if c.ok != (err == nil) || (!c.ok && !errors.Is(err, ErrForbidden)) {
			t.Errorf("%s: %v", c.label, err)
		}
	}
	if err := st.SetSongPublic(ctx, f.admin, f.catalogueSong, true); err != nil {
		t.Fatal(err)
	}

	// The band's songbook, public: the band's song in it goes public;
	// the catalogue's song it links stays as it is; an outsider opens it.
	book, err := st.GetDefaultSongbook(ctx, "band", f.band)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddBandCatalogEntry(ctx, f.band, f.catalogueSong, f.core); err != nil {
		t.Fatal(err)
	}
	if ok, _ := st.CanAccessSongbook(ctx, f.outsider, book); ok {
		t.Fatal("an outsider opens the band's private songbook")
	}
	if err := st.SetSongbookPublic(ctx, f.guest, book, true); !errors.Is(err, ErrForbidden) {
		t.Errorf("a guest member publishing the band's songbook: %v", err)
	}
	if err := st.SetSongbookPublic(ctx, f.core, book, true); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.GetVariant(ctx, f.bandSong); !v.Public {
		t.Error("the band's song didn't go public with its songbook")
	}
	if ok, _ := st.CanAccessSongbook(ctx, f.outsider, book); !ok {
		t.Error("an outsider can't open the public songbook")
	}
	if ok, _ := st.CanEditSongbook(ctx, f.outsider, book); ok {
		t.Error("an outsider can change the public songbook")
	}

	// A closed songbook is never public, nor closed while public.
	if err := st.SetSongbookPublic(ctx, f.staff, f.closedBook, true); !errors.Is(err, ErrForbidden) {
		t.Errorf("publishing a closed songbook: %v", err)
	}
	if err := st.SetSongbookPublic(ctx, f.staff, f.openBook, true); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSongbookClosed(ctx, f.openBook, true); !errors.Is(err, ErrConflict) {
		t.Errorf("closing a public songbook: %v", err)
	}
}
