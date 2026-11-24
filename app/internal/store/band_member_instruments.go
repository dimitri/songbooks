package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Limits on what NormalizeInstruments keeps: enough for anyone's real
// list, short enough that a stray paste can't fill a roster row.
const (
	maxInstruments      = 20
	maxInstrumentLength = 40
)

// MayEditMemberInstruments is who may change a band member's instruments
// from the band's roster: any member for someone who hasn't joined yet
// (a placeholder has no one else to keep it right), the person
// themselves, and for anyone a member whose role grants manage_members
// (by default, a core member). A real person can also change theirs on
// their own profile.
func MayEditMemberInstruments(actorManagesMembers, actorIsTarget, targetIsShell bool) bool {
	return targetIsShell || actorIsTarget || actorManagesMembers
}

// NormalizeInstruments cleans a list of instruments as typed: each entry
// may itself be comma separated ("bass, vocals"); blanks go, spaces are
// trimmed, repeats (ignoring case) keep their first spelling, and the
// list and each name are capped. Never nil (people.instruments is NOT
// NULL).
func NormalizeInstruments(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, entry := range in {
		for _, inst := range strings.Split(entry, ",") {
			inst = strings.Join(strings.Fields(inst), " ")
			if r := []rune(inst); len(r) > maxInstrumentLength {
				inst = strings.TrimSpace(string(r[:maxInstrumentLength]))
			}
			key := strings.ToLower(inst)
			if inst == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, inst)
			if len(out) == maxInstruments {
				return out
			}
		}
	}
	return out
}

// SetBandMemberInstruments replaces personID's instruments
// (people.instruments) from bandID's roster, on actorID's behalf, and
// returns the list as saved (see NormalizeInstruments). actorIsAdmin is
// a site admin, who may do what a core member may. ErrNotFound if
// personID isn't a current member of bandID, ErrForbidden if
// MayEditMemberInstruments says actorID may not.
func (st *Store) SetBandMemberInstruments(ctx context.Context, bandID, actorID, personID int64, actorIsAdmin bool, instruments []string) ([]string, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var targetIsShell bool
	err = tx.QueryRow(ctx, `
		SELECT p.is_shell FROM band_members m JOIN people p ON p.id = m.person_id
		WHERE m.band_id = $1 AND m.person_id = $2 AND upper_inf(m.validity)
		FOR UPDATE OF p`, bandID, personID).Scan(&targetIsShell)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("person %d is not a current member of band %d: %w", personID, bandID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}

	actorIsCore := actorIsAdmin
	if !actorIsCore {
		if actorIsCore, err = hasBandPrivilege(ctx, tx, bandID, actorID, BandPrivManageMembers); err != nil {
			return nil, err
		}
	}
	if !MayEditMemberInstruments(actorIsCore, actorID == personID, targetIsShell) {
		return nil, ErrForbidden
	}

	clean := NormalizeInstruments(instruments)
	if _, err := tx.Exec(ctx, `UPDATE people SET instruments = $1 WHERE id = $2`, clean, personID); err != nil {
		return nil, err
	}
	return clean, tx.Commit(ctx)
}
