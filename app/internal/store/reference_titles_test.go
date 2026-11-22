package store

import (
	"context"
	"testing"
	"time"
)

// A reference's title is kept for its link: another link has none until
// it's fetched; a link tried without finding one is tried again later,
// not at once.
func TestReferenceTitle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, err := st.CreatePerson(ctx, "reftitle", "Ref Title", "reftitle@example.com")
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.CreateVariant(ctx, OwnerPerson, me, "Valerie", "Amy Winehouse", me)
	if err != nil {
		t.Fatal(err)
	}
	link := "https://www.youtube.com/watch?v=jNQXAC9IVRw"
	rt, err := st.ReferenceTitle(ctx, id)
	if err != nil || rt.For(link) != "" || !rt.Due(link, time.Hour, time.Now()) {
		t.Fatalf("new song: %+v %v", rt, err)
	}
	if err := st.SetReferenceTitle(ctx, id, " "+link+" ", "Me at the zoo"); err != nil {
		t.Fatal(err)
	}
	rt, _ = st.ReferenceTitle(ctx, id)
	if rt.For(link) != "Me at the zoo" || rt.Due(link, time.Hour, time.Now()) {
		t.Errorf("found: %+v", rt)
	}
	other := "https://vimeo.com/1"
	if rt.For(other) != "" || !rt.Due(other, time.Hour, time.Now()) {
		t.Errorf("another link: %+v", rt)
	}
	if err := st.SetReferenceTitle(ctx, id, other, ""); err != nil {
		t.Fatal(err)
	}
	rt, _ = st.ReferenceTitle(ctx, id)
	if rt.For(other) != "" || rt.Due(other, time.Hour, time.Now()) || !rt.Due(other, time.Hour, time.Now().Add(2*time.Hour)) {
		t.Errorf("not found: %+v", rt)
	}
	if rt.Due("", time.Hour, time.Now()) {
		t.Error("no link is never due")
	}
	if _, err := st.ReferenceTitle(ctx, -1); err != ErrNotFound {
		t.Errorf("no such song: %v", err)
	}
}
