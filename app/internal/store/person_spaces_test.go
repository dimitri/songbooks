package store

import (
	"context"
	"testing"
)

func TestSpacesOrder(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, err := st.CreatePerson(ctx, "lea", "Léa", "lea@example.org")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreatePerson(ctx, "max", "Max", "max@example.org")
	if err != nil {
		t.Fatal(err)
	}
	alpha, err := st.CreateBand(ctx, "Alpha", me)
	if err != nil {
		t.Fatal(err)
	}
	beta, err := st.CreateBand(ctx, "Beta", me)
	if err != nil {
		t.Fatal(err)
	}
	jam, err := st.CreateCommunity(ctx, "Jam", me)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateBand(ctx, "Not mine", other); err != nil {
		t.Fatal(err)
	}
	names := func() []string {
		t.Helper()
		spaces, err := st.ListSpaces(ctx, me)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, s := range spaces {
			n := s.Name
			if s.Pinned {
				n += "*"
			}
			out = append(out, n)
		}
		return out
	}
	check := func(want ...string) {
		t.Helper()
		got := names()
		if len(got) != len(want) {
			t.Fatalf("spaces %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("spaces %v, want %v", got, want)
			}
		}
	}
	// Never used: by name; only one's own.
	check("Alpha", "Beta", "Jam")
	// The last used first.
	if err := st.TouchSpace(ctx, me, "community", jam); err != nil {
		t.Fatal(err)
	}
	check("Jam", "Alpha", "Beta")
	if err := st.TouchSpace(ctx, me, "band", beta); err != nil {
		t.Fatal(err)
	}
	check("Beta", "Jam", "Alpha")
	// Pinned before anything used.
	if err := st.PinSpace(ctx, me, "band", alpha, true); err != nil {
		t.Fatal(err)
	}
	check("Alpha*", "Beta", "Jam")
	// Using a pinned one keeps it pinned; unpinning keeps its use.
	if err := st.TouchSpace(ctx, me, "band", alpha); err != nil {
		t.Fatal(err)
	}
	check("Alpha*", "Beta", "Jam")
	if err := st.PinSpace(ctx, me, "band", alpha, false); err != nil {
		t.Fatal(err)
	}
	check("Alpha", "Beta", "Jam")
	if err := st.TouchSpace(ctx, me, "galaxy", 1); err == nil {
		t.Error("a kind that isn't a space")
	}
}
