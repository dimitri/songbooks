package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Band is a lightweight group of people who collaborate on shared
// ("band"-visibility) catalog material. No band "owner"/"admin" role
// exists in this phase: any current member may add/remove/promote/demote
// another member, and any current member may delete the whole band — see
// the handlers in internal/httpapi/bands.go for where that's enforced.
type Band struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

// BandMember is one row of band_members — one person's one stint of
// membership in one band. History = every row ever inserted (see
// migration 0010); IsCurrent reports whether this particular row is the
// person's current membership.
type BandMember struct {
	ID                int64
	BandID            int64
	PersonID          int64
	Username          string
	DisplayName       string
	Status            string
	JoinedAt          time.Time
	LeftAt            *time.Time
	InvitedByPersonID *int64
	InvitedByUsername string
}

// IsCurrent reports whether m is an active (not left) membership row.
func (m BandMember) IsCurrent() bool { return m.LeftAt == nil }

// BandMemberSnapshot is the entity_type 'band_member' snapshot shape
// recorded by RecordRevision: the state of one band_members row
// immediately before a mutation. Exists=false represents "this row didn't
// exist yet" — the pre-state for an Add (a brand new row, so there's
// nothing to capture via a SELECT before the INSERT the way variant edits
// capture OLD values before an UPDATE).
type BandMemberSnapshot struct {
	Exists            bool       `json:"exists"`
	BandID            int64      `json:"band_id"`
	PersonID          int64      `json:"person_id"`
	Status            string     `json:"status"`
	JoinedAt          time.Time  `json:"joined_at"`
	LeftAt            *time.Time `json:"left_at"`
	InvitedByPersonID *int64     `json:"invited_by_person_id"`
}

// CreateBand makes a new band and inserts creatorPersonID as its first
// (core) member, in one transaction — a band with no members at all would
// be a dead end nobody could ever add to (see AddBandMember's
// current-member gate). The initial membership is itself revision-logged,
// same as any other band_member mutation.
func (st *Store) CreateBand(ctx context.Context, name string, creatorPersonID int64) (int64, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var bandID int64
	if err := tx.QueryRow(ctx, `INSERT INTO bands (name) VALUES ($1) RETURNING id`, name).Scan(&bandID); err != nil {
		return 0, err
	}

	var memberID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO band_members (band_id, person_id, status) VALUES ($1, $2, 'core')
		RETURNING id`, bandID, creatorPersonID).Scan(&memberID); err != nil {
		return 0, err
	}
	actor := &creatorPersonID
	if err := st.RecordRevision(ctx, tx, "band_member", fmt.Sprint(memberID), actor, BandMemberSnapshot{Exists: false}); err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return bandID, nil
}

// GetBand loads one band by id.
func (st *Store) GetBand(ctx context.Context, bandID int64) (*Band, error) {
	var b Band
	err := st.db.QueryRow(ctx, `SELECT id, name, created_at FROM bands WHERE id = $1`, bandID).
		Scan(&b.ID, &b.Name, &b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such band %d: %w", bandID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// ListBandsForPerson lists every band personID is currently a member of,
// alphabetically by name.
func (st *Store) ListBandsForPerson(ctx context.Context, personID int64) ([]Band, error) {
	rows, err := st.db.Query(ctx, `
		SELECT DISTINCT b.id, b.name, b.created_at
		FROM bands b JOIN band_members m ON m.band_id = b.id
		WHERE m.person_id = $1 AND m.left_at IS NULL
		ORDER BY b.name`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Band
	for rows.Next() {
		var b Band
		if err := rows.Scan(&b.ID, &b.Name, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// IsCurrentBandMember reports whether personID currently (left_at IS
// NULL) belongs to bandID, core or guest status alike — the permission
// gate for every band-scoped mutation in this phase, and the "actor is a
// current member of owner_band_id" half of CanEditVariant.
func (st *Store) IsCurrentBandMember(ctx context.Context, bandID, personID int64) (bool, error) {
	var ok bool
	err := st.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM band_members WHERE band_id = $1 AND person_id = $2 AND left_at IS NULL)`,
		bandID, personID).Scan(&ok)
	return ok, err
}

