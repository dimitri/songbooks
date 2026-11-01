package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// GetBandLastSeen returns when personID last viewed bandID's page, or nil
// if they never have. Callers computing a "changes since your last visit"
// digest must call this BEFORE calling MarkBandSeen, so the digest is
// built against the OLD value, not the one the current page view is
// about to set.
func (st *Store) GetBandLastSeen(ctx context.Context, bandID, personID int64) (*time.Time, error) {
	var seenAt time.Time
	err := st.db.QueryRow(ctx, `
		SELECT seen_at FROM band_member_last_seen WHERE band_id = $1 AND person_id = $2`,
		bandID, personID).Scan(&seenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // never visited before — no digest baseline, not an error
	}
	if err != nil {
		return nil, err
	}
	return &seenAt, nil
}

// MarkBandSeen records that personID is viewing bandID's page right now —
// an upsert, since this is a single "last seen" value, not a history (see
// migration 0021's comment on why this is its own table rather than a
// band_members column).
func (st *Store) MarkBandSeen(ctx context.Context, bandID, personID int64) error {
	_, err := st.db.Exec(ctx, `
		INSERT INTO band_member_last_seen (band_id, person_id, seen_at)
		VALUES ($1, $2, now())
		ON CONFLICT (band_id, person_id) DO UPDATE SET seen_at = now()`,
		bandID, personID)
	return err
}
