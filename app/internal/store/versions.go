package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Versions (docs/ownership-model.md §5): any change to a song is a
// version, a new song owned by the songbook's owner, private, its
// forked_from_id the song it came from. A songbook holds links and own
// songs; Edit on a link offers "Make Mercy's version" (MakeVersion), and
// the version takes the link's place; on an own song, Edit edits it, and
// "New version from this" makes another, named, beside it — one row per
// version.

// ownerMayChange reports whether personID may put songs in owner's
// songbooks and make owner's songs: the person; a band's current members;
// a community's members with edit_repertoire.
func ownerMayChange(ctx context.Context, q dbtx, personID int64, o Owner) (bool, error) {
	switch o.Kind {
	case "person":
		return o.ID == personID, nil
	case "band":
		return isCurrentBandMember(ctx, q, o.ID, personID)
	case "community":
		var global bool
		if err := q.QueryRow(ctx, `SELECT coalesce((SELECT kind = 'global' FROM communities WHERE id = $1), false)`, o.ID).Scan(&global); err != nil {
			return false, err
		}
		if global {
			var admin bool
			err := q.QueryRow(ctx, `SELECT coalesce((SELECT is_admin FROM people WHERE id = $1), false)`, personID).Scan(&admin)
			return admin, err
		}
		return hasCommunityPrivilege(ctx, q, o.ID, personID, PrivEditRepertoire)
	}
	return false, nil
}

// OwnerMayChange is ownerMayChange for the HTTP layer.
func (st *Store) OwnerMayChange(ctx context.Context, personID int64, o Owner) (bool, error) {
	return ownerMayChange(ctx, st.db, personID, o)
}