// ListBandMembers lists bandID's membership rows: every row ever inserted
// if includePast, otherwise only current (left_at IS NULL) ones. Newest
// first.
func (st *Store) ListBandMembers(ctx context.Context, bandID int64, includePast bool) ([]BandMember, error) {
	q := `
		SELECT m.id, m.band_id, m.person_id, p.username, p.display_name, m.status::text,
		       m.joined_at, m.left_at, m.invited_by_person_id, coalesce(ip.username, '')
		FROM band_members m
		JOIN people p ON p.id = m.person_id
		LEFT JOIN people ip ON ip.id = m.invited_by_person_id
		WHERE m.band_id = $1`
	if !includePast {
		q += ` AND m.left_at IS NULL`
	}
	q += ` ORDER BY m.joined_at DESC, m.id DESC`
	rows, err := st.db.Query(ctx, q, bandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BandMember
	for rows.Next() {
		var m BandMember
		if err := rows.Scan(&m.ID, &m.BandID, &m.PersonID, &m.Username, &m.DisplayName, &m.Status,
			&m.JoinedAt, &m.LeftAt, &m.InvitedByPersonID, &m.InvitedByUsername); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddBandMember adds personID to bandID at status ("core" or "guest"),
// invited by invitedByPersonID — a brand-new band_members row, never a
// reuse of a past one even if personID left and is rejoining (see
// migration 0010's comment). Errors if personID is already a current
// member of the band.
func (st *Store) AddBandMember(ctx context.Context, bandID, personID int64, status string, invitedByPersonID int64) (int64, error) {
	if status != "core" && status != "guest" {
		return 0, fmt.Errorf("invalid band member status %q", status)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	already, err := st.IsCurrentBandMember(ctx, bandID, personID)
	if err != nil {
		return 0, err
	}
	if already {
		return 0, fmt.Errorf("that person is already a current member of this band")
	}

	var invitedBy *int64
	if invitedByPersonID != 0 {
		invitedBy = &invitedByPersonID
	}
	var memberID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO band_members (band_id, person_id, status, invited_by_person_id)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		bandID, personID, status, invitedBy).Scan(&memberID); err != nil {
		return 0, err
	}

	actor := invitedBy
	if actor == nil && invitedByPersonID != 0 {
		actor = &invitedByPersonID
	}
	if err := st.RecordRevision(ctx, tx, "band_member", fmt.Sprint(memberID), actor, BandMemberSnapshot{Exists: false}); err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return memberID, nil
}

// RemoveBandMember ends personID's current membership in bandID (sets
// left_at = now() on their current row — a soft removal, same "history is
// never destroyed" rule as everything else in this phase). actorPersonID
// is the revision's actor.
func (st *Store) RemoveBandMember(ctx context.Context, bandID, personID, actorPersonID int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	row, err := lockCurrentMembership(ctx, tx, bandID, personID)
	if err != nil {
		return err
	}

	actor := &actorPersonID
	if err := st.RecordRevision(ctx, tx, "band_member", fmt.Sprint(row.ID), actor, row.snapshot()); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `UPDATE band_members SET left_at = now() WHERE id = $1`, row.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetBandMemberStatus promotes/demotes personID's current membership in
// bandID between "core" and "guest". actorPersonID is the revision's
// actor.
func (st *Store) SetBandMemberStatus(ctx context.Context, bandID, personID int64, status string, actorPersonID int64) error {
	if status != "core" && status != "guest" {
		return fmt.Errorf("invalid band member status %q", status)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	row, err := lockCurrentMembership(ctx, tx, bandID, personID)
	if err != nil {
		return err
	}

	actor := &actorPersonID
	if err := st.RecordRevision(ctx, tx, "band_member", fmt.Sprint(row.ID), actor, row.snapshot()); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `UPDATE band_members SET status = $1 WHERE id = $2`, status, row.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// currentMembershipRow is lockCurrentMembership's result: the row's own
// id alongside the fields BandMemberSnapshot needs — kept separate from
// BandMemberSnapshot itself since that type doubles as the revisions JSON
// shape and shouldn't carry its own row id inside its own snapshot.
type currentMembershipRow struct {
	ID int64
	BandMemberSnapshot
}

func (row currentMembershipRow) snapshot() BandMemberSnapshot {
	s := row.BandMemberSnapshot
	s.Exists = true
	return s
}

// lockCurrentMembership locks (FOR UPDATE) and returns personID's current
// membership row in bandID, or ErrNotFound if they have none.
func lockCurrentMembership(ctx context.Context, tx pgx.Tx, bandID, personID int64) (currentMembershipRow, error) {
	var row currentMembershipRow
	err := tx.QueryRow(ctx, `
		SELECT id, band_id, person_id, status::text, joined_at, left_at, invited_by_person_id
		FROM band_members WHERE band_id = $1 AND person_id = $2 AND left_at IS NULL
		FOR UPDATE`, bandID, personID).
		Scan(&row.ID, &row.BandID, &row.PersonID, &row.Status, &row.JoinedAt, &row.LeftAt, &row.InvitedByPersonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return currentMembershipRow{}, fmt.Errorf("no current membership for person %d in band %d: %w", personID, bandID, ErrNotFound)
	}
	if err != nil {
		return currentMembershipRow{}, err
	}
	return row, nil
}

// DeleteBand permanently deletes a band and everything under it (band
// memberships, band catalog entries — cascades via FK). No "owner" role
// exists in this phase, so this is gated (see internal/httpapi/bands.go)
// on "any current member", same as every other band mutation; there's no
// narrower role to restrict it to.
func (st *Store) DeleteBand(ctx context.Context, bandID int64) error {
	tag, err := st.db.Exec(ctx, `DELETE FROM bands WHERE id = $1`, bandID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such band %d: %w", bandID, ErrNotFound)
	}
	return nil
}
