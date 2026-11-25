package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// A personal link (/invite/{token}, a placeholder's: CreatePersonalInvite)
// keeps working until it expires, for its one person (decided
// 2026-11-25: people use several devices, and open the same link on each).
// Its first use makes the account (ClaimPersonalInvite, or
// ClaimPersonalInviteAs for someone signed in): used_at is set, and the
// link names the account from then on (the placeholder IS the account,
// or was merged into it). Every later use is that person coming back:
// they sign in with the account's own password or passkey, the link
// never signs anyone in by itself (it is shareable), and it can't make
// another account nor change this one's email or password (the claims
// read used_at IS NULL and is_shell).
//
// Which use a link is for reads the person, not the link: is_shell, the
// first use; an account, coming back. A new link made for someone who
// has joined (New link on a roster) is never used to join: it is for
// coming back from the start (NewReturningLink: a leaked link replaced).

// ReturningLink is a personal link for coming back: its person has an
// account, and it hasn't expired.
type ReturningLink struct {
	Token       string
	PersonID    int64
	DisplayName string
	// Email is the account's, for its page to show masked; never in
	// full.
	Email     string
	ExpiresAt time.Time
	// UsedAt is when it was used to join (nil: made after they joined).
	UsedAt *time.Time
	// TargetKind ("band", "community" or "") and TargetID are where the
	// link takes them: where it took them when they joined
	// (joined_band_id / joined_community_id), else their first band,
	// else first community.
	TargetKind string
	TargetID   int64
	TargetName string
}

