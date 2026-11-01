package store

import (
	"context"
	"time"
)

// SectionAnnotation is one lightweight, per-section note ("solo: sax") —
// NOT an edit to the variant's own body, just a tag attached to one
// section (SectionIndex lines up with however internal/song/httpapi's
// buildSections numbers a parsed variant's sections — zero-based, in
// document order). Exactly one of (PersonID, JamProfileID) is set in
// practice (enforced by migration 0015's check constraint, which only
// requires "at least one"): PersonID for a logged-in person annotating
// outside any jam, JamProfileID (+JamEventID) for an unlogged jam
// participant annotating during a jam — jam_profiles has no person_id of
// its own, so this is how an unlogged annotator's identity (their
// nickname) is captured instead.
type SectionAnnotation struct {
	ID           int64
	VariantID    int64
	SectionIndex int
	JamEventID   *int64
	PersonID     *int64
	PersonName   string // "" if PersonID is nil
	JamProfileID *int64
	Nickname     string // "" if JamProfileID is nil
	Body         string
	CreatedAt    time.Time
}

// CreateSectionAnnotation records one note on variantID's sectionIndex.
// Callers pass exactly one of personID/jamProfileID (the other nil) —
// see SectionAnnotation's doc comment; jamEventID travels alongside
// jamProfileID (nil together with it for the non-jam case).
func (st *Store) CreateSectionAnnotation(ctx context.Context, variantID int64, sectionIndex int, personID, jamEventID, jamProfileID *int64, body string) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx, `
		INSERT INTO section_annotations (variant_id, section_index, jam_event_id, person_id, jam_profile_id, body)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		variantID, sectionIndex, jamEventID, personID, jamProfileID, body).Scan(&id)
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
		if err := rows.Scan(&a.ID, &a.VariantID, &a.SectionIndex, &a.JamEventID,
			&a.PersonID, &a.PersonName, &a.JamProfileID, &a.Nickname, &a.Body, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

const sectionAnnotationCols = `
	a.id, a.variant_id, a.section_index, a.jam_event_id,
	a.person_id, coalesce(p.display_name, ''),
	a.jam_profile_id, coalesce(jp.nickname, ''),
	a.body, a.created_at
	FROM section_annotations a
	LEFT JOIN people p ON p.id = a.person_id
	LEFT JOIN jam_profiles jp ON jp.id = a.jam_profile_id`

// ListSectionAnnotations lists every note on variantID's sectionIndex,
// oldest first — the single-section shape GET
// /api/variants/{id}/sections/{index}/annotations returns.
func (st *Store) ListSectionAnnotations(ctx context.Context, variantID int64, sectionIndex int) ([]SectionAnnotation, error) {
	rows, err := st.db.Query(ctx, `SELECT `+sectionAnnotationCols+`
		WHERE a.variant_id = $1 AND a.section_index = $2
		ORDER BY a.created_at`, variantID, sectionIndex)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSectionAnnotations(rows)
}

// ListAllSectionAnnotations lists every note on variantID, across every
// section, ordered by section then oldest-first — the bulk shape GET
// /api/variants/{id}/annotations returns, for a page that wants to render
// every section's notes inline in one query rather than one per section.
func (st *Store) ListAllSectionAnnotations(ctx context.Context, variantID int64) ([]SectionAnnotation, error) {
	rows, err := st.db.Query(ctx, `SELECT `+sectionAnnotationCols+`
		WHERE a.variant_id = $1
		ORDER BY a.section_index, a.created_at`, variantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSectionAnnotations(rows)
}
