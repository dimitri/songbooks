package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// SongAudio is one reference-recording MP3 for a song variant — see
// app/docs/mp3-reference-audio.md for why this is stored as bytea in
// Postgres rather than a filesystem volume or object storage.
type SongAudio struct {
	ID               int64
	VariantID        int64
	ContentType      string
	ByteSize         int64
	SHA256           string
	SourceURL        string
	CreatedAt        time.Time
	CreatedByPersonID *int64
}

// AudioMarker ties a playback timestamp to one of the song's own sections
// (by index into its parsed Section list — see song_audio_markers' own
// migration comment for why there's no separate per-section id).
type AudioMarker struct {
	ID           int64
	SectionIndex int
	TimeSeconds  float64
}

// GetSongAudioMeta loads a variant's audio row WITHOUT its bytes — for
// pages that just need to know "is there audio, how big, what markers,"
// not stream it.
func (st *Store) GetSongAudioMeta(ctx context.Context, variantID int64) (*SongAudio, error) {
	var a SongAudio
	err := st.db.QueryRow(ctx, `
		SELECT id, variant_id, content_type, byte_size, sha256, source_url, created_at, created_by_person_id
		FROM song_audio WHERE variant_id = $1`, variantID).
		Scan(&a.ID, &a.VariantID, &a.ContentType, &a.ByteSize, &a.SHA256, &a.SourceURL, &a.CreatedAt, &a.CreatedByPersonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// GetSongAudioData loads one audio row's actual bytes, by its own id (not
// variant_id) — used by the serving handler, which already resolved the
// id from GetSongAudioMeta and wants a second, cheap round-trip rather
// than ever holding every variant's audio bytes in memory at once.
func (st *Store) GetSongAudioData(ctx context.Context, audioID int64) ([]byte, string, error) {
	var data []byte
	var contentType string
	err := st.db.QueryRow(ctx, `SELECT data, content_type FROM song_audio WHERE id = $1`, audioID).Scan(&data, &contentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	return data, contentType, err
}

// SetSongAudio replaces (or creates) a variant's reference audio — one
// row per variant (song_audio_variant_idx), so a re-upload overwrites
// the previous recording rather than accumulating takes.
func (st *Store) SetSongAudio(ctx context.Context, variantID int64, data []byte, contentType, sha256, sourceURL string, createdByPersonID int64) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx, `
		INSERT INTO song_audio (variant_id, content_type, byte_size, sha256, source_url, data, created_by_person_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (variant_id) DO UPDATE SET
			content_type = EXCLUDED.content_type, byte_size = EXCLUDED.byte_size,
			sha256 = EXCLUDED.sha256, source_url = EXCLUDED.source_url,
			data = EXCLUDED.data, created_at = now(), created_by_person_id = EXCLUDED.created_by_person_id
		RETURNING id`,
		variantID, contentType, len(data), sha256, sourceURL, data, createdByPersonID).Scan(&id)
	return id, err
}

// DeleteSongAudio removes a variant's reference audio (and, via ON DELETE
// CASCADE, its markers).
func (st *Store) DeleteSongAudio(ctx context.Context, variantID int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM song_audio WHERE variant_id = $1`, variantID)
	return err
}

// ListAudioMarkers returns a song_audio row's markers, ordered by
// playback position — the order the structure rail / highlight-tracking
// in the player actually wants them in.
func (st *Store) ListAudioMarkers(ctx context.Context, songAudioID int64) ([]AudioMarker, error) {
	rows, err := st.db.Query(ctx, `
		SELECT id, section_index, time_seconds FROM song_audio_markers
		WHERE song_audio_id = $1 ORDER BY time_seconds`, songAudioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AudioMarker
	for rows.Next() {
		var m AudioMarker
		if err := rows.Scan(&m.ID, &m.SectionIndex, &m.TimeSeconds); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetAudioMarker sets (or moves) the marker for one section index —
// one marker per section per audio row (song_audio_markers_unique), so
// re-tapping "set marker here" on the same section just updates its time.
func (st *Store) SetAudioMarker(ctx context.Context, songAudioID int64, sectionIndex int, timeSeconds float64) error {
	_, err := st.db.Exec(ctx, `
		INSERT INTO song_audio_markers (song_audio_id, section_index, time_seconds)
		VALUES ($1, $2, $3)
		ON CONFLICT (song_audio_id, section_index) DO UPDATE SET time_seconds = EXCLUDED.time_seconds`,
		songAudioID, sectionIndex, timeSeconds)
	return err
}

// DeleteAudioMarker removes one section's marker.
func (st *Store) DeleteAudioMarker(ctx context.Context, songAudioID int64, sectionIndex int) error {
	_, err := st.db.Exec(ctx, `DELETE FROM song_audio_markers WHERE song_audio_id = $1 AND section_index = $2`, songAudioID, sectionIndex)
	return err
}
