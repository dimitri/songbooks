package store

import (
	"context"
	"errors"
	"testing"
)

// A private songbook's songs can be opened, not copied out of it; a
// shared one's can be copied by whoever can open them (migration 0072).
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

	if err := st.SetSongbookShared(ctx, book, false); err != nil {
		t.Fatal(err)
	}
	if can(member, "user", 0) {
		t.Error("a private songbook's song: a member can copy it")
	}
	if _, err := st.ForkVariant(ctx, lesson, member, "user", 0); !errors.Is(err, ErrForbidden) {
		t.Errorf("forking a private songbook's song: %v, want ErrForbidden", err)
	}
	if !can(staff, "community", c) {
		t.Error("the community can't copy its own song into itself")
	}

	// Also in a shared songbook that's sold: copying it takes access to that one.
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
	if can(member, "user", 0) {
		t.Error("copied through a sold songbook the member hasn't got")
	}
	if err := st.GrantSongbookAccess(ctx, sold, member, staff); err != nil {
		t.Fatal(err)
	}
	if !can(member, "user", 0) {
		t.Error("can't copy through a shared songbook the member has")
	}
	if _, err := st.ForkVariant(ctx, lesson, member, "user", 0); err != nil {
		t.Errorf("fork: %v", err)
	}
}
