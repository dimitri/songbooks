package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Songbook is one named, ordered collection of variant pointers, owned by
// exactly one person, one band, or one community ("Community" at the user-
// facing layer — see migration 0042's doc comment for why the communities/
// community_members tables keep their original name internally). Every
// owner always has at least one (IsDefault) — created automatically the
// first time GetOrCreateDefault is asked for one that doesn't exist yet.
type Songbook struct {
	ID            int64
	OwnerKind     string // "person", "band", or "community"
	OwnerPersonID int64
	OwnerBandID   int64
	OwnerCommunityID int64
	Name          string
	IsDefault     bool
	PlaylistURL   string
	CreatedAt     time.Time
	// RequiresGrant marks a community songbook as gated: a current
	// community member needs an explicit songbook_access_grants row (see
	// migration 0061) to view or edit it, on top of plain membership —
	// the paywall case, where a member may have bought access to one
	// songbook/course but not the community's others. Always false
	// (meaningless) for a person/band songbook.
	RequiresGrant bool
	// Public: everyone signed in opens it, and its owner's songs in it
	// are public (docs/ownership-model.md §2). Closed: a paid songbook
	// (migration 0113): never public, its songs never linked, copied or
	// made a version of anywhere else (SongRights.Closed).
	Public bool
	Closed bool
	// Kind is "songbook", "course" for one whose songs are in lessons
	// (migration 0073, lessons.go), or "student" for the songbook a coach
	// keeps for one of their students (migration 0079, coaching.go).
	Kind string
	// StudentPersonID is a student songbook's student.
	StudentPersonID int64
}

// IsStudent reports whether the songbook is a coach's for one student.
func (sb Songbook) IsStudent() bool { return sb.Kind == SongbookKindStudent }

// IsCourse reports whether the songbook is a course.
func (sb Songbook) IsCourse() bool { return sb.Kind == SongbookKindCourse }

// songbookOwnerCol maps an ownerKind to its songbooks column — the one
// piece of per-kind logic every function below needs, kept in one place
// rather than repeated as a 3-way if/else at each call site.
func songbookOwnerCol(ownerKind string) string {
	switch ownerKind {
	case "band":
		return "owner_band_id"
	case "community":
		return "owner_community_id"
	default:
		return "owner_person_id"
	}
}

