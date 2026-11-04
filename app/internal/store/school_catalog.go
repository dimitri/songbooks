package store

import (
	"context"
	"time"
)

// SchoolCatalogEntry is one row of a school's repertoire — a pointer
// (not a copy) from a school to a variant, same shape BandCatalogEntry
// uses for bands.
type SchoolCatalogEntry struct {
	CatalogVariant
	EntryID         int64
	AddedAt         time.Time
	AddedByPersonID *int64
	AddedByUsername string
}

// AddSchoolCatalogEntry points schoolID's repertoire at variantID — a
// pointer, not a copy. addedByPersonID (0 = unknown) is stored as
// added_by_person_id. A no-op (not an error) if the school already has
// this variant — same shape as AddBandCatalogEntry.
func (st *Store) AddSchoolCatalogEntry(ctx context.Context, schoolID, variantID, addedByPersonID int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var already bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM school_catalog_entries WHERE school_id = $1 AND variant_id = $2)`,
		schoolID, variantID).Scan(&already); err != nil {
		return err
	}
	if already {
		return tx.Commit(ctx)
	}

	var addedBy *int64
	if addedByPersonID != 0 {
		addedBy = &addedByPersonID
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO school_catalog_entries (school_id, variant_id, added_by_person_id)
		VALUES ($1, $2, $3)`, schoolID, variantID, addedBy); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RemoveSchoolCatalogEntry drops schoolID's pointer to variantID, if any.
func (st *Store) RemoveSchoolCatalogEntry(ctx context.Context, schoolID, variantID int64) error {
	tag, err := st.db.Exec(ctx, `DELETE FROM school_catalog_entries WHERE school_id = $1 AND variant_id = $2`, schoolID, variantID)
	if err != nil {
		return err
	}
	_ = tag // already gone is a no-op, not an error — same as RemoveBandCatalogEntry
	return nil
}

// ListSchoolCatalog lists schoolID's repertoire, most recently added
// first.
func (st *Store) ListSchoolCatalog(ctx context.Context, schoolID int64) ([]SchoolCatalogEntry, error) {
	rows, err := st.db.Query(ctx, `
		SELECT c.id, c.added_at, c.added_by_person_id, coalesce(p.username, ''),
		       s.id, s.work_id, s.title, s.artist, s.visibility::text, s.owner_person_id, s.forked_from_id
		FROM school_catalog_entries c
		JOIN songs s ON s.id = c.variant_id
		LEFT JOIN people p ON p.id = c.added_by_person_id
		WHERE c.school_id = $1
		ORDER BY c.added_at DESC`, schoolID)
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
