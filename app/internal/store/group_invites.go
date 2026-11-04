package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// GroupInvite is one shareable, multi-use roster link, targeting exactly
// one band or school (see Kind). Unlike Invite (single-use, one accept =
// consumed), a GroupInvite stays open across many accepts — each of its
// slots is claimable exactly once instead.
type GroupInvite struct {
	ID         int64
	Token      string
	TargetKind string // "band" or "school"
	BandID     int64
	SchoolID   int64
	CreatedBy  int64
	CreatedAt  time.Time
	ClosedAt   *time.Time
	// DefaultStatus is the fixed role every claim through this invite
	// grants — used for a "community" (school-target) link, where the
	// role is chosen once when the link is minted (a staff-granting link
	// vs. a member-granting link) rather than per named roster slot the
	// way a band's group invite works. "" preserves every pre-existing
	// invite's exact old behavior: a band's unlisted claim still grants
	// 'guest', a school's still grants 'student' — see
	// grantGroupInviteMembershipTx.
	DefaultStatus string
}

// GroupInviteSlot is one pre-typed expected person on a group invite's
// roster. Instrument is only ever set for a band-targeted invite — a
// school roster entry is just a name (see this phase's design notes:
// every school slot defaults to 'student' on claim; inviting someone AS
// an instructor stays a deliberate direct-add, not a roster claim).
type GroupInviteSlot struct {
	ID                int64
	GroupInviteID     int64
	DisplayName       string
	Instrument        string
	Status            string // "core" or "guest" — the band status this slot grants on claim
	ClaimedByPersonID *int64
	ClaimedByName     string // the claiming person's display name, '' if unclaimed
	ClaimedAt         *time.Time
}

// GroupInviteView is everything the public /join/{token} page needs:
// the invite itself, the band/school name it resolves to, and its full
// slot list (claimed and unclaimed both — the claimed ones are shown,
// not pickable, so a visitor can see who's already in).
type GroupInviteView struct {
	GroupInvite
	TargetName string
	Slots      []GroupInviteSlot
}

// GroupInviteSlotInput is one roster row as typed by the organizer
// creating the invite.
type GroupInviteSlotInput struct {
	DisplayName string
	Instrument  string // band kind only; ignored for school
	Status      string // band kind only; "core" or "guest", defaults to "guest" if blank/invalid
}

func normalizeSlotStatus(status string) string {
	if status == "core" {
		return "core"
	}
	return "guest"
}

