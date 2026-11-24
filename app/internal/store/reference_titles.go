package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// RefTitle is what's known of a song's reference's title
// (migration 0110): Title for the link URL, tried At (zero: never).
type RefTitle struct {
	Title, URL string
	At         time.Time
}

// For is the title for the link ref, "" when it was fetched for another
// link or not found.
func (t RefTitle) For(ref string) string {
	if ref = strings.TrimSpace(ref); ref == "" || t.URL != ref {
		return ""
	}
	return t.Title
}

// Due reports whether the title of ref is to be fetched: never tried for
// that link, or tried without finding one more than retry ago.
func (t RefTitle) Due(ref string, retry time.Duration, now time.Time) bool {
	ref = strings.TrimSpace(ref)
	switch {
	case ref == "":
		return false
	case t.URL != ref || t.At.IsZero():
		return true
	case t.Title == "":
		return now.Sub(t.At) > retry
	}
	return false
}

// ReferenceTitle is what's known of the title of variantID's reference.
func (st *Store) ReferenceTitle(ctx context.Context, variantID int64) (RefTitle, error) {
	var t RefTitle
	var at *time.Time
	err := st.db.QueryRow(ctx, `
		SELECT reference_title, reference_title_url, reference_title_at FROM songs WHERE id = $1`, variantID).
		Scan(&t.Title, &t.URL, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return RefTitle{}, ErrNotFound
	}
	if at != nil {
		t.At = *at
	}
	return t, err
}

// SetReferenceTitle records the title found for variantID's reference
// link ref ("" when none was), and when it was tried. The song's version
// doesn't change: the title isn't part of its text.
func (st *Store) SetReferenceTitle(ctx context.Context, variantID int64, ref, title string) error {
	_, err := st.db.Exec(ctx, `
		UPDATE songs SET reference_title = $2, reference_title_url = $3, reference_title_at = now()
		WHERE id = $1`, variantID, title, strings.TrimSpace(ref))
	return err
}
