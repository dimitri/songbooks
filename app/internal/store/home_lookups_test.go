package store

import (
	"context"
	"testing"
	"time"
)

// TestStudentBooksAndNewSongs: a student's songbooks across their
// coaching communities (not a coach's students', not a community they
// left), and the songs new for a person in each community (not another
// student's, not a gated songbook they weren't granted).
func TestStudentBooksAndNewSongs(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	person := func(name string) int64 {
		id, err := st.CreatePerson(ctx, name, name+"@example.com")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	coach, me, other := person("coach"), person("me"), person("other")
	community := func(name string, members ...int64) int64 {
		id, err := st.CreateCommunity(ctx, name, coach)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range members {
			if _, err := st.AddCommunityMember(ctx, id, p, "member", coach); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	zed := community("Zed Voice", me, other)
	alto := community("alto studio", me)
	left := community("Left Behind", me)
	studentBook := func(c, p int64, name string) int64 {
		id, err := st.StudentSongbook(ctx, c, p, name, coach)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	mineZed := studentBook(zed, me, "My Zed songs")
	theirsZed := studentBook(zed, other, "Their Zed songs")
	mineAlto := studentBook(alto, me, "My alto songs")
	studentBook(left, me, "Old songs")
	if err := st.RemoveCommunityMember(ctx, left, me); err != nil {
		t.Fatal(err)
	}

	books, err := st.ListStudentBooksForPerson(ctx, me)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 || books[0].ID != mineAlto || books[0].CommunityName != "alto studio" || books[1].ID != mineZed || books[1].Name != "My Zed songs" {
		t.Fatalf("my student songbooks: %+v", books)
	}
	if books, _ := st.ListStudentBooksForPerson(ctx, coach); len(books) != 0 {
		t.Fatalf("a coach's own list has their students' songbooks: %+v", books)
	}

	// New songs: two in Zed's own songbook, one in my student songbook,
	// one in the other student's, one in a gated songbook.
	song := func(title string) int64 {
		v, err := st.CreateCommunityVariant(ctx, zed, title, "A", coach)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	song("One")
	song("Two")
	add := func(book int64, title string) {
		v := song(title)
		if err := st.AddToSongbook(ctx, book, v, coach); err != nil {
			t.Fatal(err)
		}
	}
	add(mineZed, "Mine")
	add(theirsZed, "Theirs")
	gated, err := st.CreateSongbook(ctx, "community", zed, "Course", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSongbookRequiresGrant(ctx, gated, true); err != nil {
		t.Fatal(err)
	}
	add(gated, "Gated")
	// "Mine", "Theirs" and "Gated" are also in Zed's own songbook
	// (CreateCommunityVariant adds them there): five distinct songs.
	counts, err := st.CountNewCommunitySongs(ctx, me, []int64{zed, alto}, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if counts[zed] != 5 || counts[alto] != 0 {
		t.Fatalf("new songs: %v, want zed 5, alto 0", counts)
	}
	// A song only in the gated songbook, or only in the other student's.
	only := func(book int64, title string) {
		sb, _ := st.GetDefaultSongbook(ctx, "community", zed)
		v := song(title)
		if err := st.AddToSongbook(ctx, book, v, coach); err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.Exec(ctx, `DELETE FROM songbook_entries WHERE songbook_id = $1 AND variant_id = $2`, sb, v); err != nil {
			t.Fatal(err)
		}
	}
	only(gated, "Gated only")
	only(theirsZed, "Theirs only")
	only(mineZed, "Mine only")
	counts, _ = st.CountNewCommunitySongs(ctx, me, []int64{zed}, time.Now().Add(-time.Hour))
	if counts[zed] != 6 {
		t.Fatalf("new songs after the private ones: %d, want 6 (only mine added)", counts[zed])
	}
	if err := st.GrantSongbookAccess(ctx, gated, me, coach); err != nil {
		t.Fatal(err)
	}
	counts, _ = st.CountNewCommunitySongs(ctx, me, []int64{zed}, time.Now().Add(-time.Hour))
	if counts[zed] != 7 {
		t.Fatalf("new songs once granted: %d, want 7", counts[zed])
	}
	if counts, _ := st.CountNewCommunitySongs(ctx, me, []int64{zed}, time.Now().Add(time.Hour)); counts[zed] != 0 {
		t.Fatalf("nothing new since later: %v", counts)
	}
}
