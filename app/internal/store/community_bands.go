package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// LinkBandToCommunity links bandID to communityID as a member-unit
// (see migration 0060): every one of the band's CURRENT members is
// immediately granted ordinary individual community membership (see
// ensureCommunityMembershipForBandMember), and every future member the
// band gains while the link stays current gets the same treatment (see
// AddBandMember's community-sync step in bands.go). actorPersonID is
// recorded as invited_by_person_id on the link row itself. Errors if
// bandID is already currently linked to communityID.
func (st *Store) LinkBandToCommunity(ctx context.Context, communityID, bandID, actorPersonID int64) (int64, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	already, err := isCurrentCommunityBandLink(ctx, tx, communityID, bandID)
	if err != nil {
		return 0, err
	}
	if already {
		return 0, fmt.Errorf("that band is already linked to this community")
	}

	var invitedBy *int64
	if actorPersonID != 0 {
		invitedBy = &actorPersonID
	}
	var linkID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO community_band_members (community_id, band_id, invited_by_person_id)
		VALUES ($1, $2, $3) RETURNING id`,
		communityID, bandID, invitedBy).Scan(&linkID); err != nil {
		return 0, err
	}

	// Lock the band's current roster for the duration of this tx so a
	// concurrent AddBandMember can't slip a new member in uncounted
	// between this SELECT and the INSERTs it drives — same race this
	// method's own AddBandMember-side sync (bands.go) guards against in
	// the other direction.
	rows, err := tx.Query(ctx, `
		SELECT person_id FROM band_members WHERE band_id = $1 AND upper_inf(validity) FOR UPDATE`, bandID)
	if err != nil {
		return 0, err
	}
	var personIDs []int64
	for rows.Next() {
		var personID int64
		if err := rows.Scan(&personID); err != nil {
			rows.Close()
			return 0, err
		}
		personIDs = append(personIDs, personID)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()

	for _, personID := range personIDs {
		if err := ensureCommunityMembershipForBandMember(ctx, tx, communityID, personID, bandID); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return linkID, nil
}

// UnlinkBandFromCommunity closes bandID's current link to communityID
// (soft-closes the validity range — history is never destroyed, same
// rule as RemoveBandMember/RemoveCommunityMember). Deliberately does NOT
// touch any community_members rows: a band member who got their
// community membership via this link keeps it (sticky/independent once
// granted — see the plan's "Leave" decision); staff review and clean up
// manually via the existing RemoveCommunityMember.
func (st *Store) UnlinkBandFromCommunity(ctx context.Context, communityID, bandID int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var linkID int64
	err = tx.QueryRow(ctx, `
		SELECT id FROM community_band_members WHERE community_id = $1 AND band_id = $2 AND upper_inf(validity)
		FOR UPDATE`, communityID, bandID).Scan(&linkID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("that band is not currently linked to this community: %w", ErrNotFound)
	}
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE community_band_members SET validity = tstzrange(lower(validity), now()) WHERE id = $1`, linkID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ensureCommunityMembershipForBandMember idempotently grants personID
// ordinary individual membership in communityID, because they're a
// current member of a band that's currently linked to it — a no-op if
// personID is already a current member of the community (mirroring
// grantGroupInviteMembershipTx's "already in, nothing to do" pattern in
// group_invites.go). The new membership row's joined_via_band_id names
// bandID (see migration 0060's doc comment) and its role is the
// community's own default_join_role, same as any other automatic join.
// tx must be the caller's own open transaction — see isCurrentBandMember's
// doc comment in bands.go for why this can never reach back out to the
// pool mid-transaction.
func ensureCommunityMembershipForBandMember(ctx context.Context, tx pgx.Tx, communityID, personID, bandID int64) error {
	already, err := isCurrentCommunityMember(ctx, tx, communityID, personID, "")
	if err != nil {
		return err
	}
	if already {
		return nil
	}

	var defaultJoinRole string
	if err := tx.QueryRow(ctx, `SELECT default_join_role::text FROM communities WHERE id = $1`, communityID).Scan(&defaultJoinRole); err != nil {
		return err
	}
	roleID, err := resolveCommunityRoleByName(ctx, tx, communityID, defaultJoinRole)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO community_members (community_id, person_id, role_id, joined_via_band_id)
		VALUES ($1, $2, $3, $4)`, communityID, personID, roleID, bandID)
	return err
}

// isCurrentCommunityBandLink reports whether bandID is currently linked
// to communityID — internal check mirroring isCurrentBandMember/
// isCurrentCommunityMember (same dbtx reasoning: callers already
// mid-transaction must pass that tx, never the pool).
func isCurrentCommunityBandLink(ctx context.Context, q dbtx, communityID, bandID int64) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM community_band_members WHERE community_id = $1 AND band_id = $2 AND upper_inf(validity))`,
		communityID, bandID).Scan(&ok)
	return ok, err
}

