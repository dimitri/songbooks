package store

import (
	"context"
	"strings"
	"testing"

	"app/internal/setlist"
)

func TestClicktrackNotes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, err := st.CreatePerson(ctx, "drummer", "Drummer", "drummer@example.com")
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.CreateVariant(ctx, OwnerPerson, me, "First song", "", me)
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateVariant(ctx, OwnerPerson, me, "Second song", "", me)
	if err != nil {
		t.Fatal(err)
	}
	sl, err := st.CreateSetlist(ctx, "person", "Friday", "", &me, nil, nil, me)
	if err != nil {
		t.Fatal(err)
	}
	l := &setlist.Setlist{Sets: []setlist.Set{{Label: "Set 1", Entries: []setlist.Entry{{VariantID: a}, {VariantID: b}}}}}
	if _, err := st.SaveSetlistByID(ctx, sl, l, "Friday", "", "", me); err != nil {
		t.Fatal(err)
	}

	notes, err := st.ListClicktrackNotes(ctx, sl)
	if err != nil || len(notes) != 0 {
		t.Fatalf("empty: %v %v", notes, err)
	}

	got, err := st.SetClicktrackNote(ctx, sl, a, "  intro 4 bars piano alone \n", me)
	if err != nil || got != "intro 4 bars piano alone" {
		t.Fatalf("set: %q %v", got, err)
	}
	if _, err := st.SetClicktrackNote(ctx, sl, b, "watch the break at bar 32", me); err != nil {
		t.Fatal(err)
	}
	// Saving again replaces the note.
	if _, err := st.SetClicktrackNote(ctx, sl, a, "intro 8 bars", me); err != nil {
		t.Fatal(err)
	}
	notes, _ = st.ListClicktrackNotes(ctx, sl)
	if len(notes) != 2 || notes[a] != "intro 8 bars" || notes[b] != "watch the break at bar 32" {
		t.Fatalf("listed: %v", notes)
	}

	// Too long: cut to the limit.
	got, _ = st.SetClicktrackNote(ctx, sl, b, strings.Repeat("é", MaxClicktrackNote+50), me)
	if len([]rune(got)) != MaxClicktrackNote {
		t.Fatalf("cut to %d runes, got %d", MaxClicktrackNote, len([]rune(got)))
	}

	// An empty note removes it.
	if got, err := st.SetClicktrackNote(ctx, sl, b, "   ", me); err != nil || got != "" {
		t.Fatalf("clear: %q %v", got, err)
	}
	notes, _ = st.ListClicktrackNotes(ctx, sl)
	if len(notes) != 1 || notes[a] != "intro 8 bars" {
		t.Fatalf("after clear: %v", notes)
	}

	// Deleting the set list deletes its notes.
	if err := st.DeleteSetlistByID(ctx, sl); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.db.QueryRow(ctx, `SELECT count(*) FROM setlist_clicktrack_notes`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("after delete: %d %v", n, err)
	}
}
