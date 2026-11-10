// Package membersfmt is the plain-text members.txt format written
// alongside a band or community's songbook export (see cmd/songexport)
// and read back by cmd/songimport — readable and git-diffable on its
// own, no database required to inspect who belongs to a band/community,
// in what role, and (for a community) exactly what each role can do.
//
// Format, by example (a community; a band's is the same minus the
// "role " lines and with a plain status instead of a role name):
//
//	name: Florent Passamonti
//	default_join_role: member
//
//	role staff: edit_repertoire, manage_members, manage_events, manage_submissions, manage_settings
//	role member: edit_repertoire
//	role guest:
//
//	tapoueh   staff   joined=2026-11-06T14:01:41Z
//	florent   staff   joined=2026-11-06T14:05:00Z invited_by=tapoueh
//	alice     member  joined=2026-11-10T09:00:00Z left=2026-12-01T00:00:00Z
//
// A band's roster line uses its member's status ("core" or "guest") in
// the same column a community roster uses its role name — band
// membership has no custom roles/privileges, just that one flag — so
// the file keeps a consistent 3-column roster shape (username, role-or-
// status, attributes) regardless of which kind of group it's for.
//
// Lines are whitespace-separated; a line starting with "role " defines
// one community role (name before the ":", comma-separated privileges
// after — empty means no privileges); any other non-blank, non-"#"-
// comment, non-"key: value" header line is one roster entry: username,
// then role/status, then zero or more "key=value" attributes
// (joined=..., left=..., invited_by=...). Header lines ("key: value",
// no leading "role ") must come before the first blank line; roles and
// roster entries may follow in either order, distinguished by the
// leading "role " keyword.
package membersfmt

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Role is one community role definition — name plus the privileges it
// grants. Not used for a band file (bands have no custom roles).
type Role struct {
	Name       string
	Builtin    string // "" for a custom (non-builtin) role
	Privileges []string
}

// Member is one roster line — RoleOrStatus is a community role name or
// a band's "core"/"guest" status, whichever this file is for.
type Member struct {
	Username     string
	RoleOrStatus string
	JoinedAt     string // RFC3339; "" only possible for malformed input
	LeftAt       string // "" if still a current member
	InvitedBy    string // username, "" if unknown/not recorded
}

// File is one members.txt's full parsed content.
type File struct {
	Header  map[string]string // e.g. {"name": "...", "music_type": "..."} or {"name": "...", "default_join_role": "..."}
	Roles   []Role            // nil for a band file
	Members []Member
}

// Write renders f in the format this package's own doc comment
// describes and saves it to path.
func Write(path string, f File) error {
	var b strings.Builder
	keys := make([]string, 0, len(f.Header))
	for k := range f.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// "name" always first, for readability, even though key order
	// inside a map is otherwise arbitrary.
	sort.SliceStable(keys, func(i, j int) bool { return keys[i] == "name" && keys[j] != "name" })
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\n", k, f.Header[k])
	}
	if len(f.Roles) > 0 {
		b.WriteByte('\n')
		for _, r := range f.Roles {
			fmt.Fprintf(&b, "role %s: %s\n", r.Name, strings.Join(r.Privileges, ", "))
		}
	}
	b.WriteByte('\n')
	for _, m := range f.Members {
		fmt.Fprintf(&b, "%s\t%s\tjoined=%s", m.Username, m.RoleOrStatus, m.JoinedAt)
		if m.LeftAt != "" {
			fmt.Fprintf(&b, " left=%s", m.LeftAt)
		}
		if m.InvitedBy != "" {
			fmt.Fprintf(&b, " invited_by=%s", m.InvitedBy)
		}
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// Read parses a members.txt file written by Write.
//
// Header lines end at the FIRST blank line — not inferred from a line's
// shape. An earlier version tried to recognize a roster line by "does it
// contain a space before any colon," which broke on every real file:
// an RFC3339 timestamp (joined=2026-11-02T12:07:28Z) has colons of its
// own, and the roster's own field separator is a tab, not a space, so
// "contains a space" never fired — every roster line was silently
// swallowed into the header map instead, and Members came back empty
// for every band/community file this package ever wrote. The format's
// own blank-line structure (header \n\n role/roster lines) already
// says unambiguously where the header ends; this now actually uses it.
func Read(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	f := File{Header: map[string]string{}}
	inHeader := true
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			inHeader = false
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if inHeader {
			key, val, ok := strings.Cut(trimmed, ":")
			if !ok {
				return File{}, fmt.Errorf("malformed header line: %q", line)
			}
			f.Header[strings.TrimSpace(key)] = strings.TrimSpace(val)
			continue
		}
		if strings.HasPrefix(trimmed, "role ") {
			rest := strings.TrimPrefix(trimmed, "role ")
			name, privs, ok := strings.Cut(rest, ":")
			if !ok {
				return File{}, fmt.Errorf("malformed role line: %q", line)
			}
			var privileges []string
			for _, p := range strings.Split(privs, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					privileges = append(privileges, p)
				}
			}
			f.Roles = append(f.Roles, Role{Name: strings.TrimSpace(name), Privileges: privileges})
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			return File{}, fmt.Errorf("malformed roster line: %q", line)
		}
		m := Member{Username: fields[0], RoleOrStatus: fields[1]}
		for _, kv := range fields[2:] {
			key, val, ok := strings.Cut(kv, "=")
			if !ok {
				continue
			}
			switch key {
			case "joined":
				m.JoinedAt = val
			case "left":
				m.LeftAt = val
			case "invited_by":
				m.InvitedBy = val
			}
		}
		f.Members = append(f.Members, m)
	}
	return f, nil
}
