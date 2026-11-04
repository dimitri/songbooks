package store

import (
	"context"
	"time"
)

// SchoolCatalogEntry is one row of a school(community)'s DEFAULT
// songbook — a pointer (not a copy) from a school to a variant. Thin
// wrapper over the shared songbooks/songbook_entries model (migration
// 0042), same pattern as BandCatalogEntry/ListBandCatalog — a community
// can also have additional named songbooks beyond this default one; see
// ListSongbooks for those.
type SchoolCatalogEntry struct {
	CatalogVariant
	EntryID         int64
	AddedAt         time.Time
	AddedByPersonID *int64
	AddedByUsername string
}

// AddSchoolCatalogEntry points schoolID's default songbook at variantID —
// a pointer, not a copy. A no-op if the school already has this variant.
func (st *Store) AddSchoolCatalogEntry(ctx context.Context, schoolID, variantID, addedByPersonID int64) error {
	sbID, err := st.GetDefaultSongbook(ctx, "school", schoolID)
	if err != nil {
		return err
	}
	return st.AddToSongbook(ctx, sbID, variantID, addedByPersonID)
}

// RemoveSchoolCatalogEntry drops schoolID's pointer to variantID, if any.
func (st *Store) RemoveSchoolCatalogEntry(ctx context.Context, schoolID, variantID int64) error {
	sbID, err := st.GetDefaultSongbook(ctx, "school", schoolID)
	if err != nil {
		return err
	}
	return st.RemoveFromSongbook(ctx, sbID, variantID)
}

// ListSchoolCatalog lists schoolID's default songbook, most recently
// added first.
func (st *Store) ListSchoolCatalog(ctx context.Context, schoolID int64) ([]SchoolCatalogEntry, error) {
	sbID, err := st.GetDefaultSongbook(ctx, "school", schoolID)
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
	var out []SchoolCatalogEntry
	for rows.Next() {
		var e SchoolCatalogEntry
		if err := rows.Scan(&e.EntryID, &e.AddedAt, &e.AddedByPersonID, &e.AddedByUsername,
			&e.VariantID, &e.WorkID, &e.Title, &e.Artist, &e.Visibility, &e.OwnerID, &e.ForkedFromID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
