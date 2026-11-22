package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"app/internal/setlist"
)

// A set list's lines are rows of setlist_items (migration 0115,
// docs/ownership-model.md §4): in position order, a heading row starts a
// set ("Set 1", "Encore"), a song row is one song of it — the version
// this set list plays — with its note. The header's title, date and venue
// are the set list's columns. setlist.Setlist stays the shape every
// reader walks (sets of entries), built from the rows; its text form
// (setlist.Write) is what a revision keeps and what the export writes.

// Owner is the set list's owner.
func (m SetlistMeta) Owner() Owner {
	switch {
	case m.OwnerBandID != nil:
		return BandOwner(*m.OwnerBandID)
	case m.OwnerCommunityID != nil:
		return CommunityOwner(*m.OwnerCommunityID)
	case m.OwnerPersonID != nil:
		return PersonOwner(*m.OwnerPersonID)
	}
	return Owner{}
}

// loadSetlist builds setlistID's Setlist from its columns and rows.
func loadSetlist(ctx context.Context, q dbtx, setlistID int64) (*setlist.Setlist, error) {
	var title, date, venue string
	err := q.QueryRow(ctx, `SELECT title, date, venue FROM setlists WHERE id = $1`, setlistID).Scan(&title, &date, &venue)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such setlist %d: %w", setlistID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	l := &setlist.Setlist{}
	for _, f := range []setlist.MetaField{{Key: "title", Value: title}, {Key: "date", Value: date}, {Key: "venue", Value: venue}} {
		if f.Value != "" {
			l.Meta = append(l.Meta, f)
		}
	}
	rows, err := q.Query(ctx, `
		SELECT id, section, variant_id, note FROM setlist_items
		WHERE setlist_id = $1 ORDER BY position`, setlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var section *string
		var variantID *int64
		var note string
		if err := rows.Scan(&id, &section, &variantID, &note); err != nil {
			return nil, err
		}
		if section != nil {
			l.Sets = append(l.Sets, setlist.Set{Label: *section})
			continue
		}
		if len(l.Sets) == 0 {
			l.Sets = append(l.Sets, setlist.Set{}) // a song before any heading: never written, but read as a set of its own
		}
		cur := &l.Sets[len(l.Sets)-1]
		cur.Entries = append(cur.Entries, setlist.Entry{ID: id, VariantID: *variantID, Note: note})
	}
	return l, rows.Err()
}

