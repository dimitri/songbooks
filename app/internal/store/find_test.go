package store

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"app/internal/setlist"
)

// found indexes FindSongs' songs by id.
func found(t *testing.T, st *Store, person int64, f FindFilter) map[int64]FoundSong {
	t.Helper()
	works, err := st.FindSongs(context.Background(), person, f)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]FoundSong{}
	for _, w := range works {
		for _, s := range w.Songs {
			if s.WorkID != w.WorkID {
				t.Errorf("song %d of work %d listed under work %d", s.ID, s.WorkID, w.WorkID)
			}
			out[s.ID] = s
		}
	}
	return out
}

// TestFindSongsPerRole: the union of everything a person sees
// (song_visible), each song with whose it is and who sees it, per role.
func TestFindSongsPerRole(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := newAccessFixture(t, st)
	// Mercy plays originals, and made one of them public.
	if _, err := st.db.Exec(ctx, `UPDATE bands SET music_type = 'original' WHERE id = $1`, f.band); err != nil {
		t.Fatal(err)
	}
	original, err := st.CreateVariant(ctx, OwnerBand, f.band, "Our Own", "Mercy", f.core)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSongPublic(ctx, f.core, original, true); err != nil {
		t.Fatal(err)
	}

	type want struct {
		seen                                      bool
		owner                                     string
		mine, catalogue, original, public, closed bool
	}
	for _, c := range []struct {
		who  string
		id   int64
		want map[int64]want
	}{
		{"alice", f.alice, map[int64]want{
			f.aliceSong: {seen: true, owner: "Alice", mine: true}, f.catalogueSong: {seen: true, owner: "The Big Songbook", catalogue: true, public: true},
			original:   {seen: true, owner: "Mercy", original: true, public: true},
			f.bandSong: {}, f.communitySong: {}, f.closedSong: {},
		}},
		{"a guest of Mercy", f.guest, map[int64]want{
			f.bandSong: {seen: true, owner: "Mercy", original: true}, f.aliceSong: {}, f.communitySong: {},
		}},
		{"a former member", f.former, map[int64]want{f.bandSong: {}}},
		{"a community member", f.member, map[int64]want{
			f.communitySong: {seen: true, owner: "Florent"}, f.closedSong: {seen: true, owner: "Florent", closed: true}, f.bandSong: {},
		}},
		{"an outsider", f.outsider, map[int64]want{f.catalogueSong: {seen: true, catalogue: true, public: true, owner: "The Big Songbook"}, f.bandSong: {}, f.closedSong: {}}},
	} {
		got := found(t, st, c.id, FindFilter{})
		for id, w := range c.want {
			s, ok := got[id]
			if ok != w.seen {
				t.Errorf("%s: song %d listed %v, want %v", c.who, id, ok, w.seen)
				continue
			}
			if !ok {
				continue
			}
			if s.OwnerName != w.owner || s.Mine != w.mine || s.Catalogue != w.catalogue || s.Original != w.original || s.Public != w.public || s.Closed != w.closed {
				t.Errorf("%s: song %d is %+v, want %+v", c.who, id, s, w)
			}
		}
	}
}