// MakeVersion makes owner's own version of songID for personID and
// returns its id: a new song owned by owner, private, its content and
// media copied, forked_from_id songID, named name ("" when it has none).
//
// When songID is a link of owner's (another owner's song): the version
// takes its place in songbookID (0: in every songbook of owner's holding
// it), in place — its position, key and lesson — and in owner's set lists.
// An unnamed version owner already has of songID is that version: it
// takes the link's place, nothing new is made (a second one is New
// version from this, named).
// When songID is owner's own (New version from this): the version is a
// new row next to it in songbookID (0: in each of owner's songbooks
// holding it), nothing replaced. A song in none of them goes into owner's
// default songbook. personID needs CanVersionFor(owner) and the right to
// change owner's songbooks (ownerMayChange): ErrForbidden, or a
// *LinkRefusal saying why.
func (st *Store) MakeVersion(ctx context.Context, personID, songID int64, owner Owner, songbookID int64, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if len(name) > 80 {
		return 0, fmt.Errorf("a version name is 80 characters at most: %w", ErrInvalid)
	}
	r, err := st.SongRightsFor(ctx, personID, songID)
	if err != nil {
		return 0, err
	}
	if !r.CanVersionFor(owner) {
		if r.Owner == owner {
			return 0, fmt.Errorf("person %d may not edit song %d: %w", personID, songID, ErrForbidden)
		}
		return 0, st.linkRefusal(ctx, r)
	}
	if ok, err := st.OwnerMayChange(ctx, personID, owner); err != nil {
		return 0, err
	} else if !ok {
		return 0, fmt.Errorf("person %d may not change %s %d's songbooks: %w", personID, owner.Kind, owner.ID, ErrForbidden)
	}
	own := r.Owner == owner
	if songbookID != 0 {
		sb, err := st.GetSongbook(ctx, songbookID)
		if err != nil {
			return 0, err
		}
		if sb.Owner() != owner {
			return 0, fmt.Errorf("songbook %d isn't %s %d's: %w", songbookID, owner.Kind, owner.ID, ErrForbidden)
		}
	}
	defaultSb, err := st.GetDefaultSongbook(ctx, owner.Kind, owner.ID)
	if err != nil {
		return 0, err
	}

	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var workID int64
	var title, artist, referenceURL, body string
	var srcVersion int
	err = tx.QueryRow(ctx, `
		SELECT work_id, title, artist, reference_url, body, version
		FROM songs WHERE id = $1 FOR UPDATE`, songID).
		Scan(&workID, &title, &artist, &referenceURL, &body, &srcVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("no such song %d: %w", songID, ErrNotFound)
	}
	if err != nil {
		return 0, err
	}
	var personCol, bandCol, communityCol *int64
	switch owner.Kind {
	case "person":
		personCol = &owner.ID
	case "band":
		bandCol = &owner.ID
	case "community":
		communityCol = &owner.ID
	default:
		return 0, fmt.Errorf("no owner kind %q", owner.Kind)
	}
	var newID int64
	if !own && name == "" {
		err := tx.QueryRow(ctx, `
			SELECT id FROM songs
			WHERE forked_from_id = $1 AND owner_kind = $2
			  AND coalesce(owner_person_id, owner_band_id, owner_community_id) = $3
			ORDER BY (version_name = '') DESC, id LIMIT 1`, songID, owner.Kind, owner.ID).Scan(&newID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
	}
	if newID == 0 {
		if err := tx.QueryRow(ctx, `
			INSERT INTO songs (title, artist, reference_url, body, work_id, version_name,
			                   owner_person_id, owner_band_id, owner_community_id, forked_from_id, created_by_person_id,
			                   base_body, base_version, base_synced_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $4, $12, now())
			RETURNING id`,
			title, artist, referenceURL, body, workID, name, personCol, bandCol, communityCol, songID, personID, srcVersion).Scan(&newID); err != nil {
			return 0, err
		}
		if err := copySongMedia(ctx, tx, songID, newID); err != nil {
			return 0, err
		}
	}

	// Owner's songbooks holding songID: songbookID, or all of them.
	rows, err := tx.Query(ctx, `
		SELECT se.songbook_id FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
		WHERE se.variant_id = $1 AND sb.owner_kind = $2
		  AND coalesce(sb.owner_person_id, sb.owner_band_id, sb.owner_community_id) = $3
		  AND ($4 = 0 OR sb.id = $4)
		ORDER BY sb.id`, songID, owner.Kind, owner.ID, songbookID)
	if err != nil {
		return 0, err
	}
	var books []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		books = append(books, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(books) == 0 {
		target := songbookID
		if target == 0 {
			target = defaultSb
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO songbook_entries (songbook_id, variant_id, added_by_person_id) VALUES ($1, $2, $3)
			ON CONFLICT (songbook_id, variant_id) DO NOTHING`, target, newID, personID); err != nil {
			return 0, err
		}
	}
	for _, sbID := range books {
		if own {
			// A new row beside it: the same key and lesson, next in order.
			if _, err := tx.Exec(ctx, `
				INSERT INTO songbook_entries (songbook_id, variant_id, added_by_person_id, lesson_id, lesson_position, semitones)
				SELECT songbook_id, $3, $4, lesson_id, lesson_position, semitones
				FROM songbook_entries WHERE songbook_id = $1 AND variant_id = $2`, sbID, songID, newID, personID); err != nil {
				return 0, err
			}
			if err := insertInSongOrder(ctx, tx, sbID, songID, newID); err != nil {
				return 0, err
			}
			continue
		}
		// The version in the link's place (a songbook holding both keeps
		// the version's row).
		if _, err := tx.Exec(ctx, `
			UPDATE songbook_entries SET variant_id = $3 WHERE songbook_id = $1 AND variant_id = $2
			  AND NOT EXISTS (SELECT 1 FROM songbook_entries x WHERE x.songbook_id = $1 AND x.variant_id = $3)`, sbID, songID, newID); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM songbook_entries WHERE songbook_id = $1 AND variant_id = $2`, sbID, songID); err != nil {
			return 0, err
		}
		if err := replaceInSongOrder(ctx, tx, sbID, songID, newID); err != nil {
			return 0, err
		}
	}
	if !own {
		if err := st.repointOwnerSetlists(ctx, tx, owner, songID, newID, personID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return newID, nil
}

// SetVersionName names songID among its owner's versions of the same song.
func (st *Store) SetVersionName(ctx context.Context, songID int64, name string) error {
	name = strings.TrimSpace(name)
	if len(name) > 80 {
		return fmt.Errorf("a version name is 80 characters at most: %w", ErrInvalid)
	}
	tag, err := st.db.Exec(ctx, `UPDATE songs SET version_name = $2 WHERE id = $1`, songID, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such song %d: %w", songID, ErrNotFound)
	}
	return nil
}

// VersionNames maps each of songIDs that has a version name to it (a
// songbook's rows: "Bad Spell — psychedelic").
func (st *Store) VersionNames(ctx context.Context, songIDs []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(songIDs) == 0 {
		return out, nil
	}
	rows, err := st.db.Query(ctx, `SELECT id, version_name FROM songs WHERE id = ANY($1) AND version_name <> ''`, songIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// songOrder loads songbookID's song_order for change.
func songOrder(ctx context.Context, tx pgx.Tx, songbookID int64) ([]SongGroup, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT song_order FROM songbooks WHERE id = $1 FOR UPDATE`, songbookID).Scan(&raw); err != nil {
		return nil, err
	}
	var groups []SongGroup
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, fmt.Errorf("songbook %d's order: %w", songbookID, err)
	}
	return groups, nil
}

func saveSongOrder(ctx context.Context, tx pgx.Tx, songbookID int64, groups []SongGroup) error {
	raw, err := json.Marshal(groups)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE songbooks SET song_order = $1 WHERE id = $2`, raw, songbookID)
	return err
}

// replaceInSongOrder puts `to` where `from` is in songbookID's order.
func replaceInSongOrder(ctx context.Context, tx pgx.Tx, songbookID, from, to int64) error {
	groups, err := songOrder(ctx, tx, songbookID)
	if err != nil || len(groups) == 0 {
		return err
	}
	changed := false
	for i := range groups {
		for j, id := range groups[i].VariantIDs {
			if id == from {
				groups[i].VariantIDs[j] = to
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	return saveSongOrder(ctx, tx, songbookID, groups)
}

// insertInSongOrder puts `to` right after `after` in songbookID's order.
func insertInSongOrder(ctx context.Context, tx pgx.Tx, songbookID, after, to int64) error {
	groups, err := songOrder(ctx, tx, songbookID)
	if err != nil || len(groups) == 0 {
		return err
	}
	for i := range groups {
		for j, id := range groups[i].VariantIDs {
			if id == after {
				ids := append([]int64{}, groups[i].VariantIDs[:j+1]...)
				ids = append(ids, to)
				groups[i].VariantIDs = append(ids, groups[i].VariantIDs[j+1:]...)
				return saveSongOrder(ctx, tx, songbookID, groups)
			}
		}
	}
	return nil
}

// repointOwnerSetlists changes owner's set lists' rows that are `from` to
// `to`, a revision recorded for each set list changed.
func (st *Store) repointOwnerSetlists(ctx context.Context, tx pgx.Tx, owner Owner, from, to, actorID int64) error {
	_, err := st.repointSetlistItems(ctx, tx, from, to, &actorID, `sl.`+songbookOwnerCol(owner.Kind)+` = $2`, owner.ID)
	return err
}

// Publishing (§2): a song or a whole songbook goes public — everything
// with the song (chart, reference, recordings, stems, Guitar Pro file:
// they're the song's, and the song's rights open them) — or private
// again.

// SetSongPublic makes songID public or private for personID, who needs
// SongRights.Publish (a person their own; a band's core members; a
// community's staff; the site's admins for The Big Songbook's). A closed
// songbook's song is never public: ErrForbidden.
func (st *Store) SetSongPublic(ctx context.Context, personID, songID int64, public bool) error {
	r, err := st.SongRightsFor(ctx, personID, songID)
	if err != nil {
		return err
	}
	if !r.Publish {
		return fmt.Errorf("person %d may not publish song %d: %w", personID, songID, ErrForbidden)
	}
	if public || !r.Public {
		_, err = st.db.Exec(ctx, `UPDATE songs SET public = $2 WHERE id = $1`, songID, public)
		return err
	}
	// Private again: whoever linked it and can't see it any more keeps a
	// frozen copy (frozen.go).
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE songs SET public = false WHERE id = $1`, songID); err != nil {
		return err
	}
	if _, err := st.freezeLinks(ctx, tx, songID, false, personID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetSongbookPublic makes songbookID public (everyone signed in opens it)
// with its owner's songs in it, or private again (its songs stay as they
// are: each is made private on its own). personID needs
// SongbookRights.Publish; a closed songbook never: ErrForbidden.
func (st *Store) SetSongbookPublic(ctx context.Context, personID, songbookID int64, public bool) error {
	r, err := st.SongbookRightsFor(ctx, personID, songbookID)
	if err != nil {
		return err
	}
	if !r.Publish {
		return fmt.Errorf("person %d may not publish songbook %d: %w", personID, songbookID, ErrForbidden)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE songbooks SET public = $2 WHERE id = $1`, songbookID, public); err != nil {
		return err
	}
	if public {
		if _, err := tx.Exec(ctx, `
			UPDATE songs s SET public = true
			FROM songbook_entries se, songbooks sb
			WHERE se.songbook_id = $1 AND sb.id = se.songbook_id AND s.id = se.variant_id
			  AND (s.owner_person_id = sb.owner_person_id OR s.owner_band_id = sb.owner_band_id
			       OR s.owner_community_id = sb.owner_community_id)
			  AND NOT song_closed(s.id)`, songbookID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