// ListSongbooks lists every songbook ownerKind+ownerID has, default
// first then by name.
func (st *Store) ListSongbooks(ctx context.Context, ownerKind string, ownerID int64) ([]Songbook, error) {
	col := songbookOwnerCol(ownerKind)
	rows, err := st.db.Query(ctx, `
		SELECT id, owner_kind, coalesce(owner_person_id,0), coalesce(owner_band_id,0), coalesce(owner_community_id,0), name, is_default, playlist_url, created_at, requires_grant, public, closed, kind, coalesce(student_person_id,0)
		FROM songbooks WHERE owner_kind = $1 AND `+col+` = $2
		ORDER BY (NOT is_default), name`, ownerKind, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Songbook
	for rows.Next() {
		var sb Songbook
		if err := rows.Scan(&sb.ID, &sb.OwnerKind, &sb.OwnerPersonID, &sb.OwnerBandID, &sb.OwnerCommunityID, &sb.Name, &sb.IsDefault, &sb.PlaylistURL, &sb.CreatedAt, &sb.RequiresGrant, &sb.Public, &sb.Closed, &sb.Kind, &sb.StudentPersonID); err != nil {
			return nil, err
		}
		out = append(out, sb)
	}
	return out, rows.Err()
}

// GetSongbook loads one songbook by id.
func (st *Store) GetSongbook(ctx context.Context, songbookID int64) (*Songbook, error) {
	var sb Songbook
	err := st.db.QueryRow(ctx, `
		SELECT id, owner_kind, coalesce(owner_person_id,0), coalesce(owner_band_id,0), coalesce(owner_community_id,0), name, is_default, playlist_url, created_at, requires_grant, public, closed, kind, coalesce(student_person_id,0)
		FROM songbooks WHERE id = $1`, songbookID).
		Scan(&sb.ID, &sb.OwnerKind, &sb.OwnerPersonID, &sb.OwnerBandID, &sb.OwnerCommunityID, &sb.Name, &sb.IsDefault, &sb.PlaylistURL, &sb.CreatedAt, &sb.RequiresGrant, &sb.Public, &sb.Closed, &sb.Kind, &sb.StudentPersonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such songbook %d: %w", songbookID, ErrNotFound)
	}
	return &sb, err
}

// CanAccessSongbook reports whether personID may view/edit songbookID —
// a person songbook is owner-only, a band or community(community) songbook
// is any current member (same open-collaboration-within-the-group rule
// as everything else band/community-owned — a read-only community
// member still has full read-write access to the community's own
// songbooks, same as a band's guest; the read-only/write-only split the
// user asked for is about MEMBERSHIP MANAGEMENT, not song content) —
// UNLESS the songbook itself is gated (RequiresGrant, the paywall case:
// a member may belong to the community but not have paid for this
// particular songbook/course), in which case membership alone isn't
// enough: personID also needs an explicit songbook_access_grants row, or
// to hold a manage-type privilege (staff administer the grants, so they
// can always see what they're granting).
func (st *Store) CanAccessSongbook(ctx context.Context, personID, songbookID int64) (bool, error) {
	sb, err := st.GetSongbook(ctx, songbookID)
	if err != nil {
		return false, err
	}
	if sb.Public && personID > 0 {
		return true, nil // everyone signed in (docs/ownership-model.md §2)
	}
	return st.memberAccessesSongbook(ctx, personID, sb)
}

// memberAccessesSongbook is CanAccessSongbook as the songbook's owner's
// member: what changing it builds on (a public songbook opens for
// everyone, and stays its owner's to change).
func (st *Store) memberAccessesSongbook(ctx context.Context, personID int64, sb *Songbook) (bool, error) {
	songbookID := sb.ID
	switch sb.OwnerKind {
	case "band":
		return st.IsCurrentBandMember(ctx, sb.OwnerBandID, personID)
	case "community":
		isMember, err := isCurrentCommunityMember(ctx, st.db, sb.OwnerCommunityID, personID, "")
		if err != nil || !isMember {
			return false, err
		}
		if !sb.RequiresGrant {
			return true, nil
		}
		if isStaff, err := st.HasAnyCommunityManagePrivilege(ctx, sb.OwnerCommunityID, personID); err != nil {
			return false, err
		} else if isStaff {
			return true, nil
		}
		return st.HasSongbookAccessGrant(ctx, songbookID, personID)
	default:
		return sb.OwnerPersonID == personID, nil
	}
}

// HasSongbookAccessGrant reports whether personID holds an explicit
// grant on songbookID (see migration 0061) — the paywall check, only
// meaningful for a RequiresGrant songbook; irrelevant otherwise.
func (st *Store) HasSongbookAccessGrant(ctx context.Context, songbookID, personID int64) (bool, error) {
	var exists bool
	err := st.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM songbook_access_grants
		              WHERE songbook_id = $1 AND person_id = $2 AND (expires_at IS NULL OR expires_at > now()))`,
		songbookID, personID).Scan(&exists)
	return exists, err
}

// SongbookAccessGrant is one row of a gated songbook's grant list.
type SongbookAccessGrant struct {
	PersonID        int64
	DisplayName     string
	GrantedAt       time.Time
	GrantedByPersonID *int64
	// Source is where the grant came from: "manual" (staff, from the
	// access page), "purchase" or "bundle" (a payment, later); Reference
	// is the order it came with. ExpiresAt, when set, ends it.
	Source    string
	Reference string
	ExpiresAt *time.Time
}

// Expired reports whether the grant has ended.
func (g SongbookAccessGrant) Expired() bool {
	return g.ExpiresAt != nil && !g.ExpiresAt.After(time.Now())
}

// ListSongbookAccessGrants lists everyone currently granted access to
// songbookID, most recently granted first.
func (st *Store) ListSongbookAccessGrants(ctx context.Context, songbookID int64) ([]SongbookAccessGrant, error) {
	rows, err := st.db.Query(ctx, `
		SELECT g.person_id, p.display_name, g.granted_at, g.granted_by_person_id, g.source, g.reference, g.expires_at
		FROM songbook_access_grants g JOIN people p ON p.id = g.person_id
		WHERE g.songbook_id = $1 ORDER BY g.granted_at DESC`, songbookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SongbookAccessGrant
	for rows.Next() {
		var g SongbookAccessGrant
		if err := rows.Scan(&g.PersonID, &g.DisplayName, &g.GrantedAt, &g.GrantedByPersonID, &g.Source, &g.Reference, &g.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GrantSongbookAccess gives personID access to a gated songbookID by hand
// (staff) — a no-op if already granted, except that it renews an expired
// grant.
func (st *Store) GrantSongbookAccess(ctx context.Context, songbookID, personID, grantedByPersonID int64) error {
	return st.GrantSongbookAccessFrom(ctx, songbookID, personID, grantedByPersonID, "manual", "", nil)
}

// GrantSongbookAccessFrom is GrantSongbookAccess for a grant that comes
// from somewhere else: source "purchase" or "bundle" with the order's
// reference, until expiresAt (nil: for good). A grant someone already
// holds is kept, and extended to the later end of the two.
func (st *Store) GrantSongbookAccessFrom(ctx context.Context, songbookID, personID, grantedByPersonID int64, source, reference string, expiresAt *time.Time) error {
	var grantedBy *int64
	if grantedByPersonID != 0 {
		grantedBy = &grantedByPersonID
	}
	_, err := st.db.Exec(ctx, `
		INSERT INTO songbook_access_grants (songbook_id, person_id, granted_by_person_id, source, reference, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (songbook_id, person_id) DO UPDATE SET
			source = EXCLUDED.source, reference = EXCLUDED.reference,
			granted_by_person_id = EXCLUDED.granted_by_person_id, granted_at = now(),
			expires_at = EXCLUDED.expires_at
		WHERE songbook_access_grants.expires_at IS NOT NULL
		  AND (EXCLUDED.expires_at IS NULL OR EXCLUDED.expires_at > songbook_access_grants.expires_at)`,
		songbookID, personID, grantedBy, source, reference, expiresAt)
	return err
}

// RevokeSongbookAccess drops personID's grant on songbookID, if any.
func (st *Store) RevokeSongbookAccess(ctx context.Context, songbookID, personID int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM songbook_access_grants WHERE songbook_id = $1 AND person_id = $2`, songbookID, personID)
	return err
}

