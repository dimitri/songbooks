package store

import "context"

// IRealSongInput is one parsed tune ready to cache — the normalized
// shape SaveIRealLibrary writes to ireal_songs.
type IRealSongInput struct {
	Title, Composer, Style, Key string
	TimeSigNum, TimeSigDen      int
	GridText                    string
	ReactionCount               int
}

// SaveIRealLibrary upserts one fetched forum link's raw bytes (by
// sourceURL) and replaces its normalized song rows wholesale — a
// library thread can be re-fetched and re-parsed later (e.g. after a
// parser fix) without accumulating duplicate song rows from the same
// source.
func (st *Store) SaveIRealLibrary(ctx context.Context, sourceURL, title, rawBody string, songs []IRealSongInput) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var fetchID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO ireal_library_fetches (source_url, title, raw_body)
		VALUES ($1, $2, $3)
		ON CONFLICT (source_url) DO UPDATE SET title = $2, raw_body = $3, fetched_at = now()
		RETURNING id`, sourceURL, title, rawBody).Scan(&fetchID)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM ireal_songs WHERE fetch_id = $1`, fetchID); err != nil {
		return err
	}
	for _, s := range songs {
		_, err := tx.Exec(ctx, `
			INSERT INTO ireal_songs (fetch_id, title, composer, style, key, time_sig_num, time_sig_den, grid_text, reaction_count)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			fetchID, s.Title, s.Composer, s.Style, s.Key, s.TimeSigNum, s.TimeSigDen, s.GridText, s.ReactionCount)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// IRealFetch is one cached forum link fetch — the raw layer
// SaveIRealLibrary writes to ireal_library_fetches.
type IRealFetch struct {
	ID                        int64
	SourceURL, Title, RawBody string
}

// ListIRealFetches returns every cached raw fetch, for re-deriving
// ireal_songs from stored bytes after a parser fix (see
// cmd/irealreparse) without re-fetching the forum.
func (st *Store) ListIRealFetches(ctx context.Context) ([]IRealFetch, error) {
	rows, err := st.db.Query(ctx, `SELECT id, source_url, title, raw_body FROM ireal_library_fetches ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IRealFetch
	for rows.Next() {
		var f IRealFetch
		if err := rows.Scan(&f.ID, &f.SourceURL, &f.Title, &f.RawBody); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// IRealSong is one cached, normalized iReal Pro chart.
type IRealSong struct {
	ID                          int64
	Title, Composer, Style, Key string
	TimeSigNum, TimeSigDen      int
	GridText                    string
	ReactionCount               int
	SourceURL                   string
}

// FindIRealSong looks up the best cached iReal chart for a title (exact
// normalized match — case/whitespace-insensitive only, no fuzzy
// matching, since the cache is meant to be checked before falling back
// to a live UG-only import, not to guess at a loose match) — highest
// reaction_count wins among duplicates (the same tune shared in several
// threads). artist is accepted for the caller's convenience but not
// filtered on: iReal's own "composer" field is inconsistently filled
// (often blank, sometimes a songwriting credit rather than the
// performing artist UG/our catalog uses), so title alone is the
// reliable key; a caller that wants extra confidence can compare the
// returned Composer itself.
func (st *Store) FindIRealSong(ctx context.Context, title string) (*IRealSong, error) {
	var s IRealSong
	err := st.db.QueryRow(ctx, `
		SELECT s.id, s.title, s.composer, s.style, s.key, s.time_sig_num, s.time_sig_den,
		       s.grid_text, s.reaction_count, f.source_url
		FROM ireal_songs s
		JOIN ireal_library_fetches f ON f.id = s.fetch_id
		WHERE s.norm_title = lower(trim($1))
		ORDER BY s.reaction_count DESC
		LIMIT 1`, title).Scan(&s.ID, &s.Title, &s.Composer, &s.Style, &s.Key,
		&s.TimeSigNum, &s.TimeSigDen, &s.GridText, &s.ReactionCount, &s.SourceURL)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// CountIRealSongs reports the cache's total size, for coverage reporting.
func (st *Store) CountIRealSongs(ctx context.Context) (int, error) {
	var n int
	err := st.db.QueryRow(ctx, `SELECT count(*) FROM ireal_songs`).Scan(&n)
	return n, err
}
