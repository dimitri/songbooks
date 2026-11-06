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
}

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
		SELECT id, owner_kind, coalesce(owner_person_id,0), coalesce(owner_band_id,0), coalesce(owner_community_id,0), name, is_default, playlist_url, created_at
		FROM songbooks WHERE owner_kind = $1 AND `+col+` = $2
		ORDER BY (NOT is_default), name`, ownerKind, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Songbook
	for rows.Next() {
		var sb Songbook
		if err := rows.Scan(&sb.ID, &sb.OwnerKind, &sb.OwnerPersonID, &sb.OwnerBandID, &sb.OwnerCommunityID, &sb.Name, &sb.IsDefault, &sb.PlaylistURL, &sb.CreatedAt); err != nil {
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
		SELECT id, owner_kind, coalesce(owner_person_id,0), coalesce(owner_band_id,0), coalesce(owner_community_id,0), name, is_default, playlist_url, created_at
		FROM songbooks WHERE id = $1`, songbookID).
		Scan(&sb.ID, &sb.OwnerKind, &sb.OwnerPersonID, &sb.OwnerBandID, &sb.OwnerCommunityID, &sb.Name, &sb.IsDefault, &sb.PlaylistURL, &sb.CreatedAt)
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
// user asked for is about MEMBERSHIP MANAGEMENT, not song content).
func (st *Store) CanAccessSongbook(ctx context.Context, personID, songbookID int64) (bool, error) {
	sb, err := st.GetSongbook(ctx, songbookID)
	if err != nil {
		return false, err
	}
	switch sb.OwnerKind {
	case "band":
		return st.IsCurrentBandMember(ctx, sb.OwnerBandID, personID)
	case "community":
		return isCurrentCommunityMember(ctx, st.db, sb.OwnerCommunityID, personID, "")
	default:
		return sb.OwnerPersonID == personID, nil
	}
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
		return st.HasCommunityPrivilege(ctx, sb.OwnerCommunityID, personID, PrivEditRepertoire)
	}
	return st.CanAccessSongbook(ctx, personID, songbookID)
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
		SELECT s.id, s.work_id, s.title, s.artist, s.visibility::text, s.owner_person_id, s.owner_band_id, s.forked_from_id, se.added_at
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
		if err := rows.Scan(&e.VariantID, &e.WorkID, &e.Title, &e.Artist, &e.Visibility, &e.OwnerID, &e.OwnerBandID, &e.ForkedFromID, &e.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
