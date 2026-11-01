package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// WebAuthnCredential is one registered passkey (see migration
// 0029_webauthn_credentials.sql for the column-by-column rationale). This
// package deliberately has no dependency on github.com/go-webauthn/webauthn
// — httpapi/webauthn.go is the only place that converts between this plain
// struct and that library's own webauthn.Credential, same separation the
// rest of this package already keeps from httpapi-layer concerns.
type WebAuthnCredential struct {
	ID                 int64
	PersonID           int64
	CredentialID       []byte
	PublicKey          []byte
	AttestationType    string
	Transports         []string
	SignCount          uint32
	FlagBackupEligible bool
	FlagBackupState    bool
	FlagUserPresent    bool
	FlagUserVerified   bool
	CreatedAt          time.Time
	LastUsedAt         *time.Time
	Nickname           string
}

// AddWebAuthnCredential inserts a newly-registered passkey for personID.
func (st *Store) AddWebAuthnCredential(ctx context.Context, personID int64, credentialID, publicKey []byte, attestationType string, transports []string, signCount uint32, backupEligible, backupState, userPresent, userVerified bool, nickname string) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx, `
		INSERT INTO webauthn_credentials
			(person_id, credential_id, public_key, attestation_type, transports, sign_count,
			 flag_backup_eligible, flag_backup_state, flag_user_present, flag_user_verified, nickname)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id`,
		personID, credentialID, publicKey, attestationType, transports, signCount,
		backupEligible, backupState, userPresent, userVerified, nullableString(nickname)).Scan(&id)
	return id, err
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

const webauthnCredentialColumns = `id, person_id, credential_id, public_key, attestation_type, transports, sign_count,
	flag_backup_eligible, flag_backup_state, flag_user_present, flag_user_verified,
	created_at, last_used_at, coalesce(nickname, '')`

func scanWebAuthnCredential(row pgx.Row) (*WebAuthnCredential, error) {
	var c WebAuthnCredential
	if err := row.Scan(&c.ID, &c.PersonID, &c.CredentialID, &c.PublicKey, &c.AttestationType, &c.Transports, &c.SignCount,
		&c.FlagBackupEligible, &c.FlagBackupState, &c.FlagUserPresent, &c.FlagUserVerified,
		&c.CreatedAt, &c.LastUsedAt, &c.Nickname); err != nil {
		return nil, err
	}
	return &c, nil
}

// ListWebAuthnCredentials returns personID's registered passkeys, newest
// first — for both the profile page's management list and for building the
// webauthn.User adapter's credential set at ceremony time.
func (st *Store) ListWebAuthnCredentials(ctx context.Context, personID int64) ([]WebAuthnCredential, error) {
	rows, err := st.db.Query(ctx, `SELECT `+webauthnCredentialColumns+` FROM webauthn_credentials WHERE person_id = $1 ORDER BY created_at DESC`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebAuthnCredential
	for rows.Next() {
		c, err := scanWebAuthnCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// GetWebAuthnCredentialByCredentialID looks up a credential by its
// authenticator-assigned id, regardless of owner — used by the discoverable
// login flow, which doesn't know the person until the credential id (the
// assertion's rawId) arrives.
func (st *Store) GetWebAuthnCredentialByCredentialID(ctx context.Context, credentialID []byte) (*WebAuthnCredential, error) {
	c, err := scanWebAuthnCredential(st.db.QueryRow(ctx, `SELECT `+webauthnCredentialColumns+` FROM webauthn_credentials WHERE credential_id = $1`, credentialID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such passkey: %w", ErrNotFound)
	}
	return c, err
}

// UpdateWebAuthnCredentialAfterLogin persists the post-ceremony state
// go-webauthn's FinishPasskeyLogin/FinishDiscoverableLogin hands back:
// the (possibly advanced) sign count and the current flag values, plus
// last_used_at — called once per successful passkey login, never on a
// failed one.
func (st *Store) UpdateWebAuthnCredentialAfterLogin(ctx context.Context, credentialID []byte, signCount uint32, backupEligible, backupState, userPresent, userVerified bool) error {
	tag, err := st.db.Exec(ctx, `
		UPDATE webauthn_credentials
		SET sign_count = $2, flag_backup_eligible = $3, flag_backup_state = $4,
		    flag_user_present = $5, flag_user_verified = $6, last_used_at = now()
		WHERE credential_id = $1`,
		credentialID, signCount, backupEligible, backupState, userPresent, userVerified)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such passkey: %w", ErrNotFound)
	}
	return nil
}

// SetWebAuthnCredentialNickname sets a passkey's user-given label (e.g.
// "MacBook", "iPhone") — scoped to personID so a person can only rename
// their own passkeys.
func (st *Store) SetWebAuthnCredentialNickname(ctx context.Context, personID, credentialDBID int64, nickname string) error {
	tag, err := st.db.Exec(ctx, `UPDATE webauthn_credentials SET nickname = $1 WHERE id = $2 AND person_id = $3`,
		nullableString(nickname), credentialDBID, personID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such passkey for this person: %w", ErrNotFound)
	}
	return nil
}

// RemoveWebAuthnCredential deletes one of personID's own passkeys — scoped
// to personID so a person can only remove their own, same reasoning as
// SetWebAuthnCredentialNickname.
func (st *Store) RemoveWebAuthnCredential(ctx context.Context, personID, credentialDBID int64) error {
	tag, err := st.db.Exec(ctx, `DELETE FROM webauthn_credentials WHERE id = $1 AND person_id = $2`, credentialDBID, personID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such passkey for this person: %w", ErrNotFound)
	}
	return nil
}