// TestFindSongsFilters: All / Mine / My bands / My communities / Public,
// the query, and the versions of one work grouped, the person's first.
func TestFindSongsFilters(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := newAccessFixture(t, st)
	if _, err := st.AddCommunityMember(ctx, f.community, f.core, "member", f.staff); err != nil {
		t.Fatal(err)
	}
	mine, err := st.CreateVariant(ctx, OwnerPerson, f.core, "Le Vent", "Someone", f.core)
	if err != nil {
		t.Fatal(err)
	}
	for scope, want := range map[string][]int64{
		FindAll:         {mine, f.bandSong, f.communitySong, f.closedSong, f.catalogueSong},
		FindMine:        {mine},
		FindBands:       {f.bandSong},
		FindCommunities: {f.communitySong, f.closedSong},
		FindPublic:      {f.catalogueSong},
	} {
		got := found(t, st, f.core, FindFilter{Scope: scope})
		if len(got) != len(want) {
			t.Errorf("scope %q: %d songs, want %d (%v)", scope, len(got), len(want), got)
		}
		for _, id := range want {
			if _, ok := got[id]; !ok {
				t.Errorf("scope %q: song %d missing", scope, id)
			}
		}
	}
	// "le vent": the person's own version and the catalogue's, one work,
	// theirs first.
	works, err := st.FindSongs(ctx, f.core, FindFilter{Query: "le vent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 1 || len(works[0].Songs) != 2 || works[0].Songs[0].ID != mine || works[0].Songs[1].ID != f.catalogueSong {
		t.Errorf("\"le vent\": %+v, want one work, core's version then the catalogue's", works)
	}
	if got := found(t, st, f.core, FindFilter{Query: "bad spe"}); len(got) != 1 || got[f.bandSong].ID == 0 {
		t.Errorf("\"bad spe\": %v", got)
	}
	// The Big Songbook's page: public songs, by where they come from.
	if err := st.SetSongPublic(ctx, f.core, f.bandSong, true); err != nil {
		t.Fatal(err)
	}
	for from, want := range map[string]int64{FromCatalogue: f.catalogueSong, FromBands: f.bandSong} {
		got := found(t, st, f.outsider, FindFilter{OnlyPublic: true, From: from})
		if len(got) != 1 || got[want].ID == 0 {
			t.Errorf("from %q: %v, want song %d alone", from, got, want)
		}
	}
	if got := found(t, st, f.core, FindFilter{OnlyPublic: true, From: FromCommunities}); len(got) != 0 {
		t.Errorf("from communities: %v, want none public", got)
	}
}

// TestFindSongsForASongbook: the Add dialog's search for a songbook says
// what's in it and what it may link — never a closed songbook's song
// elsewhere, a band's private song only in its own songbooks.
func TestFindSongsForASongbook(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := newAccessFixture(t, st)
	if _, err := st.AddCommunityMember(ctx, f.community, f.core, "member", f.staff); err != nil {
		t.Fatal(err)
	}
	bandBook, err := st.GetDefaultSongbook(ctx, "band", f.band)
	if err != nil {
		t.Fatal(err)
	}
	coreBook, err := st.GetDefaultSongbook(ctx, "person", f.core)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		book     int64
		song     int64
		in, link bool
		refusal  string
	}{
		{bandBook, f.bandSong, true, true, ""},
		{bandBook, f.catalogueSong, false, true, ""},
		{bandBook, f.communitySong, false, false, "private"},
		{bandBook, f.closedSong, false, false, "closed"},
		{coreBook, f.bandSong, false, true, ""}, // a person links what they see
		{coreBook, f.communitySong, false, true, ""},
		{coreBook, f.closedSong, false, false, "closed"},
	} {
		sb, err := st.GetSongbook(ctx, c.book)
		if err != nil {
			t.Fatal(err)
		}
		s := found(t, st, f.core, FindFilter{Songbook: sb})[c.song]
		if s.InSongbook != c.in || s.Linkable != c.link || s.Refusal != c.refusal {
			t.Errorf("songbook %d, song %d: in %v linkable %v (%q), want %v %v (%q)", c.book, c.song, s.InSongbook, s.Linkable, s.Refusal, c.in, c.link, c.refusal)
		}
		// The rule itself agrees.
		if err := st.LinkCheck(ctx, f.core, c.song, sb); (err == nil) != c.link {
			t.Errorf("songbook %d, song %d: LinkCheck %v, FindSongs linkable %v", c.book, c.song, err, c.link)
		}
	}
}

