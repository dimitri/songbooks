package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// RepointCounts is what repointSong moved from one song to another.
type RepointCounts struct {
	Entries      int // songbook entries
	SetlistItems int // set list lines
	Recordings   int // band recordings matched to the song
	Other        int // everything else that pointed at it (notes, practice, visits, ...)
	Media        int // recordings and Guitar Pro files moved because `to` had none of that kind
}

func (c *RepointCounts) Add(o RepointCounts) {
	c.Entries += o.Entries
	c.SetlistItems += o.SetlistItems
	c.Recordings += o.Recordings
	c.Other += o.Other
	c.Media += o.Media
}

// songRefColumn is one column pointing at songs(id).
type songRefColumn struct{ table, column string }

// songRefColumns lists every column with a foreign key to songs(id), from
// the database itself, so a table added later is repointed too.
func songRefColumns(ctx context.Context, tx pgx.Tx) ([]songRefColumn, error) {
	rows, err := tx.Query(ctx, `
		SELECT c.conrelid::regclass::text, a.attname
		FROM pg_constraint c
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
		WHERE c.contype = 'f' AND c.confrelid = 'songs'::regclass AND array_length(c.conkey, 1) = 1
		ORDER BY 1, 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []songRefColumn
	for rows.Next() {
		var r songRefColumn
		if err := rows.Scan(&r.table, &r.column); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// repointSong points everything that points at the song `from` at the
// song `to` instead, so `from` can go: songbook entries (in place: their
// position, key and lesson kept; a songbook that holds both keeps `to`'s
// entry, with `from`'s key when it had none), the songbooks' orders, set
// list lines (a revision recorded for each set list changed), the bands'
// recordings and their choices, notes, practice, visits, versions'
// forked_from_id — every column with a foreign key to songs, a row that
// would be a duplicate dropped. `from`'s recordings and Guitar Pro file
// move to `to` when `to` has none of that kind; the rest goes with `from`.
// It doesn't delete `from`.
func (st *Store) repointSong(ctx context.Context, tx pgx.Tx, from, to int64, actor *int64) (RepointCounts, error) {
	var c RepointCounts
	if from == to {
		return c, nil
	}

	// Songbook entries.
	if _, err := tx.Exec(ctx, `
		UPDATE songbook_entries t SET semitones = f.semitones
		FROM songbook_entries f
		WHERE f.variant_id = $1 AND t.variant_id = $2 AND t.songbook_id = f.songbook_id
		  AND t.semitones = 0 AND f.semitones <> 0`, from, to); err != nil {
		return c, err
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM songbook_entries f
		WHERE f.variant_id = $1
		  AND EXISTS (SELECT 1 FROM songbook_entries t WHERE t.songbook_id = f.songbook_id AND t.variant_id = $2)`, from, to)
	if err != nil {
		return c, err
	}
	c.Entries += int(tag.RowsAffected())
	tag, err = tx.Exec(ctx, `UPDATE songbook_entries SET variant_id = $2 WHERE variant_id = $1`, from, to)
	if err != nil {
		return c, err
	}
	c.Entries += int(tag.RowsAffected())
	if err := repointSongOrders(ctx, tx, from, to); err != nil {
		return c, err
	}

	// Set lists.
	n, err := st.repointSetlists(ctx, tx, from, to, actor)
	if err != nil {
		return c, err
	}
	c.SetlistItems = n
	if _, err := tx.Exec(ctx, `
		UPDATE band_recording_fixes SET to_value = $2::bigint::text
		WHERE kind = 'title' AND to_value = $1::bigint::text`, from, to); err != nil {
		return c, err
	}

	// Media: `from`'s, where `to` has none of the kind.
	tag, err = tx.Exec(ctx, `
		UPDATE song_audio a SET variant_id = $2
		WHERE a.variant_id = $1
		  AND NOT EXISTS (SELECT 1 FROM song_audio b WHERE b.variant_id = $2 AND b.category = a.category)`, from, to)
	if err != nil {
		return c, err
	}
	c.Media += int(tag.RowsAffected())
	var toHasGP bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM song_gp_source_file WHERE variant_id = $1)`, to).Scan(&toHasGP); err != nil {
		return c, err
	}
	if !toHasGP {
		tag, err := tx.Exec(ctx, `UPDATE song_gp_source_file SET variant_id = $2 WHERE variant_id = $1`, from, to)
		if err != nil {
			return c, err
		}
		if tag.RowsAffected() > 0 {
			c.Media++
			if _, err := tx.Exec(ctx, `UPDATE song_gp_source_file_version SET variant_id = $2 WHERE variant_id = $1`, from, to); err != nil {
				return c, err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM gp_drift_ack WHERE variant_id = $1`, to); err != nil {
				return c, err
			}
			if _, err := tx.Exec(ctx, `UPDATE gp_drift_ack SET variant_id = $2 WHERE variant_id = $1`, from, to); err != nil {
				return c, err
			}
		}
	}

	// Versions made from `from` are versions of `to` now.
	if _, err := tx.Exec(ctx, `UPDATE songs SET forked_from_id = $2 WHERE forked_from_id = $1 AND id <> $2`, from, to); err != nil {
		return c, err
	}
	if _, err := tx.Exec(ctx, `UPDATE songs SET forked_from_id = NULL WHERE id = $2 AND forked_from_id = $1`, from, to); err != nil {
		return c, err
	}
	if err := resyncVersions(ctx, tx, to); err != nil {
		return c, err
	}

	// Everything else.
	cols, err := songRefColumns(ctx, tx)
	if err != nil {
		return c, err
	}
	for _, col := range cols {
		switch col.table + "." + col.column {
		case "songbook_entries.variant_id", "songs.forked_from_id", "setlist_items.variant_id",
			"song_audio.variant_id", "song_gp_source_file.variant_id",
			"song_gp_source_file_version.variant_id", "gp_drift_ack.variant_id":
			continue // done above; or the media `to` has its own of, which go with `from`
		}
		n, err := repointColumn(ctx, tx, col, from, to)
		if err != nil {
			return c, fmt.Errorf("%s.%s: %w", col.table, col.column, err)
		}
		switch col.table {
		case "band_recordings":
			c.Recordings += n
		default:
			c.Other += n
		}
	}
	return c, nil
}

