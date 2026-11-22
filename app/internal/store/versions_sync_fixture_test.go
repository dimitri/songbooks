package store

import (
	"context"
	"net/http"
	"net/http/httptest"
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

// StubPCloud sends every request the server makes to pCloud to stub
// instead (its scheme and host replaced), for the test.
func StubPCloud(t *testing.T, stub *httptest.Server) {
	t.Helper()
	saved := pcloudDo
	pcloudDo = func(req *http.Request) (*http.Response, error) {
		req.Header.Set("X-Pcloud-Host", req.URL.Host)
		req.URL.Scheme, req.URL.Host = "http", stub.Listener.Addr().String()
		return stub.Client().Do(req)
	}
	pcloudLinks.Lock()
	pcloudLinks.m = map[string]pcloudCached{}
	pcloudLinks.Unlock()
	t.Cleanup(func() { pcloudDo = saved })
}

// AddTestPractice gives personID a day's practice of songID (seconds) in
// a community made for it, and one checked take; returns the community.
func AddTestPractice(t *testing.T, st *Store, personID, songID int64, seconds int) int64 {
	t.Helper()
	ctx := context.Background()
	var cid int64
	if err := st.db.QueryRow(ctx, `INSERT INTO communities (name) VALUES ('Studio') RETURNING id`).Scan(&cid); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(ctx, `
		INSERT INTO practice_logs (person_id, community_id, variant_id, practiced_on, seconds, source, pitch_takes, pitch_score)
		VALUES ($1, $2, $3, current_date, $4, 'player', 1, 80)`, personID, cid, songID, seconds); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(ctx, `
		INSERT INTO practice_takes (person_id, community_id, variant_id, level, pitch, rhythm)
		VALUES ($1, $2, $3, 'beginner', 80, 70)`, personID, cid, songID); err != nil {
		t.Fatal(err)
	}
	return cid
}

// PracticeOf is personID's practice_logs and practice_takes rows' songs
// (0 for none) and the logs' kept titles.
func PracticeOf(t *testing.T, st *Store, personID int64) (logSongs []int64, titles []string, takeSongs []int64) {
	t.Helper()
	ctx := context.Background()
	rows, err := st.db.Query(ctx, `SELECT coalesce(variant_id, 0), song_title FROM practice_logs WHERE person_id = $1 ORDER BY id`, personID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		var title string
		if err := rows.Scan(&id, &title); err != nil {
			t.Fatal(err)
		}
		logSongs, titles = append(logSongs, id), append(titles, title)
	}
	rows.Close()
	rows, err = st.db.Query(ctx, `SELECT coalesce(variant_id, 0) FROM practice_takes WHERE person_id = $1 ORDER BY id`, personID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		takeSongs = append(takeSongs, id)
	}
	return logSongs, titles, takeSongs
}

// AnnotationsBy counts personID's notes on songs.
func AnnotationsBy(t *testing.T, st *Store, personID int64) int {
	t.Helper()
	var n int
	if err := st.db.QueryRow(context.Background(), `
		SELECT (SELECT count(*) FROM variant_annotations WHERE person_id = $1) + (SELECT count(*) FROM section_annotations WHERE person_id = $1)`,
		personID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
