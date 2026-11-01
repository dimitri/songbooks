package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// BandCatalogEntry is one row of a band's repertoire — a pointer (not a
// copy) from a band to a variant, same CatalogVariant shape the personal
// catalog view uses, plus who/when it was added.
type BandCatalogEntry struct {
	CatalogVariant
	EntryID         int64
	AddedAt         time.Time
	AddedByPersonID *int64
	AddedByUsername string
}

// BandCatalogEntrySnapshot is the entity_type 'band_catalog_entry'
// snapshot shape — same Exists convention as BandMemberSnapshot.
type BandCatalogEntrySnapshot struct {
	Exists          bool      `json:"exists"`
	BandID          int64     `json:"band_id"`
	VariantID       int64     `json:"variant_id"`
	AddedAt         time.Time `json:"added_at"`
	AddedByPersonID *int64    `json:"added_by_person_id"`
}

// AddBandCatalogEntry points bandID's repertoire at variantID — a
// pointer, not a copy, same as the personal catalog. addedByPersonID (0
// = unknown) is both the stored added_by_person_id and the revision's
// actor. A no-op (not an error) if the band already has this variant.
func (st *Store) AddBandCatalogEntry(ctx context.Context, bandID, variantID, addedByPersonID int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var already bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM band_catalog_entries WHERE band_id = $1 AND variant_id = $2)`,
		bandID, variantID).Scan(&already); err != nil {
		return err
	}
	if already {
		return tx.Commit(ctx)
	}

	var addedBy *int64
	if addedByPersonID != 0 {
		addedBy = &addedByPersonID
	}
	var entryID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO band_catalog_entries (band_id, variant_id, added_by_person_id)
		VALUES ($1, $2, $3) RETURNING id`, bandID, variantID, addedBy).Scan(&entryID); err != nil {
		return err
	}

	if err := st.RecordRevision(ctx, tx, "band_catalog_entry", fmt.Sprint(entryID), addedBy, BandCatalogEntrySnapshot{Exists: false}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RemoveBandCatalogEntry drops bandID's pointer to variantID, if any —
// revision-logged the same as every other band mutation, so it can be
// undone via the generic revert endpoint.
func (st *Store) RemoveBandCatalogEntry(ctx context.Context, bandID, variantID, actorPersonID int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var entryID int64
	var addedAt time.Time
	var addedBy *int64
	err = tx.QueryRow(ctx, `
		SELECT id, added_at, added_by_person_id FROM band_catalog_entries
		WHERE band_id = $1 AND variant_id = $2 FOR UPDATE`, bandID, variantID).
		Scan(&entryID, &addedAt, &addedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // already gone — removing twice is a no-op, not an error
	}
	if err != nil {
		return err
	}

	actor := &actorPersonID
	if err := st.RecordRevision(ctx, tx, "band_catalog_entry", fmt.Sprint(entryID), actor, BandCatalogEntrySnapshot{
		Exists: true, BandID: bandID, VariantID: variantID, AddedAt: addedAt, AddedByPersonID: addedBy,
	}); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM band_catalog_entries WHERE id = $1`, entryID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListBandCatalog lists bandID's repertoire, most recently added first.
func (st *Store) ListBandCatalog(ctx context.Context, bandID int64) ([]BandCatalogEntry, error) {
	rows, err := st.db.Query(ctx, `
		SELECT c.id, c.added_at, c.added_by_person_id, coalesce(p.username, ''),
		       s.id, s.work_id, s.title, s.artist, pr.slug, s.stem, s.visibility::text, s.owner_person_id, s.forked_from_id
		FROM band_catalog_entries c
		JOIN songs s ON s.id = c.variant_id
		JOIN projects pr ON pr.id = s.project_id
		LEFT JOIN people p ON p.id = c.added_by_person_id
		WHERE c.band_id = $1
		ORDER BY c.added_at DESC`, bandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BandCatalogEntry
	for rows.Next() {
		var e BandCatalogEntry
		if err := rows.Scan(&e.EntryID, &e.AddedAt, &e.AddedByPersonID, &e.AddedByUsername,
			&e.VariantID, &e.WorkID, &e.Title, &e.Artist, &e.Book, &e.Stem, &e.Visibility, &e.OwnerID, &e.ForkedFromID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
