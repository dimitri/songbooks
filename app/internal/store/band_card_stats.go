package store

import (
	"context"
	"time"
)

// BandCardStats is what a band's card on My bands says beyond its name
// and members: how many songs its songbook holds, and when something
// last happened in the band.
type BandCardStats struct {
	SongCount int
	// LastActivity is the latest of: a song added to one of its
	// songbooks, an edit of one of its songs or set lists, a new set
	// list, a proposal. Nil when nothing happened yet.
	LastActivity *time.Time
}

// ListBandCardStats is BandCardStats for each of bandIDs, in one round
// trip; a band missing from the map has none.
func (st *Store) ListBandCardStats(ctx context.Context, bandIDs []int64) (map[int64]BandCardStats, error) {
	out := map[int64]BandCardStats{}
	if len(bandIDs) == 0 {
		return out, nil
	}
	rows, err := st.db.Query(ctx, `
		SELECT b.id,
		       (SELECT count(*) FROM songbook_entries se
		        JOIN songbooks sb ON sb.id = se.songbook_id
		        WHERE sb.owner_kind = 'band' AND sb.owner_band_id = b.id AND sb.is_default),
		       greatest(
		         (SELECT max(se.added_at) FROM songbook_entries se
		          JOIN songbooks sb ON sb.id = se.songbook_id
		          WHERE sb.owner_kind = 'band' AND sb.owner_band_id = b.id),
		         (SELECT max(r.created_at) FROM songs s
		          JOIN revisions r ON r.entity_type = 'variant' AND r.entity_id = s.id::text
		          WHERE s.owner_band_id = b.id),
		         (SELECT max(r.created_at) FROM setlists sl
		          JOIN revisions r ON r.entity_type = 'setlist' AND r.entity_id = sl.id::text
		          WHERE sl.owner_band_id = b.id),
		         (SELECT max(sl.created_at) FROM setlists sl WHERE sl.owner_band_id = b.id),
		         (SELECT max(p.created_at) FROM band_proposals p WHERE p.band_id = b.id))
		FROM bands b
		WHERE b.id = ANY($1)`, bandIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var s BandCardStats
		if err := rows.Scan(&id, &s.SongCount, &s.LastActivity); err != nil {
			return nil, err
		}
		out[id] = s
	}
	return out, rows.Err()
}
