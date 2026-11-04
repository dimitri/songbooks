package store

import (
	"context"
	"encoding/json"
)

// SongGroup is one "Set 1"/"Set 2"/... grouping — a songbook's own
// canonical play order, not alphabetical.
type SongGroup struct {
	Label      string
	VariantIDs []int64
}

// HasSongOrder reports whether songbookID has its own song_order set
// rather than falling back to alphabetical.
func (st *Store) HasSongOrder(ctx context.Context, songbookID int64) bool {
	groups, err := st.rawSongOrder(ctx, songbookID)
	return err == nil && len(groups) > 0
}

func (st *Store) rawSongOrder(ctx context.Context, songbookID int64) ([]SongGroup, error) {
	var raw []byte
	if err := st.db.QueryRow(ctx, `SELECT song_order FROM songbooks WHERE id = $1`, songbookID).Scan(&raw); err != nil {
		return nil, err
	}
	var groups []SongGroup
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

// ListSongsOrdered returns songbookID's entries grouped and ordered
// exactly as its song_order lists them, falling back to a single
// alphabetical group (by title) of every entry when none is set.
func (st *Store) ListSongsOrdered(ctx context.Context, songbookID int64) ([]SongGroup, error) {
	groups, err := st.rawSongOrder(ctx, songbookID)
	if err != nil {
		return nil, err
	}
	if len(groups) > 0 {
		return groups, nil
	}
	entries, err := st.ListSongbookEntries(ctx, songbookID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(entries))
	for i, e := range entries {
		ids[i] = e.VariantID
	}
	return []SongGroup{{Label: "Songs", VariantIDs: ids}}, nil
}

// SaveOrder writes songbookID's song_order.
func (st *Store) SaveOrder(ctx context.Context, songbookID int64, groups []SongGroup) error {
	raw, err := json.Marshal(groups)
	if err != nil {
		return err
	}
	_, err = st.db.Exec(ctx, `UPDATE songbooks SET song_order = $1 WHERE id = $2`, raw, songbookID)
	return err
}