// SetSongbookRequiresGrant flips songbookID's gate on or off.
func (st *Store) SetSongbookRequiresGrant(ctx context.Context, songbookID int64, requiresGrant bool) error {
	_, err := st.db.Exec(ctx, `UPDATE songbooks SET requires_grant = $1 WHERE id = $2`, requiresGrant, songbookID)
	return err
}

// SetSongbookClosed closes songbookID (a paid songbook: its songs go
// nowhere else, SongRights.Closed) or opens it again. A public songbook,
// or one holding its owner's public songs, isn't closed: ErrConflict (make
// them private first).
func (st *Store) SetSongbookClosed(ctx context.Context, songbookID int64, closed bool) error {
	if closed {
		var public bool
		if err := st.db.QueryRow(ctx, `
			SELECT sb.public OR EXISTS (
			  SELECT 1 FROM songbook_entries se JOIN songs s ON s.id = se.variant_id
			  WHERE se.songbook_id = sb.id AND s.public
			    AND (s.owner_person_id = sb.owner_person_id OR s.owner_band_id = sb.owner_band_id
			         OR s.owner_community_id = sb.owner_community_id))
			FROM songbooks sb WHERE sb.id = $1`, songbookID).Scan(&public); errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("no such songbook %d: %w", songbookID, ErrNotFound)
		} else if err != nil {
			return err
		}
		if public {
			return fmt.Errorf("songbook %d is public, or holds public songs: %w", songbookID, ErrConflict)
		}
	}
	_, err := st.db.Exec(ctx, `UPDATE songbooks SET closed = $1 WHERE id = $2`, closed, songbookID)
	return err
}

// CanEditSongbook reports whether personID may change songbookID itself
// or its contents (rename, delete, reorder, add/remove/create a song) —
// identical to CanAccessSongbook for a person or band songbook (no
// read-only tier there), but gated on the edit_repertoire privilege for
// a community songbook: a member whose role doesn't grant it (e.g. the
// built-in "guest" role) can open and play every song in it (see
// CanAccessSongbook) but not edit the songbook or add/remove entries.
func (st *Store) CanEditSongbook(ctx context.Context, personID, songbookID int64) (bool, error) {
	sb, err := st.GetSongbook(ctx, songbookID)
	if err != nil {
		return false, err
	}
	if sb.OwnerKind == "community" {
		// Editing a sold songbook needs both the privilege and the
		// songbook itself: edit_repertoire alone doesn't open it.
		if ok, err := st.HasCommunityPrivilege(ctx, sb.OwnerCommunityID, personID, PrivEditRepertoire); err != nil || !ok {
			return false, err
		}
	}
	if sb.OwnerKind == "band" {
		return st.HasBandPrivilege(ctx, sb.OwnerBandID, personID, BandPrivEditSongbook)
	}
	return st.memberAccessesSongbook(ctx, personID, sb)
}

