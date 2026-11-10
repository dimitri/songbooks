package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Community privileges: the fixed, code-defined catalog a role's
// privilege set is drawn from (see migration 0052's own doc comment).
// Adding a new gated action means adding a constant here and a check at
// its call site — the DB only stores which of these strings a role
// grants, never the catalog itself.
const (
	PrivEditRepertoire   = "edit_repertoire"   // add/fork/remove a song in the community's songbook, edit its content
	PrivManageMembers    = "manage_members"    // add/remove members, assign roles, create invites/QR links
	PrivManageEvents     = "manage_events"     // create events, assign/remove event managers
	PrivManageSubmissions = "manage_submissions" // accept/reject a legacy work-review submission
	PrivManageSettings   = "manage_settings"   // rename the community, set its default join role
)

// AllCommunityPrivileges lists every privilege in the catalog, in the
// order the role-editor UI presents them.
var AllCommunityPrivileges = []string{
	PrivEditRepertoire, PrivManageMembers, PrivManageEvents, PrivManageSubmissions, PrivManageSettings,
}

// CommunityRole is one named, per-community set of privileges. The three
// built-in roles (builtin_key staff/member/guest) exist in every
// community from creation; builtin_key is nil for a custom role.
type CommunityRole struct {
	ID          int64
	CommunityID int64
	Name        string
	BuiltinKey  string // "" for a custom role
	Privileges  []string
}

// IsBuiltin reports whether r is one of the 3 seeded roles (its
// privileges can still be edited, but it can't be renamed or deleted).
func (r CommunityRole) IsBuiltin() bool { return r.BuiltinKey != "" }

// HasPrivilege reports whether r's privilege set includes priv.
func (r CommunityRole) HasPrivilege(priv string) bool {
	for _, p := range r.Privileges {
		if p == priv {
			return true
		}
	}
	return false
}