// GetReturningLink resolves token to a personal link for coming back:
// its person has an account (not a placeholder), and it hasn't expired.
// ErrNotFound otherwise: unknown, revoked, expired, a placeholder's link
// still to be taken (GetPersonalInvite's), or not a personal one.
func (st *Store) GetReturningLink(ctx context.Context, token string) (*ReturningLink, error) {
	var l ReturningLink
	var email *string
	var bandID, communityID *int64
	err := st.db.QueryRow(ctx, `
		SELECT i.token, p.id, p.display_name, p.email, i.expires_at, i.used_at,
		       i.joined_band_id, i.joined_community_id
		FROM invites i JOIN people p ON p.id = i.person_id
		WHERE i.token = $1 AND i.expires_at > now() AND NOT p.is_shell`, token).
		Scan(&l.Token, &l.PersonID, &l.DisplayName, &email, &l.ExpiresAt, &l.UsedAt, &bandID, &communityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such returning link: %w", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	if email != nil {
		l.Email = *email
	}
	switch {
	case bandID != nil:
		l.TargetKind, l.TargetID = "band", *bandID
		err = st.db.QueryRow(ctx, `SELECT name FROM bands WHERE id = $1`, *bandID).Scan(&l.TargetName)
	case communityID != nil:
		l.TargetKind, l.TargetID = "community", *communityID
		err = st.db.QueryRow(ctx, `SELECT name FROM communities WHERE id = $1`, *communityID).Scan(&l.TargetName)
	default:
		ms, merr := st.GetPersonMemberships(ctx, l.PersonID)
		if merr != nil {
			return nil, merr
		}
		for _, m := range ms {
			if m.Kind == "band" || m.Kind == "community" {
				l.TargetKind, l.TargetID, l.TargetName = m.Kind, m.ID, m.Name
				break
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// RecordLinkSignIn counts a sign-in through token (signins,
// last_signin_at: migration 0139).
func (st *Store) RecordLinkSignIn(ctx context.Context, token string) error {
	_, err := st.db.Exec(ctx, `
		UPDATE invites SET signins = signins + 1, last_signin_at = now() WHERE token = $1`, token)
	return err
}

// ReturningLinkInfo is someone's personal link for coming back as a
// roster or the admin shows it.
type ReturningLinkInfo struct {
	PersonID    int64
	DisplayName string
	Token       string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	UsedAt      *time.Time // when it was used to join; nil: made after
	Signins     int
	LastSignin  *time.Time
}

// ListReturningLinksForBand is the personal links for coming back of
// bandID's current members who have joined: the newest still working
// one each.
func (st *Store) ListReturningLinksForBand(ctx context.Context, bandID int64) ([]ReturningLinkInfo, error) {
	return st.listReturningLinks(ctx, `p.id IN (SELECT person_id FROM band_members WHERE band_id = $1 AND upper_inf(validity))`, bandID)
}

// ListReturningLinksForCommunity is ListReturningLinksForBand for a
// community.
func (st *Store) ListReturningLinksForCommunity(ctx context.Context, communityID int64) ([]ReturningLinkInfo, error) {
	return st.listReturningLinks(ctx, `p.id IN (SELECT person_id FROM community_members WHERE community_id = $1 AND upper_inf(validity))`, communityID)
}

// ReturningLinkFor is personID's personal link for coming back, the
// newest still working one; nil without one (or while they haven't
// joined: theirs is a link to take, GetPersonalInviteFor).
func (st *Store) ReturningLinkFor(ctx context.Context, personID int64) (*ReturningLinkInfo, error) {
	ls, err := st.listReturningLinks(ctx, `p.id = $1`, personID)
	if err != nil || len(ls) == 0 {
		return nil, err
	}
	return &ls[0], nil
}

func (st *Store) listReturningLinks(ctx context.Context, cond string, arg int64) ([]ReturningLinkInfo, error) {
	rows, err := st.db.Query(ctx, `
		SELECT DISTINCT ON (p.id) p.id, p.display_name, i.token, i.created_at, i.expires_at, i.used_at,
		       i.signins, i.last_signin_at
		FROM invites i JOIN people p ON p.id = i.person_id
		WHERE NOT p.is_shell AND i.expires_at > now() AND `+cond+`
		ORDER BY p.id, i.created_at DESC`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReturningLinkInfo
	for rows.Next() {
		var l ReturningLinkInfo
		if err := rows.Scan(&l.PersonID, &l.DisplayName, &l.Token, &l.CreatedAt, &l.ExpiresAt, &l.UsedAt,
			&l.Signins, &l.LastSignin); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// newReturningLinkTx makes a new personal link for personID, who has
// joined, for coming back; their links still working stop (expired now:
// a used one stays, used, as migration 0138 keeps it). The new one was
// never used to join (used_at NULL).
func newReturningLinkTx(ctx context.Context, tx pgx.Tx, token string, personID, createdBy int64, ttl time.Duration) error {
	if _, err := tx.Exec(ctx, `
		UPDATE invites SET expires_at = now() WHERE person_id = $1 AND expires_at > now()`, personID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO invites (token, person_id, created_by_person_id, expires_at) VALUES ($1, $2, $3, $4)`,
		token, personID, nullableID(createdBy), time.Now().Add(ttl))
	return err
}

// ErrHasntJoined is NewReturningLink's answer for someone who hasn't
// joined: their link is one to take (CreatePersonalInvite).
var ErrHasntJoined = errors.New("that person hasn't joined yet")

// ErrNoReturningLink is NewReturningLink's answer for someone who has
// joined and whose personal link no longer works (expired, revoked, or
// they joined another way): they log in, from the login page.
var ErrNoReturningLink = errors.New("they have joined, and have no personal link that still works: they log in")

// NewReturningLink replaces personID's personal link, which still opens
// for them (they've joined), with a new one, and returns it; the one
// they had stops working (a leaked link, say). Only while it works: a
// link for coming back is for the first weeks on more devices, its
// expiry is its end (ErrNoReturningLink then). ErrNotFound if there's
// no such person, ErrHasntJoined for a placeholder or an account nobody
// ever got into (CreatePersonalInvite's).
func (st *Store) NewReturningLink(ctx context.Context, personID, createdBy int64, ttl time.Duration) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var hasntJoined, hasLink bool
	err = tx.QueryRow(ctx, `
		SELECT p.is_shell OR `+neverJoinedSQL+`,
		       EXISTS (SELECT 1 FROM invites i WHERE i.person_id = p.id AND i.expires_at > now())
		FROM people p WHERE p.id = $1 FOR UPDATE`, personID).Scan(&hasntJoined, &hasLink)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("no such person id %d: %w", personID, ErrNotFound)
	}
	if err != nil {
		return "", err
	}
	if hasntJoined {
		return "", ErrHasntJoined
	}
	if !hasLink {
		return "", ErrNoReturningLink
	}
	if err := newReturningLinkTx(ctx, tx, token, personID, createdBy, ttl); err != nil {
		return "", err
	}
	return token, tx.Commit(ctx)
}
