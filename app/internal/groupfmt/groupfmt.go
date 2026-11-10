// Package groupfmt is the members.json format written alongside a band
// or community's songbook export (see cmd/songexport) and read back by
// cmd/songimport — plain JSON, not a hand-rolled text format: an earlier
// version of this used a custom line-based format with its own parser,
// which had a real, silent bug (an RFC3339 timestamp's own colons broke
// the header/roster boundary heuristic — see this repo's git history on
// this file for the postmortem). JSON sidesteps that whole class of
// problem for free.
//
// A member is recorded by UserID, a reference into the export's own
// users.json (see cmd/songexport's user type) — not by embedding that
// person's username/display name/etc. redundantly in every group file
// they belong to. InvitedBy is the same kind of reference.
package groupfmt

import (
	"encoding/json"
	"os"
)

// Role is one community role definition — name plus the privileges it
// grants. Not used for a band file (bands have no custom roles).
type Role struct {
	Name       string   `json:"name"`
	Builtin    string   `json:"builtin,omitempty"`
	Privileges []string `json:"privileges,omitempty"`
}

// Member is one roster entry — RoleOrStatus is a community role name or
// a band's "core"/"guest" status, whichever this file is for.
type Member struct {
	UserID       int64  `json:"userId"`
	RoleOrStatus string `json:"roleOrStatus"`
	JoinedAt     string `json:"joinedAt"` // RFC3339
	LeftAt       string `json:"leftAt,omitempty"`
	InvitedBy    int64  `json:"invitedBy,omitempty"` // 0 if unknown/not recorded
}

// File is one members.json's full content — a band or a community,
// told apart by which of MusicType/DefaultJoinRole is set.
type File struct {
	Name            string   `json:"name"`
	MusicType       string   `json:"musicType,omitempty"`       // band only
	DefaultJoinRole string   `json:"defaultJoinRole,omitempty"` // community only
	Roles           []Role   `json:"roles,omitempty"`           // community only
	Members         []Member `json:"members"`
}

func Write(path string, f File) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func Read(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	var f File
	err = json.Unmarshal(data, &f)
	return f, err
}
