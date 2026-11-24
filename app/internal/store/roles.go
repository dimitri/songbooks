package store

import (
	"context"
	"fmt"
	"strings"

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
	PrivDownloadFiles    = "download_files"    // download, play and open the files of the songbooks one has
	PrivManageFiles      = "manage_files"      // add, move and remove a songbook's files
)

// AllCommunityPrivileges lists every privilege in the catalog, in the
// order the role-editor UI presents them.
var AllCommunityPrivileges = []string{
	PrivEditRepertoire, PrivDownloadFiles, PrivManageFiles, PrivManageMembers, PrivManageEvents, PrivManageSubmissions, PrivManageSettings,
}

// Role is one named set of privileges in a group — a community's
// (community_roles, CommunityRole) or a band's (band_roles, BandRole):
// the same model and the same code for both (roleScope). The built-in
// roles (builtin_key staff/member/guest in a community, core/guest in a
// band) exist in every group from creation; BuiltinKey is "" for a
// group's own role.
type Role struct {
	ID         int64
	GroupID    int64 // the community's or the band's id
	Name       string
	BuiltinKey string // "" for a custom role
	Privileges []string
}

// CommunityRole is a community's Role.
type CommunityRole = Role

// IsBuiltin reports whether r is one of the seeded roles (its
// privileges can still be edited, but it can't be renamed or deleted).
func (r Role) IsBuiltin() bool { return r.BuiltinKey != "" }

// HasPrivilege reports whether r's privilege set includes priv.
func (r Role) HasPrivilege(priv string) bool {
	for _, p := range r.Privileges {
		if p == priv {
			return true
		}
	}
	return false
}

// roleScope is where one kind of group keeps its roles: the tables, the
// privilege catalog, the built-ins' order. Every role operation is
// written once against it (communityRoles, bandRoles).
type roleScope struct {
	roles    string   // the roles table
	privs    string   // the role → privilege table
	members  string   // the membership table (role_id, validity)
	groupCol string   // the group's column in roles and members
	group    string   // "community" or "band", for messages
	catalog  []string // the privileges a role may grant
	order    string   // SQL: the built-ins first, in their order
	// keeper: a privilege the group must keep someone holding (the one
	// that edits the roles): a change that leaves no current member with
	// it is refused. "" for none.
	keeper string
	// fallback: the built-in a past membership of a deleted role is
	// moved to.
	fallback string
}

var communityRoles = roleScope{
	roles: "community_roles", privs: "community_role_privileges", members: "community_members",
	groupCol: "community_id", group: "community", catalog: AllCommunityPrivileges,
	order:    "CASE r.builtin_key WHEN 'staff' THEN 0 WHEN 'member' THEN 1 WHEN 'guest' THEN 2 ELSE 3 END, r.name",
	fallback: "member",
}

func (sc roleScope) validPrivilege(p string) bool {
	for _, c := range sc.catalog {
		if p == c {
			return true
		}
	}
	return false
}

func (sc roleScope) checkPrivileges(privileges []string) error {
	for _, p := range privileges {
		if !sc.validPrivilege(p) {
			return fmt.Errorf("invalid privilege %q: %w", p, ErrInvalid)
		}
	}
	return nil
}

// roleSelect is the role columns, privileges aggregated, for a WHERE.
func (sc roleScope) roleSelect(where string) string {
	return fmt.Sprintf(`
		SELECT r.id, r.%[3]s, r.name, coalesce(r.builtin_key, ''),
		       coalesce(array_agg(rp.privilege ORDER BY rp.privilege) FILTER (WHERE rp.privilege IS NOT NULL), '{}')
		FROM %[1]s r
		LEFT JOIN %[2]s rp ON rp.role_id = r.id
		WHERE %[4]s
		GROUP BY r.id`, sc.roles, sc.privs, sc.groupCol, where)
}