// writeSetlistItems replaces setlistID's rows with l's sets (a heading row
// for each set, then its songs) and sets its date and venue from l's
// header. An entry without a song (a book/stem line of the project-scoped
// set lists, gone since migration 0041), or whose song is gone (an old
// revision brought back), has nothing to point at: it isn't kept. The title is the caller's (SaveSetlistByID's title).
func writeSetlistItems(ctx context.Context, tx pgx.Tx, setlistID int64, l *setlist.Setlist) error {
	if _, err := tx.Exec(ctx, `DELETE FROM setlist_items WHERE setlist_id = $1`, setlistID); err != nil {
		return err
	}
	var sections []*string
	var variants []*int64
	var notes []string
	for _, set := range l.Sets {
		label := set.Label
		sections, variants, notes = append(sections, &label), append(variants, nil), append(notes, "")
		for _, e := range set.Entries {
			if !e.IsVariant() {
				continue
			}
			v := e.VariantID
			sections, variants, notes = append(sections, nil), append(variants, &v), append(notes, e.Note)
		}
	}
	if len(sections) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO setlist_items (setlist_id, position, section, variant_id, note)
			SELECT $1, n, s, v, coalesce(t, '')
			FROM unnest($2::text[], $3::bigint[], $4::text[]) WITH ORDINALITY AS u(s, v, t, n)
			WHERE v IS NULL OR EXISTS (SELECT 1 FROM songs WHERE id = v)`,
			setlistID, sections, variants, notes); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `UPDATE setlists SET date = $2, venue = $3 WHERE id = $1`, setlistID, l.Get("date"), l.Get("venue"))
	return err
}

// setlistSnapshot is setlistID's state for its revision history, read
// inside tx (the row locked by the caller): its text form in Body.
// exists is false when there's no such set list.
func setlistSnapshot(ctx context.Context, tx pgx.Tx, setlistID int64) (snap SetlistSnapshot, version int, exists bool, err error) {
	err = tx.QueryRow(ctx, `
		SELECT scope::text, title, notes, owner_person_id, owner_band_id, version
		FROM setlists WHERE id = $1 FOR UPDATE`, setlistID).
		Scan(&snap.Scope, &snap.Title, &snap.Notes, &snap.OwnerPersonID, &snap.OwnerBandID, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return SetlistSnapshot{}, 0, false, nil
	}
	if err != nil {
		return SetlistSnapshot{}, 0, false, err
	}
	l, err := loadSetlist(ctx, tx, setlistID)
	if err != nil {
		return SetlistSnapshot{}, 0, false, err
	}
	snap.Exists = true
	snap.Body = setlist.Write(l)
	return snap, version, true, nil
}

// SetlistTextOf is setlistID's text form, read through q (a pool, a
// transaction): the .setlist file cmd/songexport writes, which
// cmd/songimport reads back.
func SetlistTextOf(ctx context.Context, q dbtx, setlistID int64) (string, error) {
	l, err := loadSetlist(ctx, q, setlistID)
	if err != nil {
		return "", err
	}
	return setlist.Write(l), nil
}

// repointSetlistItems changes the song rows that are `from` to `to` in
// the set lists ownerWhere picks (a condition on setlists sl, $2 being
// ownerArg; "" for every set list), a revision recorded for each set list
// changed and its version bumped, and says how many rows changed.
func (st *Store) repointSetlistItems(ctx context.Context, tx pgx.Tx, from, to int64, actor *int64, ownerWhere string, ownerArg any) (int, error) {
	q := `SELECT DISTINCT i.setlist_id FROM setlist_items i JOIN setlists sl ON sl.id = i.setlist_id
	      WHERE i.variant_id = $1`
	args := []any{from}
	if ownerWhere != "" {
		q += ` AND ` + ownerWhere
		args = append(args, ownerArg)
	}
	rows, err := tx.Query(ctx, q+` ORDER BY 1`, args...)
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		snap, _, ok, err := setlistSnapshot(ctx, tx, id)
		if err != nil || !ok {
			return n, err
		}
		if err := st.RecordRevision(ctx, tx, "setlist", strconv.FormatInt(id, 10), actor, snap); err != nil {
			return n, err
		}
		tag, err := tx.Exec(ctx, `UPDATE setlist_items SET variant_id = $3 WHERE setlist_id = $1 AND variant_id = $2`, id, from, to)
		if err != nil {
			return n, err
		}
		if _, err := tx.Exec(ctx, `UPDATE setlists SET version = version + 1 WHERE id = $1`, id); err != nil {
			return n, err
		}
		n += int(tag.RowsAffected())
	}
	return n, nil
}

// SetlistVersion is one version a set list's song row may play: a song of
// the same work in the set list's owner's songbooks (or the one it plays).
type SetlistVersion struct {
	VariantID   int64
	Title       string
	Artist      string
	VersionName string
}

// SetlistVersionChoices maps each song row of setlistID (its item id) for
// which the owner's songbooks hold more than one version of the work —
// "Bad Spell" and "Bad Spell — psychedelic" — to those versions, the one
// it plays among them, by title, version name, id. A row with nothing to
// choose isn't in the map.
func (st *Store) SetlistVersionChoices(ctx context.Context, setlistID int64) (map[int64][]SetlistVersion, error) {
	rows, err := st.db.Query(ctx, `
		WITH owner_songs AS (
		  SELECT DISTINCT se.variant_id FROM songbook_entries se
		  JOIN songbooks sb ON sb.id = se.songbook_id
		  JOIN setlists sl ON sl.id = $1
		  WHERE sb.owner_person_id IS NOT DISTINCT FROM sl.owner_person_id
		    AND sb.owner_band_id IS NOT DISTINCT FROM sl.owner_band_id
		    AND sb.owner_community_id IS NOT DISTINCT FROM sl.owner_community_id
		)
		SELECT i.id, v.id, v.title, v.artist, v.version_name
		FROM setlist_items i
		JOIN songs s ON s.id = i.variant_id
		JOIN songs v ON v.work_id = s.work_id
		WHERE i.setlist_id = $1
		  AND (v.id = s.id OR v.id IN (SELECT variant_id FROM owner_songs))
		ORDER BY i.id, v.title, v.version_name, v.id`, setlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]SetlistVersion{}
	for rows.Next() {
		var itemID int64
		var v SetlistVersion
		if err := rows.Scan(&itemID, &v.VariantID, &v.Title, &v.Artist, &v.VersionName); err != nil {
			return nil, err
		}
		out[itemID] = append(out[itemID], v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id, vs := range out {
		if len(vs) < 2 {
			delete(out, id)
		}
	}
	return out, nil
}

// ChooseSetlistVersion makes setlistID's song row itemID play variantID,
// one of SetlistVersionChoices' versions for it, for personID (who may
// edit the set list: the caller checks). The song's click-track note
// follows it when the set list doesn't hold the old song any more. If
// expectVersion is non-empty, a set list changed since is ErrConflict.
// Returns the set list's new version.
func (st *Store) ChooseSetlistVersion(ctx context.Context, personID, setlistID, itemID, variantID int64, expectVersion string) (string, error) {
	choices, err := st.SetlistVersionChoices(ctx, setlistID)
	if err != nil {
		return "", err
	}
	ok := false
	for _, v := range choices[itemID] {
		ok = ok || v.VariantID == variantID
	}
	if !ok {
		return "", fmt.Errorf("song %d isn't a version set list %d's row %d may play: %w", variantID, setlistID, itemID, ErrInvalid)
	}

	tx, err := st.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	snap, version, exists, err := setlistSnapshot(ctx, tx, setlistID)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("no such setlist %d: %w", setlistID, ErrNotFound)
	}
	if expectVersion != "" && strconv.Itoa(version) != expectVersion {
		return "", ErrConflict
	}
	var from int64
	err = tx.QueryRow(ctx, `SELECT variant_id FROM setlist_items WHERE id = $1 AND setlist_id = $2 AND variant_id IS NOT NULL`,
		itemID, setlistID).Scan(&from)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("no song row %d in set list %d: %w", itemID, setlistID, ErrNotFound)
	}
	if err != nil {
		return "", err
	}
	if from == variantID {
		return strconv.Itoa(version), nil
	}
	actor := &personID
	if err := st.RecordRevision(ctx, tx, "setlist", strconv.FormatInt(setlistID, 10), actor, snap); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE setlist_items SET variant_id = $2 WHERE id = $1`, itemID, variantID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE setlist_clicktrack_notes SET variant_id = $3
		WHERE setlist_id = $1 AND variant_id = $2
		  AND NOT EXISTS (SELECT 1 FROM setlist_items WHERE setlist_id = $1 AND variant_id = $2)
		  AND NOT EXISTS (SELECT 1 FROM setlist_clicktrack_notes WHERE setlist_id = $1 AND variant_id = $3)`,
		setlistID, from, variantID); err != nil {
		return "", err
	}
	var newVersion int
	if err := tx.QueryRow(ctx, `UPDATE setlists SET version = version + 1 WHERE id = $1 RETURNING version`, setlistID).Scan(&newVersion); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return strconv.Itoa(newVersion), nil
}
