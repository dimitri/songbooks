package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// LoginTokenTTL is how long a magic login link stays valid (see
// CreateLoginToken), for the pages that tell someone so.
const LoginTokenTTL = loginTokenTTL

// IsShellPerson reports whether personID is a not-yet-claimed person
// (see CreateShellPerson): a name on a roster, no account to log in to.
func (st *Store) IsShellPerson(ctx context.Context, personID int64) (bool, error) {
	var shell bool
	err := st.db.QueryRow(ctx, `SELECT is_shell FROM people WHERE id = $1`, personID).Scan(&shell)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("no such person id %d: %w", personID, ErrNotFound)
	}
	return shell, err
}

// LoginTokenState is where one magic-link token stands, for the admin's
// login-link dialog: whether it has been used, and until when it works.
type LoginTokenState struct {
	PersonID  int64
	Used      bool
	ExpiresAt time.Time
}

// GetLoginTokenState looks up token without consuming it.
func (st *Store) GetLoginTokenState(ctx context.Context, token string) (*LoginTokenState, error) {
	var s LoginTokenState
	err := st.db.QueryRow(ctx,
		`SELECT person_id, used_at IS NOT NULL, expires_at FROM login_tokens WHERE token = $1`, token).
		Scan(&s.PersonID, &s.Used, &s.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such login token: %w", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// RevokeLoginToken makes personID's still-unused token stop working, as
// if it had been used: the link an admin had on screen, when they send
// the person an email instead. A token that isn't personID's, or is
// already used, is left alone.
func (st *Store) RevokeLoginToken(ctx context.Context, token string, personID int64) error {
	_, err := st.db.Exec(ctx,
		`UPDATE login_tokens SET used_at = now() WHERE token = $1 AND person_id = $2 AND used_at IS NULL`,
		token, personID)
	return err
}
