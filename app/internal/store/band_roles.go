package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Band privileges (docs/band-roles.md): the fixed catalog a band role's
// privileges are drawn from, the community's model (roles.go) for bands.
// Seeing the band — its pages, songbook, set lists, calendar, recordings,
// exports — is every current member's; these are what a member may
// change. Adding a gated action means a constant here, a check at its
// call site (HasBandPrivilege, or requireBandPrivilege on its route) and
// its default in BandRoleDefaults and seed_band_roles (its latest
// version: migration 0137).
const (
	BandPrivEditSongbook     = "edit_songbook"     // add, remove, arrange songs; make and edit the band's songs and arrangements; the band's songbooks
	BandPrivPublish          = "publish"           // make the band's songs, songbooks and recordings public
	BandPrivManageSetlists   = "manage_setlists"   // create, edit, delete set lists, their gigs and click track notes
	BandPrivManageEvents     = "manage_events"     // create and change the band's events (rehearsals, gigs), sign the band up for one
	BandPrivManageRecordings = "manage_recordings" // the recordings' source, sync, names and fixes, a song's recording
	BandPrivPropose          = "propose"           // propose songs, add to Discover and Influences
	BandPrivVote             = "vote"              // vote on the proposals (counted in their tallies)
	BandPrivDecideProposals  = "decide_proposals"  // add a proposed song or turn it down
	BandPrivCurateListen     = "curate_listen"     // remove anyone's Discover and Influences entries (one's own: always)
	BandPrivManageMembers    = "manage_members"    // invite people (every way: links, QR, roster), edit others' instruments
	BandPrivRemoveMembers    = "remove_members"    // remove someone from the band
	BandPrivManageRoles      = "manage_roles"      // give members a role, edit the band's roles
	BandPrivRenameArchive    = "rename_archive"    // rename the band, archive and unarchive it (still granted while archived)
	BandPrivEditBand         = "edit_band"         // say what the band plays (covers or original music), its tagline and website, the language of its pages
	BandPrivEditLook         = "edit_look"         // the band's look: logo, card picture, colours and fonts
)

// AllBandPrivileges is the catalog, in the roles editor's order.
var AllBandPrivileges = []string{
	BandPrivEditSongbook, BandPrivPublish, BandPrivManageSetlists, BandPrivManageEvents, BandPrivManageRecordings,
	BandPrivPropose, BandPrivVote, BandPrivDecideProposals, BandPrivCurateListen,
	BandPrivManageMembers, BandPrivRemoveMembers, BandPrivManageRoles,
	BandPrivRenameArchive, BandPrivEditBand, BandPrivEditLook,
}

// BandRoleDefaults is what the two built-in roles grant in a new band:
// the rights core and guest members had before roles (the inventory in
// docs/band-roles.md), but renaming and archiving, core's only,
// deleting, site admins' only, editing the band's songs and removing
// members, no longer a guest's (migration 0132), and removing others'
// Discover and Influences entries, new and core's; saying what the band
// plays, no longer a guest's either (migration 0137: a set-once choice,
// and the only one that gave every guest the band's ⚙). A guest doesn't
// vote (vote: counted), but rates proposals all the same, listed apart.
// seed_band_roles (migration 0137) seeds the same
// (TestBandRoleDefaultsMatchSeed).
var BandRoleDefaults = map[string][]string{
	"core": AllBandPrivileges,
	"guest": {
		BandPrivManageSetlists, BandPrivManageEvents, BandPrivManageRecordings,
		BandPrivPropose, BandPrivDecideProposals,
	},
}

var bandRoles = roleScope{
	roles: "band_roles", privs: "band_role_privileges", members: "band_members",
	groupCol: "band_id", group: "band", catalog: AllBandPrivileges,
	order:    "CASE r.builtin_key WHEN 'core' THEN 0 WHEN 'guest' THEN 1 ELSE 2 END, r.name",
	keeper:   BandPrivManageRoles,
	fallback: "core",
}

// BandRole is a band's Role: the built-ins "core" and "guest", and the
// band's own.
type BandRole = Role

// HasBandPrivilege reports whether personID's current role in bandID
// grants priv — the one rule every band action checks (site admins are
// let through by the HTTP layer, as before: requireBandPrivilege,
// Server.bandPrivilege). Not a current member: false.
func (st *Store) HasBandPrivilege(ctx context.Context, bandID, personID int64, priv string) (bool, error) {
	return hasBandPrivilege(ctx, st.db, bandID, personID, priv)
}

