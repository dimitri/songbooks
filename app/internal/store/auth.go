package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Person is one login-capable account. PasswordHash is empty until
// SetPassword has been called for them (e.g. the accounts Phase 1's
// --import tool creates, or a fresh invite acceptance before its own
// SetPassword call lands).
type Person struct {
	ID           int64
	Username     string
	DisplayName  string
	PasswordHash string
	IsAdmin      bool
	// Instruments is the set of instruments this person plays, as
	// free-form strings (reusing the jam-signup vocabulary — see
	// DefaultInstrumentLimits — plus anything else they type). Used to
	// pre-fill jam signup rather than to gate anything.
	Instruments []string
}

// ProjectSummary is one project plus the asking person's own role in it —
// what the index/dashboard page needs, as opposed to ListBooks' unscoped
// slug list (still used by the --import tool and other admin paths).
type ProjectSummary struct {
	Slug string
	Name string
	Role string
}

// Member is one project's membership row, joined with the person's own
// identity, for a project's members/invite-management page.
type Member struct {
	PersonID    int64
	Username    string
	DisplayName string
	Role        string
}

// Invite is a pending, single-use invitation to join one project at a role.
type Invite struct {
	Token       string
	ProjectSlug string
	ProjectName string
	Role        string
	ExpiresAt   time.Time
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GetPersonByUsername looks up a person by their login name.
func (st *Store) GetPersonByUsername(ctx context.Context, username string) (*Person, error) {
	return st.scanPerson(ctx, `SELECT id, username, display_name, coalesce(password_hash, ''), is_admin, instruments FROM people WHERE username = $1`, username)
}

// GetPersonByID looks up a person by id.
func (st *Store) GetPersonByID(ctx context.Context, id int64) (*Person, error) {
	return st.scanPerson(ctx, `SELECT id, username, display_name, coalesce(password_hash, ''), is_admin, instruments FROM people WHERE id = $1`, id)
}

func (st *Store) scanPerson(ctx context.Context, query string, arg any) (*Person, error) {
	var p Person
	err := st.db.QueryRow(ctx, query, arg).Scan(&p.ID, &p.Username, &p.DisplayName, &p.PasswordHash, &p.IsAdmin, &p.Instruments)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such person: %w", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SetInstruments replaces personID's set of instruments (see Person.Instruments).
func (st *Store) SetInstruments(ctx context.Context, personID int64, instruments []string) error {
	tag, err := st.db.Exec(ctx, `UPDATE people SET instruments = $1 WHERE id = $2`, instruments, personID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such person id %d: %w", personID, ErrNotFound)
	}
	return nil
}

// SetAdmin grants or revokes site-wide admin status for personID.
func (st *Store) SetAdmin(ctx context.Context, personID int64, isAdmin bool) error {
	tag, err := st.db.Exec(ctx, `UPDATE people SET is_admin = $1 WHERE id = $2`, isAdmin, personID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such person id %d: %w", personID, ErrNotFound)
	}
	return nil
}

// PersonAdminRow is one row of the admin panel's people list.
type PersonAdminRow struct {
	ID          int64
	Username    string
	DisplayName string
	IsAdmin     bool
	CreatedAt   time.Time
}

// ListAllPeople lists every person in the system, for the admin panel.
func (st *Store) ListAllPeople(ctx context.Context) ([]PersonAdminRow, error) {
	rows, err := st.db.Query(ctx, `SELECT id, username, display_name, is_admin, created_at FROM people ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PersonAdminRow
	for rows.Next() {
		var p PersonAdminRow
		if err := rows.Scan(&p.ID, &p.Username, &p.DisplayName, &p.IsAdmin, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProjectAdminRow is one row of the admin panel's projects list.
type ProjectAdminRow struct {
	Slug        string
	Name        string
	Owners      int
	Editors     int
	Viewers     int
	MemberTotal int
}

// ListAllProjectsWithCounts lists every project with its member counts by
// role, for the admin panel.
func (st *Store) ListAllProjectsWithCounts(ctx context.Context) ([]ProjectAdminRow, error) {
	rows, err := st.db.Query(ctx, `
		SELECT p.slug, p.name,
			count(*) FILTER (WHERE m.role = 'owner'),
			count(*) FILTER (WHERE m.role = 'editor'),
			count(*) FILTER (WHERE m.role = 'viewer'),
			count(*)
		FROM projects p LEFT JOIN project_members m ON m.project_id = p.id
		GROUP BY p.id, p.slug, p.name
		ORDER BY p.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectAdminRow
	for rows.Next() {
		var p ProjectAdminRow
		if err := rows.Scan(&p.Slug, &p.Name, &p.Owners, &p.Editors, &p.Viewers, &p.MemberTotal); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreatePerson inserts a brand-new account (used by invite acceptance).
func (st *Store) CreatePerson(ctx context.Context, username, displayName, bcryptHash string) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx,
		`INSERT INTO people (username, display_name, password_hash) VALUES ($1, $2, $3) RETURNING id`,
		username, displayName, bcryptHash).Scan(&id)
	return id, err
}

// SetPassword sets (or replaces) a person's password hash.
func (st *Store) SetPassword(ctx context.Context, personID int64, bcryptHash string) error {
	tag, err := st.db.Exec(ctx, `UPDATE people SET password_hash = $1 WHERE id = $2`, bcryptHash, personID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such person id %d: %w", personID, ErrNotFound)
	}
	return nil
}

// sessionTTL is how long a session cookie stays valid after creation.
const sessionTTL = 30 * 24 * time.Hour

// CreateSession starts a new session for personID, returning its token.
func (st *Store) CreateSession(ctx context.Context, personID int64) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	_, err = st.db.Exec(ctx,
		`INSERT INTO sessions (token, person_id, expires_at) VALUES ($1, $2, $3)`,
		token, personID, time.Now().Add(sessionTTL))
	if err != nil {
		return "", err
	}
	return token, nil
}

// GetSession resolves a session token to the person it belongs to, or
// ErrNotFound if the token is missing or expired.
func (st *Store) GetSession(ctx context.Context, token string) (*Person, error) {
	var p Person
	err := st.db.QueryRow(ctx, `
		SELECT p.id, p.username, p.display_name, coalesce(p.password_hash, ''), p.is_admin, p.instruments
		FROM sessions s JOIN people p ON p.id = s.person_id
		WHERE s.token = $1 AND s.expires_at > now()`, token).
		Scan(&p.ID, &p.Username, &p.DisplayName, &p.PasswordHash, &p.IsAdmin, &p.Instruments)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such session: %w", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// DeleteSession ends a session (logout). Deleting an unknown token is a
// no-op, not an error — logging out twice should never fail.
func (st *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := st.db.Exec(ctx, `DELETE FROM sessions WHERE token = $1`, token)
	return err
}

// ListProjectsForPerson returns every project personID is a member of, with
// their own role in each, alphabetically by name — the person-scoped
// replacement for the unscoped ListBooks on the dashboard.
func (st *Store) ListProjectsForPerson(ctx context.Context, personID int64) ([]ProjectSummary, error) {
	rows, err := st.db.Query(ctx, `
		SELECT p.slug, p.name, m.role::text
		FROM project_members m JOIN projects p ON p.id = m.project_id
		WHERE m.person_id = $1
		ORDER BY p.name`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectSummary
	for rows.Next() {
		var ps ProjectSummary
		if err := rows.Scan(&ps.Slug, &ps.Name, &ps.Role); err != nil {
			return nil, err
		}
		out = append(out, ps)
	}
	return out, rows.Err()
}

// GetRole returns personID's role on the project identified by slug, and
// whether they're a member at all.
func (st *Store) GetRole(ctx context.Context, personID int64, projectSlug string) (string, bool, error) {
	var role string
	err := st.db.QueryRow(ctx, `
		SELECT m.role::text FROM project_members m JOIN projects p ON p.id = m.project_id
		WHERE m.person_id = $1 AND p.slug = $2`, personID, projectSlug).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return role, true, nil
}

// AddMember grants personID role on the project identified by slug —
// INSERT ... ON CONFLICT so it also serves as "set the role" when the
// person is already a member (e.g. re-running an owner grant is harmless).
func (st *Store) AddMember(ctx context.Context, projectSlug string, personID int64, role string) error {
	pid, err := st.projectID(ctx, projectSlug)
	if err != nil {
		return err
	}
	_, err = st.db.Exec(ctx, `
		INSERT INTO project_members (project_id, person_id, role) VALUES ($1, $2, $3)
		ON CONFLICT (project_id, person_id) DO UPDATE SET role = EXCLUDED.role`,
		pid, personID, role)
	return err
}

// ListMembers returns a project's members, by username.
func (st *Store) ListMembers(ctx context.Context, projectSlug string) ([]Member, error) {
	rows, err := st.db.Query(ctx, `
		SELECT p.id, p.username, p.display_name, m.role::text
		FROM project_members m
		JOIN people p ON p.id = m.person_id
		JOIN projects pr ON pr.id = m.project_id
		WHERE pr.slug = $1
		ORDER BY p.username`, projectSlug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.PersonID, &m.Username, &m.DisplayName, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetMemberRole changes an existing member's role.
func (st *Store) SetMemberRole(ctx context.Context, projectSlug string, personID int64, role string) error {
	return st.AddMember(ctx, projectSlug, personID, role)
}

// RemoveMember revokes personID's membership in a project entirely.
func (st *Store) RemoveMember(ctx context.Context, projectSlug string, personID int64) error {
	pid, err := st.projectID(ctx, projectSlug)
	if err != nil {
		return err
	}
	_, err = st.db.Exec(ctx, `DELETE FROM project_members WHERE project_id = $1 AND person_id = $2`, pid, personID)
	return err
}

// CreateInvite makes a single-use, time-limited invite to join a project at
// role.
func (st *Store) CreateInvite(ctx context.Context, projectSlug, role string, ttl time.Duration) (string, error) {
	pid, err := st.projectID(ctx, projectSlug)
	if err != nil {
		return "", err
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	_, err = st.db.Exec(ctx,
		`INSERT INTO invites (token, project_id, role, expires_at) VALUES ($1, $2, $3, $4)`,
		token, pid, role, time.Now().Add(ttl))
	if err != nil {
		return "", err
	}
	return token, nil
}

// GetInvite resolves a token to its still-valid invite, or ErrNotFound if
// missing/expired.
func (st *Store) GetInvite(ctx context.Context, token string) (*Invite, error) {
	var inv Invite
	err := st.db.QueryRow(ctx, `
		SELECT i.token, p.slug, p.name, i.role::text, i.expires_at
		FROM invites i JOIN projects p ON p.id = i.project_id
		WHERE i.token = $1 AND i.expires_at > now()`, token).
		Scan(&inv.Token, &inv.ProjectSlug, &inv.ProjectName, &inv.Role, &inv.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such invite: %w", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// AcceptInvite grants an already-logged-in person (personID) the invite's
// role on its project, and deletes the invite — single-use by design, so a
// link can't be replayed to add an unbounded number of people (anyone
// wanting more invitees gets a fresh invite instead).
func (st *Store) AcceptInvite(ctx context.Context, token string, personID int64) (projectSlug string, err error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var projectID int64
	var role string
	err = tx.QueryRow(ctx, `
		SELECT project_id, role::text, (SELECT slug FROM projects WHERE id = project_id)
		FROM invites WHERE token = $1 AND expires_at > now()`, token).
		Scan(&projectID, &role, &projectSlug)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("no such invite: %w", ErrNotFound)
	}
	if err != nil {
		return "", err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO project_members (project_id, person_id, role) VALUES ($1, $2, $3)
		ON CONFLICT (project_id, person_id) DO UPDATE SET role = EXCLUDED.role`,
		projectID, personID, role); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM invites WHERE token = $1`, token); err != nil {
		return "", err
	}
	return projectSlug, tx.Commit(ctx)
}

// CreatePersonAndAcceptInvite is AcceptInvite's counterpart for a brand-new
// visitor (not already logged in): it creates the person row, then accepts
// the invite as them, in one transaction so a failed membership insert
// never leaves a passwordless orphan account behind.
func (st *Store) CreatePersonAndAcceptInvite(ctx context.Context, token, username, displayName, bcryptHash string) (personID int64, projectSlug string, err error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback(ctx)

	var projectID int64
	var role string
	err = tx.QueryRow(ctx, `
		SELECT project_id, role::text, (SELECT slug FROM projects WHERE id = project_id)
		FROM invites WHERE token = $1 AND expires_at > now()`, token).
		Scan(&projectID, &role, &projectSlug)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", fmt.Errorf("no such invite: %w", ErrNotFound)
	}
	if err != nil {
		return 0, "", err
	}

	if err := tx.QueryRow(ctx,
		`INSERT INTO people (username, display_name, password_hash) VALUES ($1, $2, $3) RETURNING id`,
		username, displayName, bcryptHash).Scan(&personID); err != nil {
		return 0, "", err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO project_members (project_id, person_id, role) VALUES ($1, $2, $3)`,
		projectID, personID, role); err != nil {
		return 0, "", err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM invites WHERE token = $1`, token); err != nil {
		return 0, "", err
	}
	return personID, projectSlug, tx.Commit(ctx)
}