// repointColumn moves col's rows from `from` to `to`; a row that would
// then duplicate one of `to`'s (a unique constraint) is dropped instead.
func repointColumn(ctx context.Context, tx pgx.Tx, col songRefColumn, from, to int64) (int, error) {
	table := pgx.Identifier{col.table}.Sanitize()
	column := pgx.Identifier{col.column}.Sanitize()
	if _, err := tx.Exec(ctx, `SAVEPOINT repoint`); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `UPDATE `+table+` SET `+column+` = $2 WHERE `+column+` = $1`, from, to)
	if err == nil {
		_, err = tx.Exec(ctx, `RELEASE SAVEPOINT repoint`)
		return int(tag.RowsAffected()), err
	}
	if !isUniqueViolation(err) {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT repoint`); err != nil {
		return 0, err
	}
	// One row at a time, by ctid.
	rows, err := tx.Query(ctx, `SELECT ctid::text FROM `+table+` WHERE `+column+` = $1`, from)
	if err != nil {
		return 0, err
	}
	var ctids []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return 0, err
		}
		ctids = append(ctids, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, ctid := range ctids {
		if _, err := tx.Exec(ctx, `SAVEPOINT repoint_row`); err != nil {
			return n, err
		}
		_, err := tx.Exec(ctx, `UPDATE `+table+` SET `+column+` = $2 WHERE ctid = $1::tid`, ctid, to)
		if err != nil {
			if !isUniqueViolation(err) {
				return n, err
			}
			if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT repoint_row`); err != nil {
				return n, err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE ctid = $1::tid`, ctid); err != nil {
				return n, err
			}
		} else if _, err := tx.Exec(ctx, `RELEASE SAVEPOINT repoint_row`); err != nil {
			return n, err
		}
		n++
	}
	_, err = tx.Exec(ctx, `RELEASE SAVEPOINT repoint`)
	return n, err
}

// repointSongOrders changes `from` to `to` in every songbook's own order
// (song_order), `from` dropped where `to` is listed already.
func repointSongOrders(ctx context.Context, tx pgx.Tx, from, to int64) error {
	rows, err := tx.Query(ctx, `
		SELECT id, song_order FROM songbooks
		WHERE song_order @? ('$[*].VariantIDs[*] ? (@ == ' || $1::bigint::text || ')')::jsonpath
		FOR UPDATE`, from)
	if err != nil {
		return err
	}
	type sbOrder struct {
		id  int64
		raw []byte
	}
	var books []sbOrder
	for rows.Next() {
		var b sbOrder
		if err := rows.Scan(&b.id, &b.raw); err != nil {
			rows.Close()
			return err
		}
		books = append(books, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, b := range books {
		var groups []SongGroup
		if err := json.Unmarshal(b.raw, &groups); err != nil {
			return fmt.Errorf("songbook %d's order: %w", b.id, err)
		}
		hasTo := false
		for _, g := range groups {
			for _, id := range g.VariantIDs {
				if id == to {
					hasTo = true
				}
			}
		}
		for i := range groups {
			ids := groups[i].VariantIDs[:0]
			for _, id := range groups[i].VariantIDs {
				switch {
				case id == from && hasTo:
					continue
				case id == from:
					ids = append(ids, to)
					hasTo = true
				default:
					ids = append(ids, id)
				}
			}
			groups[i].VariantIDs = ids
		}
		raw, err := json.Marshal(groups)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE songbooks SET song_order = $1 WHERE id = $2`, raw, b.id); err != nil {
			return err
		}
	}
	return nil
}

// repointSetlists changes the rows of every set list that are the song
// `from` to the song `to`, a revision recorded for each set list changed,
// and says how many rows changed.
func (st *Store) repointSetlists(ctx context.Context, tx pgx.Tx, from, to int64, actor *int64) (int, error) {
	return st.repointSetlistItems(ctx, tx, from, to, actor, "", nil)
}

// deleteSong removes a song repointSong has emptied.
func deleteSong(ctx context.Context, tx pgx.Tx, id int64) error {
	tag, err := tx.Exec(ctx, `DELETE FROM songs WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such song %d: %w", id, ErrNotFound)
	}
	return nil
}
