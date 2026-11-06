package store

import (
	"context"
	"fmt"
	"time"
)

// SectionAnnotation is one lightweight, per-section note ("solo: sax") —
// NOT an edit to the variant's own body, just a tag attached to one
// section (SectionIndex lines up with however internal/song/httpapi's
// buildSections numbers a parsed variant's sections — zero-based, in
// document order).
type SectionAnnotation struct {
	ID           int64
	VariantID    int64
	SectionIndex int
	PersonID     int64
	PersonName   string
	Body         string
	Visibility   string // "private" (default, author-only) or "shared"
	CreatedAt    time.Time
}

// CreateSectionAnnotation records one note on variantID's sectionIndex.
// visibility must be "private" or "shared" ("" defaults to "private").
func (st *Store) CreateSectionAnnotation(ctx context.Context, variantID int64, sectionIndex int, personID int64, body, visibility string) (int64, error) {
	if visibility == "" {
		visibility = "private"
	}
	if visibility != "private" && visibility != "shared" {
		return 0, fmt.Errorf("invalid annotation visibility %q", visibility)
	}
	var id int64
	err := st.db.QueryRow(ctx, `
		INSERT INTO section_annotations (variant_id, section_index, person_id, body, visibility)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		variantID, sectionIndex, personID, body, visibility).Scan(&id)
	return id, err
}

func scanSectionAnnotations(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]SectionAnnotation, error) {
	var out []SectionAnnotation
	for rows.Next() {
		var a SectionAnnotation
		if err := rows.Scan(&a.ID, &a.VariantID, &a.SectionIndex,
			&a.PersonID, &a.PersonName, &a.Body, &a.Visibility, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

const sectionAnnotationCols = `
	a.id, a.variant_id, a.section_index,
	a.person_id, coalesce(p.display_name, ''),
	a.body, a.visibility, a.created_at
	FROM section_annotations a
	LEFT JOIN people p ON p.id = a.person_id`

// The "shared, or mine" filter below is shared logic between
// ListSectionAnnotations/ListAllSectionAnnotations: a 'shared' row is
// visible to anyone who can already reach this query (i.e. anyone who
// can view the variant at all), and a 'private' row is visible only to
// its own author.

// ListSectionAnnotations lists every note on variantID's sectionIndex
// that viewerPersonID may see (shared notes, plus their own private
// ones), oldest first — the single-section shape GET
// /api/variants/{id}/sections/{index}/annotations returns.
func (st *Store) ListSectionAnnotations(ctx context.Context, variantID int64, sectionIndex int, viewerPersonID int64) ([]SectionAnnotation, error) {
	rows, err := st.db.Query(ctx, `SELECT `+sectionAnnotationCols+`
		WHERE a.variant_id = $1 AND a.section_index = $2
		  AND (a.visibility = 'shared' OR a.person_id = $3)
		ORDER BY a.created_at`, variantID, sectionIndex, viewerPersonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSectionAnnotations(rows)
}

// ListAllSectionAnnotations lists every note on variantID that
// viewerPersonID may see (shared notes, plus their own private ones),
// across every section, ordered by section then oldest-first — the bulk
// shape GET /api/variants/{id}/annotations returns, for a page that
// wants to render every section's notes inline in one query rather than
// one per section.
func (st *Store) ListAllSectionAnnotations(ctx context.Context, variantID int64, viewerPersonID int64) ([]SectionAnnotation, error) {
	rows, err := st.db.Query(ctx, `SELECT `+sectionAnnotationCols+`
		WHERE a.variant_id = $1
		  AND (a.visibility = 'shared' OR a.person_id = $2)
		ORDER BY a.section_index, a.created_at`, variantID, viewerPersonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSectionAnnotations(rows)
}
