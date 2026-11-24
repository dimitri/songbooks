package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The site admin's person page (httpapi/admin_person.go): where one
// person's account stands, and their personal invite link.

// neverJoinedSQL is the condition on people p for an account nobody
// ever got into: no email, no password, no passkey, and no session nor
// used login link, ever. Such an account hasn't joined, as a
// placeholder hasn't (CreatePersonalInvite makes it one).
const neverJoinedSQL = `(p.email IS NULL AND coalesce(p.password_hash, '') = ''
	AND NOT EXISTS (SELECT 1 FROM webauthn_credentials w WHERE w.person_id = p.id)
	AND NOT EXISTS (SELECT 1 FROM sessions s WHERE s.person_id = p.id)
	AND NOT EXISTS (SELECT 1 FROM login_tokens l WHERE l.person_id = p.id AND l.used_at IS NOT NULL))`

// PersonAccess is where personID's account stands.
type PersonAccess struct {
	IsShell     bool
	NeverJoined bool // not a placeholder, but nobody ever got into it (neverJoinedSQL)
	HasEmail    bool
	HasPassword bool
	HasPasskey  bool
	CreatedAt   time.Time
}

// HasntJoined: a placeholder, or an account nobody ever got into. Its
// way in is a personal invite link.
func (a PersonAccess) HasntJoined() bool { return a.IsShell || a.NeverJoined }

// GetPersonAccess reads where personID's account stands; ErrNotFound if
// there's no such person.
func (st *Store) GetPersonAccess(ctx context.Context, personID int64) (*PersonAccess, error) {
	var a PersonAccess
	err := st.db.QueryRow(ctx, `
		SELECT p.is_shell, NOT p.is_shell AND `+neverJoinedSQL+`, p.email IS NOT NULL, coalesce(p.password_hash, '') <> '',
		       EXISTS (SELECT 1 FROM webauthn_credentials w WHERE w.person_id = p.id), p.created_at
		FROM people p WHERE p.id = $1`, personID).
		Scan(&a.IsShell, &a.NeverJoined, &a.HasEmail, &a.HasPassword, &a.HasPasskey, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such person id %d: %w", personID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// PersonalInviteInfo is a person's personal invite link as the admin
// sees it: who made it, when, and until when it works.
type PersonalInviteInfo struct {
	Token         string
	CreatedAt     time.Time
	CreatedByID   int64  // 0: unknown (the command line, or before the column)
	CreatedByName string // "" then
	ExpiresAt     time.Time
}

// Expired reports whether the link no longer works.
func (i PersonalInviteInfo) Expired() bool { return !i.ExpiresAt.After(time.Now()) }

// GetPersonalInviteFor is personID's personal invite link, the newest
// (there's one at most: CreatePersonalInvite replaces it), expired or
// not; nil without one.
func (st *Store) GetPersonalInviteFor(ctx context.Context, personID int64) (*PersonalInviteInfo, error) {
	var i PersonalInviteInfo
	var by *int64
	err := st.db.QueryRow(ctx, `
		SELECT i.token, i.created_at, i.created_by_person_id, coalesce(c.display_name, ''), i.expires_at
		FROM invites i LEFT JOIN people c ON c.id = i.created_by_person_id
		WHERE i.person_id = $1
		ORDER BY i.created_at DESC LIMIT 1`, personID).
		Scan(&i.Token, &i.CreatedAt, &by, &i.CreatedByName, &i.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if by != nil {
		i.CreatedByID = *by
	}
	return &i, nil
}

// RevokePersonalInvites ends personID's personal invite link(s): the
// rows go, so the link says it isn't valid any more. It reports whether
// there was one.
func (st *Store) RevokePersonalInvites(ctx context.Context, personID int64) (bool, error) {
	tag, err := st.db.Exec(ctx, `DELETE FROM invites WHERE person_id = $1`, personID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
