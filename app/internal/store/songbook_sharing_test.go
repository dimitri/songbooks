package store

import (
	"context"
	"errors"
	"testing"
)

// A closed songbook's songs (docs/ownership-model.md §2) can be opened,
// never linked, copied or made a version of anywhere else, not even
// with the grant of a sold songbook that holds them too; an open one's can
// be made a version of by whoever can open them.
func TestCanCopyVariant(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	staff, err := st.CreatePerson(ctx, "author", "Author", "author@example.com")
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreatePerson(ctx, "student", "Student", "student@example.com")
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := st.CreatePerson(ctx, "outsider", "Outsider", "outsider@example.com")
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.CreateCommunity(ctx, "Lessons", staff)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddCommunityMember(ctx, c, member, "member", staff); err != nil {
		t.Fatal(err)
	}
	lesson, err := st.CreateCommunityVariant(ctx, c, "Mon premier Blues", "", staff)
	if err != nil {
		t.Fatal(err)
	}
	book, err := st.GetDefaultSongbook(ctx, "community", c)
	if err != nil {
		t.Fatal(err)
	}
	can := func(who int64, scope string, target int64) bool {
		t.Helper()
		ok, err := st.CanCopyVariant(ctx, who, lesson, scope, target)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}

	if !can(member, "user", 0) {
		t.Error("a shared songbook's song: a member can't copy it")
	}
	if can(outsider, "user", 0) {
		t.Error("an outsider can copy a song they can't open")
	}

	if err := st.SetSongbookClosed(ctx, book, true); err != nil {
		t.Fatal(err)
	}
	if can(member, "user", 0) {
		t.Error("a closed songbook's song: a member can copy it")
	}
	if _, err := st.ForkVariant(ctx, lesson, member, "user", 0); !errors.Is(err, ErrForbidden) {
		t.Errorf("forking a closed songbook's song: %v, want ErrForbidden", err)
	}
	if err := st.LinkCheckFor(ctx, member, lesson, PersonOwner(member)); !errors.Is(err, ErrForbidden) {
		t.Errorf("linking a closed songbook's song from a person's songbook: %v, want ErrForbidden", err)
	}
	if ok, _ := st.CanViewVariant(ctx, member, lesson); !ok {
		t.Error("a member can't open a closed songbook's song")
	}
	if !can(staff, "community", c) {
		t.Error("the community can't make a new version of its own song")
	}
	if err := st.SetSongPublic(ctx, staff, lesson, true); !errors.Is(err, ErrForbidden) {
		t.Errorf("publishing a closed songbook's song: %v, want ErrForbidden", err)
	}

	// Also in a sold songbook the member has: still closed.
	sold, err := st.CreateSongbook(ctx, "community", c, "Course", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSongbookRequiresGrant(ctx, sold, true); err != nil {
		t.Fatal(err)
	}
	if err := st.AddToSongbook(ctx, sold, lesson, staff); err != nil {
		t.Fatal(err)
	}
	if err := st.GrantSongbookAccess(ctx, sold, member, staff); err != nil {
		t.Fatal(err)
	}
	if can(member, "user", 0) {
		t.Error("copied out of a closed songbook through a sold one")
	}

	// Open again: whoever opens it makes their version.
	if err := st.SetSongbookClosed(ctx, book, false); err != nil {
		t.Fatal(err)
	}
	if !can(member, "user", 0) {
		t.Error("can't copy an open songbook's song")
	}
	if _, err := st.ForkVariant(ctx, lesson, member, "user", 0); err != nil {
		t.Errorf("fork: %v", err)
	}
}
