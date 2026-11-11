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
// current membership. Role is that membership's role NAME (the 3
// built-ins are "staff"/"member"/"guest"; a custom role carries
// whatever name its community gave it) — see roles.go.
type CommunityMember struct {
	ID                int64
	CommunityID       int64
	PersonID          int64
	Username          string
	DisplayName       string
	RoleID            int64
	Role              string
	JoinedAt          time.Time
	LeftAt            *time.Time
	InvitedByPersonID *int64
	InvitedByUsername string
	// JoinedViaBandID/JoinedViaBandName: non-nil when this stint was
	// granted automatically because the person joined a band linked to
	// this community (see community_bands.go's
	// ensureCommunityMembershipForBandMember) rather than joined
	// directly. Nil forever after, even if that band later unlinks or
	// the person leaves it — see migration 0060's doc comment.
	JoinedViaBandID   *int64
	JoinedViaBandName string
}

// IsCurrent reports whether m is an active (not left) membership row.
func (m CommunityMember) IsCurrent() bool { return m.LeftAt == nil }

// IsStaff reports whether m currently holds the built-in staff role —
// used only where a true/false "is this the staff tier" reading is
// wanted (e.g. a role-editor's "this is the staff role" marker); actual
// permission checks go through HasCommunityPrivilege instead, since a
// community may have re-tuned what "staff" itself grants.
func (m CommunityMember) IsStaff() bool { return m.Role == "staff" }

// CreateCommunity makes a new community, seeds its 3 built-in roles
// (staff/member/guest — see seedDefaultCommunityRoles), and inserts
// creatorPersonID as its first member holding the staff role — a
// community with no staff at all would be a dead end nobody could ever
// manage, same "first member must be able to bootstrap the rest"
// reasoning CreateBand uses, except here the tier matters: only staff
// can add further members at all, so the creator cannot start out as
// anything else.
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
	staffRoleID, err := seedDefaultCommunityRoles(ctx, tx, communityID)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO community_members (community_id, person_id, role_id) VALUES ($1, $2, $3)`,
		communityID, creatorPersonID, staffRoleID); err != nil {
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
// user's own explicit spec. role must name one of this community's own
// roles (built-in or custom) — see resolveCommunityRoleByName.
func (st *Store) SetCommunityDefaultJoinRole(ctx context.Context, communityID int64, role string) error {
	var exists bool
	if err := st.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM community_roles WHERE community_id = $1 AND name = $2)`, communityID, role).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("no such role %q in this community", role)
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
		       count(*) FILTER (WHERE cr.builtin_key = 'staff' AND upper_inf(sm.validity)),
		       count(*) FILTER (WHERE cr.builtin_key != 'staff' AND upper_inf(sm.validity))
		FROM communities sc
		LEFT JOIN community_members sm ON sm.community_id = sc.id
		LEFT JOIN community_roles cr ON cr.id = sm.role_id
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
		WHERE m.person_id = $1 AND upper_inf(m.validity)
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

// CommunityCardRow is one community as the person's own "My communities"
// listing needs it — same digest shape bands.html's own card uses
// (member count, default songbook size, the viewer's own role) so the two
// listing pages read as one consistent picture, not two different UIs.
type CommunityCardRow struct {
	ID          int64
	Name        string
	MemberCount int
	SongCount   int
	MyRole      string
}