// seedDefaultCommunityRoles creates the 3 built-in roles for a
// brand-new community (within tx, as part of CreateCommunity) and
// returns the staff role's id, for the creator's own membership row.
func seedDefaultCommunityRoles(ctx context.Context, tx pgx.Tx, communityID int64) (staffRoleID int64, err error) {
	var memberRoleID, guestRoleID int64
	if err := tx.QueryRow(ctx, `INSERT INTO community_roles (community_id, name, builtin_key) VALUES ($1, 'staff', 'staff') RETURNING id`, communityID).Scan(&staffRoleID); err != nil {
		return 0, err
	}
	if err := tx.QueryRow(ctx, `INSERT INTO community_roles (community_id, name, builtin_key) VALUES ($1, 'member', 'member') RETURNING id`, communityID).Scan(&memberRoleID); err != nil {
		return 0, err
	}
	if err := tx.QueryRow(ctx, `INSERT INTO community_roles (community_id, name, builtin_key) VALUES ($1, 'guest', 'guest') RETURNING id`, communityID).Scan(&guestRoleID); err != nil {
		return 0, err
	}
	for _, p := range AllCommunityPrivileges {
		if _, err := tx.Exec(ctx, `INSERT INTO community_role_privileges (role_id, privilege) VALUES ($1, $2)`, staffRoleID, p); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO community_role_privileges (role_id, privilege) VALUES ($1, $2)`, memberRoleID, PrivEditRepertoire); err != nil {
		return 0, err
	}
	_ = guestRoleID // no privileges for guest
	return staffRoleID, nil
}

// ListCommunityRoles lists every role communityID has defined, built-in
// roles first (staff, member, guest, in that fixed order) then any
// custom roles alphabetically — the role-editor and member-assignment
// dropdown's own ordering.
func (st *Store) ListCommunityRoles(ctx context.Context, communityID int64) ([]CommunityRole, error) {
	rows, err := st.db.Query(ctx, `
		SELECT cr.id, cr.community_id, cr.name, coalesce(cr.builtin_key, ''),
		       coalesce(array_agg(crp.privilege) FILTER (WHERE crp.privilege IS NOT NULL), '{}')
		FROM community_roles cr
		LEFT JOIN community_role_privileges crp ON crp.role_id = cr.id
		WHERE cr.community_id = $1
		GROUP BY cr.id
		ORDER BY CASE cr.builtin_key WHEN 'staff' THEN 0 WHEN 'member' THEN 1 WHEN 'guest' THEN 2 ELSE 3 END, cr.name`,
		communityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommunityRole
	for rows.Next() {
		var r CommunityRole
		if err := rows.Scan(&r.ID, &r.CommunityID, &r.Name, &r.BuiltinKey, &r.Privileges); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetCommunityRole loads one role by id, with its privilege set.
func (st *Store) GetCommunityRole(ctx context.Context, roleID int64) (*CommunityRole, error) {
	var r CommunityRole
	err := st.db.QueryRow(ctx, `
		SELECT cr.id, cr.community_id, cr.name, coalesce(cr.builtin_key, ''),
		       coalesce(array_agg(crp.privilege) FILTER (WHERE crp.privilege IS NOT NULL), '{}')
		FROM community_roles cr
		LEFT JOIN community_role_privileges crp ON crp.role_id = cr.id
		WHERE cr.id = $1
		GROUP BY cr.id`, roleID).
		Scan(&r.ID, &r.CommunityID, &r.Name, &r.BuiltinKey, &r.Privileges)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("no such role %d: %w", roleID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// validPrivilege reports whether p is a real entry in the catalog —
// callers reject anything else rather than silently storing junk no
// check will ever look for.
func validPrivilege(p string) bool {
	for _, c := range AllCommunityPrivileges {
		if p == c {
			return true
		}
	}
	return false
}

// CreateCommunityRole makes a new custom role in communityID with the
// given name and privilege set. name must be unique within the
// community (including against the 3 built-in names).
func (st *Store) CreateCommunityRole(ctx context.Context, communityID int64, name string, privileges []string) (int64, error) {
	for _, p := range privileges {
		if !validPrivilege(p) {
			return 0, fmt.Errorf("invalid privilege %q", p)
		}
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var roleID int64
	if err := tx.QueryRow(ctx, `INSERT INTO community_roles (community_id, name) VALUES ($1, $2) RETURNING id`, communityID, name).Scan(&roleID); err != nil {
		if isUniqueViolation(err) {
			return 0, fmt.Errorf("a role named %q already exists in this community: %w", name, ErrConflict)
		}
		return 0, err
	}
	for _, p := range privileges {
		if _, err := tx.Exec(ctx, `INSERT INTO community_role_privileges (role_id, privilege) VALUES ($1, $2)`, roleID, p); err != nil {
			return 0, err
		}
	}
	return roleID, tx.Commit(ctx)
}

// SetCommunityRolePrivileges replaces roleID's entire privilege set —
// works for both a custom role and a built-in one (a community may want
// to loosen/tighten what "member" or "staff" itself grants), never
// renames or deletes it.
func (st *Store) SetCommunityRolePrivileges(ctx context.Context, roleID int64, privileges []string) error {
	for _, p := range privileges {
		if !validPrivilege(p) {
			return fmt.Errorf("invalid privilege %q", p)
		}
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM community_role_privileges WHERE role_id = $1`, roleID); err != nil {
		return err
	}
	for _, p := range privileges {
		if _, err := tx.Exec(ctx, `INSERT INTO community_role_privileges (role_id, privilege) VALUES ($1, $2)`, roleID, p); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// RenameCommunityRole renames a CUSTOM role (refuses — ErrForbidden — for
// one of the 3 built-ins, whose names other code/UI relies on).
func (st *Store) RenameCommunityRole(ctx context.Context, roleID int64, name string) error {
	tag, err := st.db.Exec(ctx, `UPDATE community_roles SET name = $1 WHERE id = $2 AND builtin_key IS NULL`, name, roleID)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("a role named %q already exists in this community: %w", name, ErrConflict)
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such custom role %d (built-in roles can't be renamed): %w", roleID, ErrForbidden)
	}
	return nil
}

// DeleteCommunityRole removes a CUSTOM role (refuses for a built-in).
// Refuses — ErrConflict — if any current member still holds it; reassign
// them first (SetCommunityMemberRoleID), same "can't delete what's in
// use" shape as other list-backed deletes in this codebase.
func (st *Store) DeleteCommunityRole(ctx context.Context, roleID int64) error {
	var builtin bool
	var inUse bool
	err := st.db.QueryRow(ctx, `
		SELECT builtin_key IS NOT NULL,
		       EXISTS(SELECT 1 FROM community_members WHERE role_id = $1 AND upper_inf(validity))
		FROM community_roles WHERE id = $1`, roleID).Scan(&builtin, &inUse)
	if err == pgx.ErrNoRows {
		return fmt.Errorf("no such role %d: %w", roleID, ErrNotFound)
	}
	if err != nil {
		return err
	}
	if builtin {
		return fmt.Errorf("built-in roles can't be deleted: %w", ErrForbidden)
	}
	if inUse {
		return fmt.Errorf("this role is still assigned to a current member: %w", ErrConflict)
	}
	_, err = st.db.Exec(ctx, `DELETE FROM community_roles WHERE id = $1`, roleID)
	return err
}

// HasCommunityPrivilege reports whether personID's CURRENT role in
// communityID grants priv — the central gate every fine-grained
// community action checks, in place of the old flat staff/member check.
func (st *Store) HasCommunityPrivilege(ctx context.Context, communityID, personID int64, priv string) (bool, error) {
	var ok bool
	err := st.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM community_members cm
			JOIN community_role_privileges crp ON crp.role_id = cm.role_id
			WHERE cm.community_id = $1 AND cm.person_id = $2 AND upper_inf(cm.validity) AND crp.privilege = $3
		)`, communityID, personID, priv).Scan(&ok)
	return ok, err
}

// HasAnyCommunityManagePrivilege reports whether personID's current role
// in communityID grants at least one of the 4 management privileges
// (edit_repertoire deliberately excluded — that's "can edit content,"
// not "can administer the community") — drives whether a viewer sees
// the admin page's nav link at all, same set requireCommunityAnyManagePrivilege
// gates the admin page itself on.
func (st *Store) HasAnyCommunityManagePrivilege(ctx context.Context, communityID, personID int64) (bool, error) {
	var ok bool
	err := st.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM community_members cm
			JOIN community_role_privileges crp ON crp.role_id = cm.role_id
			WHERE cm.community_id = $1 AND cm.person_id = $2 AND upper_inf(cm.validity)
			  AND crp.privilege IN ($3, $4, $5, $6)
		)`, communityID, personID, PrivManageMembers, PrivManageEvents, PrivManageSubmissions, PrivManageSettings).Scan(&ok)
	return ok, err
}

