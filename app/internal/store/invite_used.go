package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// A single-use invite that was taken stays in the table, used (migration
// 0138): the link's page can then say it was used, and to log in,
// rather than that it never existed.

// markInviteUsedTx marks token taken, by usedBy. A personal link also
// records where it took them: placeholderID's first band, else first
// community, by name (GetPersonMemberships' order, the one
// personalInviteDest lands on) — read before the placeholder is merged
// away. placeholderID 0 (a plain invite) records none.
func markInviteUsedTx(ctx context.Context, tx pgx.Tx, token string, usedBy, placeholderID int64) error {
	_, err := tx.Exec(ctx, `
		WITH j AS (
			SELECT (SELECT bm.band_id FROM band_members bm JOIN bands b ON b.id = bm.band_id
			        WHERE bm.person_id = $3 AND upper_inf(bm.validity) ORDER BY b.name LIMIT 1) AS band_id,
			       (SELECT cm.community_id FROM community_members cm JOIN communities c ON c.id = cm.community_id
			        WHERE cm.person_id = $3 AND upper_inf(cm.validity) ORDER BY c.name LIMIT 1) AS community_id)
		UPDATE invites SET used_at = now(), used_by_person_id = $2,
		       joined_band_id = j.band_id,
		       joined_community_id = CASE WHEN j.band_id IS NULL THEN j.community_id END
		FROM j WHERE invites.token = $1`, token, usedBy, placeholderID)
	return err
}

// DeadInvite is why an /invite/ link GetInvite refused doesn't work, for
// its page to say so.
type DeadInvite struct {
	Used bool // taken (else expired)
	// Personal: a personal link (else a plain invite). A used personal
	// link works until it expires (GetReturningLink): this one has.
	Personal bool
	// UsedBy is the account that took it (0: not used, or since deleted).
	UsedBy int64
	// TargetKind ("band", "community" or "") and TargetID are where the
	// link takes (took) its holder: for a used personal link, where it
	// took them; for an expired one, its placeholder's first group; ""
	// for a plain invite. TargetName is that group's name.
	TargetKind string
	TargetID   int64
	TargetName string
}

// DeadInviteFor says why token, an /invite/ link GetInvite refused, no
// longer works: used (a plain invite), or expired (a personal link, used
// or not: a used one works until then). ErrNotFound when there's no such
// link (never was, revoked before it was used, replaced by a new one, or
// a band's nameless one-off link: notNamelessBandInvite), and when it is
// in fact still open.
func (st *Store) DeadInviteFor(ctx context.Context, token string) (*DeadInvite, error) {
	var d DeadInvite
	var open bool
	var usedBy, personID, bandID, communityID *int64
	err := st.db.QueryRow(ctx, `
		SELECT i.used_at IS NOT NULL, i.expires_at > now() AND (i.used_at IS NULL OR i.person_id IS NOT NULL),
		       i.used_by_person_id, i.person_id, i.joined_band_id, i.joined_community_id
		FROM invites i
		WHERE i.token = $1 AND `+notNamelessBandInvite, token).
		Scan(&d.Used, &open, &usedBy, &personID, &bandID, &communityID)
	if errors.Is(err, pgx.ErrNoRows) || open {
		return nil, fmt.Errorf("no such dead invite: %w", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	if usedBy != nil {
		d.UsedBy = *usedBy
	}
	d.Personal = personID != nil
	switch {
	case d.Used && bandID != nil:
		d.TargetKind, d.TargetID = "band", *bandID
		err = st.db.QueryRow(ctx, `SELECT name FROM bands WHERE id = $1`, *bandID).Scan(&d.TargetName)
	case d.Used && communityID != nil:
		d.TargetKind, d.TargetID = "community", *communityID
		err = st.db.QueryRow(ctx, `SELECT name FROM communities WHERE id = $1`, *communityID).Scan(&d.TargetName)
	case !d.Used && personID != nil:
		ms, merr := st.GetPersonMemberships(ctx, *personID)
		if merr != nil {
			return nil, merr
		}
		for _, m := range ms {
			if m.Kind == "band" || m.Kind == "community" {
				d.TargetKind, d.TargetID, d.TargetName = m.Kind, m.ID, m.Name
				break
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}