// TestCreateSongInGoesInto: Create's "Goes into" lists the songbooks the
// person adds to, each saying whether a song made there may be public;
// the song is the songbook's owner's, in that songbook only, private
// unless they may publish there.
func TestCreateSongInGoesInto(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := newAccessFixture(t, st)
	bandBook, _ := st.GetDefaultSongbook(ctx, "band", f.band)
	targets := func(who int64) map[int64]SongbookTarget {
		t.Helper()
		ts, err := st.SongbookTargets(ctx, who, 0)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]SongbookTarget{}
		for _, x := range ts {
			out[x.ID] = x
		}
		return out
	}
	if x, ok := targets(f.guest)[bandBook]; !ok || x.CanPublish {
		t.Errorf("a guest's Goes into: Mercy's songbook %+v (listed %v), want listed, not publishable", x, ok)
	}
	if x := targets(f.core)[bandBook]; !x.CanPublish {
		t.Error("a core member can't make a song public in the band's songbook")
	}
	if _, ok := targets(f.outsider)[bandBook]; ok {
		t.Error("an outsider adds to Mercy's songbook")
	}
	if _, ok := targets(f.former)[bandBook]; ok {
		t.Error("a former member adds to Mercy's songbook")
	}
	staff := targets(f.staff)
	if x := staff[f.closedBook]; !x.Closed || x.CanPublish {
		t.Errorf("the closed songbook for its staff: %+v, want closed, never public", x)
	}
	if x := staff[f.openBook]; !x.CanPublish {
		t.Error("the community's staff can't publish in its open songbook")
	}
	if _, ok := targets(f.cguest)[f.openBook]; ok {
		t.Error("a guest without edit_repertoire adds to the community's songbook")
	}

	if _, err := st.CreateSongIn(ctx, f.guest, bandBook, "Guest's", "", "", true); !errors.Is(err, ErrForbidden) {
		t.Errorf("a guest's public song: %v, want forbidden", err)
	}
	if _, err := st.CreateSongIn(ctx, f.outsider, bandBook, "Nope", "", "", false); !errors.Is(err, ErrForbidden) {
		t.Errorf("an outsider's song in Mercy's songbook: %v", err)
	}
	if _, err := st.CreateSongIn(ctx, f.staff, f.closedBook, "Leçon 2", "", "", true); !errors.Is(err, ErrForbidden) {
		t.Errorf("a public song in a closed songbook: %v", err)
	}
	id, err := st.CreateSongIn(ctx, f.guest, bandBook, "New One", "Mercy", "https://youtu.be/abc", false)
	if err != nil {
		t.Fatal(err)
	}
	v, err := st.GetVariant(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if v.OwnerBandID == nil || *v.OwnerBandID != f.band || v.Public {
		t.Errorf("the new song: %+v, want Mercy's, private", v)
	}
	var books []int64
	rows, _ := st.db.Query(ctx, `SELECT songbook_id FROM songbook_entries WHERE variant_id = $1`, id)
	for rows.Next() {
		var b int64
		_ = rows.Scan(&b)
		books = append(books, b)
	}
	rows.Close()
	if len(books) != 1 || books[0] != bandBook {
		t.Errorf("the new song is in songbooks %v, want Mercy's alone", books)
	}
	pub, err := st.CreateSongIn(ctx, f.core, bandBook, "Public One", "Mercy", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := st.SongRightsFor(ctx, f.outsider, pub); !r.See {
		t.Error("a core member's public song isn't seen by everyone")
	}
	closedNew, err := st.CreateSongIn(ctx, f.staff, f.closedBook, "Leçon 2", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := st.SongRightsFor(ctx, f.staff, closedNew); !r.Closed {
		t.Error("a song made in a closed songbook isn't closed")
	}
}

// TestDuplicatesAndMerge: public songs with the same normalised title and
// artist, or the same reference video, are listed; staff merge one into
// the other — dry run first, nothing changed — and everything moves.
func TestDuplicatesAndMerge(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := newAccessFixture(t, st)
	song := func(title, artist, ref string) int64 {
		t.Helper()
		id, err := st.CreateVariant(ctx, OwnerCatalog, 0, title, artist, f.admin)
		if err != nil {
			t.Fatal(err)
		}
		if ref != "" {
			if err := st.SetCatalogueReference(ctx, id, ref); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	keep := song("What's Up", "4 Non Blondes", "https://www.youtube.com/watch?v=6NXnxTNIWkc")
	drop := song("WHAT'S UP!", "4 non blondes", "")
	byVideo := song("Black Magic Woman", "Santana", "https://youtu.be/Gw5nh3_rq6g")
	byVideo2 := song("Black Magic Woman (live)", "Santana", "https://www.youtube.com/watch?v=Gw5nh3_rq6g&t=3")
	// A private song of the same title isn't a duplicate of the public ones.
	if _, err := st.CreateVariant(ctx, OwnerPerson, f.alice, "What’s Up", "4 Non Blondes", f.alice); err != nil {
		t.Fatal(err)
	}
	groups, err := st.FindDuplicates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("%d groups of duplicates, want 2: %+v", len(groups), groups)
	}
	for _, g := range groups {
		ids := []int64{g.Songs[0].ID, g.Songs[1].ID}
		switch {
		case len(g.Songs) == 2 && ids[0] == byVideo && ids[1] == byVideo2:
			if len(g.Reasons) != 1 || g.Reasons[0] != "video" || g.VideoID != "Gw5nh3_rq6g" {
				t.Errorf("Black Magic Woman: %+v", g)
			}
		case len(g.Songs) == 2 && ids[0] == keep && ids[1] == drop:
			if len(g.Reasons) != 1 || g.Reasons[0] != "title" {
				t.Errorf("What's Up: %+v", g)
			}
		default:
			t.Errorf("unexpected group %+v", g)
		}
	}
	if r := groupDuplicates([]DuplicateSong{
		{ID: 1, Title: "The Wall", Artist: "Pink Floyd", VideoID: "x"}, {ID: 2, Title: "Wall", Artist: "Pink Floyd", VideoID: "x"},
	}); len(r) != 1 || len(r[0].Reasons) != 2 {
		t.Errorf("same title and video: %+v, want one group, both reasons", r)
	}

	// What points at the one merged: a songbook entry with its key, one
	// in a songbook that holds both, a set list line, a version made from it.
	aliceBook, _ := st.GetDefaultSongbook(ctx, "person", f.alice)
	bandBook, _ := st.GetDefaultSongbook(ctx, "band", f.band)
	for _, b := range []int64{aliceBook, bandBook} {
		if err := st.AddToSongbook(ctx, b, drop, f.core); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetEntrySemitones(ctx, aliceBook, drop, 2); err != nil {
		t.Fatal(err)
	}
	if err := st.AddToSongbook(ctx, bandBook, keep, f.core); err != nil {
		t.Fatal(err)
	}
	// Alice's set list: Mercy's version below takes Mercy's own lines.
	alice := f.alice
	sl, err := st.CreateSetlist(ctx, "person", "Friday", "", &alice, nil, nil, f.alice)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveSetlistByID(ctx, sl, &setlist.Setlist{Sets: []setlist.Set{{Label: "Set 1", Entries: []setlist.Entry{{VariantID: drop}}}}}, "Friday", "", "", f.alice); err != nil {
		t.Fatal(err)
	}
	version, err := st.MakeVersion(ctx, f.core, drop, BandOwner(f.band), bandBook, "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := st.MergeSongs(ctx, f.core, keep, drop, true); !errors.Is(err, ErrForbidden) {
		t.Errorf("a non-admin's merge: %v", err)
	}
	if _, err := st.MergeSongs(ctx, f.admin, keep, byVideo, true); !errors.Is(err, ErrNotDuplicates) {
		t.Errorf("merging songs that aren't duplicates: %v", err)
	}
	dry, err := st.MergeSongs(ctx, f.admin, keep, drop, true)
	if err != nil {
		t.Fatal(err)
	}
	if !dry.DryRun || dry.Moved.SetlistItems != 1 || dry.Moved.Entries < 1 {
		t.Errorf("the dry run: %+v", dry)
	}
	if _, err := st.GetVariant(ctx, drop); err != nil {
		t.Fatalf("the dry run deleted the song: %v", err)
	}
	if to, _ := st.MergedSongTarget(ctx, drop); to != 0 {
		t.Error("the dry run recorded a merge")
	}

	rep, err := st.MergeSongs(ctx, f.admin, keep, drop, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.DryRun || rep.Moved != dry.Moved {
		t.Errorf("the merge %+v, the dry run said %+v", rep.Moved, dry.Moved)
	}
	if _, err := st.GetVariant(ctx, drop); !errors.Is(err, ErrNotFound) {
		t.Errorf("the merged song is still there: %v", err)
	}
	if n, in, _ := st.SongbookEntryKey(ctx, aliceBook, keep); !in || n != 2 {
		t.Errorf("alice's entry: in %v, key %d; want the kept song, key 2", in, n)
	}
	var entries int
	_ = st.db.QueryRow(ctx, `SELECT count(*) FROM songbook_entries WHERE songbook_id = $1 AND variant_id = $2`, bandBook, keep).Scan(&entries)
	if entries != 1 {
		t.Errorf("Mercy's songbook holds the kept song %d times", entries)
	}
	var items int
	_ = st.db.QueryRow(ctx, `SELECT count(*) FROM setlist_items WHERE setlist_id = $1 AND variant_id = $2`, sl, keep).Scan(&items)
	if items != 1 {
		t.Errorf("the set list's line: %d lines of the kept song", items)
	}
	if v, _ := st.GetVariant(ctx, version); v == nil || v.ForkedFromID == nil || *v.ForkedFromID != keep {
		t.Errorf("the version made from the merged song: %+v, want forked from %d", v, keep)
	}
	if to, _ := st.MergedSongTarget(ctx, drop); to != keep {
		t.Errorf("the merged song goes to %d, want %d", to, keep)
	}
	ms, err := st.MergedInto(ctx, keep)
	if err != nil || len(ms) != 1 || ms[0].MergedSongID != drop || ms[0].Reason != "title" {
		t.Errorf("merged into the kept song: %+v %v", ms, err)
	}
	revs, err := st.ListRevisions(ctx, "variant", strconv.FormatInt(drop, 10))
	if err != nil || len(revs) == 0 {
		t.Errorf("the merged song's history: %d revisions, %v", len(revs), err)
	}
	if groups, _ := st.FindDuplicates(ctx); len(groups) != 1 {
		t.Errorf("after the merge, %d groups, want Black Magic Woman's alone", len(groups))
	}
}

// ExecForTest runs sql on st's database, for this directory's external
// tests that set up rows no API writes (a recording's analysis).
func ExecForTest(t *testing.T, st *Store, sql string, args ...any) {
	t.Helper()
	if _, err := st.db.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}
