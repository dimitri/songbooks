package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// A catalogue song's reference label (version_name: the album, "Live at
// …", "Cover"…) filled from its reference recording's title when nobody
// typed one (reftitle.Label cleans the title): at a new catalogue song or
// a new reference (httpapi refLabelNow), and for the songs there before,
// by cmd/reflabels. Never over a label someone typed: only an empty one,
// and only for the reference the title was found for. No revision and no
// new version: the label was empty, nothing is lost, and the chart's
// text doesn't change (like the reference's title, SetReferenceTitle).

// LabelCandidate is a catalogue song with a reference and no label.
type LabelCandidate struct {
	ID                          int64
	Title, Artist, ReferenceURL string
	// Stored is the reference's title as kept (RefTitle.For tells
	// whether it is this reference's).
	Stored RefTitle
}

const labelCandidateSQL = `
	SELECT s.id, s.title, s.artist, s.reference_url, s.reference_title, s.reference_title_url, s.reference_title_at
	FROM songs s
	WHERE s.owner_community_id = ` + bigSongbookSQL + `
	  AND s.version_name = '' AND btrim(s.reference_url) <> ''`

func scanLabelCandidate(row pgx.Row) (LabelCandidate, error) {
	var c LabelCandidate
	var at *time.Time
	err := row.Scan(&c.ID, &c.Title, &c.Artist, &c.ReferenceURL, &c.Stored.Title, &c.Stored.URL, &at)
	if at != nil {
		c.Stored.At = *at
	}
	c.ReferenceURL = strings.TrimSpace(c.ReferenceURL)
	return c, err
}

// ReferenceLabelCandidates is every catalogue song with a reference and
// an empty label, by id.
func (st *Store) ReferenceLabelCandidates(ctx context.Context) ([]LabelCandidate, error) {
	rows, err := st.db.Query(ctx, labelCandidateSQL+` ORDER BY s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LabelCandidate
	for rows.Next() {
		c, err := scanLabelCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ReferenceLabelCandidate is songID when it is a candidate (a catalogue
// song with a reference and an empty label), nil otherwise.
func (st *Store) ReferenceLabelCandidate(ctx context.Context, songID int64) (*LabelCandidate, error) {
	c, err := scanLabelCandidate(st.db.QueryRow(ctx, labelCandidateSQL+` AND s.id = $1`, songID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ReferenceLabelCounts is how the catalogue's songs stand: all of them,
// with a label (typed or filled), without a reference.
type ReferenceLabelCounts struct {
	Songs, Labelled, NoReference int
}

// CountReferenceLabels counts the catalogue's songs by label.
func (st *Store) CountReferenceLabels(ctx context.Context) (ReferenceLabelCounts, error) {
	var c ReferenceLabelCounts
	err := st.db.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE s.version_name <> ''),
		       count(*) FILTER (WHERE s.version_name = '' AND btrim(s.reference_url) = '')
		FROM songs s WHERE s.owner_community_id = `+bigSongbookSQL).
		Scan(&c.Songs, &c.Labelled, &c.NoReference)
	return c, err
}

// FillReferenceLabel sets catalogue song songID's label to label, made
// from the title of its reference ref, when its label is still empty
// and ref still its reference; it reports whether it did.
func (st *Store) FillReferenceLabel(ctx context.Context, songID int64, ref, label string) (bool, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return false, nil
	}
	tag, err := st.db.Exec(ctx, `
		UPDATE songs s SET version_name = $2
		WHERE s.id = $1 AND s.version_name = '' AND btrim(s.reference_url) = $3
		  AND s.owner_community_id = `+bigSongbookSQL, songID, label, strings.TrimSpace(ref))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
