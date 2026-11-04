package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// School is a named group of people with a REAL two-tier role
// (staff/member — "Community" at the user-facing layer, see migration
// 0042's doc comment for why this table keeps its original name) —
// unlike a Band (deliberately flat, no hierarchy — see bands.go's doc
// comment), a school/community explicitly has a hierarchy: staff manage
// membership and the community's shared songbooks/setlists and review
// member submissions; members get read-only access to the community's
// repertoire (see CanEditVariant's "school" case, CanEditSongbook) plus
// their own ordinary personal space.
type School struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

// SchoolMember is one row of school_members — one person's one stint of
// membership in one school, same history-preserving shape as
// BandMember (see migration 0023's comment): every row ever inserted is
// kept forever; IsCurrent reports whether THIS row is the person's
// current membership.
type SchoolMember struct {
	ID                int64
	SchoolID          int64
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
func (m SchoolMember) IsCurrent() bool { return m.LeftAt == nil }

// IsStaff reports whether m's role is the instructor tier.
func (m SchoolMember) IsStaff() bool { return m.Role == "staff" }

// CreateSchool makes a new school and inserts creatorPersonID as its
// first member, AS AN INSTRUCTOR (not a student) — a school with no
// instructor at all would be a dead end nobody could ever manage, same
// "first member must be able to bootstrap the rest" reasoning
// CreateBand uses, except here the tier matters: only an instructor can
// add further members at all (see requireSchoolStaff), so the
// creator cannot start out as a student.
func (st *Store) CreateSchool(ctx context.Context, name string, creatorPersonID int64) (int64, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var schoolID int64
	if err := tx.QueryRow(ctx, `INSERT INTO schools (name) VALUES ($1) RETURNING id`, name).Scan(&schoolID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO school_members (school_id, person_id, role) VALUES ($1, $2, 'instructor')`,
		schoolID, creatorPersonID); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return schoolID, nil
}

// GetSchool loads one school by id.
func (st *Store) GetSchool(ctx context.Context, schoolID int64) (*School, error) {
	var sc School
	err := st.db.QueryRow(ctx, `SELECT id, name, created_at FROM schools WHERE id = $1`, schoolID).
		Scan(&sc.ID, &sc.Name, &sc.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such school %d: %w", schoolID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &sc, nil
}

// RenameSchool changes a school's name — the admin page's one "basic
// school settings" action.
func (st *Store) RenameSchool(ctx context.Context, schoolID int64, name string) error {
	tag, err := st.db.Exec(ctx, `UPDATE schools SET name = $1 WHERE id = $2`, name, schoolID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such school %d: %w", schoolID, ErrNotFound)
	}
	return nil
}

// ListSchoolsForPerson lists every school personID is currently a
// member of (either role), alphabetically by name.
// SchoolAdminRow is one community as the site-wide admin panel needs it —
// name plus current staff/member counts, no per-person detail (that's
// the community's own admin page's job).
type SchoolAdminRow struct {
	ID          int64
	Name        string
	StaffCount  int
	MemberCount int
	CreatedAt   time.Time
}

// ListAllSchoolsWithCounts lists every community on the instance, newest
// first — the site admin panel's "Communities" tab.
func (st *Store) ListAllSchoolsWithCounts(ctx context.Context) ([]SchoolAdminRow, error) {
	rows, err := st.db.Query(ctx, `
		SELECT sc.id, sc.name, sc.created_at,
		       count(*) FILTER (WHERE sm.role = 'staff' AND sm.left_at IS NULL),
		       count(*) FILTER (WHERE sm.role = 'member' AND sm.left_at IS NULL)
		FROM schools sc
		LEFT JOIN school_members sm ON sm.school_id = sc.id
		GROUP BY sc.id, sc.name, sc.created_at
		ORDER BY sc.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SchoolAdminRow
	for rows.Next() {
		var r SchoolAdminRow
		if err := rows.Scan(&r.ID, &r.Name, &r.CreatedAt, &r.StaffCount, &r.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (st *Store) ListSchoolsForPerson(ctx context.Context, personID int64) ([]School, error) {
	rows, err := st.db.Query(ctx, `
		SELECT DISTINCT sc.id, sc.name, sc.created_at
		FROM schools sc JOIN school_members m ON m.school_id = sc.id
		WHERE m.person_id = $1 AND m.left_at IS NULL
		ORDER BY sc.name`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []School
	for rows.Next() {
		var sc School
		if err := rows.Scan(&sc.ID, &sc.Name, &sc.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// IsCurrentSchoolMember reports whether personID currently (left_at IS
// NULL) belongs to schoolID, instructor or student alike.
func (st *Store) IsCurrentSchoolMember(ctx context.Context, schoolID, personID int64) (bool, error) {
	return isCurrentSchoolMember(ctx, st.db, schoolID, personID, "")
}

// IsCurrentSchoolStaff reports whether personID is currently a
// member of schoolID at the instructor tier specifically — the
// permission gate for every instructor-only action (membership
// management, catalog add/fork/remove, submission review, rename).
func (st *Store) IsCurrentSchoolStaff(ctx context.Context, schoolID, personID int64) (bool, error) {
	return isCurrentSchoolMember(ctx, st.db, schoolID, personID, "staff")
}

// isCurrentSchoolMember is IsCurrentSchoolMember/IsCurrentSchoolStaff's
// shared implementation, taking a dbtx (pool or an already-open tx) —
// same reasoning as isCurrentBandMember in bands.go: a method that's
// already mid-transaction must run this check against THAT SAME tx
// handle, never reach back out to the pool for a second connection while
// the first one's transaction is still open (that's exactly the
// AddBandMember connection-pool bug this phase's bugfix commit
// addresses). role == "" means "any role"; otherwise it's an exact
// match ("staff").
func isCurrentSchoolMember(ctx context.Context, q dbtx, schoolID, personID int64, role string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM school_members
		              WHERE school_id = $1 AND person_id = $2 AND left_at IS NULL
		                AND ($3 = '' OR role = $3::school_member_role))`,
		schoolID, personID, role).Scan(&ok)
	return ok, err
}

// ListSchoolMembers lists schoolID's membership rows: every row ever
// inserted if includePast, otherwise only current (left_at IS NULL)
// ones. Newest first — same shape/ordering as ListBandMembers.
func (st *Store) ListSchoolMembers(ctx context.Context, schoolID int64, includePast bool) ([]SchoolMember, error) {
	q := `
		SELECT m.id, m.school_id, m.person_id, p.username, p.display_name, m.role::text,
		       m.joined_at, m.left_at, m.invited_by_person_id, coalesce(ip.username, '')
		FROM school_members m
		JOIN people p ON p.id = m.person_id
		LEFT JOIN people ip ON ip.id = m.invited_by_person_id
		WHERE m.school_id = $1`
	if !includePast {
		q += ` AND m.left_at IS NULL`
	}
	q += ` ORDER BY m.joined_at DESC, m.id DESC`
	rows, err := st.db.Query(ctx, q, schoolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SchoolMember
	for rows.Next() {
		var m SchoolMember
		if err := rows.Scan(&m.ID, &m.SchoolID, &m.PersonID, &m.Username, &m.DisplayName, &m.Role,
			&m.JoinedAt, &m.LeftAt, &m.InvitedByPersonID, &m.InvitedByUsername); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddSchoolMember adds personID to schoolID at role ("staff" or
// "member"), invited by invitedByPersonID — a brand-new school_members
// row, never a reuse of a past one even if personID left and is
// rejoining (same rule as AddBandMember/migration 0010). Errors if
// personID is already a current member of the school.
//
// The already-current check runs against THIS SAME tx (isCurrentSchoolMember
// with tx, not st.IsCurrentSchoolMember with the pool) — the exact
// pattern AddBandMember got wrong before this phase's bugfix: issuing a
// second, pool-level query while this tx is still open risks a
// connection-pool deadlock/exhaustion under load.
func (st *Store) AddSchoolMember(ctx context.Context, schoolID, personID int64, role string, invitedByPersonID int64) (int64, error) {
	if role != "staff" && role != "member" {
		return 0, fmt.Errorf("invalid school member role %q", role)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	already, err := isCurrentSchoolMember(ctx, tx, schoolID, personID, "")
	if err != nil {
		return 0, err
	}
	if already {
		return 0, fmt.Errorf("that person is already a current member of this school")
	}

	var invitedBy *int64
	if invitedByPersonID != 0 {
		invitedBy = &invitedByPersonID
	}
	var memberID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO school_members (school_id, person_id, role, invited_by_person_id)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		schoolID, personID, role, invitedBy).Scan(&memberID); err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return memberID, nil
}

// RemoveSchoolMember ends personID's current membership in schoolID
// (sets left_at = now() on their current row — history is never
// destroyed, same rule as RemoveBandMember). Refuses to remove a
// school's last current instructor (same reasoning as CreateSchool: a
// school with no instructor left would be a dead end nobody could ever
// manage again).
func (st *Store) RemoveSchoolMember(ctx context.Context, schoolID, personID int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	row, err := lockCurrentSchoolMembership(ctx, tx, schoolID, personID)
	if err != nil {
		return err
	}
	if row.Role == "staff" {
		if err := requireAnotherCurrentInstructor(ctx, tx, schoolID, personID); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE school_members SET left_at = now() WHERE id = $1`, row.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetSchoolMemberRole promotes/demotes personID's current membership in
// schoolID between "staff" and "member". Refuses to demote a
// school's last current instructor — see RemoveSchoolMember's doc
// comment for why.
func (st *Store) SetSchoolMemberRole(ctx context.Context, schoolID, personID int64, role string) error {
	if role != "staff" && role != "member" {
		return fmt.Errorf("invalid school member role %q", role)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	row, err := lockCurrentSchoolMembership(ctx, tx, schoolID, personID)
	if err != nil {
		return err
	}
	if row.Role == "staff" && role == "member" {
		if err := requireAnotherCurrentInstructor(ctx, tx, schoolID, personID); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE school_members SET role = $1 WHERE id = $2`, role, row.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// requireAnotherCurrentInstructor errors unless schoolID has a current
// instructor OTHER than exceptPersonID — called (inside the same tx)
// before demoting/removing an instructor, so a school can never be left
// with zero instructors able to manage it.
func requireAnotherCurrentInstructor(ctx context.Context, tx pgx.Tx, schoolID, exceptPersonID int64) error {
	var ok bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM school_members
		              WHERE school_id = $1 AND role = 'instructor' AND left_at IS NULL AND person_id != $2)`,
		schoolID, exceptPersonID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("a school must keep at least one current instructor: %w", ErrForbidden)
	}
	return nil
}

// currentSchoolMembershipRow is lockCurrentSchoolMembership's result.
type currentSchoolMembershipRow struct {
	ID       int64
	SchoolID int64
	PersonID int64
	Role     string
}

// lockCurrentSchoolMembership locks (FOR UPDATE) and returns personID's
// current membership row in schoolID, or ErrNotFound if they have none.
func lockCurrentSchoolMembership(ctx context.Context, tx pgx.Tx, schoolID, personID int64) (currentSchoolMembershipRow, error) {
	var row currentSchoolMembershipRow
	err := tx.QueryRow(ctx, `
		SELECT id, school_id, person_id, role::text
		FROM school_members WHERE school_id = $1 AND person_id = $2 AND left_at IS NULL
		FOR UPDATE`, schoolID, personID).
		Scan(&row.ID, &row.SchoolID, &row.PersonID, &row.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return currentSchoolMembershipRow{}, fmt.Errorf("no current membership for person %d in school %d: %w", personID, schoolID, ErrNotFound)
	}
	if err != nil {
		return currentSchoolMembershipRow{}, err
	}
	return row, nil
}
