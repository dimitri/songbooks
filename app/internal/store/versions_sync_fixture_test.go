package store

import (
	"context"
	"testing"
)

// AddTestRecording gives bandID a recording of songID (its pCloud folder
// made when it has none), public or not, for the external tests of
// public band recordings (versions_sync_test.go).
func AddTestRecording(t *testing.T, st *Store, bandID, songID int64, public bool) int64 {
	t.Helper()
	ctx := context.Background()
	var srcID int64
	if err := st.db.QueryRow(ctx, `
		INSERT INTO band_recording_sources (band_id, provider, link, code, api_host) VALUES ($1, 'pcloud', 'l', 'thecode', 'eapi.pcloud.com')
		ON CONFLICT (band_id) DO UPDATE SET link = EXCLUDED.link RETURNING id`, bandID).Scan(&srcID); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := st.db.QueryRow(ctx, `
		INSERT INTO band_recordings (source_id, band_id, file_id, path, name, kind, session_date, variant_id, public)
		VALUES ($1, $2, (SELECT coalesce(max(file_id), 0) + 1 FROM band_recordings), 'Repetes/2026-11-12', 'take.mp3', 'rehearsal', '2026-11-12', $3, $4)
		RETURNING id`, srcID, bandID, songID, public).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// SongBase is songID's base_body (the original's chart it last synced
// with), "" when it has none.
func SongBase(t *testing.T, st *Store, songID int64) string {
	t.Helper()
	var base *string
	if err := st.db.QueryRow(context.Background(), `SELECT base_body FROM songs WHERE id = $1`, songID).Scan(&base); err != nil {
		t.Fatal(err)
	}
	if base == nil {
		return ""
	}
	return *base
}

// SongOwnerAndOrigin is songID's owner and forked_from_id (0: none).
func SongOwnerAndOrigin(t *testing.T, st *Store, songID int64) (Owner, int64) {
	t.Helper()
	var o Owner
	var from *int64
	if err := st.db.QueryRow(context.Background(), `
		SELECT owner_kind, coalesce(owner_person_id, owner_band_id, owner_community_id), forked_from_id FROM songs WHERE id = $1`, songID).
		Scan(&o.Kind, &o.ID, &from); err != nil {
		t.Fatal(err)
	}
	if from == nil {
		return o, 0
	}
	return o, *from
}

// SongbookSongIDs is songbookID's songs, by id.
func SongbookSongIDs(t *testing.T, st *Store, songbookID int64) []int64 {
	t.Helper()
	rows, err := st.db.Query(context.Background(), `SELECT variant_id FROM songbook_entries WHERE songbook_id = $1 ORDER BY variant_id`, songbookID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	return out
}
