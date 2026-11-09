package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// SongScore is one section's "score:" source text — see song_scores'
// migration comment.
type SongScore struct {
	ID                int64
	VariantID         int64
	SectionIndex      int
	LySource          string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	CreatedByPersonID *int64
}

// GetSongScore loads one section's score source, if any.
func (st *Store) GetSongScore(ctx context.Context, variantID int64, sectionIndex int) (*SongScore, error) {
	var s SongScore
	err := st.db.QueryRow(ctx, `
		SELECT id, variant_id, section_index, ly_source, created_at, updated_at, created_by_person_id
		FROM song_scores WHERE variant_id = $1 AND section_index = $2`, variantID, sectionIndex).
		Scan(&s.ID, &s.VariantID, &s.SectionIndex, &s.LySource, &s.CreatedAt, &s.UpdatedAt, &s.CreatedByPersonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListSongScores loads every section a variant has score source for, in
// section order — used by LoadVariant to resolve every "score:" field in
// one round trip rather than one query per section.
func (st *Store) ListSongScores(ctx context.Context, variantID int64) ([]SongScore, error) {
	rows, err := st.db.Query(ctx, `
		SELECT id, variant_id, section_index, ly_source, created_at, updated_at, created_by_person_id
		FROM song_scores WHERE variant_id = $1 ORDER BY section_index`, variantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SongScore
	for rows.Next() {
		var s SongScore
		if err := rows.Scan(&s.ID, &s.VariantID, &s.SectionIndex, &s.LySource, &s.CreatedAt, &s.UpdatedAt, &s.CreatedByPersonID); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetSongScore replaces (or creates) one section's score source.
func (st *Store) SetSongScore(ctx context.Context, variantID int64, sectionIndex int, lySource string, createdByPersonID int64) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx, `
		INSERT INTO song_scores (variant_id, section_index, ly_source, created_by_person_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (variant_id, section_index) DO UPDATE SET
			ly_source = EXCLUDED.ly_source, updated_at = now(), created_by_person_id = EXCLUDED.created_by_person_id
		RETURNING id`,
		variantID, sectionIndex, lySource, createdByPersonID).Scan(&id)
	return id, err
}

// DeleteSongScore removes one section's score source entirely.
func (st *Store) DeleteSongScore(ctx context.Context, variantID int64, sectionIndex int) error {
	_, err := st.db.Exec(ctx, `DELETE FROM song_scores WHERE variant_id = $1 AND section_index = $2`, variantID, sectionIndex)
	return err
}
