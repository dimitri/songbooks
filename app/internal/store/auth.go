package store

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Person is one login-capable account, identified by its Email (see
// docs/accounts.md): there is no login name (people.username is legacy,
// migration 0135) and people are shown by DisplayName. PasswordHash is
// empty until SetPassword has been called for them. Email is empty for
// a placeholder (shell) account and for the odd old account that never
// had one backfilled (see SetEmail / the --set-email flag): such an
// account can't log in at all until it gets one (ListPasswordNoEmail).
type Person struct {
	ID           int64
	DisplayName  string
	Email        string
	PasswordHash string
	IsAdmin      bool
	// Instruments is the set of instruments this person plays, as
	// free-form strings (reusing the jam-signup vocabulary — see
	// DefaultInstrumentLimits — plus anything else they type). Used to
	// pre-fill jam signup rather than to gate anything.
	Instruments []string

	// Public-profile fields (see migration 0038): shown instead of
	// DisplayName wherever a "public" view of this person is
	// shown (not built yet beyond the profile editor itself). ShareEmail/
	// SharePhone are separate opt-ins — a nickname/picture alone doesn't
	// expose contact info.
	Nickname   string
	PictureURL string
	Phone      string
	ShareEmail bool
	SharePhone bool
	// GravatarOptOut suppresses the Gravatar-by-email fallback the public
	// profile picture uses when PictureURL is blank (see EffectivePictureURL).
	GravatarOptOut bool
}

// EffectivePictureURL is what the public profile picture should actually
// render: PictureURL if the person set one, else a Gravatar URL derived
// from their email (d=404, so a browser <img onerror> can fall back to
// the plain-initials avatar when no Gravatar is registered for that
// address) — unless they've opted out, in which case there's no photo at
// all (empty string, same as "no picture set" everywhere else already
// handles).
func (p Person) EffectivePictureURL() string {
	if p.PictureURL != "" {
		return p.PictureURL
	}
	if p.GravatarOptOut || p.Email == "" {
		return ""
	}
	return gravatarURL(p.Email)
}

func gravatarURL(email string) string {
	h := md5.Sum([]byte(strings.ToLower(strings.TrimSpace(email))))
	return fmt.Sprintf("https://www.gravatar.com/avatar/%x?s=200&d=404", h)
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
	DisplayName string
	Role        string
}

// Invite is a pending, single-use invitation: a personal one (PersonID
// set: a placeholder's link, see CreateShellPerson), or a "plain" one
// that just lets its holder create an account with no membership
// attached (see Kind).
//
// A band's nameless one-off link (band_id set, no person) is gone
// (decided 2026-11-24: invites are made by name, or by the band's open
// link): GetInvite and accepting no longer take one still in the
// table (notNamelessBandInvite).
type Invite struct {
	Token     string
	PersonID  int64 // set for a personal invite — see Kind, CreateShellPerson
	CreatedBy int64 // 0/unknown for invites predating this column
	ExpiresAt time.Time
}

// Kind reports which shape inv is, for templates and handlers that need
// to branch on it: "personal" (PersonID set — completes an existing
// shell person's account, see ClaimPersonalInvite) or "plain" (just an
// account with no attached membership).
func (inv Invite) Kind() string {
	if inv.PersonID != 0 {
		return "personal"
	}
	return "plain"
}

// notNamelessBandInvite is the condition on invites i that leaves out a
// band's nameless one-off link, made until 2026-11-24 (see Invite).
const notNamelessBandInvite = `(i.band_id IS NULL OR i.person_id IS NOT NULL)`

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GetPersonByID looks up a person by id.
func (st *Store) GetPersonByID(ctx context.Context, id int64) (*Person, error) {
	return st.scanPerson(ctx, `SELECT id, display_name, coalesce(email, ''), coalesce(password_hash, ''), is_admin, instruments, nickname, picture_url, phone, share_email, share_phone, gravatar_opt_out FROM people WHERE id = $1`, id)
}

// GetPersonByEmail looks up a person by their email address — the
// login lookup (magic link and password alike), the email being the
// account. Emails are lowercased on input (handleLogin, SetEmail,
// CreatePerson); the match ignores case all the same, an exact match
// first should two old rows differ only by case.
func (st *Store) GetPersonByEmail(ctx context.Context, email string) (*Person, error) {
	return st.scanPerson(ctx, `SELECT id, display_name, coalesce(email, ''), coalesce(password_hash, ''), is_admin, instruments, nickname, picture_url, phone, share_email, share_phone, gravatar_opt_out FROM people
		WHERE lower(email) = lower($1) ORDER BY email = $1 DESC, id LIMIT 1`, strings.TrimSpace(email))
}

// FindPerson resolves how a command line or a config names a person
// (cmd/songbooks --set-email, SONGBOOKS_DEV_AUTO_LOGIN, cmd/sbcoach…):
// a numeric id, or an email (matched lowercased, as stored). There is
// no login name any more (migration 0135). ErrNotFound if nobody.
func (st *Store) FindPerson(ctx context.Context, emailOrID string) (*Person, error) {
	ref := strings.TrimSpace(emailOrID)
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		return st.GetPersonByID(ctx, id)
	}
	if !strings.Contains(ref, "@") {
		return nil, fmt.Errorf("%q is neither an email nor an id (there are no usernames any more): %w", ref, ErrNotFound)
	}
	return st.GetPersonByEmail(ctx, strings.ToLower(ref))
}

