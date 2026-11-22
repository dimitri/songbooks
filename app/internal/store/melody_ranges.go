package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// A recording's melody range (migration 0112): found from its vocals
// part in the background (httpapi/melody_ranges.go), once per split, and
// drawn over a student's range where their key is chosen (the song
// page's Voice & key panel, the coach's key pickers).

// MelodyRangeJob is a vocals part whose melody range is to be found.
type MelodyRangeJob struct {
	SongAudioID int64
	StemSHA256  string
	StemAt      time.Time
}

// NextMelodyRange is a vocals part with no range found yet by analyzer
// for it as it is now (never looked at, split again since, or looked at
// by another analyzer); nil when there's none. Only a part of the
// recording as it is now: a replaced one's old parts wait for its split. The newest recordings
// first: a song just added is the one someone is waiting for.
func (st *Store) NextMelodyRange(ctx context.Context, analyzer string) (*MelodyRangeJob, error) {
	var j MelodyRangeJob
	err := st.db.QueryRow(ctx, `
		SELECT s.song_audio_id, s.source_sha256, s.created_at
		FROM song_audio_stems s
		JOIN song_audio a ON a.id = s.song_audio_id AND a.sha256 = s.source_sha256
		LEFT JOIN song_melody_ranges m ON m.song_audio_id = s.song_audio_id
		WHERE s.stem = 'vocals'
		  AND (m.song_audio_id IS NULL OR m.stem_created_at <> s.created_at OR m.stem_sha256 <> s.source_sha256 OR m.analyzer <> $1)
		ORDER BY s.created_at DESC LIMIT 1`, analyzer).Scan(&j.SongAudioID, &j.StemSHA256, &j.StemAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// SaveMelodyRange keeps what was found for a job (ok false: no range),
// and how long it took.
func (st *Store) SaveMelodyRange(ctx context.Context, j MelodyRangeJob, analyzer string, ok bool, low, high int, voiced, took float64) error {
	var lo, hi *int
	if ok {
		lo, hi = &low, &high
	}
	_, err := st.db.Exec(ctx, `
		INSERT INTO song_melody_ranges (song_audio_id, stem_sha256, stem_created_at, analyzer, ok, low_midi, high_midi, voiced_seconds, took_seconds)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (song_audio_id) DO UPDATE SET stem_sha256 = EXCLUDED.stem_sha256, stem_created_at = EXCLUDED.stem_created_at,
			analyzer = EXCLUDED.analyzer, ok = EXCLUDED.ok, low_midi = EXCLUDED.low_midi, high_midi = EXCLUDED.high_midi,
			voiced_seconds = EXCLUDED.voiced_seconds, took_seconds = EXCLUDED.took_seconds, created_at = now()`,
		j.SongAudioID, j.StemSHA256, j.StemAt, analyzer, ok, lo, hi, voiced, took)
	return err
}

// SongMelodyRange is a song's melody range, MIDI notes: from the
// recording with a singer the player offers, the reference recording
// (YouTube) first, then the band's, then a Guitar Pro backing (rarely
// sung); only a range found for the recording's vocals part as it is
// now. known false when none is.
func (st *Store) SongMelodyRange(ctx context.Context, variantID int64) (low, high int, known bool, err error) {
	err = st.db.QueryRow(ctx, `
		SELECT m.low_midi, m.high_midi
		FROM song_audio a
		JOIN song_audio_stems s ON s.song_audio_id = a.id AND s.stem = 'vocals' AND s.source_sha256 = a.sha256
		JOIN song_melody_ranges m ON m.song_audio_id = a.id AND m.stem_created_at = s.created_at AND m.ok
		WHERE a.variant_id = $1
		ORDER BY CASE a.category WHEN 'youtube' THEN 0 WHEN 'band' THEN 1 ELSE 2 END LIMIT 1`, variantID).Scan(&low, &high)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, false, nil
	}
	return low, high, err == nil, err
}