// hasBandPrivilege is HasBandPrivilege inside an open transaction (see
// isCurrentBandMember).
// It is has_band_privilege (migration 0131): the role's privilege, and
// nothing but rename_archive while the band is archived.
func hasBandPrivilege(ctx context.Context, q dbtx, bandID, personID int64, priv string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT has_band_privilege($1, $2, $3)`, bandID, personID, priv).Scan(&ok)
	return ok, err
}

// BandPrivs is what one member's role grants, for a page to ask
// several questions at once ({{if .Privs.Can "edit_look"}}).
type BandPrivs map[string]bool

// Can reports whether the role grants priv.
func (p BandPrivs) Can(priv string) bool { return p[priv] }

// AllBandPrivs is every privilege: a site admin's (only rename_archive
// while the band is archived).
func AllBandPrivs(archived bool) BandPrivs {
	out := BandPrivs{}
	for _, p := range AllBandPrivileges {
		if !archived || p == BandPrivRenameArchive {
			out[p] = true
		}
	}
	return out
}

// BandPrivileges is what personID's current role in bandID grants
// (empty: not a current member).
func (st *Store) BandPrivileges(ctx context.Context, bandID, personID int64) (BandPrivs, error) {
	list, err := bandRoles.privilegesOf(ctx, st.db, bandID, personID)
	if err != nil {
		return nil, err
	}
	archived, err := st.BandArchived(ctx, bandID)
	if err != nil {
		return nil, err
	}
	out := BandPrivs{}
	for _, p := range list {
		if !archived || p == BandPrivRenameArchive {
			out[p] = true
		}
	}
	return out, nil
}

// ListBandRoles lists bandID's roles: core, guest, then its own by name.
func (st *Store) ListBandRoles(ctx context.Context, bandID int64) ([]BandRole, error) {
	return bandRoles.listRoles(ctx, st.db, bandID)
}

// BandRoleHolders counts bandID's current members per role id.
func (st *Store) BandRoleHolders(ctx context.Context, bandID int64) (map[int64]int, error) {
	rows, err := st.db.Query(ctx, `
		SELECT role_id, count(*) FROM band_members WHERE band_id = $1 AND upper_inf(validity) GROUP BY role_id`, bandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// SetBandMemberRole gives personID's current membership in bandID the
// role roleID (one of bandID's). Its status follows (guest for the
// guest role, core for any other: migration 0130's trigger). Refused
// (ErrLastKeeper) if it would leave no current member able to manage
// the roles. actorPersonID is the revision's actor.
func (st *Store) SetBandMemberRole(ctx context.Context, bandID, personID, roleID, actorPersonID int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := bandRoles.getRole(ctx, tx, bandID, roleID); err != nil {
		return err
	}
	before, err := bandRoles.keepers(ctx, tx, bandID)
	if err != nil {
		return err
	}
	row, err := lockCurrentMembership(ctx, tx, bandID, personID)
	if err != nil {
		return err
	}
	actor := &actorPersonID
	if err := st.RecordRevision(ctx, tx, "band_member", fmt.Sprint(row.ID), actor, row.snapshot()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE band_members SET role_id = $1 WHERE id = $2`, roleID, row.ID); err != nil {
		return err
	}
	if err := bandRoles.keepKeeper(ctx, tx, bandID, before); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// BandArchived reports whether bandID is archived (read only).
func (st *Store) BandArchived(ctx context.Context, bandID int64) (bool, error) {
	var archived bool
	err := st.db.QueryRow(ctx, `SELECT coalesce((SELECT archived_at IS NOT NULL FROM bands WHERE id = $1), false)`, bandID).Scan(&archived)
	return archived, err
}

// SetBandArchived archives bandID (read only for everyone, out of the
// menus and the home page, still its members' from "All my groups") or
// unarchives it. Archiving closes every invite into the band still open
// — its roster links, its open links (QR codes) and its one-off links —
// so no one joins an archived band; unarchiving reopens none of them (new
// ones can be made). A placeholder's personal link stays: it finishes an
// account already in the band, it lets no one new in.
func (st *Store) SetBandArchived(ctx context.Context, bandID int64, archived bool) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE bands SET archived_at = CASE WHEN $2 THEN coalesce(archived_at, now()) END WHERE id = $1`, bandID, archived)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such band %d: %w", bandID, ErrNotFound)
	}
	if archived {
		if _, err := tx.Exec(ctx, `
			UPDATE group_invites SET closed_at = now() WHERE target_kind = 'band' AND band_id = $1 AND closed_at IS NULL`, bandID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE invites SET expires_at = now() WHERE band_id = $1 AND expires_at > now()`, bandID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ResolveBandInviteRole is the role an invite into bandID gives (every
// way of inviting: one-off, personal and roster links, the open link, an
// account added by name): roleID, one of the band's roles, a band's own
// included; else, roleID 0, the built-in role status names — "core" or
// "guest", what invites gave before roles. ErrInvalid for a role of
// another band or another status.
func (st *Store) ResolveBandInviteRole(ctx context.Context, bandID, roleID int64, status string) (int64, error) {
	if roleID != 0 {
		if _, err := bandInviteStatus(ctx, st.db, bandID, roleID); err != nil {
			return 0, err
		}
		return roleID, nil
	}
	if status != "core" && status != "guest" {
		return 0, fmt.Errorf("invalid band member status %q: %w", status, ErrInvalid)
	}
	return builtinBandRoleID(ctx, st.db, bandID, status)
}

// builtinBandRoleID is bandID's built-in role key ("core" or "guest").
func builtinBandRoleID(ctx context.Context, q dbtx, bandID int64, key string) (int64, error) {
	var id int64
	err := q.QueryRow(ctx, `SELECT id FROM band_roles WHERE band_id = $1 AND builtin_key = $2`, bandID, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("no band %d: %w", bandID, ErrNotFound)
	}
	return id, err
}

// bandInviteStatus is the status an invite giving roleID keeps beside
// it: the built-in role's own ("core", "guest"), "guest" for a band's
// own role — what the invite gives if that role is deleted before it's
// used (its band_role_id is then NULL: migration 0132). ErrInvalid when
// roleID isn't one of bandID's roles.
func bandInviteStatus(ctx context.Context, q dbtx, bandID, roleID int64) (string, error) {
	var key string
	err := q.QueryRow(ctx, `SELECT coalesce(builtin_key, '') FROM band_roles WHERE id = $1 AND band_id = $2`, roleID, bandID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("role %d isn't one of band %d's: %w", roleID, bandID, ErrInvalid)
	}
	if err != nil {
		return "", err
	}
	if key == "core" {
		return "core", nil
	}
	return "guest", nil
}
