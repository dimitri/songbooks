package store

import (
	"context"
	"testing"
)

func TestListBandCardStats(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, err := st.CreatePerson(ctx, "lea", "Léa", "lea@example.org")
	if err != nil {
		t.Fatal(err)
	}
	quiet, err := st.CreateBand(ctx, "Quiet", me)
	if err != nil {
		t.Fatal(err)
	}
	busy, err := st.CreateBand(ctx, "Busy", me)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSetlist(ctx, "band", "Friday", "", nil, &busy, nil, me); err != nil {
		t.Fatal(err)
	}

	stats, err := st.ListBandCardStats(ctx, []int64{quiet, busy})
	if err != nil {
		t.Fatal(err)
	}
	if s := stats[quiet]; s.SongCount != 0 || s.LastActivity != nil {
		t.Errorf("a band where nothing happened: %+v", s)
	}
	if s := stats[busy]; s.LastActivity == nil {
		t.Errorf("a band with a new set list: %+v", s)
	}
	if none, err := st.ListBandCardStats(ctx, nil); err != nil || len(none) != 0 {
		t.Errorf("no bands: %v, %v", none, err)
	}
}