// GetDefaultSongbook resolves ownerKind+ownerID's default songbook id —
// every person/band/community gets one automatically, so this never needs
// to create one on demand in practice, but falls back to doing so
// defensively (e.g. a brand-new owner created after this phase shipped)
// rather than erroring.
func (st *Store) GetDefaultSongbook(ctx context.Context, ownerKind string, ownerID int64) (int64, error) {
	col := songbookOwnerCol(ownerKind)
	defaultName := "My songbook"
	switch ownerKind {
	case "band":
		defaultName = "Songbook"
	case "community":
		defaultName = "Community songbook"
	}
	var id int64
	err := st.db.QueryRow(ctx, `
		SELECT id FROM songbooks WHERE owner_kind = $1 AND `+col+` = $2 AND is_default`, ownerKind, ownerID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return st.CreateSongbook(ctx, ownerKind, ownerID, defaultName, true)
	}
	return id, err
}

// CreateSongbook makes a new, empty songbook for ownerKind+ownerID.
func (st *Store) CreateSongbook(ctx context.Context, ownerKind string, ownerID int64, name string, isDefault bool) (int64, error) {
	if !validName(name) {
		return 0, fmt.Errorf("invalid songbook name: %q", name)
	}
	var personCol, bandCol, communityCol any
	switch ownerKind {
	case "band":
		bandCol = ownerID
	case "community":
		communityCol = ownerID
	default:
		personCol = ownerID
	}
	var id int64
	err := st.db.QueryRow(ctx, `
		INSERT INTO songbooks (owner_kind, owner_person_id, owner_band_id, owner_community_id, name, is_default)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		ownerKind, personCol, bandCol, communityCol, name, isDefault).Scan(&id)
	return id, err
}

// RenameSongbook renames songbookID — any songbook, default or not, can
// be renamed (only deletion is restricted to non-default).
func (st *Store) RenameSongbook(ctx context.Context, songbookID int64, name string) error {
	if !validName(name) {
		return fmt.Errorf("invalid songbook name: %q", name)
	}
	tag, err := st.db.Exec(ctx, `UPDATE songbooks SET name = $1 WHERE id = $2`, name, songbookID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such songbook %d: %w", songbookID, ErrNotFound)
	}
	return nil
}

// DeleteSongbook removes a non-default songbook (its entries, i.e. the
// pointers — never the underlying variants) — "every profile/band always
// has at least one songbook" means the default one can never be deleted,
// enforced here rather than left to the caller.
func (st *Store) DeleteSongbook(ctx context.Context, songbookID int64) error {
	tag, err := st.db.Exec(ctx, `DELETE FROM songbooks WHERE id = $1 AND NOT is_default`, songbookID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such non-default songbook %d: %w", songbookID, ErrNotFound)
	}
	return nil
}

// SongbookEntry is one variant pointer on a songbook.
type SongbookEntry struct {
	CatalogVariant
	AddedAt time.Time
}

// AddToSongbook points songbookID at variantID — "add this (global, or
// anyone's) variant to this songbook," by reference, not by copy. A
// no-op if it's already there.
func (st *Store) AddToSongbook(ctx context.Context, songbookID, variantID, addedByPersonID int64) error {
	var by *int64
	if addedByPersonID != 0 {
		by = &addedByPersonID
	}
	_, err := st.db.Exec(ctx, `
		INSERT INTO songbook_entries (songbook_id, variant_id, added_by_person_id) VALUES ($1, $2, $3)
		ON CONFLICT (songbook_id, variant_id) DO NOTHING`, songbookID, variantID, by)
	return err
}

// RemoveFromSongbook removes songbookID's pointer to variantID, if any.
func (st *Store) RemoveFromSongbook(ctx context.Context, songbookID, variantID int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM songbook_entries WHERE songbook_id = $1 AND variant_id = $2`, songbookID, variantID)
	return err
}

// ListSongbookEntries lists songbookID's entries, most recently added
// first.
func (st *Store) ListSongbookEntries(ctx context.Context, songbookID int64) ([]SongbookEntry, error) {
	rows, err := st.db.Query(ctx, `
		SELECT s.id, s.work_id, s.title, s.artist, `+songKindSQL("s")+`, s.owner_person_id, s.owner_band_id, s.forked_from_id, se.added_at,
		       s.public, s.version_name
		FROM songbook_entries se
		JOIN songs s ON s.id = se.variant_id
		WHERE se.songbook_id = $1
		ORDER BY se.added_at DESC`, songbookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SongbookEntry
	for rows.Next() {
		var e SongbookEntry
		if err := rows.Scan(&e.VariantID, &e.WorkID, &e.Title, &e.Artist, &e.Visibility, &e.OwnerID, &e.OwnerBandID, &e.ForkedFromID, &e.AddedAt,
			&e.Public, &e.VersionName); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
