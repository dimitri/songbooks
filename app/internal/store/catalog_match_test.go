package store

import (
	"context"
	"testing"
)

func TestMatchLibrary(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, err := st.CreatePerson(ctx, "me", "Me", "me@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	other, err := st.CreatePerson(ctx, "other", "Other", "other@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	mk := func(kind OwnerKind, owner int64, title, artist string) int64 {
		id, err := st.CreateVariant(ctx, kind, owner, title, artist, me)
		if err != nil {
			t.Fatalf("CreateVariant(%q): %v", title, err)
		}
		return id
	}
	wall := mk(OwnerCatalog, 0, "The Wall", "Pink Floyd")
	brick := mk(OwnerCatalog, 0, "Another Brick in the Wall", "Pink Floyd")
	aventurier := mk(OwnerCatalog, 0, "L’Aventurier", "Indochine")
	mine := mk(OwnerPerson, me, "Wall", "Me")
	mk(OwnerPerson, other, "Wall", "Someone else") // private to someone else: never offered

	ids := func(ms []LibraryMatch) []int64 {
		out := make([]int64, len(ms))
		for i, m := range ms {
			out[i] = m.VariantID
		}
		return out
	}

	got, err := st.MatchLibrary(ctx, me, "wall!", "pink floyd", 5)
	if err != nil {
		t.Fatalf("MatchLibrary: %v", err)
	}
	if want := []int64{wall, mine, brick}; !equalIDs(ids(got), want) {
		t.Errorf("wall/pink floyd: got %v, want %v", ids(got), want)
	}
	if len(got) > 0 && !got[0].Exact {
		t.Errorf("The Wall should be an exact match for %q", "wall!")
	}

	got, err = st.MatchLibrary(ctx, me, "Aventurier", "", 5)
	if err != nil {
		t.Fatalf("MatchLibrary: %v", err)
	}
	if want := []int64{aventurier}; !equalIDs(ids(got), want) {
		t.Errorf("aventurier: got %v, want %v", ids(got), want)
	}

	got, err = st.MatchLibrary(ctx, me, "Hotel California", "", 5)
	if err != nil {
		t.Fatalf("MatchLibrary: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("no match expected, got %v", ids(got))
	}
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
