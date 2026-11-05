package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Community is a named group of people with a REAL two-tier role
// (staff/member — "Community" at the user-facing layer, see migration
// 0042's doc comment for why this table keeps its original name) —
// unlike a Band (deliberately flat, no hierarchy — see bands.go's doc
// comment), a community/community explicitly has a hierarchy: staff manage
// membership and the community's shared songbooks/setlists and review
// member submissions; members get read-only access to the community's
// repertoire (see CanEditVariant's "community" case, CanEditSongbook) plus
// their own ordinary personal space.
type Community struct {
	ID              int64
	Name            string
	DefaultJoinRole string
	CreatedAt       time.Time
}

// CommunityMember is one row of community_members — one person's one stint of
// membership in one community, same history-preserving shape as
// BandMember (see migration 0023's comment): every row ever inserted is
// kept forever; IsCurrent reports whether THIS row is the person's
// current membership.
type CommunityMember struct {
	ID                int64
	CommunityID          int64
	PersonID          int64
	Username          string
	DisplayName       string
	Role              string // "staff" or "member"
	JoinedAt          time.Time
	LeftAt            *time.Time
	InvitedByPersonID *int64
	InvitedByUsername string
}

// IsCurrent reports whether m is an active (not left) membership row.
func (m CommunityMember) IsCurrent() bool { return m.LeftAt == nil }

// IsStaff reports whether m's role is the instructor tier.
func (m CommunityMember) IsStaff() bool { return m.Role == "staff" }

