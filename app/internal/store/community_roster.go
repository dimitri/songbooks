package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Who sees a community's Members page (migration 0099).
const (
	// MembersVisibleToAdmins: the people whose role grants at least one
	// management privilege (HasAnyCommunityManagePrivilege), the default.
	MembersVisibleToAdmins = "admins"
	// MembersVisibleToMembers: every current member.
	MembersVisibleToMembers = "members"
)

// CommunityMembersVisibility is who sees communityID's Members page:
// MembersVisibleToAdmins or MembersVisibleToMembers.
func (st *Store) CommunityMembersVisibility(ctx context.Context, communityID int64) (string, error) {
	var v string
	err := st.db.QueryRow(ctx, `SELECT members_visibility FROM communities WHERE id = $1`, communityID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("no such community %d: %w", communityID, ErrNotFound)
	}
	return v, err
}

// SetCommunityMembersVisibility sets who sees communityID's Members page.
func (st *Store) SetCommunityMembersVisibility(ctx context.Context, communityID int64, visibility string) error {
	if visibility != MembersVisibleToAdmins && visibility != MembersVisibleToMembers {
		return fmt.Errorf("unknown members visibility %q", visibility)
	}
	tag, err := st.db.Exec(ctx, `UPDATE communities SET members_visibility = $1 WHERE id = $2`, visibility, communityID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such community %d: %w", communityID, ErrNotFound)
	}
	return nil
}

// CanSeeCommunityMembers reports whether personID may open communityID's
// Members page: a current member, and either the community shows the list
// to its members or their role grants a management privilege (the same
// set HasAnyCommunityManagePrivilege checks). A site admin's bypass is the
// HTTP layer's, as for every other community gate.
func (st *Store) CanSeeCommunityMembers(ctx context.Context, communityID, personID int64) (bool, error) {
	var ok bool
	err := st.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM community_members cm
			JOIN communities c ON c.id = cm.community_id
			WHERE cm.community_id = $1 AND cm.person_id = $2 AND upper_inf(cm.validity)
			  AND (c.members_visibility = 'members'
			       OR EXISTS(SELECT 1 FROM community_role_privileges crp
			                 WHERE crp.role_id = cm.role_id
			                   AND crp.privilege IN ($3, $4, $5, $6, $7)))
		)`, communityID, personID, PrivManageMembers, PrivManageEvents, PrivManageSubmissions, PrivManageSettings, PrivManageFiles).Scan(&ok)
	return ok, err
}

// CommunityRosterEntry is one person on a community's Members page: who
// they are as the other members see them (name, picture, instruments),
// their role and when they joined. IsShell marks someone added by name
// who hasn't made their account yet.
type CommunityRosterEntry struct {
	PersonID    int64
	Username    string
	DisplayName string
	PictureURL  string
	Instruments []string
	IsShell     bool
	Role        string // the role's name, a custom role's as its community wrote it
	RoleKey     string // "staff", "member" or "guest" for a built-in role, "" for a custom one
	JoinedAt    time.Time
	LeftAt      *time.Time
}

// IsCurrent reports whether this is a current membership (not a past one).
func (e CommunityRosterEntry) IsCurrent() bool { return e.LeftAt == nil }

// ListCommunityRoster lists communityID's members for its Members page:
// current members, staff first then by name; with includePast, then the
// past memberships, the latest to leave first. Unlike
// ListCommunityMembers it carries what the page shows of each person
// (picture, instruments) and nothing about how they joined.
func (st *Store) ListCommunityRoster(ctx context.Context, communityID int64, includePast bool) ([]CommunityRosterEntry, error) {
	q := `
		SELECT p.id, p.username, p.display_name, p.picture_url, coalesce(p.instruments, '{}'), p.is_shell,
		       cr.name, coalesce(cr.builtin_key, ''),
		       lower(m.validity), CASE WHEN upper_inf(m.validity) THEN NULL ELSE upper(m.validity) END
		FROM community_members m
		JOIN people p ON p.id = m.person_id
		JOIN community_roles cr ON cr.id = m.role_id
		WHERE m.community_id = $1`
	if !includePast {
		q += ` AND upper_inf(m.validity)`
	}
	q += `
		ORDER BY upper_inf(m.validity) DESC,
		         CASE WHEN upper_inf(m.validity) THEN cr.builtin_key IS DISTINCT FROM 'staff' END,
		         CASE WHEN upper_inf(m.validity) THEN lower(p.display_name) END,
		         upper(m.validity) DESC, m.id DESC`
	rows, err := st.db.Query(ctx, q, communityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommunityRosterEntry
	for rows.Next() {
		var e CommunityRosterEntry
		if err := rows.Scan(&e.PersonID, &e.Username, &e.DisplayName, &e.PictureURL, &e.Instruments, &e.IsShell,
			&e.Role, &e.RoleKey, &e.JoinedAt, &e.LeftAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
