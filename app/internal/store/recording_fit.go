package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"app/internal/chartalign"
)

// Whether a song follows its reference recording (docs/ownership-model.md
// §3, flags only), kept per song (song_recording_fit, migration 0116):
// songimport.RecordingFit reads it from the recording's analysis
// (migration 0106) and the chart; it's read again when the song has a new
// revision or its recording a new analysis.

// RecordingFit is one song's kept verdict: "follows", "differs" or
// "unknown", and the reasons it differs (songimport.FitReason, JSON).
type RecordingFit struct {
	Fit    string
	Detail json.RawMessage
}

// FitInput is a song whose verdict is to be read (again): its version, its
// reference recording's analysis, and its chart.
type FitInput struct {
	SongID   int64
	Version  int
	AudioID  int64
	SHA      string
	Body     string
	Analysis *chartalign.Recording
}

// RecordingFits is the kept verdict of each of songIDs that has one for
// its current version and analysis, and the ids of those whose reference
// recording has an analysis but no verdict yet for it (stale).
func (st *Store) RecordingFits(ctx context.Context, songIDs []int64) (map[int64]RecordingFit, []int64, error) {
	out := map[int64]RecordingFit{}
	if len(songIDs) == 0 {
		return out, nil, nil
	}
	rows, err := st.db.Query(ctx, `
		SELECT s.id, f.fit, f.detail
		FROM songs s
		JOIN song_audio a ON a.variant_id = s.id AND a.category = 'youtube'
		JOIN song_audio_analysis x ON x.song_audio_id = a.id AND x.source_sha256 = a.sha256
		LEFT JOIN song_recording_fit f ON f.song_id = s.id AND f.song_version = s.version
		                              AND f.song_audio_id = a.id AND f.source_sha256 = a.sha256
		WHERE s.id = ANY($1)`, songIDs)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var stale []int64
	for rows.Next() {
		var id int64
		var fit, detail *string
		if err := rows.Scan(&id, &fit, &detail); err != nil {
			return nil, nil, err
		}
		if fit == nil {
			stale = append(stale, id)
			continue
		}
		f := RecordingFit{Fit: *fit}
		if detail != nil && *detail != "" {
			f.Detail = json.RawMessage(*detail)
		}
		out[id] = f
	}
	return out, stale, rows.Err()
}

// FitInputOf loads what reading songID's verdict takes; nil when its
// reference recording has no analysis.
func (st *Store) FitInputOf(ctx context.Context, songID int64) (*FitInput, error) {
	in := &FitInput{SongID: songID}
	var data []byte
	err := st.db.QueryRow(ctx, `
		SELECT s.version, s.body, a.id, a.sha256, x.data
		FROM songs s
		JOIN song_audio a ON a.variant_id = s.id AND a.category = 'youtube'
		JOIN song_audio_analysis x ON x.song_audio_id = a.id AND x.source_sha256 = a.sha256
		WHERE s.id = $1`, songID).Scan(&in.Version, &in.Body, &in.AudioID, &in.SHA, &data)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var r chartalign.Recording
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	in.Analysis = &r
	return in, nil
}

// SaveRecordingFit keeps a verdict read from in.
func (st *Store) SaveRecordingFit(ctx context.Context, in *FitInput, fit string, detail any) error {
	raw := []byte("")
	if detail != nil {
		b, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		raw = b
	}
	_, err := st.db.Exec(ctx, `
		INSERT INTO song_recording_fit (song_id, song_version, song_audio_id, source_sha256, fit, detail)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (song_id) DO UPDATE SET song_version = EXCLUDED.song_version, song_audio_id = EXCLUDED.song_audio_id,
		  source_sha256 = EXCLUDED.source_sha256, fit = EXCLUDED.fit, detail = EXCLUDED.detail, checked_at = now()`,
		in.SongID, in.Version, in.AudioID, in.SHA, fit, string(raw))
	return err
}
