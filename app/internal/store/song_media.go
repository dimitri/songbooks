package store

import (
	"context"
	"fmt"
)

// SongMedia is what a song listing shows about each song beyond its
// title: its text (to tell chords, lyrics and score tracks apart), whether
// a Guitar Pro file is attached, and its reference recordings.
type SongMedia struct {
	Body   string
	GPFile bool
	Audio  []AudioBadge
}

// AudioBadge is one reference recording of a song, as a listing shows it.
type AudioBadge struct {
	Category AudioCategory
	Kind     string // "full", "backing" or "" — see migration 0063
	Aligned  bool   // has bar sync points or section markers
}

// SongMediaFor returns SongMedia for each of variantIDs, in one round
// trip per table rather than one per song.
func (st *Store) SongMediaFor(ctx context.Context, variantIDs []int64) (map[int64]*SongMedia, error) {
	out := make(map[int64]*SongMedia, len(variantIDs))
	if len(variantIDs) == 0 {
		return out, nil
	}
	rows, err := st.db.Query(ctx, `
		SELECT s.id, s.body, EXISTS (SELECT 1 FROM song_gp_source_file g WHERE g.variant_id = s.id)
		FROM songs s WHERE s.id = ANY($1)`, variantIDs)
	if err != nil {
		return nil, fmt.Errorf("song media: %w", err)
	}
	for rows.Next() {
		var id int64
		m := &SongMedia{}
		if err := rows.Scan(&id, &m.Body, &m.GPFile); err != nil {
			rows.Close()
			return nil, err
		}
		out[id] = m
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = st.db.Query(ctx, `
		SELECT a.variant_id, a.category, a.content_kind,
		       EXISTS (SELECT 1 FROM gp_bar_sync_point p WHERE p.song_audio_id = a.id)
		       OR EXISTS (SELECT 1 FROM song_audio_markers m WHERE m.song_audio_id = a.id)
		FROM song_audio a WHERE a.variant_id = ANY($1) ORDER BY a.variant_id, a.category`, variantIDs)
	if err != nil {
		return nil, fmt.Errorf("song media audio: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var b AudioBadge
		if err := rows.Scan(&id, &b.Category, &b.Kind, &b.Aligned); err != nil {
			return nil, err
		}
		if m := out[id]; m != nil {
			m.Audio = append(m.Audio, b)
		}
	}
	return out, rows.Err()
}

// SetSongAudioKind records what a reference recording is ("full",
// "backing" or "" for unknown).
func (st *Store) SetSongAudioKind(ctx context.Context, variantID int64, category AudioCategory, kind string) error {
	tag, err := st.db.Exec(ctx, `UPDATE song_audio SET content_kind = $3 WHERE variant_id = $1 AND category = $2`, variantID, category, kind)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