// CreateCommunity makes a new community and inserts creatorPersonID as its
// first member, AS AN INSTRUCTOR (not a student) — a community with no
// instructor at all would be a dead end nobody could ever manage, same
// "first member must be able to bootstrap the rest" reasoning
// CreateBand uses, except here the tier matters: only an instructor can
// add further members at all (see requireCommunityStaff), so the
// creator cannot start out as a student.
func (st *Store) CreateCommunity(ctx context.Context, name string, creatorPersonID int64) (int64, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var communityID int64
	if err := tx.QueryRow(ctx, `INSERT INTO communities (name) VALUES ($1) RETURNING id`, name).Scan(&communityID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO community_members (community_id, person_id, role) VALUES ($1, $2, 'staff')`,
		communityID, creatorPersonID); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return communityID, nil
}

// GetCommunity loads one community by id.
func (st *Store) GetCommunity(ctx context.Context, communityID int64) (*Community, error) {
	var sc Community
	err := st.db.QueryRow(ctx, `SELECT id, name, default_join_role::text, created_at FROM communities WHERE id = $1`, communityID).
		Scan(&sc.ID, &sc.Name, &sc.DefaultJoinRole, &sc.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such community %d: %w", communityID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &sc, nil
}

// RenameCommunity changes a community's name — the admin page's one "basic
// community settings" action.
func (st *Store) RenameCommunity(ctx context.Context, communityID int64, name string) error {
	tag, err := st.db.Exec(ctx, `UPDATE communities SET name = $1 WHERE id = $2`, name, communityID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such community %d: %w", communityID, ErrNotFound)
	}
	return nil
}

// SetCommunityDefaultJoinRole sets the role granted to someone who joins
// this community automatically by registering for one of its events
// (see RegisterForEvent) — "apply community edited defaults," the
// user's own explicit spec.
func (st *Store) SetCommunityDefaultJoinRole(ctx context.Context, communityID int64, role string) error {
	if role != "staff" && role != "member" {
		return fmt.Errorf("invalid default join role %q", role)
	}
	tag, err := st.db.Exec(ctx, `UPDATE communities SET default_join_role = $1 WHERE id = $2`, role, communityID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such community %d: %w", communityID, ErrNotFound)
	}
	return nil
}

// ListCommunitiesForPerson lists every community personID is currently a
// member of (either role), alphabetically by name.
// CommunityAdminRow is one community as the site-wide admin panel needs it —
// name plus current staff/member counts, no per-person detail (that's
// the community's own admin page's job).
type CommunityAdminRow struct {
	ID          int64
	Name        string
	StaffCount  int
	MemberCount int
	CreatedAt   time.Time
}

// ListAllCommunitiesWithCounts lists every community on the instance, newest
// first — the site admin panel's "Communities" tab.
func (st *Store) ListAllCommunitiesWithCounts(ctx context.Context) ([]CommunityAdminRow, error) {
	rows, err := st.db.Query(ctx, `
		SELECT sc.id, sc.name, sc.created_at,
		       count(*) FILTER (WHERE sm.role = 'staff' AND sm.left_at IS NULL),
		       count(*) FILTER (WHERE sm.role = 'member' AND sm.left_at IS NULL)
		FROM communities sc
		LEFT JOIN community_members sm ON sm.community_id = sc.id
		GROUP BY sc.id, sc.name, sc.created_at
		ORDER BY sc.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommunityAdminRow
	for rows.Next() {
		var r CommunityAdminRow
		if err := rows.Scan(&r.ID, &r.Name, &r.CreatedAt, &r.StaffCount, &r.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (st *Store) ListCommunitiesForPerson(ctx context.Context, personID int64) ([]Community, error) {
	rows, err := st.db.Query(ctx, `
		SELECT DISTINCT sc.id, sc.name, sc.created_at
		FROM communities sc JOIN community_members m ON m.community_id = sc.id
		WHERE m.person_id = $1 AND m.left_at IS NULL
		ORDER BY sc.name`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Community
	for rows.Next() {
		var sc Community
		if err := rows.Scan(&sc.ID, &sc.Name, &sc.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// IsCurrentCommunityMember reports whether personID currently (left_at IS
// NULL) belongs to communityID, instructor or student alike.
func (st *Store) IsCurrentCommunityMember(ctx context.Context, communityID, personID int64) (bool, error) {
	return isCurrentCommunityMember(ctx, st.db, communityID, personID, "")
}

// IsCurrentCommunityStaff reports whether personID is currently a
// member of communityID at the instructor tier specifically — the
// permission gate for every instructor-only action (membership
// management, catalog add/fork/remove, submission review, rename).
func (st *Store) IsCurrentCommunityStaff(ctx context.Context, communityID, personID int64) (bool, error) {
	return isCurrentCommunityMember(ctx, st.db, communityID, personID, "staff")
}

// isCurrentCommunityMember is IsCurrentCommunityMember/IsCurrentCommunityStaff's
// shared implementation, taking a dbtx (pool or an already-open tx) —
// same reasoning as isCurrentBandMember in bands.go: a method that's
// already mid-transaction must run this check against THAT SAME tx
// handle, never reach back out to the pool for a second connection while
// the first one's transaction is still open (that's exactly the
// AddBandMember connection-pool bug this phase's bugfix commit
// addresses). role == "" means "any role"; otherwise it's an exact
// match ("staff").
func isCurrentCommunityMember(ctx context.Context, q dbtx, communityID, personID int64, role string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM community_members
		              WHERE community_id = $1 AND person_id = $2 AND left_at IS NULL
		                AND ($3 = '' OR role = $3::community_member_role))`,
		communityID, personID, role).Scan(&ok)
	return ok, err
}

// ListCommunityMembers lists communityID's membership rows: every row ever
// inserted if includePast, otherwise only current (left_at IS NULL)
// ones. Newest first — same shape/ordering as ListBandMembers.
func (st *Store) ListCommunityMembers(ctx context.Context, communityID int64, includePast bool) ([]CommunityMember, error) {
	q := `
		SELECT m.id, m.community_id, m.person_id, p.username, p.display_name, m.role::text,
		       m.joined_at, m.left_at, m.invited_by_person_id, coalesce(ip.username, '')
		FROM community_members m
		JOIN people p ON p.id = m.person_id
		LEFT JOIN people ip ON ip.id = m.invited_by_person_id
		WHERE m.community_id = $1`
	if !includePast {
		q += ` AND m.left_at IS NULL`
	}
	q += ` ORDER BY m.joined_at DESC, m.id DESC`
	rows, err := st.db.Query(ctx, q, communityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommunityMember
	for rows.Next() {
		var m CommunityMember
		if err := rows.Scan(&m.ID, &m.CommunityID, &m.PersonID, &m.Username, &m.DisplayName, &m.Role,
			&m.JoinedAt, &m.LeftAt, &m.InvitedByPersonID, &m.InvitedByUsername); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddCommunityMember adds personID to communityID at role ("staff" or
// "member"), invited by invitedByPersonID — a brand-new community_members
// row, never a reuse of a past one even if personID left and is
// rejoining (same rule as AddBandMember/migration 0010). Errors if
// personID is already a current member of the community.
//
// The already-current check runs against THIS SAME tx (isCurrentCommunityMember
// with tx, not st.IsCurrentCommunityMember with the pool) — the exact
// pattern AddBandMember got wrong before this phase's bugfix: issuing a
// second, pool-level query while this tx is still open risks a
// connection-pool deadlock/exhaustion under load.
func (st *Store) AddCommunityMember(ctx context.Context, communityID, personID int64, role string, invitedByPersonID int64) (int64, error) {
	if role != "staff" && role != "member" {
		return 0, fmt.Errorf("invalid community member role %q", role)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	already, err := isCurrentCommunityMember(ctx, tx, communityID, personID, "")
	if err != nil {
		return 0, err
	}
	if already {
		return 0, fmt.Errorf("that person is already a current member of this community")
	}

	var invitedBy *int64
	if invitedByPersonID != 0 {
		invitedBy = &invitedByPersonID
	}
	var memberID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO community_members (community_id, person_id, role, invited_by_person_id)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		communityID, personID, role, invitedBy).Scan(&memberID); err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return memberID, nil
}

// RemoveCommunityMember ends personID's current membership in communityID
// (sets left_at = now() on their current row — history is never
// destroyed, same rule as RemoveBandMember). Refuses to remove a
// community's last current instructor (same reasoning as CreateCommunity: a
// community with no instructor left would be a dead end nobody could ever
// manage again).
func (st *Store) RemoveCommunityMember(ctx context.Context, communityID, personID int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	row, err := lockCurrentCommunityMembership(ctx, tx, communityID, personID)
	if err != nil {
		return err
	}
	if row.Role == "staff" {
		if err := requireAnotherCurrentInstructor(ctx, tx, communityID, personID); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE community_members SET left_at = now() WHERE id = $1`, row.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetCommunityMemberRole promotes/demotes personID's current membership in
// communityID between "staff" and "member". Refuses to demote a
// community's last current instructor — see RemoveCommunityMember's doc
// comment for why.
func (st *Store) SetCommunityMemberRole(ctx context.Context, communityID, personID int64, role string) error {
	if role != "staff" && role != "member" {
		return fmt.Errorf("invalid community member role %q", role)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	row, err := lockCurrentCommunityMembership(ctx, tx, communityID, personID)
	if err != nil {
		return err
	}
	if row.Role == "staff" && role == "member" {
		if err := requireAnotherCurrentInstructor(ctx, tx, communityID, personID); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE community_members SET role = $1 WHERE id = $2`, role, row.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// requireAnotherCurrentInstructor errors unless communityID has a current
// instructor OTHER than exceptPersonID — called (inside the same tx)
// before demoting/removing an instructor, so a community can never be left
// with zero instructors able to manage it.
func requireAnotherCurrentInstructor(ctx context.Context, tx pgx.Tx, communityID, exceptPersonID int64) error {
	var ok bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM community_members
		              WHERE community_id = $1 AND role = 'staff' AND left_at IS NULL AND person_id != $2)`,
		communityID, exceptPersonID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("a community must keep at least one current staff member: %w", ErrForbidden)
	}
	return nil
}

// currentCommunityMembershipRow is lockCurrentCommunityMembership's result.
type currentCommunityMembershipRow struct {
	ID       int64
	CommunityID int64
	PersonID int64
	Role     string
}

// lockCurrentCommunityMembership locks (FOR UPDATE) and returns personID's
// current membership row in communityID, or ErrNotFound if they have none.
func lockCurrentCommunityMembership(ctx context.Context, tx pgx.Tx, communityID, personID int64) (currentCommunityMembershipRow, error) {
	var row currentCommunityMembershipRow
	err := tx.QueryRow(ctx, `
		SELECT id, community_id, person_id, role::text
		FROM community_members WHERE community_id = $1 AND person_id = $2 AND left_at IS NULL
		FOR UPDATE`, communityID, personID).
		Scan(&row.ID, &row.CommunityID, &row.PersonID, &row.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return currentCommunityMembershipRow{}, fmt.Errorf("no current membership for person %d in community %d: %w", personID, communityID, ErrNotFound)
	}
	if err != nil {
		return currentCommunityMembershipRow{}, err
	}
	return row, nil
}
