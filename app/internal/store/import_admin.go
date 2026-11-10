package store

import "context"

// SetMemberLeftAt marks a band or community member as having departed
// at a specific time — used by cmd/songimport to restore a departed
// member's left_at exactly as recorded in members.txt (AddBandMember/
// AddCommunityMember only ever add a CURRENT member; there's no "add
// already-departed" variant, since nothing in the normal app flow ever
// needs one).
func (st *Store) SetMemberLeftAt(ctx context.Context, kind string, groupID, personID int64, leftAt string) error {
	var table, col string
	switch kind {
	case "band":
		table, col = "band_members", "band_id"
	case "community":
		table, col = "community_members", "community_id"
	default:
		return ErrNotFound
	}
	_, err := st.db.Exec(ctx, `UPDATE `+table+` SET left_at = $1::timestamptz WHERE `+col+` = $2 AND person_id = $3 AND left_at IS NULL`,
		leftAt, groupID, personID)
	return err
}

// SetForkedFrom records variantID as a fork of forkedFromID — used by
// cmd/songimport, after every variant has a new id, to relink fork
// lineage without going through ForkVariant's own copy semantics (the
// importer already wrote the correct body straight from the export's
// own file).
func (st *Store) SetForkedFrom(ctx context.Context, variantID, forkedFromID int64) error {
	_, err := st.db.Exec(ctx, `UPDATE songs SET forked_from_id = $1 WHERE id = $2`, forkedFromID, variantID)
	return err
}

// CreateBandNoMembers inserts a band row with no members at all —
// CreateBand always requires and adds a creator, which real data can't
// always supply: a band imported from this app's legacy pre-member-
// tracking corpus can have zero band_members rows (confirmed live:
// "Mercy", one of the original LaTeX-era bands). cmd/songimport falls
// back to this only when a members.txt roster is genuinely empty.
func (st *Store) CreateBandNoMembers(ctx context.Context, name string) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx, `INSERT INTO bands (name) VALUES ($1) RETURNING id`, name).Scan(&id)
	return id, err
}

// CreateCommunityNoMembers is CreateBandNoMembers' community
// counterpart — CreateCommunity always requires and seeds a staff
// creator, which an empty members.txt roster can't supply. Role rows
// still need seeding by hand here since CreateCommunity's own seeding
// is skipped along with the membership insert.
func (st *Store) CreateCommunityNoMembers(ctx context.Context, name string) (int64, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO communities (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		return 0, err
	}
	if _, err := seedDefaultCommunityRoles(ctx, tx, id); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// FindSongbookByName looks for ownerKind/ownerID's songbook named name
// among its existing songbooks (e.g. the one CreateBand/CreateCommunity
// seed automatically) — used by cmd/songimport so it only creates a NEW
// songbook row when one by that exact name doesn't already exist.
func (st *Store) FindSongbookByName(ctx context.Context, ownerKind string, ownerID int64, name string) (int64, error) {
	books, err := st.ListSongbooks(ctx, ownerKind, ownerID)
	if err != nil {
		return 0, err
	}
	for _, b := range books {
		if b.Name == name {
			return b.ID, nil
		}
	}
	return 0, ErrNotFound
}
