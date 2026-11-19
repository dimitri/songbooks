package store

import (
	"context"
	"testing"
)

func TestSectionAnnotationsAuthorOnly(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, _ := st.CreatePerson(ctx, "me", "Me", "me@example.com")
	other, _ := st.CreatePerson(ctx, "other", "Other", "other@example.com")
	song, err := st.CreateVariant(ctx, OwnerPerson, me, "Song", "", me)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := st.CreateSectionAnnotation(ctx, song, 1, me, "watch the key change", "")
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := st.CreateSectionAnnotation(ctx, song, 2, other, "bass enters alone", "shared")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSectionAnnotation(ctx, song, 2, other, "my secret", "private"); err != nil {
		t.Fatal(err)
	}

	notes, err := st.ListAllSectionAnnotations(ctx, song, me)
	if err != nil || len(notes) != 2 {
		t.Fatalf("my view: %+v %v", notes, err)
	}
	if !notes[0].Mine || notes[0].Visibility != "private" || notes[1].Mine {
		t.Fatalf("mine flags: %+v", notes)
	}

	// Someone else's shared note: readable, never editable or deletable.
	if ok, err := st.UpdateSectionAnnotation(ctx, song, theirs, me, nil, "rewritten", "shared"); err != nil || ok {
		t.Fatalf("edited someone else's note: %v %v", ok, err)
	}
	if ok, err := st.DeleteSectionAnnotation(ctx, song, theirs, me); err != nil || ok {
		t.Fatalf("deleted someone else's note: %v %v", ok, err)
	}
	// The wrong variant doesn't reach it either.
	if ok, _ := st.DeleteSectionAnnotation(ctx, song+1, mine, me); ok {
		t.Fatal("deleted through another variant")
	}

	// Edit keeps the section unless one is given, then moves it.
	if ok, err := st.UpdateSectionAnnotation(ctx, song, mine, me, nil, "watch the modulation", "shared"); err != nil || !ok {
		t.Fatalf("edit: %v %v", ok, err)
	}
	three := 3
	if ok, err := st.UpdateSectionAnnotation(ctx, song, mine, me, &three, "watch the modulation", "shared"); err != nil || !ok {
		t.Fatalf("move: %v %v", ok, err)
	}
	if _, err := st.UpdateSectionAnnotation(ctx, song, mine, me, nil, "x", "public"); err == nil {
		t.Fatal("accepted an unknown visibility")
	}
	notes, _ = st.ListAllSectionAnnotations(ctx, song, other)
	if len(notes) != 3 || notes[2].Body != "watch the modulation" || notes[2].SectionIndex != 3 || notes[2].Mine {
		t.Fatalf("after edit, as other: %+v", notes)
	}

	if ok, err := st.DeleteSectionAnnotation(ctx, song, mine, me); err != nil || !ok {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if notes, _ := st.ListSectionAnnotations(ctx, song, 3, me); len(notes) != 0 {
		t.Fatalf("still there: %+v", notes)
	}
}
