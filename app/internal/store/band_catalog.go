package store

import (
	"context"
	"time"
)

// BandCatalogEntry is one row of a band's songbook — a pointer (not a
// copy) from a band to a variant, same CatalogVariant shape the personal
// songbook view uses, plus who/when it was added.
type BandCatalogEntry struct {
	CatalogVariant
	EntryID         int64
	AddedAt         time.Time
	AddedByPersonID *int64
	AddedByUsername string
}

// AddBandCatalogEntry points bandID's default songbook at variantID — a
// pointer, not a copy. A no-op if the band already has this variant.
func (st *Store) AddBandCatalogEntry(ctx context.Context, bandID, variantID, addedByPersonID int64) error {
	sbID, err := st.GetDefaultSongbook(ctx, "band", bandID)
	if err != nil {
		return err
	}
	return st.AddToSongbook(ctx, sbID, variantID, addedByPersonID)
}

// RemoveBandCatalogEntry drops bandID's pointer to variantID, if any.
func (st *Store) RemoveBandCatalogEntry(ctx context.Context, bandID, variantID, actorPersonID int64) error {
	sbID, err := st.GetDefaultSongbook(ctx, "band", bandID)
	if err != nil {
		return err
	}
	return st.RemoveFromSongbook(ctx, sbID, variantID)
}

// ListBandCatalog lists bandID's default songbook, most recently added
// first.
func (st *Store) ListBandCatalog(ctx context.Context, bandID int64) ([]BandCatalogEntry, error) {
	sbID, err := st.GetDefaultSongbook(ctx, "band", bandID)
	if err != nil {
		return nil, err
	}
	rows, err := st.db.Query(ctx, `
		SELECT se.id, se.added_at, se.added_by_person_id, coalesce(p.username, ''),
		       s.id, s.work_id, s.title, s.artist, s.visibility::text, s.owner_person_id, s.forked_from_id
		FROM songbook_entries se
		JOIN songs s ON s.id = se.variant_id
		LEFT JOIN people p ON p.id = se.added_by_person_id
		WHERE se.songbook_id = $1
		ORDER BY se.added_at DESC`, sbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BandCatalogEntry
	for rows.Next() {
		var e BandCatalogEntry
		if err := rows.Scan(&e.EntryID, &e.AddedAt, &e.AddedByPersonID, &e.AddedByUsername,
			&e.VariantID, &e.WorkID, &e.Title, &e.Artist, &e.Visibility, &e.OwnerID, &e.ForkedFromID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
