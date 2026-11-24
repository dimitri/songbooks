package httpapi

import (
	"net/http"
	"sort"
	"strings"

	"app/internal/store"
)

// The band's roles editor (band_roles.html, on the band's Settings;
// docs/band-roles.md): the band's roles as columns, the privileges as
// rows grouped by what they're about, and who holds which role.

// bandPrivilegeInfo is one privilege as the editor shows it (Label and
// Hint are English, translated in the template).
type bandPrivilegeInfo struct {
	Key, Label, Hint string
}

// bandPrivilegeGroup is a heading of the editor's rows.
type bandPrivilegeGroup struct {
	Label      string
	Privileges []bandPrivilegeInfo
}

// bandPrivilegeGroups is store.AllBandPrivileges, in the editor's words
// and order.
var bandPrivilegeGroups = []bandPrivilegeGroup{
	{"Songs", []bandPrivilegeInfo{
		{store.BandPrivEditSongbook, "Edit the songbook", "Add, remove and arrange songs; make and edit the band’s songs and arrangements."},
		{store.BandPrivPublish, "Make things public", "The band’s songs, songbooks and recordings."},
		{store.BandPrivManageSetlists, "Manage set lists", "Create, change and delete set lists, their gigs and click track notes."},
		{store.BandPrivManageRecordings, "Manage recordings", "Where they come from, their names and fixes, a song’s recording."},
		{store.BandPrivManageEvents, "Manage the calendar", "Rehearsals, gigs and other events; sign the band up for one."},
	}},
	{"Proposals", []bandPrivilegeInfo{
		{store.BandPrivPropose, "Propose songs", "Propose a song, add to Discover and Influences."},
		{store.BandPrivVote, "Vote", "Their votes count: in the average, the counts and who hasn’t voted. Without it, a member’s votes are listed apart, with the guests’."},
		{store.BandPrivDecideProposals, "Decide", "Add a proposed song to the songbook, or turn it down."},
		{store.BandPrivCurateListen, "Tidy Discover and Influences", "Remove anyone’s songs from them (whoever added one can always remove it)."},
	}},
	{"Members", []bandPrivilegeInfo{
		{store.BandPrivManageMembers, "Invite people", "Links, QR codes and the roster; change others’ instruments."},
		{store.BandPrivRemoveMembers, "Remove members", "Take someone off the band."},
		{store.BandPrivManageRoles, "Manage roles", "Give members a role, change what each role allows."},
	}},
	{"The band", []bandPrivilegeInfo{
		{store.BandPrivRenameArchive, "Rename and archive the band", "Archived, it’s read only and out of the menus; unarchive it here."},
		{store.BandPrivEditBand, "Say what it plays", "Covers or original music."},
		{store.BandPrivEditLook, "Change its look", "Colours, logo, card picture and language."},
	}},
}

// bandRolesData is the roles editor's data.
type bandRolesData struct {
	BandID  int64
	Roles   []store.BandRole
	Holders map[int64]int // current members per role id
	Groups  []bandPrivilegeGroup
	Members []store.BandMember // current ones, for "who has which role"
	Privs   store.BandPrivs    // the viewer's
	CanEdit bool               // manage_roles
}

// RoleCount is how many current members hold roleID.
func (d bandRolesData) RoleCount(roleID int64) int { return d.Holders[roleID] }

// ColSpan is the table's width: the privilege column and one per role.
func (d bandRolesData) ColSpan() int { return len(d.Roles) + 1 }

// RoleOf is the role roleID (zero if not the band's).
func (d bandRolesData) RoleOf(roleID int64) store.BandRole {
	for _, r := range d.Roles {
		if r.ID == roleID {
			return r
		}
	}
	return store.BandRole{}
}

// Custom is the band's own roles (not the built-ins).
func (d bandRolesData) Custom() []store.BandRole {
	var out []store.BandRole
	for _, r := range d.Roles {
		if !r.IsBuiltin() {
			out = append(out, r)
		}
	}
	return out
}

// bandRolesEditor loads the roles editor's data for the signed-in
// person.
func (s *Server) bandRolesEditor(r *http.Request, bandID int64) (*bandRolesData, error) {
	ctx := r.Context()
	privs, err := s.bandPrivs(r, bandID)
	if err != nil {
		return nil, err
	}
	roles, err := s.store.ListBandRoles(ctx, bandID)
	if err != nil {
		return nil, err
	}
	holders, err := s.store.BandRoleHolders(ctx, bandID)
	if err != nil {
		return nil, err
	}
	members, err := s.store.ListBandMembers(ctx, bandID, false)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(members, func(i, j int) bool {
		return strings.ToLower(members[i].DisplayName) < strings.ToLower(members[j].DisplayName)
	})
	return &bandRolesData{
		BandID: bandID, Roles: roles, Holders: holders, Groups: bandPrivilegeGroups,
		Members: members, Privs: privs, CanEdit: privs.Can(store.BandPrivManageRoles),
	}, nil
}