func (sc roleScope) listRoles(ctx context.Context, q dbtx, groupID int64) ([]Role, error) {
	rows, err := q.Query(ctx, sc.roleSelect("r."+sc.groupCol+" = $1")+" ORDER BY "+sc.order, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.GroupID, &r.Name, &r.BuiltinKey, &r.Privileges); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// getRole loads roleID; groupID 0 takes it from any group, else only
// from that one (ErrNotFound otherwise).
func (sc roleScope) getRole(ctx context.Context, q dbtx, groupID, roleID int64) (*Role, error) {
	var r Role
	err := q.QueryRow(ctx, sc.roleSelect("r.id = $1 AND ($2 = 0 OR r."+sc.groupCol+" = $2)"), roleID, groupID).
		Scan(&r.ID, &r.GroupID, &r.Name, &r.BuiltinKey, &r.Privileges)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("no such role %d: %w", roleID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (sc roleScope) createRole(ctx context.Context, db dbBeginner, groupID int64, name string, privileges []string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("a role needs a name: %w", ErrInvalid)
	}
	if err := sc.checkPrivileges(privileges); err != nil {
		return 0, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var roleID int64
	if err := tx.QueryRow(ctx, `INSERT INTO `+sc.roles+` (`+sc.groupCol+`, name) VALUES ($1, $2) RETURNING id`, groupID, name).Scan(&roleID); err != nil {
		if isUniqueViolation(err) {
			return 0, fmt.Errorf("a role named %q already exists in this %s: %w", name, sc.group, ErrConflict)
		}
		return 0, err
	}
	for _, p := range privileges {
		if _, err := tx.Exec(ctx, `INSERT INTO `+sc.privs+` (role_id, privilege) VALUES ($1, $2) ON CONFLICT DO NOTHING`, roleID, p); err != nil {
			return 0, err
		}
	}
	return roleID, tx.Commit(ctx)
}

// keepers counts groupID's current members whose role grants sc.keeper.
func (sc roleScope) keepers(ctx context.Context, q dbtx, groupID int64) (int, error) {
	var n int
	err := q.QueryRow(ctx, `
		SELECT count(*) FROM `+sc.members+` m JOIN `+sc.privs+` rp ON rp.role_id = m.role_id
		WHERE m.`+sc.groupCol+` = $1 AND upper_inf(m.validity) AND rp.privilege = $2`, groupID, sc.keeper).Scan(&n)
	return n, err
}

// keepKeeper is the end of a change made in tx to groupID's roles or
// members: if someone held sc.keeper before (before > 0), someone must
// still, else ErrLastKeeper.
func (sc roleScope) keepKeeper(ctx context.Context, tx pgx.Tx, groupID int64, before int) error {
	if sc.keeper == "" || before == 0 {
		return nil
	}
	after, err := sc.keepers(ctx, tx, groupID)
	if err != nil {
		return err
	}
	if after == 0 {
		return ErrLastKeeper
	}
	return nil
}

// ErrLastKeeper: the change would leave no current member able to
// change the roles.
var ErrLastKeeper = fmt.Errorf("someone must keep the right to manage roles: %w", ErrConflict)

func (sc roleScope) setRolePrivileges(ctx context.Context, db dbBeginner, groupID, roleID int64, privileges []string) error {
	if err := sc.checkPrivileges(privileges); err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	role, err := sc.getRole(ctx, tx, groupID, roleID)
	if err != nil {
		return err
	}
	var before int
	if sc.keeper != "" {
		if before, err = sc.keepers(ctx, tx, role.GroupID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM `+sc.privs+` WHERE role_id = $1`, roleID); err != nil {
		return err
	}
	for _, p := range privileges {
		if _, err := tx.Exec(ctx, `INSERT INTO `+sc.privs+` (role_id, privilege) VALUES ($1, $2) ON CONFLICT DO NOTHING`, roleID, p); err != nil {
			return err
		}
	}
	if err := sc.keepKeeper(ctx, tx, role.GroupID, before); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (sc roleScope) renameRole(ctx context.Context, q dbtx, groupID, roleID int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("a role needs a name: %w", ErrInvalid)
	}
	tag, err := q.Exec(ctx, `UPDATE `+sc.roles+` SET name = $1 WHERE id = $2 AND ($3 = 0 OR `+sc.groupCol+` = $3) AND builtin_key IS NULL`, name, roleID, groupID)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("a role named %q already exists in this %s: %w", name, sc.group, ErrConflict)
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such custom role %d (built-in roles can't be renamed): %w", roleID, ErrForbidden)
	}
	return nil
}

func (sc roleScope) deleteRole(ctx context.Context, db dbBeginner, groupID, roleID int64) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var builtin, inUse bool
	var gid int64
	err = tx.QueryRow(ctx, `
		SELECT builtin_key IS NOT NULL, `+sc.groupCol+`,
		       EXISTS(SELECT 1 FROM `+sc.members+` WHERE role_id = $1 AND upper_inf(validity))
		FROM `+sc.roles+` WHERE id = $1 AND ($2 = 0 OR `+sc.groupCol+` = $2)`, roleID, groupID).Scan(&builtin, &gid, &inUse)
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
	// Past memberships keep their dates, with the fallback built-in.
	if _, err := tx.Exec(ctx, `
		UPDATE `+sc.members+` SET role_id = (SELECT id FROM `+sc.roles+` WHERE `+sc.groupCol+` = $2 AND builtin_key = $3)
		WHERE role_id = $1`, roleID, gid, sc.fallback); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM `+sc.roles+` WHERE id = $1`, roleID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (sc roleScope) hasPrivilege(ctx context.Context, q dbtx, groupID, personID int64, priv string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM `+sc.members+` m
			JOIN `+sc.privs+` rp ON rp.role_id = m.role_id
			WHERE m.`+sc.groupCol+` = $1 AND m.person_id = $2 AND upper_inf(m.validity) AND rp.privilege = $3
		)`, groupID, personID, priv).Scan(&ok)
	return ok, err
}

// privilegesOf lists what personID's current role in groupID grants
// (nil: not a current member).
func (sc roleScope) privilegesOf(ctx context.Context, q dbtx, groupID, personID int64) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT rp.privilege FROM `+sc.members+` m
		JOIN `+sc.privs+` rp ON rp.role_id = m.role_id
		WHERE m.`+sc.groupCol+` = $1 AND m.person_id = $2 AND upper_inf(m.validity)`, groupID, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
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
	for _, p := range []string{PrivEditRepertoire, PrivDownloadFiles} {
		if _, err := tx.Exec(ctx, `INSERT INTO community_role_privileges (role_id, privilege) VALUES ($1, $2)`, memberRoleID, p); err != nil {
			return 0, err
		}
	}
	_ = guestRoleID // no privileges for guest
	return staffRoleID, nil
}

// The community's roles (the operations above, for communities).

// ListCommunityRoles lists every role communityID has defined, built-in
// roles first (staff, member, guest, in that fixed order) then any
// custom roles alphabetically — the role-editor and member-assignment
// dropdown's own ordering.
func (st *Store) ListCommunityRoles(ctx context.Context, communityID int64) ([]CommunityRole, error) {
	return communityRoles.listRoles(ctx, st.db, communityID)
}

// GetCommunityRole loads one role by id, with its privilege set.
func (st *Store) GetCommunityRole(ctx context.Context, roleID int64) (*CommunityRole, error) {
	return communityRoles.getRole(ctx, st.db, 0, roleID)
}

// validPrivilege reports whether p is a real entry in the community
// catalog — callers reject anything else rather than silently storing
// junk no check will ever look for.
func validPrivilege(p string) bool { return communityRoles.validPrivilege(p) }

// CreateCommunityRole makes a new custom role in communityID with the
// given name and privilege set. name must be unique within the
// community (including against the 3 built-in names).
func (st *Store) CreateCommunityRole(ctx context.Context, communityID int64, name string, privileges []string) (int64, error) {
	return communityRoles.createRole(ctx, st.db, communityID, name, privileges)
}

// SetCommunityRolePrivileges replaces roleID's entire privilege set —
// works for both a custom role and a built-in one (a community may want
// to loosen/tighten what "member" or "staff" itself grants), never
// renames or deletes it. Whichever community it's in: the HTTP layer
// goes through GroupRoles, which holds it to the community in the path.
func (st *Store) SetCommunityRolePrivileges(ctx context.Context, roleID int64, privileges []string) error {
	return communityRoles.setRolePrivileges(ctx, st.db, 0, roleID, privileges)
}

// RenameCommunityRole renames a CUSTOM role (refuses — ErrForbidden — for
// one of the 3 built-ins, whose names other code/UI relies on).
func (st *Store) RenameCommunityRole(ctx context.Context, roleID int64, name string) error {
	return communityRoles.renameRole(ctx, st.db, 0, roleID, name)
}

// DeleteCommunityRole removes a CUSTOM role (refuses for a built-in).
// Refuses — ErrConflict — if any current member still holds it; reassign
// them first (SetCommunityMemberRoleID), same "can't delete what's in
// use" shape as other list-backed deletes in this codebase. Past members
// who held it are moved to "member".
func (st *Store) DeleteCommunityRole(ctx context.Context, roleID int64) error {
	return communityRoles.deleteRole(ctx, st.db, 0, roleID)
}

// dbBeginner is what opens a transaction (the pool).
type dbBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// GroupRoles is one kind of group's roles, every operation held to the
// group whose id it's given (a role of another group is ErrNotFound /
// ErrForbidden): the roles editor's API, the same for a community and a
// band (httpapi/roles.go).
type GroupRoles struct {
	st *Store
	sc roleScope
}

// GroupRoles is kind's ("community" or "band") roles.
func (st *Store) GroupRoles(kind string) GroupRoles {
	if kind == "band" {
		return GroupRoles{st, bandRoles}
	}
	return GroupRoles{st, communityRoles}
}

// Catalog is the privileges a role may grant, in the editor's order.
func (g GroupRoles) Catalog() []string { return g.sc.catalog }

// List lists groupID's roles, the built-ins first.
func (g GroupRoles) List(ctx context.Context, groupID int64) ([]Role, error) {
	return g.sc.listRoles(ctx, g.st.db, groupID)
}

// Get loads groupID's role roleID.
func (g GroupRoles) Get(ctx context.Context, groupID, roleID int64) (*Role, error) {
	return g.sc.getRole(ctx, g.st.db, groupID, roleID)
}

// Create makes a role of groupID's own.
func (g GroupRoles) Create(ctx context.Context, groupID int64, name string, privileges []string) (int64, error) {
	return g.sc.createRole(ctx, g.st.db, groupID, name, privileges)
}

// SetPrivileges replaces what groupID's role roleID grants.
func (g GroupRoles) SetPrivileges(ctx context.Context, groupID, roleID int64, privileges []string) error {
	return g.sc.setRolePrivileges(ctx, g.st.db, groupID, roleID, privileges)
}

// Rename renames groupID's own role roleID.
func (g GroupRoles) Rename(ctx context.Context, groupID, roleID int64, name string) error {
	return g.sc.renameRole(ctx, g.st.db, groupID, roleID, name)
}

// Delete deletes groupID's own role roleID, held by no current member.
func (g GroupRoles) Delete(ctx context.Context, groupID, roleID int64) error {
	return g.sc.deleteRole(ctx, g.st.db, groupID, roleID)
}

// HasCommunityPrivilege reports whether personID's CURRENT role in
// communityID grants priv — the central gate every fine-grained
// community action checks, in place of the old flat staff/member check.
func (st *Store) HasCommunityPrivilege(ctx context.Context, communityID, personID int64, priv string) (bool, error) {
	return communityRoles.hasPrivilege(ctx, st.db, communityID, personID, priv)
}

// HasAnyCommunityManagePrivilege reports whether personID's current role
// in communityID grants at least one of the 5 management privileges
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
			  AND crp.privilege IN ($3, $4, $5, $6, $7)
		)`, communityID, personID, PrivManageMembers, PrivManageEvents, PrivManageSubmissions, PrivManageSettings, PrivManageFiles).Scan(&ok)
	return ok, err
}

// hasCommunityPrivilege is HasCommunityPrivilege's tx-scoped twin, for a
// caller already inside an open transaction — same dbtx reasoning as
// isCurrentBandMember/isCurrentCommunityMember.
func hasCommunityPrivilege(ctx context.Context, q dbtx, communityID, personID int64, priv string) (bool, error) {
	return communityRoles.hasPrivilege(ctx, q, communityID, personID, priv)
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