func (st *Store) scanPerson(ctx context.Context, query string, arg any) (*Person, error) {
	var p Person
	err := st.db.QueryRow(ctx, query, arg).Scan(&p.ID, &p.DisplayName, &p.Email, &p.PasswordHash, &p.IsAdmin, &p.Instruments, &p.Nickname, &p.PictureURL, &p.Phone, &p.ShareEmail, &p.SharePhone, &p.GravatarOptOut)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such person: %w", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SetPublicProfile replaces personID's public-profile fields wholesale
// (see migration 0038 / Person's doc comment) — the "public profile" tab
// on /profile, a whole-form save.
func (st *Store) SetPublicProfile(ctx context.Context, personID int64, nickname, pictureURL, phone string, shareEmail, sharePhone, gravatarOptOut bool) error {
	tag, err := st.db.Exec(ctx, `
		UPDATE people SET nickname = $1, picture_url = $2, phone = $3, share_email = $4, share_phone = $5, gravatar_opt_out = $6
		WHERE id = $7`, nickname, pictureURL, phone, shareEmail, sharePhone, gravatarOptOut, personID)
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
	DisplayName string
	Email       string // "" for a placeholder, or an account that can't log in
	IsAdmin     bool
	IsShell     bool
	// NeverJoined: not a placeholder, but nobody ever got into it either
	// (neverJoinedSQL): it hasn't joined, as a placeholder hasn't.
	NeverJoined bool
	CreatedAt   time.Time
}

// ListAllPeople lists every person in the system, for the admin panel.
func (st *Store) ListAllPeople(ctx context.Context) ([]PersonAdminRow, error) {
	rows, err := st.db.Query(ctx, `
		SELECT p.id, p.display_name, coalesce(p.email, ''), p.is_admin, p.is_shell, NOT p.is_shell AND `+neverJoinedSQL+`, p.created_at
		FROM people p ORDER BY p.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PersonAdminRow
	for rows.Next() {
		var p PersonAdminRow
		if err := rows.Scan(&p.ID, &p.DisplayName, &p.Email, &p.IsAdmin, &p.IsShell, &p.NeverJoined, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListPasswordNoEmail lists the accounts that have a password but no
// email: with email the only identity (migration 0135) they can't log
// in any more — no magic link to send, no email to type with the
// password — until someone gives them one (songbooks --set-email
// <id> <email>). Placeholder (shell) accounts aren't in it: they have
// neither, and their invite link is how they get both. For the admin
// panel and a line at startup.
func (st *Store) ListPasswordNoEmail(ctx context.Context) ([]PersonAdminRow, error) {
	rows, err := st.db.Query(ctx, `
		SELECT id, display_name, is_admin, is_shell, created_at FROM people
		WHERE email IS NULL AND password_hash IS NOT NULL AND password_hash <> '' AND NOT is_shell
		ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PersonAdminRow
	for rows.Next() {
		var p PersonAdminRow
		if err := rows.Scan(&p.ID, &p.DisplayName, &p.IsAdmin, &p.IsShell, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListEmailNoPassword lists the accounts with an email but neither a
// password nor a passkey: registering for an event used to make them
// (the password was optional until 2026-11-24). Their only way in is an
// emailed login link, and no login email reaches anyone yet (the console
// mailer, no SMTP): once their session ends they can't log in, until
// they get a login link from a site admin (a members page's "Login
// link") and set a password on their profile, or someone sets one
// (songbooks --set-password). Placeholders aren't in it. For the admin
// panel and a line at startup, as ListPasswordNoEmail.
func (st *Store) ListEmailNoPassword(ctx context.Context) ([]PersonAdminRow, error) {
	rows, err := st.db.Query(ctx, `
		SELECT p.id, p.display_name, p.email, p.is_admin, p.is_shell, p.created_at FROM people p
		WHERE p.email IS NOT NULL AND coalesce(p.password_hash, '') = '' AND NOT p.is_shell
		  AND NOT EXISTS (SELECT 1 FROM webauthn_credentials w WHERE w.person_id = p.id)
		ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PersonAdminRow
	for rows.Next() {
		var p PersonAdminRow
		if err := rows.Scan(&p.ID, &p.DisplayName, &p.Email, &p.IsAdmin, &p.IsShell, &p.CreatedAt); err != nil {
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
func (st *Store) CreatePerson(ctx context.Context, displayName, email string) (int64, error) {
	// email is UNIQUE but nullable — a literal "" (no caller ever passed
	// one until cmd/songimport started restoring people with no email on
	// file) collides with itself on the SECOND such person, since two
	// empty strings are equal under a unique constraint while two SQL
	// NULLs are not. Store "no email" as NULL, same as every other no-
	// email person already in the table.
	var emailArg *string
	if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
		emailArg = &email
	}
	var id int64
	err := st.db.QueryRow(ctx,
		`INSERT INTO people (display_name, email) VALUES ($1, $2) RETURNING id`,
		displayName, emailArg).Scan(&id)
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
		SELECT p.id, p.display_name, coalesce(p.email, ''), coalesce(p.password_hash, ''), p.is_admin, p.instruments,
		       p.nickname, p.picture_url, p.phone, p.share_email, p.share_phone, p.gravatar_opt_out
		FROM sessions s JOIN people p ON p.id = s.person_id
		WHERE s.token = $1 AND s.expires_at > now()`, token).
		Scan(&p.ID, &p.DisplayName, &p.Email, &p.PasswordHash, &p.IsAdmin, &p.Instruments, &p.Nickname, &p.PictureURL, &p.Phone, &p.ShareEmail, &p.SharePhone, &p.GravatarOptOut)
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

	p, err := st.scanPersonTx(ctx, tx, `SELECT id, display_name, coalesce(email, ''), coalesce(password_hash, ''), is_admin, instruments, nickname, picture_url, phone, share_email, share_phone, gravatar_opt_out FROM people WHERE id = $1`, personID)
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
	err := tx.QueryRow(ctx, query, arg).Scan(&p.ID, &p.DisplayName, &p.Email, &p.PasswordHash, &p.IsAdmin, &p.Instruments, &p.Nickname, &p.PictureURL, &p.Phone, &p.ShareEmail, &p.SharePhone, &p.GravatarOptOut)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such person: %w", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
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

// CreateShellPerson makes a not-yet-claimed person row — displayName
// set, no email (the real person gives theirs when they claim their
// invite: the email is the account), is_shell true. The caller attaches whatever band_members/
// community_members rows this person should have (AddBandMember/
// AddCommunityMember both work unchanged against a shell's id — they're
// ordinary membership rows, nothing shell-specific about them) before
// ever generating an invite link — see CreatePersonalInvite.
func (st *Store) CreateShellPerson(ctx context.Context, displayName string) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx,
		`INSERT INTO people (display_name, is_shell) VALUES ($1, true) RETURNING id`,
		displayName).Scan(&id)
	return id, err
}

// ErrNotShell is returned by CreatePersonalInvite when the person
// already has a real account: a personal invite finishes a placeholder
// (shell) account, and one for a real account would hand that account
// to whoever opens the link.
var ErrNotShell = errors.New("that person already has an account")

// CreatePersonalInvite makes a single-use token for personID, a shell
// (see CreateShellPerson), to complete their own account, and returns
// it. It replaces any link the person had: one live link per person, so
// "New link" always works and the old link stops working as soon as a
// new one exists. ErrNotFound if there's no such person, ErrNotShell if
// they already have an account. Unlike CreatePlainInvite/
// CreateBandInvite, claiming it never creates a new person row:
// ClaimPersonalInvite finishes the EXISTING one.
//
// An account nobody ever joined (no email, no password, no passkey, no
// session nor used login link ever: neverJoinedSQL — an import's or an
// early roster's name) is a placeholder in all but the flag: it becomes
// one here, in the same transaction, and gets its link. Nothing is
// taken from anyone (nobody ever got into it), and everything that
// tells a placeholder apart (claiming, rosters' "hasn't joined yet",
// the login link's refusal) goes on reading is_shell alone.
func (st *Store) CreatePersonalInvite(ctx context.Context, personID, createdBy int64, ttl time.Duration) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var isShell, neverJoined bool
	err = tx.QueryRow(ctx, `SELECT p.is_shell, `+neverJoinedSQL+` FROM people p WHERE p.id = $1 FOR UPDATE`, personID).Scan(&isShell, &neverJoined)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("no such person id %d: %w", personID, ErrNotFound)
	}
	if err != nil {
		return "", err
	}
	if !isShell && !neverJoined {
		return "", ErrNotShell
	}
	if !isShell {
		if _, err := tx.Exec(ctx, `UPDATE people SET is_shell = true WHERE id = $1`, personID); err != nil {
			return "", err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM invites WHERE person_id = $1`, personID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO invites (token, person_id, created_by_person_id, expires_at) VALUES ($1, $2, $3, $4)`,
		token, personID, nullableID(createdBy), time.Now().Add(ttl)); err != nil {
		return "", err
	}
	return token, tx.Commit(ctx)
}

// PersonalInviteMembership is one band/community PersonalInviteView's
// shell person is already a member of (set before the invite was even
// created — see CreatePersonalInvite's doc comment), for the landing
// page to list ("you're invited to join: What's Up as guitar/bass/
// singer (core), Couleur Acoustique (core)").
type PersonalInviteMembership struct {
	Kind   string // "band" or "community"
	ID     int64
	Name   string
	Status string // band_members.status, or community_members.role
	// RoleID is the membership's role (band_roles or community_roles),
	// RoleKey its built-in key ("" for a group's own role), for the admin
	// person page's role select.
	RoleID  int64
	RoleKey string
	// GroupKind is the community's kind ("community", "coaching",
	// "global"); "" for a band.
	GroupKind string
}

// PersonalInviteView is what the /invite/{token} landing page needs
// for a personal invite: the shell person's chosen display name and
// every membership already granted to them.
type PersonalInviteView struct {
	Token       string
	PersonID    int64
	DisplayName string
	Memberships []PersonalInviteMembership
	ExpiresAt   time.Time
}

// GetPersonalInvite resolves token to its still-valid personal invite
// (person_id set), or ErrNotFound if missing/expired/used/not personal.
func (st *Store) GetPersonalInvite(ctx context.Context, token string) (*PersonalInviteView, error) {
	var v PersonalInviteView
	var personID sql.NullInt64
	err := st.db.QueryRow(ctx, `
		SELECT i.token, i.person_id, p.display_name, i.expires_at
		FROM invites i JOIN people p ON p.id = i.person_id
		WHERE i.token = $1 AND i.expires_at > now() AND i.used_at IS NULL AND i.person_id IS NOT NULL`, token).
		Scan(&v.Token, &personID, &v.DisplayName, &v.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such invite: %w", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	v.PersonID = personID.Int64

	v.Memberships, err = st.GetPersonMemberships(ctx, v.PersonID)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// GetPersonMemberships lists personID's current band and community
// memberships (kind/id/name plus that membership's own status or role,
// unified as PersonalInviteMembership.Status) — shared by the personal-
// invite flows above and by the admin profile-review page
// (handleAdminPersonPage) to show and adjust a person's roster
// assignments in one place, whether or not they've claimed their
// account yet.
func (st *Store) GetPersonMemberships(ctx context.Context, personID int64) ([]PersonalInviteMembership, error) {
	rows, err := st.db.Query(ctx, `
		SELECT 'band', b.id, b.name, bm.status::text, bm.role_id, coalesce(br.builtin_key, ''), ''
		FROM band_members bm JOIN bands b ON b.id = bm.band_id
		JOIN band_roles br ON br.id = bm.role_id
		WHERE bm.person_id = $1 AND upper_inf(bm.validity)
		UNION ALL
		SELECT 'community', sc.id, sc.name, cr.name, cr.id, coalesce(cr.builtin_key, ''), sc.kind
		FROM community_members sm
		JOIN communities sc ON sc.id = sm.community_id
		JOIN community_roles cr ON cr.id = sm.role_id
		WHERE sm.person_id = $1 AND upper_inf(sm.validity)
		ORDER BY 1, 3`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PersonalInviteMembership
	for rows.Next() {
		var m PersonalInviteMembership
		if err := rows.Scan(&m.Kind, &m.ID, &m.Name, &m.Status, &m.RoleID, &m.RoleKey, &m.GroupKind); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ClaimPersonalInvite finishes a shell person's account: sets their
// email — the account itself from now on — and password (bcryptHash
// may be "" for an email-only/magic-link account, same as the plain-
// invite path), clears is_shell, and marks the invite used
// (markInviteUsedTx) — no new person row, no new memberships (those
// were already granted when the shell was made). ErrEmailTaken if
// another account has that email.
func (st *Store) ClaimPersonalInvite(ctx context.Context, token, email, bcryptHash string) (personID int64, err error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// Only a placeholder account can be claimed: a link left over for
	// someone who has an account since must not hand it to anyone else.
	err = tx.QueryRow(ctx, `
		SELECT i.person_id FROM invites i JOIN people p ON p.id = i.person_id
		WHERE i.token = $1 AND i.expires_at > now() AND i.used_at IS NULL AND p.is_shell
		FOR UPDATE OF i`, token).
		Scan(&personID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("no such invite: %w", ErrNotFound)
	}
	if err != nil {
		return 0, err
	}

	var pwHash *string
	if bcryptHash != "" {
		pwHash = &bcryptHash
	}
	_, err = tx.Exec(ctx, `
		UPDATE people SET email = $1, password_hash = $2, is_shell = false WHERE id = $3`,
		nullableString(email), pwHash, personID)
	if err != nil {
		if constraintViolated(err, "people_email_key") {
			return 0, ErrEmailTaken
		}
		return 0, err
	}

	if err := markInviteUsedTx(ctx, tx, token, personID, personID); err != nil {
		return 0, err
	}
	return personID, tx.Commit(ctx)
}

// ShellPersonRow is one row of ListShellPeople/ListShellPeopleForBand —
// a not-yet-claimed person plus their pre-set memberships and whether
// an invite link has ever been generated for them (a shell can exist
// with no live invite yet, e.g. right after AddBandMember but before
// "create invite link" was clicked).
type ShellPersonRow struct {
	PersonID        int64
	DisplayName     string
	CreatedAt       time.Time
	Memberships     []PersonalInviteMembership
	OpenInviteToken string    // "" if no still-valid invite exists
	OpenInviteUntil time.Time // when OpenInviteToken expires (zero without one)
	// OpenInviteExpiresAt is the same expiry, nil without one.
	OpenInviteExpiresAt *time.Time
	CreatedByName       string
}

// ListShellPeople lists every not-yet-claimed shell person on the
// instance — the admin panel's "pending accounts" view.
func (st *Store) ListShellPeople(ctx context.Context) ([]ShellPersonRow, error) {
	return st.listShellPeople(ctx, "")
}

// ListShellPeopleForBand lists shell people with a current membership
// in bandID specifically — the band's own Members page's "pending"
// section, same "any current member can see/manage this" rule the rest
// of this app's band pages already follow.
func (st *Store) ListShellPeopleForBand(ctx context.Context, bandID int64) ([]ShellPersonRow, error) {
	return st.listShellPeople(ctx, fmt.Sprintf(
		"p.id IN (SELECT person_id FROM band_members WHERE band_id = %d AND upper_inf(validity))", bandID))
}

// ListShellPeopleForCommunity is ListShellPeopleForBand's community/community
// counterpart — the community admin page's own "pending" section.
func (st *Store) ListShellPeopleForCommunity(ctx context.Context, communityID int64) ([]ShellPersonRow, error) {
	return st.listShellPeople(ctx, fmt.Sprintf(
		"p.id IN (SELECT person_id FROM community_members WHERE community_id = %d AND upper_inf(validity))", communityID))
}

func (st *Store) listShellPeople(ctx context.Context, memberOfSQL string) ([]ShellPersonRow, error) {
	cond := "p.is_shell"
	if memberOfSQL != "" {
		cond = memberOfSQL + " AND p.is_shell"
	}
	rows, err := st.db.Query(ctx, `
		SELECT p.id, p.display_name, p.created_at
		FROM people p
		WHERE `+cond+`
		ORDER BY p.created_at DESC`)
	if err != nil {
		return nil, err
	}
	var out []ShellPersonRow
	for rows.Next() {
		var r ShellPersonRow
		if err := rows.Scan(&r.PersonID, &r.DisplayName, &r.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		out[i].Memberships, err = st.GetPersonMemberships(ctx, out[i].PersonID)
		if err != nil {
			return nil, err
		}

		var token sql.NullString
		var createdByName sql.NullString
		var until sql.NullTime
		err = st.db.QueryRow(ctx, `
			SELECT i.token, p.display_name, i.expires_at FROM invites i
			LEFT JOIN people p ON p.id = i.created_by_person_id
			WHERE i.person_id = $1 AND i.expires_at > now() AND i.used_at IS NULL
			ORDER BY i.expires_at DESC LIMIT 1`, out[i].PersonID).
			Scan(&token, &createdByName, &until)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		out[i].OpenInviteToken = token.String
		out[i].OpenInviteUntil = until.Time
		if until.Valid {
			t := until.Time
			out[i].OpenInviteExpiresAt = &t
		}
		out[i].CreatedByName = createdByName.String
	}
	return out, nil
}

// PlainInviteRow is one row of ListPlainInvites — a plain/band
// single-use invite, whether still open or already expired (GetInvite
// itself only ever returns the still-valid ones; the admin listing
// needs the expired ones too, to actually answer "did this expire, or
// never exist").
type PlainInviteRow struct {
	Token         string
	BandID        int64
	BandName      string
	PersonID      int64  // a personal invite's placeholder (0: a plain one)
	PersonName    string // its name
	CreatedByName string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	// UsedAt is when it was taken (nil: not yet), UsedByID/UsedByName
	// the account that took it (migration 0138).
	UsedAt     *time.Time
	UsedByID   int64
	UsedByName string
}

// ListPlainInvites lists every row in the invites table — single-use
// plain and personal invites, open, expired unused, or used (a used one
// stays, marked: markInviteUsedTx) — but a band's nameless one-off link, left
// over from before it went and taken by nothing since
// (notNamelessBandInvite). Newest first. The admin panel's "did the
// invite I sent actually expire, and when" view — GetInvite alone can't
// answer that since it treats an expired row exactly like a missing one.
func (st *Store) ListPlainInvites(ctx context.Context) ([]PlainInviteRow, error) {
	rows, err := st.db.Query(ctx, `
		SELECT i.token, i.band_id, coalesce(b.name, ''), i.person_id, coalesce(who.display_name, ''),
		       coalesce(p.display_name, ''), i.created_at, i.expires_at,
		       i.used_at, coalesce(i.used_by_person_id, 0), coalesce(used.display_name, '')
		FROM invites i
		LEFT JOIN bands b ON b.id = i.band_id
		LEFT JOIN people who ON who.id = i.person_id
		LEFT JOIN people p ON p.id = i.created_by_person_id
		LEFT JOIN people used ON used.id = i.used_by_person_id
		WHERE `+notNamelessBandInvite+`
		ORDER BY i.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlainInviteRow
	for rows.Next() {
		var r PlainInviteRow
		var bandID, personID sql.NullInt64
		if err := rows.Scan(&r.Token, &bandID, &r.BandName, &personID, &r.PersonName, &r.CreatedByName, &r.CreatedAt, &r.ExpiresAt,
			&r.UsedAt, &r.UsedByID, &r.UsedByName); err != nil {
			return nil, err
		}
		r.BandID = bandID.Int64
		r.PersonID = personID.Int64
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetInvite resolves a token to its still-valid invite, or ErrNotFound if
// missing/expired/used, or a band's nameless one-off link (see Invite);
// DeadInviteFor says which.
func (st *Store) GetInvite(ctx context.Context, token string) (*Invite, error) {
	var inv Invite
	var personID sql.NullInt64
	var createdBy sql.NullInt64
	err := st.db.QueryRow(ctx, `
		SELECT i.token, i.person_id, i.created_by_person_id, i.expires_at
		FROM invites i
		WHERE i.token = $1 AND i.expires_at > now() AND i.used_at IS NULL AND `+notNamelessBandInvite, token).
		Scan(&inv.Token, &personID, &createdBy, &inv.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such invite: %w", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	inv.PersonID = personID.Int64
	inv.CreatedBy = createdBy.Int64
	return &inv, nil
}

// acceptInviteTx is the shared body of AcceptInvite/CreatePersonAndAcceptInvite:
// a plain invite, the only kind accepted this way (a personal one is
// ClaimPersonalInvite's; a band's nameless one-off link isn't taken any
// more, see Invite), has no membership to grant: it is marked used
// (markInviteUsedTx's columns), and the browser goes home (dest). accepterName is unused since the band
// one-off link went (it named the accepter in the inviter's
// notification).
func (st *Store) acceptInviteTx(ctx context.Context, tx pgx.Tx, token string, personID int64, accepterName string) (dest string, err error) {
	tag, err := tx.Exec(ctx, `
		UPDATE invites i SET used_at = now(), used_by_person_id = $2
		WHERE i.token = $1 AND i.expires_at > now() AND i.used_at IS NULL
		  AND i.band_id IS NULL AND i.person_id IS NULL`, token, personID)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", fmt.Errorf("no such invite: %w", ErrNotFound)
	}
	return "/", nil
}

// AcceptInvite takes a plain invite for an already-logged-in person
// (personID, display name accepterName), and marks the invite used — single-use by design, so a link can't be replayed to add an
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
//
// bcryptHash is optional: pass "" (the invite form's password field left
// blank) for a passwordless account that can only log in via magic link,
// exactly as this always worked before password login was restored as a
// first-class option; pass an already-hashed password (handlePostInvite
// hashes it before calling this) to let the new account log in with that
// password immediately, same as any other account with a password set.
func (st *Store) CreatePersonAndAcceptInvite(ctx context.Context, token, displayName, email, bcryptHash string) (personID int64, dest string, err error) {
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
		`INSERT INTO people (display_name, email, password_hash) VALUES ($1, $2, $3) RETURNING id`,
		displayName, email, pwHash).Scan(&personID); err != nil {
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
