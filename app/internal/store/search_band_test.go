package store

import (
	"context"
	"testing"
)

// TestSearchFindsTheBandsVersion: the search palette shows a band member
// their band's version of a song in place of the catalogue's, labelled,
// one per band; someone in no band, the catalogue's.
func TestSearchFindsTheBandsVersion(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := NewBandFixture(t, st)
	ours, err := st.MakeVersion(ctx, f.Core, f.CatalogSong, BandOwner(f.Band), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateBand(ctx, "Mercy", f.Guest)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := st.MakeVersion(ctx, f.Guest, f.CatalogSong, BandOwner(other), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	songs := func(who int64) map[int64]string {
		t.Helper()
		hits, err := st.Search(ctx, who, "vent", 10)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]string{}
		for _, h := range hits {
			if h.Kind == "song" {
				out[h.ID] = h.BandName
			}
		}
		return out
	}
	if got := songs(f.Core); len(got) != 1 || got[ours] != "Other Lies" {
		t.Errorf("a member of one band: %v, want only its version %d", got, ours)
	}
	if got := songs(f.Guest); len(got) != 2 || got[ours] != "Other Lies" || got[theirs] != "Mercy" {
		t.Errorf("a member of two bands: %v, want both bands' versions", got)
	}
	if got := songs(f.Outsider); len(got) != 1 || got[f.CatalogSong] != "" {
		t.Errorf("in no band: %v, want the catalogue's %d", got, f.CatalogSong)
	}
}