// CreateGroupInvite makes a brand-new, reusable group invite for
// targetKind ("band" or "school") + targetID, with one group_invite_slots
// row per non-blank entry in slots. Returns the invite's token.
// defaultStatus fixes the role every claim through this invite grants
// (used for a community/school link minted as either "staff" or
// "member" — see GroupInvite.DefaultStatus's own doc comment); ""
// preserves the pre-existing per-slot/hardcoded behavior exactly, for
// every other caller (a band's named roster link, or a school link
// created before this existed).
func (st *Store) CreateGroupInvite(ctx context.Context, targetKind string, targetID, createdBy int64, slots []GroupInviteSlotInput, defaultStatus string) (groupInviteID int64, token string, err error) {
	if targetKind != "band" && targetKind != "school" {
		return 0, "", fmt.Errorf("invalid target kind %q", targetKind)
	}
	token, err = randomToken()
	if err != nil {
		return 0, "", err
	}

	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback(ctx)

	var bandID, schoolID *int64
	if targetKind == "band" {
		bandID = &targetID
	} else {
		schoolID = &targetID
	}
	var defaultStatusArg *string
	if defaultStatus != "" {
		defaultStatusArg = &defaultStatus
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO group_invites (token, target_kind, band_id, school_id, created_by_person_id, default_status)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		token, targetKind, bandID, schoolID, nullableID(createdBy), defaultStatusArg).Scan(&groupInviteID); err != nil {
		return 0, "", err
	}

	for _, sl := range slots {
		name := strings.TrimSpace(sl.DisplayName)
		if name == "" {
			continue
		}
		var instrument *string
		status := "guest"
		if targetKind == "band" {
			if v := strings.TrimSpace(sl.Instrument); v != "" {
				instrument = &v
			}
			status = normalizeSlotStatus(sl.Status)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO group_invite_slots (group_invite_id, display_name, instrument, status)
			VALUES ($1, $2, $3, $4)`, groupInviteID, name, instrument, status); err != nil {
			return 0, "", err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, "", err
	}
	return groupInviteID, token, nil
}

// GetGroupInviteByToken resolves a token to its invite (open or closed —
// the caller decides what to show for a closed one) plus its target's
// name and full slot list, or ErrNotFound if the token is unknown.
func (st *Store) GetGroupInviteByToken(ctx context.Context, token string) (*GroupInviteView, error) {
	gi, targetName, err := st.scanGroupInvite(ctx, st.db, `
		SELECT gi.id, gi.token, gi.target_kind, gi.band_id, gi.school_id, gi.created_by_person_id, gi.created_at, gi.closed_at, coalesce(gi.default_status, ''),
		       coalesce(b.name, sc.name, '')
		FROM group_invites gi
		LEFT JOIN bands b ON b.id = gi.band_id
		LEFT JOIN schools sc ON sc.id = gi.school_id
		WHERE gi.token = $1`, token)
	if err != nil {
		return nil, err
	}
	slots, err := st.listGroupInviteSlots(ctx, st.db, gi.ID)
	if err != nil {
		return nil, err
	}
	return &GroupInviteView{GroupInvite: *gi, TargetName: targetName, Slots: slots}, nil
}

// ListGroupInvitesForTarget lists every group invite ever created for
// targetKind+targetID ("band" or "school"), open ones first then closed,
// newest first within each, each with its full slot list (claimed and
// unclaimed) — what a band/school's own page needs to show "here's the
// roster link you already made, and who on it hasn't joined yet,"
// instead of a link that's only ever visible once, right after creation
// (see CreateGroupInvite's doc comment — the gap this fills).
func (st *Store) ListGroupInvitesForTarget(ctx context.Context, targetKind string, targetID int64) ([]GroupInviteView, error) {
	col := "band_id"
	if targetKind == "school" {
		col = "school_id"
	}
	rows, err := st.db.Query(ctx, `
		SELECT id, token, target_kind, band_id, school_id, created_by_person_id, created_at, closed_at, coalesce(default_status, '')
		FROM group_invites WHERE `+col+` = $1
		ORDER BY (closed_at IS NULL) DESC, created_at DESC`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var invites []GroupInvite
	for rows.Next() {
		var gi GroupInvite
		var bandID, schoolID, createdBy sql.NullInt64
		var closedAt sql.NullTime
		if err := rows.Scan(&gi.ID, &gi.Token, &gi.TargetKind, &bandID, &schoolID, &createdBy, &gi.CreatedAt, &closedAt, &gi.DefaultStatus); err != nil {
			return nil, err
		}
		gi.BandID, gi.SchoolID, gi.CreatedBy = bandID.Int64, schoolID.Int64, createdBy.Int64
		if closedAt.Valid {
			gi.ClosedAt = &closedAt.Time
		}
		invites = append(invites, gi)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]GroupInviteView, 0, len(invites))
	for _, gi := range invites {
		slots, err := st.listGroupInviteSlots(ctx, st.db, gi.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, GroupInviteView{GroupInvite: gi, Slots: slots})
	}
	return out, nil
}

func (st *Store) scanGroupInvite(ctx context.Context, q dbtx, query string, arg any) (*GroupInvite, string, error) {
	var gi GroupInvite
	var bandID, schoolID, createdBy sql.NullInt64
	var closedAt sql.NullTime
	var targetName string
	err := q.QueryRow(ctx, query, arg).
		Scan(&gi.ID, &gi.Token, &gi.TargetKind, &bandID, &schoolID, &createdBy, &gi.CreatedAt, &closedAt, &gi.DefaultStatus, &targetName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", fmt.Errorf("no such group invite: %w", ErrNotFound)
	}
	if err != nil {
		return nil, "", err
	}
	gi.BandID, gi.SchoolID, gi.CreatedBy = bandID.Int64, schoolID.Int64, createdBy.Int64
	if closedAt.Valid {
		gi.ClosedAt = &closedAt.Time
	}
	return &gi, targetName, nil
}

func (st *Store) listGroupInviteSlots(ctx context.Context, q dbtx, groupInviteID int64) ([]GroupInviteSlot, error) {
	rows, err := q.Query(ctx, `
		SELECT s.id, s.group_invite_id, s.display_name, coalesce(s.instrument, ''), s.status, s.claimed_by_person_id, s.claimed_at,
		       coalesce(p.display_name, '')
		FROM group_invite_slots s
		LEFT JOIN people p ON p.id = s.claimed_by_person_id
		WHERE s.group_invite_id = $1 ORDER BY s.id`, groupInviteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupInviteSlot
	for rows.Next() {
		var sl GroupInviteSlot
		var claimedBy sql.NullInt64
		var claimedAt sql.NullTime
		if err := rows.Scan(&sl.ID, &sl.GroupInviteID, &sl.DisplayName, &sl.Instrument, &sl.Status, &claimedBy, &claimedAt, &sl.ClaimedByName); err != nil {
			return nil, err
		}
		if claimedBy.Valid {
			id := claimedBy.Int64
			sl.ClaimedByPersonID = &id
		}
		if claimedAt.Valid {
			t := claimedAt.Time
			sl.ClaimedAt = &t
		}
		out = append(out, sl)
	}
	return out, rows.Err()
}

// lockOpenGroupInviteTx reads+locks a group invite row by token inside
// tx (FOR UPDATE, so two concurrent claims against the same invite
// serialize rather than race), and refuses with ErrForbidden if it's
// already been explicitly closed. Slot-exhaustion alone is deliberately
// NOT treated as closed here — see this phase's design notes: an invite
// with every named slot claimed still accepts "someone else" unlisted
// joins until a person explicitly closes it.
func (st *Store) lockOpenGroupInviteTx(ctx context.Context, tx pgx.Tx, token string) (*GroupInvite, error) {
	gi, _, err := st.scanGroupInvite(ctx, tx, `
		SELECT id, token, target_kind, band_id, school_id, created_by_person_id, created_at, closed_at, coalesce(default_status, ''), ''
		FROM group_invites WHERE token = $1 FOR UPDATE`, token)
	if err != nil {
		return nil, err
	}
	if gi.ClosedAt != nil {
		return nil, fmt.Errorf("this group invite has been closed: %w", ErrForbidden)
	}
	return gi, nil
}

// grantGroupInviteMembershipTx grants personID the membership a group
// invite's target promises — band status='guest' (a casually roster-
// invited person isn't automatically "core"), school role='student'
// (inviting someone AS an instructor stays a deliberate direct-add, see
// handleAddSchoolMember, not a roster claim) — and is a no-op if
// personID is already a CURRENT member of that band/school (an existing
// account claiming a slot should never get a duplicate membership row
// or an error just because they're already in).
func (st *Store) grantGroupInviteMembershipTx(ctx context.Context, tx pgx.Tx, gi *GroupInvite, personID int64, status string) (dest string, err error) {
	switch gi.TargetKind {
	case "band":
		already, err := isCurrentBandMember(ctx, tx, gi.BandID, personID)
		if err != nil {
			return "", err
		}
		if !already {
			if _, err := tx.Exec(ctx, `
				INSERT INTO band_members (band_id, person_id, status, invited_by_person_id)
				VALUES ($1, $2, $3, $4)`, gi.BandID, personID, normalizeSlotStatus(status), nullableID(gi.CreatedBy)); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("/bands/%d", gi.BandID), nil

	case "school":
		already, err := isCurrentSchoolMember(ctx, tx, gi.SchoolID, personID, "")
		if err != nil {
			return "", err
		}
		role := gi.DefaultStatus
		if role != "instructor" && role != "student" {
			role = "student" // every pre-existing school group invite's exact old behavior
		}
		if !already {
			if _, err := tx.Exec(ctx, `
				INSERT INTO school_members (school_id, person_id, role, invited_by_person_id)
				VALUES ($1, $2, $3, $4)`, gi.SchoolID, personID, role, nullableID(gi.CreatedBy)); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("/schools/%d", gi.SchoolID), nil

	default:
		return "", fmt.Errorf("invalid target kind %q", gi.TargetKind)
	}
}

// claimSlotTx marks slotID (belonging to groupInviteID) claimed by
// personID, after checking it isn't already claimed by someone else —
// ErrConflict (same sentinel a stale/concurrent edit uses elsewhere in
// this codebase) if a double-claim race loses.
func (st *Store) claimSlotTx(ctx context.Context, tx pgx.Tx, groupInviteID, slotID, personID int64) (status string, err error) {
	var claimed bool
	err = tx.QueryRow(ctx, `
		SELECT claimed_by_person_id IS NOT NULL, status FROM group_invite_slots
		WHERE id = $1 AND group_invite_id = $2 FOR UPDATE`, slotID, groupInviteID).Scan(&claimed, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("no such slot on this invite: %w", ErrNotFound)
	}
	if err != nil {
		return "", err
	}
	if claimed {
		return "", fmt.Errorf("this slot has already been claimed: %w", ErrConflict)
	}
	_, err = tx.Exec(ctx, `UPDATE group_invite_slots SET claimed_by_person_id = $1, claimed_at = now() WHERE id = $2`, personID, slotID)
	return status, err
}

// ClaimGroupInviteSlot is an already-logged-in person claiming a named
// slot on a still-open group invite, for their EXISTING account —
// grants the membership (on top of whatever they already have) and
// marks the slot claimed, in one transaction. dest is where the browser
// should land afterwards.
func (st *Store) ClaimGroupInviteSlot(ctx context.Context, token string, slotID, personID int64) (dest string, err error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	gi, err := st.lockOpenGroupInviteTx(ctx, tx, token)
	if err != nil {
		return "", err
	}
	status, err := st.claimSlotTx(ctx, tx, gi.ID, slotID, personID)
	if err != nil {
		return "", err
	}
	dest, err = st.grantGroupInviteMembershipTx(ctx, tx, gi, personID, status)
	if err != nil {
		return "", err
	}
	return dest, tx.Commit(ctx)
}

// ClaimGroupInviteUnlisted is an already-logged-in person joining as
// "someone else" (not on the pre-typed roster) — grants the membership
// and records a fresh, already-claimed slot for bookkeeping (labelled
// from their own display name, since no roster entry existed for them).
func (st *Store) ClaimGroupInviteUnlisted(ctx context.Context, token string, personID int64, displayName string) (dest string, err error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	gi, err := st.lockOpenGroupInviteTx(ctx, tx, token)
	if err != nil {
		return "", err
	}
	dest, err = st.grantGroupInviteMembershipTx(ctx, tx, gi, personID, "guest")
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO group_invite_slots (group_invite_id, display_name, claimed_by_person_id, claimed_at)
		VALUES ($1, $2, $3, now())`, gi.ID, displayName, personID); err != nil {
		return "", err
	}
	return dest, tx.Commit(ctx)
}

// CreatePersonAndClaimGroupInviteSlot is ClaimGroupInviteSlot's
// counterpart for a brand-new visitor: creates the person row (email
// only, no password — same "accepting is this account's first auth
// step" convention as CreatePersonAndAcceptInvite; see that function's
// doc comment for the note on the parallel optional-password auth work),
// then claims the named slot as them, all in one transaction so a failed
// claim never leaves an orphan account behind.
func (st *Store) CreatePersonAndClaimGroupInviteSlot(ctx context.Context, token string, slotID int64, username, displayName, email string) (personID int64, dest string, err error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback(ctx)

	if err := tx.QueryRow(ctx,
		`INSERT INTO people (username, display_name, email) VALUES ($1, $2, $3) RETURNING id`,
		username, displayName, email).Scan(&personID); err != nil {
		return 0, "", err
	}

	gi, err := st.lockOpenGroupInviteTx(ctx, tx, token)
	if err != nil {
		return 0, "", err
	}
	status, err := st.claimSlotTx(ctx, tx, gi.ID, slotID, personID)
	if err != nil {
		return 0, "", err
	}
	dest, err = st.grantGroupInviteMembershipTx(ctx, tx, gi, personID, status)
	if err != nil {
		return 0, "", err
	}
	return personID, dest, tx.Commit(ctx)
}

// CreatePersonAndClaimGroupInviteUnlisted is ClaimGroupInviteUnlisted's
// counterpart for a brand-new visitor joining as "someone else".
func (st *Store) CreatePersonAndClaimGroupInviteUnlisted(ctx context.Context, token, username, displayName, email string) (personID int64, dest string, err error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback(ctx)

	if err := tx.QueryRow(ctx,
		`INSERT INTO people (username, display_name, email) VALUES ($1, $2, $3) RETURNING id`,
		username, displayName, email).Scan(&personID); err != nil {
		return 0, "", err
	}

	gi, err := st.lockOpenGroupInviteTx(ctx, tx, token)
	if err != nil {
		return 0, "", err
	}
	dest, err = st.grantGroupInviteMembershipTx(ctx, tx, gi, personID, "guest")
	if err != nil {
		return 0, "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO group_invite_slots (group_invite_id, display_name, claimed_by_person_id, claimed_at)
		VALUES ($1, $2, $3, now())`, gi.ID, displayName, personID); err != nil {
		return 0, "", err
	}
	return personID, dest, tx.Commit(ctx)
}

// AddGroupInviteSlot appends one more named roster slot to an already
// open group invite — the roster-editing counterpart of typing an extra
// name into CreateGroupInvite's form, for a roster someone already
// shared a link for ("add names... even for members that have not
// joined yet"). targetKind+targetID scope it the same defense-in-depth
// way CloseGroupInvite does.
func (st *Store) AddGroupInviteSlot(ctx context.Context, groupInviteID int64, targetKind string, targetID int64, in GroupInviteSlotInput) (slotID int64, err error) {
	col := "band_id"
	if targetKind == "school" {
		col = "school_id"
	}
	var openID int64
	err = st.db.QueryRow(ctx, fmt.Sprintf(`
		SELECT id FROM group_invites WHERE id = $1 AND target_kind = $2 AND %s = $3 AND closed_at IS NULL`, col),
		groupInviteID, targetKind, targetID).Scan(&openID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("no such open group invite: %w", ErrNotFound)
	}
	if err != nil {
		return 0, err
	}
	name := strings.TrimSpace(in.DisplayName)
	if name == "" {
		return 0, fmt.Errorf("a name is required")
	}
	var instrument *string
	status := "guest"
	if targetKind == "band" {
		if v := strings.TrimSpace(in.Instrument); v != "" {
			instrument = &v
		}
		status = normalizeSlotStatus(in.Status)
	}
	err = st.db.QueryRow(ctx, `
		INSERT INTO group_invite_slots (group_invite_id, display_name, instrument, status)
		VALUES ($1, $2, $3, $4) RETURNING id`, groupInviteID, name, instrument, status).Scan(&slotID)
	return slotID, err
}

// UpdateGroupInviteSlot edits an unclaimed roster slot's name/instrument/
// status in place — the "fix a name, change someone's role before they
// join" action. ErrConflict if the slot has already been claimed (the
// roster UI never lets an editable row get this far, but a concurrent
// claim is still possible), ErrNotFound if the slot/invite doesn't
// resolve to this exact band.
func (st *Store) UpdateGroupInviteSlot(ctx context.Context, groupInviteID, slotID, bandID int64, displayName, instrument, status string) error {
	name := strings.TrimSpace(displayName)
	if name == "" {
		return fmt.Errorf("a name is required")
	}
	var instrumentArg *string
	if v := strings.TrimSpace(instrument); v != "" {
		instrumentArg = &v
	}
	tag, err := st.db.Exec(ctx, `
		UPDATE group_invite_slots s SET display_name = $1, instrument = $2, status = $3
		FROM group_invites gi
		WHERE s.id = $4 AND s.group_invite_id = $5 AND s.claimed_by_person_id IS NULL
		  AND gi.id = s.group_invite_id AND gi.target_kind = 'band' AND gi.band_id = $6`,
		name, instrumentArg, normalizeSlotStatus(status), slotID, groupInviteID, bandID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such unclaimed slot on this band's roster: %w", ErrNotFound)
	}
	return nil
}

// RemoveGroupInviteSlot deletes an unclaimed roster slot ("remove names,
// even for members that have not joined yet") — never a claimed one; use
// RemoveBandMember for an actual current member instead.
func (st *Store) RemoveGroupInviteSlot(ctx context.Context, groupInviteID, slotID, bandID int64) error {
	tag, err := st.db.Exec(ctx, `
		DELETE FROM group_invite_slots s
		USING group_invites gi
		WHERE s.id = $1 AND s.group_invite_id = $2 AND s.claimed_by_person_id IS NULL
		  AND gi.id = s.group_invite_id AND gi.target_kind = 'band' AND gi.band_id = $3`,
		slotID, groupInviteID, bandID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such unclaimed slot on this band's roster: %w", ErrNotFound)
	}
	return nil
}

// CloseGroupInvite explicitly closes a still-open group invite —
// scoped to targetKind+targetID so a caller authorized on band/school X
// can't close an invite belonging to some other band/school Y just by
// guessing its id (same defense-in-depth every other band/school-scoped
// store method in this codebase uses). ErrNotFound if groupInviteID
// doesn't resolve to an open invite on that exact target.
func (st *Store) CloseGroupInvite(ctx context.Context, groupInviteID int64, targetKind string, targetID int64) error {
	col := "band_id"
	if targetKind == "school" {
		col = "school_id"
	}
	tag, err := st.db.Exec(ctx, fmt.Sprintf(`
		UPDATE group_invites SET closed_at = now()
		WHERE id = $1 AND target_kind = $2 AND %s = $3 AND closed_at IS NULL`, col),
		groupInviteID, targetKind, targetID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such open group invite: %w", ErrNotFound)
	}
	return nil
}