// ListCommunityBandMembers lists communityID's linked bands: every band
// ever linked if includePast, otherwise only currently-linked ones.
// Alphabetically by name, same ordering convention as ListAllBands.
func (st *Store) ListCommunityBandMembers(ctx context.Context, communityID int64, includePast bool) ([]Band, error) {
	q := `
		SELECT b.id, b.name, b.created_at, coalesce(b.music_type, '')
		FROM bands b JOIN community_band_members cbm ON cbm.band_id = b.id
		WHERE cbm.community_id = $1`
	if !includePast {
		q += ` AND upper_inf(cbm.validity)`
	}
	q += ` ORDER BY b.name`
	rows, err := st.db.Query(ctx, q, communityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Band
	for rows.Next() {
		var b Band
		if err := rows.Scan(&b.ID, &b.Name, &b.CreatedAt, &b.MusicType); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListLinkedCommunitiesForBand lists every community bandID is
// currently linked to, alphabetically by name — the band-side mirror of
// ListCommunityBandMembers.
func (st *Store) ListLinkedCommunitiesForBand(ctx context.Context, bandID int64) ([]Community, error) {
	rows, err := st.db.Query(ctx, `
		SELECT sc.id, sc.name, sc.default_join_role::text, sc.created_at
		FROM communities sc JOIN community_band_members cbm ON cbm.community_id = sc.id
		WHERE cbm.band_id = $1 AND upper_inf(cbm.validity)
		ORDER BY sc.name`, bandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Community
	for rows.Next() {
		var c Community
		if err := rows.Scan(&c.ID, &c.Name, &c.DefaultJoinRole, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CommunityMemberGroup is one bucket of ListCommunityMembersGroupedByBand's
// read model: either every direct joiner (BandID == 0, BandName == "")
// or every current member who joined via one specific currently-linked
// band.
type CommunityMemberGroup struct {
	BandID   int64
	BandName string
	Members  []CommunityMember
}

// ListCommunityMembersGroupedByBand buckets communityID's CURRENT
// members into "joined via band X" (one group per currently-linked
// band that has at least one such member) vs. "direct" — the read model
// behind the community admin page's member list. Orphaned is every
// current member whose joined_via_band_id is set but whose originating
// link no longer qualifies: either that band's community link isn't
// current any more, or the person isn't a current member of that band
// any more (RemoveBandMember doesn't revoke membership, so this is the
// normal, expected way an "orphan" appears, not a bug) — surfaced
// separately so staff can review and optionally RemoveCommunityMember
// them by hand.
func (st *Store) ListCommunityMembersGroupedByBand(ctx context.Context, communityID int64) (groups []CommunityMemberGroup, orphaned []CommunityMember, err error) {
	members, err := st.ListCommunityMembers(ctx, communityID, false)
	if err != nil {
		return nil, nil, err
	}

	var direct []CommunityMember
	byBand := map[int64]*CommunityMemberGroup{}
	var bandOrder []int64
	for _, m := range members {
		if m.JoinedViaBandID == nil {
			direct = append(direct, m)
			continue
		}
		bandID := *m.JoinedViaBandID
		linkCurrent, err := isCurrentCommunityBandLink(ctx, st.db, communityID, bandID)
		if err != nil {
			return nil, nil, err
		}
		memberCurrent, err := isCurrentBandMember(ctx, st.db, bandID, m.PersonID)
		if err != nil {
			return nil, nil, err
		}
		if !linkCurrent || !memberCurrent {
			orphaned = append(orphaned, m)
			continue
		}
		g, ok := byBand[bandID]
		if !ok {
			g = &CommunityMemberGroup{BandID: bandID, BandName: m.JoinedViaBandName}
			byBand[bandID] = g
			bandOrder = append(bandOrder, bandID)
		}
		g.Members = append(g.Members, m)
	}

	for _, bandID := range bandOrder {
		groups = append(groups, *byBand[bandID])
	}
	if direct != nil {
		groups = append(groups, CommunityMemberGroup{Members: direct})
	}
	return groups, orphaned, nil
}
