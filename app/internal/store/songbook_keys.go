package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// A song in another key without a copy: a songbook entry's semitones
// (songbook_entries.semitones, migration 0079), set by a coach for a
// student's songbook first, by anyone for a songbook they can change: the
// song opened from that songbook plays in that key (the chart transposed,
// the recording pitch-shifted), and is still the one song, edited in its
// own key, for everyone who has it.

// SongbookKey is a songbook a person can change, and the key a song is in
// there. InSongbook false is the person's own songbook when the song
// isn't in it yet (saving a key there adds it).
type SongbookKey struct {
	SongbookID int64
	Name       string
	Mine       bool // the person's own default songbook
	InSongbook bool
	Semitones  int
}

// SongbookKeysFor lists the songbooks personID can change that hold
// variantID, the person's own songbook first (in it or not), then by
// name. A coaching student's or group's songbook is left out: its key is
// the coach's (coach_keys.go).
func (st *Store) SongbookKeysFor(ctx context.Context, personID, variantID int64) ([]SongbookKey, error) {
	mine, err := st.GetDefaultSongbook(ctx, "person", personID)
	if err != nil {
		return nil, err
	}
	rows, err := st.db.Query(ctx, `
		SELECT sb.id, sb.name, se.semitones
		FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
		WHERE se.variant_id = $1 AND sb.kind NOT IN ('student', 'group')
		ORDER BY lower(sb.name), sb.id`, variantID)
	if err != nil {
		return nil, err
	}
	var found []SongbookKey
	for rows.Next() {
		var k SongbookKey
		if err := rows.Scan(&k.SongbookID, &k.Name, &k.Semitones); err != nil {
			rows.Close()
			return nil, err
		}
		k.InSongbook, k.Mine = true, k.SongbookID == mine
		found = append(found, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []SongbookKey{}
	var own *SongbookKey
	for _, k := range found {
		if k.Mine {
			k := k
			own = &k
			continue
		}
		if ok, err := st.CanEditSongbook(ctx, personID, k.SongbookID); err != nil {
			return nil, err
		} else if ok {
			out = append(out, k)
		}
	}
	if own == nil {
		sb, err := st.GetSongbook(ctx, mine)
		if err != nil {
			return nil, err
		}
		own = &SongbookKey{SongbookID: mine, Name: sb.Name, Mine: true}
	}
	return append([]SongbookKey{*own}, out...), nil
}

// MySongbookKey is the key variantID is in, in personID's own songbook:
// ok false when it isn't there.
func (st *Store) MySongbookKey(ctx context.Context, personID, variantID int64) (semitones int, ok bool, err error) {
	err = st.db.QueryRow(ctx, `
		SELECT se.semitones FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
		WHERE sb.owner_kind = 'person' AND sb.owner_person_id = $1 AND sb.is_default AND se.variant_id = $2`,
		personID, variantID).Scan(&semitones)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return semitones, err == nil, err
}

// SongbookEntryKey is the key variantID is in, in songbookID: ok false
// when the songbook doesn't hold it.
func (st *Store) SongbookEntryKey(ctx context.Context, songbookID, variantID int64) (semitones int, ok bool, err error) {
	err = st.db.QueryRow(ctx, `SELECT semitones FROM songbook_entries WHERE songbook_id = $1 AND variant_id = $2`,
		songbookID, variantID).Scan(&semitones)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return semitones, err == nil, err
}

// SongbookEntryKeys is the key of each song of songbookID that isn't in
// its own (semitones != 0), by song.
func (st *Store) SongbookEntryKeys(ctx context.Context, songbookID int64) (map[int64]int, error) {
	rows, err := st.db.Query(ctx, `SELECT variant_id, semitones FROM songbook_entries WHERE songbook_id = $1 AND semitones <> 0`, songbookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var v int64
		var n int
		if err := rows.Scan(&v, &n); err != nil {
			return nil, err
		}
		out[v] = n
	}
	return out, rows.Err()
}