// hasCommunityPrivilege is HasCommunityPrivilege's tx-scoped twin, for a
// caller already inside an open transaction — same dbtx reasoning as
// isCurrentBandMember/isCurrentCommunityMember.
func hasCommunityPrivilege(ctx context.Context, q dbtx, communityID, personID int64, priv string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM community_members cm
			JOIN community_role_privileges crp ON crp.role_id = cm.role_id
			WHERE cm.community_id = $1 AND cm.person_id = $2 AND upper_inf(cm.validity) AND crp.privilege = $3
		)`, communityID, personID, priv).Scan(&ok)
	return ok, err
}

// resolveCommunityRoleByName looks up communityID's own role named
// name (case-sensitive match against that role's exact stored name,
// e.g. "staff"/"member"/"guest" for a built-in, or a custom role's own
// name) — used wherever a role is chosen by name rather than id: an
// invite link's embedded target role, a community's default_join_role.
// Falls back to the community's "member" role if name doesn't match
// anything (covers a deleted custom role a stale invite still names).
func resolveCommunityRoleByName(ctx context.Context, q dbtx, communityID int64, name string) (int64, error) {
	var roleID int64
	err := q.QueryRow(ctx, `SELECT id FROM community_roles WHERE community_id = $1 AND name = $2`, communityID, name).Scan(&roleID)
	if err == pgx.ErrNoRows {
		err = q.QueryRow(ctx, `SELECT id FROM community_roles WHERE community_id = $1 AND builtin_key = 'member'`, communityID).Scan(&roleID)
	}
	return roleID, err
}
