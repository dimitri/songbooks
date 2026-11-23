package store

import (
	"context"
	"fmt"
)

// Band privileges (docs/band-roles.md): the fixed catalog a band role's
// privileges are drawn from, the community's model (roles.go) for bands.
// Seeing the band — its pages, songbook, set lists, calendar, recordings,
// exports — is every current member's; these are what a member may
// change. Adding a gated action means a constant here, a check at its
// call site (HasBandPrivilege, or requireBandPrivilege on its route) and
// its default in BandRoleDefaults and migration 0130's seed_band_roles.
const (
	BandPrivEditSongbook     = "edit_songbook"     // add, remove, arrange songs; make and edit the band's songs and arrangements; the band's songbooks
	BandPrivPublish          = "publish"           // make the band's songs, songbooks and recordings public
	BandPrivManageSetlists   = "manage_setlists"   // create, edit, delete set lists, their gigs and click track notes
	BandPrivManageEvents     = "manage_events"     // create and change the band's events (rehearsals, gigs), sign the band up for one
	BandPrivManageRecordings = "manage_recordings" // the recordings' source, sync, names and fixes, a song's recording
	BandPrivPropose          = "propose"           // propose songs, add to Discover and Influences
	BandPrivVote             = "vote"              // vote on the proposals (counted in their tallies)
	BandPrivDecideProposals  = "decide_proposals"  // add a proposed song or turn it down
	BandPrivManageMembers    = "manage_members"    // invite people (every way: links, QR, roster), edit others' instruments
	BandPrivRemoveMembers    = "remove_members"    // remove someone from the band
	BandPrivManageRoles      = "manage_roles"      // give members a role, edit the band's roles
	BandPrivEditBand         = "edit_band"         // rename the band, say what it plays
	BandPrivEditLook         = "edit_look"         // the band's look, logo, card picture and language
	BandPrivDeleteBand       = "delete_band"       // delete the band
)

// AllBandPrivileges is the catalog, in the roles editor's order.
var AllBandPrivileges = []string{
	BandPrivEditSongbook, BandPrivPublish, BandPrivManageSetlists, BandPrivManageEvents, BandPrivManageRecordings,
	BandPrivPropose, BandPrivVote, BandPrivDecideProposals,
	BandPrivManageMembers, BandPrivRemoveMembers, BandPrivManageRoles,
	BandPrivEditBand, BandPrivEditLook, BandPrivDeleteBand,
}

// BandRoleDefaults is what the two built-in roles grant in a new band:
// the rights core and guest members had before roles (the inventory in
// docs/band-roles.md). Migration 0130's seed_band_roles seeds the same
// (TestBandRoleDefaultsMatchSeed).
var BandRoleDefaults = map[string][]string{
	"core": AllBandPrivileges,
	"guest": {
		BandPrivEditSongbook, BandPrivManageSetlists, BandPrivManageEvents, BandPrivManageRecordings,
		BandPrivPropose, BandPrivDecideProposals,
		BandPrivRemoveMembers,
		BandPrivEditBand, BandPrivDeleteBand,
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
	return bandRoles.hasPrivilege(ctx, st.db, bandID, personID, priv)
}

// hasBandPrivilege is HasBandPrivilege inside an open transaction (see
// isCurrentBandMember).
func hasBandPrivilege(ctx context.Context, q dbtx, bandID, personID int64, priv string) (bool, error) {
	return bandRoles.hasPrivilege(ctx, q, bandID, personID, priv)
}

// BandPrivs is what one member's role grants, for a page to ask
// several questions at once ({{if .Privs.Can "edit_look"}}).
type BandPrivs map[string]bool

// Can reports whether the role grants priv.
func (p BandPrivs) Can(priv string) bool { return p[priv] }

// AllBandPrivs is every privilege: a site admin's.
func AllBandPrivs() BandPrivs {
	out := BandPrivs{}
	for _, p := range AllBandPrivileges {
		out[p] = true
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
	out := BandPrivs{}
	for _, p := range list {
		out[p] = true
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
