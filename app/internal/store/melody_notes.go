package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/audioanalysis"
)

// The lead voice's notes (migration 0126): found from a recording's
// vocals part in the background (httpapi/melody_sheet.go), once per split
// and analyzer, and written as a score with the song's lyrics under them
// at request time (internal/melody, docs/melody-sheet.md).

// MelodyNotesJob is a vocals part whose notes are to be found.
type MelodyNotesJob struct {
	SongAudioID int64
	StemSHA256  string
	StemAt      time.Time
}

// NextMelodyNotes is a vocals part with no notes found by analyzer for it
// as it is now (never looked at, split again since, or looked at by
// another analyzer); nil when there's none. Not a Guitar Pro backing
// track's (not split, migration 0123). The newest first.
func (st *Store) NextMelodyNotes(ctx context.Context, analyzer string) (*MelodyNotesJob, error) {
	var j MelodyNotesJob
	err := st.db.QueryRow(ctx, `
		SELECT s.song_audio_id, s.source_sha256, s.created_at
		FROM song_audio_stems s
		JOIN song_audio a ON a.id = s.song_audio_id AND a.sha256 = s.source_sha256 AND a.category <> 'gp'
		LEFT JOIN song_melody_notes m ON m.song_audio_id = s.song_audio_id
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

// SaveMelodyNotes keeps what was found for a job (ok false: no melody),
// and how long it took.
func (st *Store) SaveMelodyNotes(ctx context.Context, j MelodyNotesJob, analyzer string, ok bool, notes []audioanalysis.SungNote, voiced, took float64) error {
	if notes == nil {
		notes = []audioanalysis.SungNote{}
	}
	data, err := json.Marshal(notes)
	if err != nil {
		return err
	}
	_, err = st.db.Exec(ctx, `
		INSERT INTO song_melody_notes (song_audio_id, stem_sha256, stem_created_at, analyzer, ok, notes, voiced_seconds, took_seconds)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (song_audio_id) DO UPDATE SET stem_sha256 = EXCLUDED.stem_sha256, stem_created_at = EXCLUDED.stem_created_at,
			analyzer = EXCLUDED.analyzer, ok = EXCLUDED.ok, notes = EXCLUDED.notes,
			voiced_seconds = EXCLUDED.voiced_seconds, took_seconds = EXCLUDED.took_seconds, created_at = now()`,
		j.SongAudioID, j.StemSHA256, j.StemAt, analyzer, ok, data, voiced, took)
	return err
}

// SongMelody is where a song's melody stands: the recording it's taken
// from (the reference recording, YouTube, first, then the band's; one
// whose notes are found before one still waiting), whether that one is
// split, and its notes when they're found for its vocals part as it is
// now by analyzer.
type SongMelody struct {
	AudioID  int64
	Category AudioCategory
	// Split: its vocals part is there. Found: its notes were looked for
	// (OK: and there's a melody, Notes).
	Split, Found, OK bool
	Notes            []audioanalysis.SungNote
}

// GetSongMelody is a song's melody; nil when it has no recording a
// melody can come from.
func (st *Store) GetSongMelody(ctx context.Context, variantID int64, analyzer string) (*SongMelody, error) {
	var m SongMelody
	var data []byte
	err := st.db.QueryRow(ctx, `
		SELECT a.id, a.category, s.song_audio_id IS NOT NULL, mn.song_audio_id IS NOT NULL, coalesce(mn.ok, false), mn.notes
		FROM song_audio a
		LEFT JOIN song_audio_stems s ON s.song_audio_id = a.id AND s.stem = 'vocals' AND s.source_sha256 = a.sha256
		LEFT JOIN song_melody_notes mn ON mn.song_audio_id = a.id AND mn.stem_created_at = s.created_at AND mn.stem_sha256 = s.source_sha256 AND mn.analyzer = $2
		WHERE a.variant_id = $1 AND a.category IN ('youtube', 'band')
		ORDER BY coalesce(mn.ok, false) DESC, s.song_audio_id IS NOT NULL DESC, CASE a.category WHEN 'youtube' THEN 0 ELSE 1 END
		LIMIT 1`, variantID, analyzer).Scan(&m.AudioID, &m.Category, &m.Split, &m.Found, &m.OK, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if m.OK && len(data) > 0 {
		if err := json.Unmarshal(data, &m.Notes); err != nil {
			return nil, err
		}
	}
	return &m, nil
}

// MelodyLyricShift is an editor's correction of a lyric line's place on
// the melody.
type MelodyLyricShift struct {
	Section, Line int
	Text          string
	Shift         int
}

// ListMelodyLyricShifts is a song's corrections.
func (st *Store) ListMelodyLyricShifts(ctx context.Context, variantID int64) ([]MelodyLyricShift, error) {
	rows, err := st.db.Query(ctx, `
		SELECT section_index, line_index, line_text, shift FROM song_melody_lyric_shifts
		WHERE variant_id = $1 ORDER BY section_index, line_index`, variantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MelodyLyricShift
	for rows.Next() {
		var s MelodyLyricShift
		if err := rows.Scan(&s.Section, &s.Line, &s.Text, &s.Shift); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetMelodyLyricShift keeps a line's correction (0 takes it back).
func (st *Store) SetMelodyLyricShift(ctx context.Context, variantID int64, s MelodyLyricShift, by int64) error {
	if s.Shift == 0 {
		_, err := st.db.Exec(ctx, `DELETE FROM song_melody_lyric_shifts WHERE variant_id = $1 AND section_index = $2 AND line_index = $3`, variantID, s.Section, s.Line)
		return err
	}
	var who *int64
	if by != 0 {
		who = &by
	}
	_, err := st.db.Exec(ctx, `
		INSERT INTO song_melody_lyric_shifts (variant_id, section_index, line_index, line_text, shift, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (variant_id, section_index, line_index) DO UPDATE SET line_text = EXCLUDED.line_text, shift = EXCLUDED.shift,
			updated_by = EXCLUDED.updated_by, updated_at = now()`,
		variantID, s.Section, s.Line, s.Text, s.Shift, who)
	return err
}

// SongsWithMelody are those of the songs whose melody is found (notes
// for the vocals part, as it is now, of a recording a melody comes
// from, found by analyzer): a songbook's rows show their lowest and
// highest bars.
func (st *Store) SongsWithMelody(ctx context.Context, variantIDs []int64, analyzer string) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(variantIDs) == 0 {
		return out, nil
	}
	rows, err := st.db.Query(ctx, `
		SELECT DISTINCT a.variant_id FROM song_audio a
		JOIN song_audio_stems s ON s.song_audio_id = a.id AND s.stem = 'vocals' AND s.source_sha256 = a.sha256
		JOIN song_melody_notes mn ON mn.song_audio_id = a.id AND mn.stem_created_at = s.created_at AND mn.ok AND mn.analyzer = $2
		WHERE a.variant_id = ANY($1) AND a.category IN ('youtube', 'band')`, variantIDs, analyzer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// MelodyStamp is what a song's melody bars are drawn from, as a string
// that changes when it does: the song's version and its melody's notes
// (when found). "" for no such song.
func (st *Store) MelodyStamp(ctx context.Context, variantID int64) (string, error) {
	var stamp string
	err := st.db.QueryRow(ctx, `
		SELECT s.version::text || '/' || coalesce((SELECT max(mn.created_at)::text FROM song_audio a
		                                           JOIN song_melody_notes mn ON mn.song_audio_id = a.id
		                                           WHERE a.variant_id = s.id), '')
		FROM songs s WHERE s.id = $1`, variantID).Scan(&stamp)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return stamp, err
}
