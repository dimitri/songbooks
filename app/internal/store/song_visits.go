package store

import (
	"context"
	"time"
)

// RecordSongVisit moves songID to the top of personID's recently opened
// songs, remembering the context it was opened in (see migration 0067).
func (st *Store) RecordSongVisit(ctx context.Context, personID, songID int64, query string) error {
	_, err := st.db.Exec(ctx, `
		INSERT INTO song_visits (person_id, song_id, query) VALUES ($1, $2, $3)
		ON CONFLICT (person_id, song_id) DO UPDATE SET query = EXCLUDED.query, visited_at = now()`,
		personID, songID, query)
	return err
}

// RecentSong is one of a person's recently opened songs.
type RecentSong struct {
	SongID    int64
	Title     string
	Artist    string
	Query     string
	VisitedAt time.Time
}

// ListRecentSongs returns personID's last opened songs, newest first.
func (st *Store) ListRecentSongs(ctx context.Context, personID int64, limit int) ([]RecentSong, error) {
	rows, err := st.db.Query(ctx, `
		SELECT v.song_id, s.title, s.artist, v.query, v.visited_at
		FROM song_visits v JOIN songs s ON s.id = v.song_id
		WHERE v.person_id = $1
		ORDER BY v.visited_at DESC
		LIMIT $2`, personID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecentSong
	for rows.Next() {
		var r RecentSong
		if err := rows.Scan(&r.SongID, &r.Title, &r.Artist, &r.Query, &r.VisitedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
