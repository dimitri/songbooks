package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Person is one login-capable account. PasswordHash is empty until
// SetPassword has been called for them (e.g. the accounts Phase 1's
// --import tool creates). Email is empty for accounts that predate the
// magic-link login phase and haven't been backfilled yet (see
// SetEmail / the --set-email bootstrap flag) — such accounts can still
// only log in via the legacy password path, if they have one.
type Person struct {
	ID           int64
	Username     string
	DisplayName  string
	Email        string
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

// Invite is a pending, single-use invitation. It targets exactly one of:
// a project (ProjectSlug/Role set), a band (BandID/BandStatus set), or
// neither — a "plain" invite that just lets its holder create an
// account with no membership attached (see Kind).
type Invite struct {
	Token       string
	ProjectSlug string
	ProjectName string
	Role        string
	BandID      int64
	BandName    string
	BandStatus  string
	CreatedBy   int64 // 0/unknown for invites predating this column
	ExpiresAt   time.Time
}

// Kind reports which of the three invite shapes inv is, for templates
// and handlers that need to branch on it.
func (inv Invite) Kind() string {
	switch {
	case inv.ProjectSlug != "":
		return "project"
	case inv.BandID != 0:
		return "band"
	default:
		return "plain"
	}
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
	return st.scanPerson(ctx, `SELECT id, username, display_name, coalesce(email, ''), coalesce(password_hash, ''), is_admin, instruments FROM people WHERE username = $1`, username)
}

// GetPersonByID looks up a person by id.
func (st *Store) GetPersonByID(ctx context.Context, id int64) (*Person, error) {
	return st.scanPerson(ctx, `SELECT id, username, display_name, coalesce(email, ''), coalesce(password_hash, ''), is_admin, instruments FROM people WHERE id = $1`, id)
}

// GetPersonByEmail looks up a person by their email address — the
// magic-link login lookup. Case-sensitive exact match, matching how the
// email was stored (emails are lowercased on input, see handleLogin/
// SetEmail).
func (st *Store) GetPersonByEmail(ctx context.Context, email string) (*Person, error) {
	return st.scanPerson(ctx, `SELECT id, username, display_name, coalesce(email, ''), coalesce(password_hash, ''), is_admin, instruments FROM people WHERE email = $1`, email)
}

func (st *Store) scanPerson(ctx context.Context, query string, arg any) (*Person, error) {
	var p Person
	err := st.db.QueryRow(ctx, query, arg).Scan(&p.ID, &p.Username, &p.DisplayName, &p.Email, &p.PasswordHash, &p.IsAdmin, &p.Instruments)
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

// GetOnboardedAt reports whether personID has finished (or explicitly
// skipped) the getting-started wizard — see migration 0031's comment on
// why this is a single timestamp, not a per-step state table.
func (st *Store) GetOnboardedAt(ctx context.Context, personID int64) (*time.Time, error) {
	var t *time.Time
	err := st.db.QueryRow(ctx, `SELECT onboarded_at FROM people WHERE id = $1`, personID).Scan(&t)
	return t, err
}

// MarkOnboarded sets onboarded_at to now, idempotently (a second call —
// e.g. finishing after an earlier skip — just moves the timestamp, which
// is harmless since nothing reads anything but "is it set").
func (st *Store) MarkOnboarded(ctx context.Context, personID int64) error {
	_, err := st.db.Exec(ctx, `UPDATE people SET onboarded_at = now() WHERE id = $1`, personID)
	return err
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

// CreatePerson inserts a brand-new, passwordless account identified by
// email (invite-only account creation — see CreatePersonAndAcceptInvite,
// which is the one actually wired to the accept flow; this is kept as
// the plain non-transactional equivalent for any future direct caller).
func (st *Store) CreatePerson(ctx context.Context, username, displayName, email string) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx,
		`INSERT INTO people (username, display_name, email) VALUES ($1, $2, $3) RETURNING id`,
		username, displayName, email).Scan(&id)
	return id, err
}

// SetPassword sets (or replaces) a person's password hash. Secondary/
// bootstrap mechanism now that /login is magic-link-only — kept working
// (the column isn't dropped) for the --set-password CLI flag and anyone
// who already has a password set.
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

// SetEmail sets (or replaces) a person's email address — used by the
// --set-email CLI flag to backfill pre-existing accounts (e.g. the
// "tapoueh" bootstrap account) that predate magic-link login and have
// no email on file, so they aren't locked out of /login.
func (st *Store) SetEmail(ctx context.Context, personID int64, email string) error {
	tag, err := st.db.Exec(ctx, `UPDATE people SET email = $1 WHERE id = $2`, email, personID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrEmailTaken
		}
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
		SELECT p.id, p.username, p.display_name, coalesce(p.email, ''), coalesce(p.password_hash, ''), p.is_admin, p.instruments
		FROM sessions s JOIN people p ON p.id = s.person_id
		WHERE s.token = $1 AND s.expires_at > now()`, token).
		Scan(&p.ID, &p.Username, &p.DisplayName, &p.Email, &p.PasswordHash, &p.IsAdmin, &p.Instruments)
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

// loginTokenTTL is how long a magic login link stays valid after it's
// requested — short, since it's emailed/shown in plaintext.
const loginTokenTTL = 15 * time.Minute

// CreateLoginToken starts a fresh single-use magic-link login token for
// personID, returning the token value to embed in the emailed URL.
func (st *Store) CreateLoginToken(ctx context.Context, personID int64) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	_, err = st.db.Exec(ctx,
		`INSERT INTO login_tokens (token, person_id, expires_at) VALUES ($1, $2, $3)`,
		token, personID, time.Now().Add(loginTokenTTL))
	if err != nil {
		return "", err
	}
	return token, nil
}

// ConsumeLoginToken validates a magic-link token — unused and
// unexpired — marks it used, and returns the person it belongs to.
// Single-use by design: a reused or unknown or expired token is
// ErrNotFound, deliberately not distinguished further (no information
// for an attacker to learn from the difference).
func (st *Store) ConsumeLoginToken(ctx context.Context, token string) (*Person, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var personID int64
	err = tx.QueryRow(ctx, `
		SELECT person_id FROM login_tokens
		WHERE token = $1 AND used_at IS NULL AND expires_at > now()`, token).
		Scan(&personID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("invalid or expired login link: %w", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE login_tokens SET used_at = now() WHERE token = $1`, token); err != nil {
		return nil, err
	}

	p, err := st.scanPersonTx(ctx, tx, `SELECT id, username, display_name, coalesce(email, ''), coalesce(password_hash, ''), is_admin, instruments FROM people WHERE id = $1`, personID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

func (st *Store) scanPersonTx(ctx context.Context, tx pgx.Tx, query string, arg any) (*Person, error) {
	var p Person
	err := tx.QueryRow(ctx, query, arg).Scan(&p.ID, &p.Username, &p.DisplayName, &p.Email, &p.PasswordHash, &p.IsAdmin, &p.Instruments)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such person: %w", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
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
// role. createdBy is the inviting person's id (0 if unknown).
func (st *Store) CreateInvite(ctx context.Context, projectSlug, role string, createdBy int64, ttl time.Duration) (string, error) {
	pid, err := st.projectID(ctx, projectSlug)
	if err != nil {
		return "", err
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	_, err = st.db.Exec(ctx,
		`INSERT INTO invites (token, project_id, role, created_by_person_id, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		token, pid, role, nullableID(createdBy), time.Now().Add(ttl))
	if err != nil {
		return "", err
	}
	return token, nil
}

// CreateBandInvite makes a single-use, time-limited invite to join a band
// at status ("core" or "guest") — the band equivalent of CreateInvite.
func (st *Store) CreateBandInvite(ctx context.Context, bandID int64, status string, createdBy int64, ttl time.Duration) (string, error) {
	if status != "core" && status != "guest" {
		return "", fmt.Errorf("invalid band member status %q", status)
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	_, err = st.db.Exec(ctx,
		`INSERT INTO invites (token, band_id, band_status, created_by_person_id, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		token, bandID, status, nullableID(createdBy), time.Now().Add(ttl))
	if err != nil {
		return "", err
	}
	return token, nil
}

// OpenBandInvite is one still-redeemable single-use band invite: just
// enough for a page to show "here's a link you can still share" and let
// it be copied again, without re-running the full Invite/project/band
// join resolution GetInvite does for the /join landing page itself.
type OpenBandInvite struct {
	Token     string
	Status    string
	ExpiresAt time.Time
}

// ListOpenBandInvites lists bandID's still-redeemable single-use invites
// — any row currently in `invites` for this band hasn't been accepted
// yet (AcceptInvite deletes its row on accept, single-use by design; see
// that doc comment), so "not expired" is the only filter needed. Fills
// the same "show a link I already made, don't force a new one" gap
// ListGroupInvitesForTarget fills for the reusable roster-link kind.
func (st *Store) ListOpenBandInvites(ctx context.Context, bandID int64) ([]OpenBandInvite, error) {
	rows, err := st.db.Query(ctx, `
		SELECT token, band_status::text, expires_at FROM invites
		WHERE band_id = $1 AND expires_at > now() ORDER BY expires_at DESC`, bandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OpenBandInvite
	for rows.Next() {
		var inv OpenBandInvite
		if err := rows.Scan(&inv.Token, &inv.Status, &inv.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// CreatePlainInvite makes a single-use, time-limited invite that carries
// no project/band attachment at all — just lets its holder create an
// account, for use where there's otherwise no self-serve registration
// path (e.g. the admin panel).
func (st *Store) CreatePlainInvite(ctx context.Context, createdBy int64, ttl time.Duration) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	_, err = st.db.Exec(ctx,
		`INSERT INTO invites (token, created_by_person_id, expires_at) VALUES ($1, $2, $3)`,
		token, nullableID(createdBy), time.Now().Add(ttl))
	if err != nil {
		return "", err
	}
	return token, nil
}

// nullableID turns a 0 person/actor id (this codebase's "unknown" sentinel
// elsewhere, e.g. actorID) into a genuine SQL NULL instead of a bogus FK
// value.
func nullableID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

// GetInvite resolves a token to its still-valid invite, or ErrNotFound if
// missing/expired. Exactly one of (ProjectSlug, BandID) is set, or
// neither for a plain invite — see Invite.Kind.
func (st *Store) GetInvite(ctx context.Context, token string) (*Invite, error) {
	var inv Invite
	var projectSlug, projectName, role sql.NullString
	var bandID sql.NullInt64
	var bandName, bandStatus sql.NullString
	var createdBy sql.NullInt64
	err := st.db.QueryRow(ctx, `
		SELECT i.token, p.slug, p.name, i.role::text,
		       i.band_id, b.name, i.band_status::text,
		       i.created_by_person_id, i.expires_at
		FROM invites i
		LEFT JOIN projects p ON p.id = i.project_id
		LEFT JOIN bands b ON b.id = i.band_id
		WHERE i.token = $1 AND i.expires_at > now()`, token).
		Scan(&inv.Token, &projectSlug, &projectName, &role,
			&bandID, &bandName, &bandStatus, &createdBy, &inv.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such invite: %w", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	inv.ProjectSlug, inv.ProjectName, inv.Role = projectSlug.String, projectName.String, role.String
	inv.BandID, inv.BandName, inv.BandStatus = bandID.Int64, bandName.String, bandStatus.String
	inv.CreatedBy = createdBy.Int64
	return &inv, nil
}

// acceptInviteTx is the shared body of AcceptInvite/CreatePersonAndAcceptInvite:
// given the invite row (already locked/read inside tx) and the person now
// accepting it, grants the right membership (project, band, or none for a
// plain invite), deletes the invite, and — for a band invite — notifies
// whoever created it. dest is where the browser should be redirected
// afterwards.
func (st *Store) acceptInviteTx(ctx context.Context, tx pgx.Tx, token string, personID int64, accepterName string) (dest string, err error) {
	var projectID, bandID, createdBy sql.NullInt64
	var role sql.NullString
	var projectSlug sql.NullString
	var bandName sql.NullString
	err = tx.QueryRow(ctx, `
		SELECT i.project_id, i.role::text, (SELECT slug FROM projects WHERE id = i.project_id),
		       i.band_id, b.name, i.created_by_person_id
		FROM invites i LEFT JOIN bands b ON b.id = i.band_id
		WHERE i.token = $1 AND i.expires_at > now()`, token).
		Scan(&projectID, &role, &projectSlug, &bandID, &bandName, &createdBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("no such invite: %w", ErrNotFound)
	}
	if err != nil {
		return "", err
	}

	switch {
	case projectID.Valid:
		if _, err := tx.Exec(ctx, `
			INSERT INTO project_members (project_id, person_id, role) VALUES ($1, $2, $3)
			ON CONFLICT (project_id, person_id) DO UPDATE SET role = EXCLUDED.role`,
			projectID.Int64, personID, role.String); err != nil {
			return "", err
		}
		dest = "/b/" + projectSlug.String

	case bandID.Valid:
		var invitedBy *int64
		if createdBy.Valid {
			invitedBy = &createdBy.Int64
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO band_members (band_id, person_id, status, invited_by_person_id)
			VALUES ($1, $2, (SELECT band_status FROM invites WHERE token = $3), $4)`,
			bandID.Int64, personID, token, invitedBy); err != nil {
			return "", err
		}
		dest = fmt.Sprintf("/bands/%d", bandID.Int64)

		// First real use case for the notification plumbing: tell the
		// inviter their invite was accepted. A plain invite has nothing to
		// notify about (no inviter is even meaningfully "waiting" on it),
		// and a project invite is unchanged existing behavior — this is
		// deliberately band-invite-only for now.
		if createdBy.Valid && createdBy.Int64 != personID {
			body := fmt.Sprintf("%s accepted your invite to join %s", accepterName, bandName.String)
			if _, err := tx.Exec(ctx, `
				INSERT INTO notifications (person_id, kind, body, link) VALUES ($1, $2, $3, $4)`,
				createdBy.Int64, "band_invite_accepted", body, fmt.Sprintf("/bands/%d", bandID.Int64)); err != nil {
				return "", err
			}
		}

	default:
		// Plain invite: no membership to grant, just the fresh account.
		dest = "/"
	}

	if _, err := tx.Exec(ctx, `DELETE FROM invites WHERE token = $1`, token); err != nil {
		return "", err
	}
	return dest, nil
}

// AcceptInvite grants an already-logged-in person (personID, display name
// accepterName — used in the accept notification) the invite's membership
// (project role, band status, or nothing for a plain invite), and deletes
// the invite — single-use by design, so a link can't be replayed to add an
// unbounded number of people (anyone wanting more invitees gets a fresh
// invite instead). dest is where the browser should land afterwards.
func (st *Store) AcceptInvite(ctx context.Context, token string, personID int64, accepterName string) (dest string, err error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	dest, err = st.acceptInviteTx(ctx, tx, token, personID, accepterName)
	if err != nil {
		return "", err
	}
	return dest, tx.Commit(ctx)
}

// CreatePersonAndAcceptInvite is AcceptInvite's counterpart for a brand-new
// visitor (not already logged in): it creates the person row — identified
// by email, plus accepts the invite as them, in one transaction so a
// failed membership insert never leaves an orphan account behind.
// username is kept only as a display label distinct from login identity
// (see Member/PersonAdminRow and the templates that show it).
//
// bcryptHash is optional: pass "" (the invite form's password field left
// blank) for a passwordless account that can only log in via magic link,
// exactly as this always worked before password login was restored as a
// first-class option; pass an already-hashed password (handlePostInvite
// hashes it before calling this) to let the new account log in with that
// password immediately, same as any other account with a password set.
func (st *Store) CreatePersonAndAcceptInvite(ctx context.Context, token, username, displayName, email, bcryptHash string) (personID int64, dest string, err error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback(ctx)

	var pwHash *string
	if bcryptHash != "" {
		pwHash = &bcryptHash
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO people (username, display_name, email, password_hash) VALUES ($1, $2, $3, $4) RETURNING id`,
		username, displayName, email, pwHash).Scan(&personID); err != nil {
		if constraintViolated(err, "people_username_key") {
			return 0, "", ErrUsernameTaken
		}
		if constraintViolated(err, "people_email_key") {
			return 0, "", ErrEmailTaken
		}
		return 0, "", err
	}

	dest, err = st.acceptInviteTx(ctx, tx, token, personID, displayName)
	if err != nil {
		return 0, "", err
	}
	return personID, dest, tx.Commit(ctx)
}
