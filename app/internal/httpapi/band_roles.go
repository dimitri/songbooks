package httpapi

import (
	"net/http"

	"app/internal/store"
)

// The band's roles editor (band_roles.html, the Roles section of the
// band's Settings, for whoever manages roles; docs/band-roles.md): the
// band's roles as columns, the privileges as rows grouped by what
// they're about, each role's count of members. Who holds which role is
// given on Members.

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
		{store.BandPrivRenameArchive, "Rename and archive the band", "Archived, it’s read only and out of the menus; unarchive it from Settings."},
		{store.BandPrivEditBand, "Say what it plays", "Covers or original music; its tagline, website and the language of its pages."},
		{store.BandPrivEditLook, "Change its look", "Logo, card picture, colours and fonts."},
	}},
}

// bandRolesData is the roles editor's data.
type bandRolesData struct {
	BandID  int64
	Roles   []store.BandRole
	Holders map[int64]int // current members per role id
	Groups  []bandPrivilegeGroup
}

// RoleCount is how many current members hold roleID.
func (d bandRolesData) RoleCount(roleID int64) int { return d.Holders[roleID] }

// ColSpan is the table's width: the privilege column, one per role and
// "+ New role".
func (d bandRolesData) ColSpan() int { return len(d.Roles) + 2 }

// bandRolesEditor loads the roles editor's data.
func (s *Server) bandRolesEditor(r *http.Request, bandID int64) (*bandRolesData, error) {
	ctx := r.Context()
	roles, err := s.store.ListBandRoles(ctx, bandID)
	if err != nil {
		return nil, err
	}
	holders, err := s.store.BandRoleHolders(ctx, bandID)
	if err != nil {
		return nil, err
	}
	return &bandRolesData{BandID: bandID, Roles: roles, Holders: holders, Groups: bandPrivilegeGroups}, nil
}