// ListCommunityCardsForPerson is ListCommunitiesForPerson plus the counts
// a listing card shows — one query per community would N+1 on a person
// in many communities, so this does it in a single round trip instead.
func (st *Store) ListCommunityCardsForPerson(ctx context.Context, personID int64) ([]CommunityCardRow, error) {
	rows, err := st.db.Query(ctx, `
		SELECT sc.id, sc.name, my_role.name,
		       count(DISTINCT cm.person_id) FILTER (WHERE upper_inf(cm.validity)),
		       count(DISTINCT se.variant_id)
		FROM communities sc
		JOIN community_members my ON my.community_id = sc.id AND my.person_id = $1 AND upper_inf(my.validity)
		JOIN community_roles my_role ON my_role.id = my.role_id
		LEFT JOIN community_members cm ON cm.community_id = sc.id
		LEFT JOIN songbooks sb ON sb.owner_community_id = sc.id AND sb.is_default
		LEFT JOIN songbook_entries se ON se.songbook_id = sb.id
		GROUP BY sc.id, sc.name, my_role.name
		ORDER BY sc.name`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommunityCardRow
	for rows.Next() {
		var r CommunityCardRow
		if err := rows.Scan(&r.ID, &r.Name, &r.MyRole, &r.MemberCount, &r.SongCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// IsCurrentCommunityMember reports whether personID currently (left_at IS
// NULL) belongs to communityID, instructor or student alike.
func (st *Store) IsCurrentCommunityMember(ctx context.Context, communityID, personID int64) (bool, error) {
	return isCurrentCommunityMember(ctx, st.db, communityID, personID, "")
}

// IsCurrentCommunityStaff reports whether personID currently holds the
// built-in staff role in communityID specifically — used only where a
// true/false "is this the staff tier" reading is wanted (see
// CommunityMember.IsStaff's own doc comment); actual permission checks
// should use HasCommunityPrivilege instead.
func (st *Store) IsCurrentCommunityStaff(ctx context.Context, communityID, personID int64) (bool, error) {
	return isCurrentCommunityMember(ctx, st.db, communityID, personID, "staff")
}

// isCurrentCommunityMember is IsCurrentCommunityMember/IsCurrentCommunityStaff's
// shared implementation, taking a dbtx (pool or an already-open tx) —
// same reasoning as isCurrentBandMember in bands.go: a method that's
// already mid-transaction must run this check against THAT SAME tx
// handle, never reach back out to the pool for a second connection while
// the first one's transaction is still open. builtinKey == "" means
// "any role"; otherwise it's an exact match against that role's
// builtin_key ("staff"/"member"/"guest" — never a custom role's name,
// since a custom role has no builtin_key to match).
func isCurrentCommunityMember(ctx context.Context, q dbtx, communityID, personID int64, builtinKey string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM community_members cm
		              JOIN community_roles cr ON cr.id = cm.role_id
		              WHERE cm.community_id = $1 AND cm.person_id = $2 AND upper_inf(cm.validity)
		                AND ($3 = '' OR cr.builtin_key = $3))`,
		communityID, personID, builtinKey).Scan(&ok)
	return ok, err
}

// ListCommunityMembers lists communityID's membership rows: every row ever
// inserted if includePast, otherwise only current (left_at IS NULL)
// ones. Newest first — same shape/ordering as ListBandMembers.
func (st *Store) ListCommunityMembers(ctx context.Context, communityID int64, includePast bool) ([]CommunityMember, error) {
	q := `
		SELECT m.id, m.community_id, m.person_id, p.username, p.display_name, m.role_id, cr.name,
		       lower(m.validity), CASE WHEN upper_inf(m.validity) THEN NULL ELSE upper(m.validity) END,
		       m.invited_by_person_id, coalesce(ip.username, ''), m.joined_via_band_id, coalesce(vb.name, '')
		FROM community_members m
		JOIN people p ON p.id = m.person_id
		JOIN community_roles cr ON cr.id = m.role_id
		LEFT JOIN people ip ON ip.id = m.invited_by_person_id
		LEFT JOIN bands vb ON vb.id = m.joined_via_band_id
		WHERE m.community_id = $1`
	if !includePast {
		q += ` AND upper_inf(m.validity)`
	}
	q += ` ORDER BY lower(m.validity) DESC, m.id DESC`
	rows, err := st.db.Query(ctx, q, communityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommunityMember
	for rows.Next() {
		var m CommunityMember
		if err := rows.Scan(&m.ID, &m.CommunityID, &m.PersonID, &m.Username, &m.DisplayName, &m.RoleID, &m.Role,
			&m.JoinedAt, &m.LeftAt, &m.InvitedByPersonID, &m.InvitedByUsername, &m.JoinedViaBandID, &m.JoinedViaBandName); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddCommunityMember adds personID to communityID holding the role named
// role (one of this community's own roles — built-in or custom),
// invited by invitedByPersonID — a brand-new community_members row,
// never a reuse of a past one even if personID left and is rejoining
// (same rule as AddBandMember/migration 0010). Errors if personID is
// already a current member of the community.
//
// The already-current check runs against THIS SAME tx (isCurrentCommunityMember
// with tx, not st.IsCurrentCommunityMember with the pool) — the exact
// pattern AddBandMember got wrong before this phase's bugfix: issuing a
// second, pool-level query while this tx is still open risks a
// connection-pool deadlock/exhaustion under load.
func (st *Store) AddCommunityMember(ctx context.Context, communityID, personID int64, role string, invitedByPersonID int64) (int64, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	roleID, err := resolveCommunityRoleByName(ctx, tx, communityID, role)
	if err != nil {
		return 0, err
	}

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
		INSERT INTO community_members (community_id, person_id, role_id, invited_by_person_id)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		communityID, personID, roleID, invitedBy).Scan(&memberID); err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return memberID, nil
}

// RemoveCommunityMember ends personID's current membership in communityID
// (sets left_at = now() on their current row — history is never
// destroyed, same rule as RemoveBandMember). Refuses to remove the
// community's last member holding manage_members (same reasoning as
// CreateCommunity: a community with nobody left who can manage it would
// be a dead end).
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
	hadManage, err := hasCommunityPrivilege(ctx, tx, communityID, personID, PrivManageMembers)
	if err != nil {
		return err
	}
	if hadManage {
		if err := requireAnotherManager(ctx, tx, communityID, personID); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE community_members SET validity = tstzrange(lower(validity), now()) WHERE id = $1`, row.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetCommunityMemberRole reassigns personID's current membership in
// communityID to the role named role (one of this community's own
// roles). Refuses to reassign away the community's last member holding
// manage_members — see RemoveCommunityMember's doc comment for why.
func (st *Store) SetCommunityMemberRole(ctx context.Context, communityID, personID int64, role string) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	newRoleID, err := resolveCommunityRoleByName(ctx, tx, communityID, role)
	if err != nil {
		return err
	}
	row, err := lockCurrentCommunityMembership(ctx, tx, communityID, personID)
	if err != nil {
		return err
	}
	hadManage, err := hasCommunityPrivilege(ctx, tx, communityID, personID, PrivManageMembers)
	if err != nil {
		return err
	}
	willManage, err := roleHasPrivilege(ctx, tx, newRoleID, PrivManageMembers)
	if err != nil {
		return err
	}
	if hadManage && !willManage {
		if err := requireAnotherManager(ctx, tx, communityID, personID); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE community_members SET role_id = $1 WHERE id = $2`, newRoleID, row.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// roleHasPrivilege reports whether roleID's privilege set includes priv.
func roleHasPrivilege(ctx context.Context, q dbtx, roleID int64, priv string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM community_role_privileges WHERE role_id = $1 AND privilege = $2)`, roleID, priv).Scan(&ok)
	return ok, err
}

// requireAnotherManager errors unless communityID has a current member,
// OTHER than exceptPersonID, whose role grants manage_members — called
// (inside the same tx) before removing/reassigning a manager, so a
// community can never be left with nobody able to manage it.
func requireAnotherManager(ctx context.Context, tx pgx.Tx, communityID, exceptPersonID int64) error {
	var ok bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM community_members cm
			JOIN community_role_privileges crp ON crp.role_id = cm.role_id
			WHERE cm.community_id = $1 AND upper_inf(cm.validity) AND cm.person_id != $2
			  AND crp.privilege = $3)`,
		communityID, exceptPersonID, PrivManageMembers).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("a community must keep at least one member who can manage it: %w", ErrForbidden)
	}
	return nil
}

// currentCommunityMembershipRow is lockCurrentCommunityMembership's result.
type currentCommunityMembershipRow struct {
	ID          int64
	CommunityID int64
	PersonID    int64
	RoleID      int64
}

// lockCurrentCommunityMembership locks (FOR UPDATE) and returns personID's
// current membership row in communityID, or ErrNotFound if they have none.
func lockCurrentCommunityMembership(ctx context.Context, tx pgx.Tx, communityID, personID int64) (currentCommunityMembershipRow, error) {
	var row currentCommunityMembershipRow
	err := tx.QueryRow(ctx, `
		SELECT id, community_id, person_id, role_id
		FROM community_members WHERE community_id = $1 AND person_id = $2 AND upper_inf(validity)
		FOR UPDATE`, communityID, personID).
		Scan(&row.ID, &row.CommunityID, &row.PersonID, &row.RoleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return currentCommunityMembershipRow{}, fmt.Errorf("no current membership for person %d in community %d: %w", personID, communityID, ErrNotFound)
	}
	if err != nil {
		return currentCommunityMembershipRow{}, err
	}
	return row, nil
}
