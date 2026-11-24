package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// SongGPSourceFile is the original .gp/.gp3/.gp4/.gp5/.gpx file a GP
// import was run from — see its own migration comment (0057) for why
// this is kept separately from the song_audio backing track and
// song_scores riff JSON the import also extracts.
type SongGPSourceFile struct {
	VariantID         int64
	Filename          string
	ContentType       string
	ByteSize          int64
	SHA256            string
	CreatedAt         time.Time
	CreatedByPersonID *int64
	// Facts is what the file says about the song, as JSON (see
	// internal/gpfacts); nil until read.
	Facts []byte
	// TrackPrograms is the instrument each track plays with, as the
	// song's editors chose it: a JSON array of {track, name, program}
	// (migration 0118); nil when none was chosen.
	TrackPrograms []byte
}

// GetSongGPSourceFileMeta loads a variant's GP source file metadata
// (without its bytes) — used by pages that just need to know whether a
// download link should show, and what filename to offer.
func (st *Store) GetSongGPSourceFileMeta(ctx context.Context, variantID int64) (*SongGPSourceFile, error) {
	var f SongGPSourceFile
	err := st.db.QueryRow(ctx, `
		SELECT variant_id, filename, content_type, byte_size, sha256, created_at, created_by_person_id, facts, track_programs
		FROM song_gp_source_file WHERE variant_id = $1`, variantID).
		Scan(&f.VariantID, &f.Filename, &f.ContentType, &f.ByteSize, &f.SHA256, &f.CreatedAt, &f.CreatedByPersonID, &f.Facts, &f.TrackPrograms)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// GetSongGPSourceFileData loads a variant's GP source file bytes, for
// the download handler — a second, cheap round-trip after
// GetSongGPSourceFileMeta, same split GetSongAudioData uses, so the
// bytes are never held in memory for a request that only needs the
// filename/size.
func (st *Store) GetSongGPSourceFileData(ctx context.Context, variantID int64) ([]byte, string, string, error) {
	var data []byte
	var contentType, filename string
	err := st.db.QueryRow(ctx, `SELECT data, content_type, filename FROM song_gp_source_file WHERE variant_id = $1`, variantID).
		Scan(&data, &contentType, &filename)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", "", ErrNotFound
	}
	return data, contentType, filename, err
}

// SetSongGPSourceFile sets a variant's current GP source file. A
// different file replacing the current one (a re-import after editing it
// in Guitar Pro) keeps the old one as a version (see
// ListSongGPSourceFileVersions); uploading the same file again changes
// nothing, except filling in facts the stored copy doesn't have yet.
// facts is the file's facts as JSON (static/js/gp-facts.js), or nil.
func (st *Store) SetSongGPSourceFile(ctx context.Context, variantID int64, filename, contentType string, data []byte, sha256 string, createdByPersonID int64, facts []byte) error {
	// Same nullable-FK fix as SetSongAudio: 0 means "unknown/unrecorded"
	// (cmd/songimport only), never a real literal person id.
	var creator *int64
	if createdByPersonID != 0 {
		creator = &createdByPersonID
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO song_gp_source_file_version (variant_id, filename, content_type, byte_size, sha256, data, created_at, created_by_person_id, facts)
		SELECT variant_id, filename, content_type, byte_size, sha256, data, created_at, created_by_person_id, facts
		FROM song_gp_source_file WHERE variant_id = $1 AND sha256 <> $2`, variantID, sha256); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO song_gp_source_file (variant_id, filename, content_type, byte_size, sha256, data, created_by_person_id, facts)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (variant_id) DO UPDATE SET
			filename = EXCLUDED.filename, content_type = EXCLUDED.content_type,
			byte_size = EXCLUDED.byte_size, sha256 = EXCLUDED.sha256,
			data = EXCLUDED.data, created_at = now(), created_by_person_id = EXCLUDED.created_by_person_id,
			facts = EXCLUDED.facts
		WHERE song_gp_source_file.sha256 <> EXCLUDED.sha256`,
		variantID, filename, contentType, len(data), sha256, data, creator, nullJSON(facts)); err != nil {
		return err
	}
	if facts != nil {
		if _, err := tx.Exec(ctx, `UPDATE song_gp_source_file SET facts = $3 WHERE variant_id = $1 AND sha256 = $2 AND facts IS NULL`,
			variantID, sha256, nullJSON(facts)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// SetSongGPSourceFileFacts fills in the facts of a variant's current file
// when it has none yet and sha256 is still that file's; ok is false
// otherwise. Any reader's browser may send them, so facts already stored
// are never replaced this way.
func (st *Store) SetSongGPSourceFileFacts(ctx context.Context, variantID int64, sha256 string, facts []byte) (bool, error) {
	tag, err := st.db.Exec(ctx, `UPDATE song_gp_source_file SET facts = $3 WHERE variant_id = $1 AND sha256 = $2 AND facts IS NULL`,
		variantID, sha256, nullJSON(facts))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// SetSongGPTrackPrograms sets the instruments a song's file plays with
// (a JSON array of {track, name, program}, already checked; nil or empty
// clears them). ok is false when the song has no file.
func (st *Store) SetSongGPTrackPrograms(ctx context.Context, variantID int64, programs []byte) (bool, error) {
	if len(programs) == 0 || string(programs) == "[]" {
		programs = nil
	}
	tag, err := st.db.Exec(ctx, `UPDATE song_gp_source_file SET track_programs = $2 WHERE variant_id = $1`, variantID, nullJSON(programs))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// nullJSON passes raw JSON to a jsonb column, nil as NULL.
func nullJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

// GPDriftAcks lists the differences between a variant's text and its
// Guitar Pro file someone chose to keep: fact -> the sig it was kept on.
func (st *Store) GPDriftAcks(ctx context.Context, variantID int64) (map[string]string, error) {
	rows, err := st.db.Query(ctx, `SELECT fact, sig FROM gp_drift_ack WHERE variant_id = $1`, variantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var fact, sig string
		if err := rows.Scan(&fact, &sig); err != nil {
			return nil, err
		}
		out[fact] = sig
	}
	return out, rows.Err()
}

// AckGPDrift keeps one difference (fact, on the values sig hashes); an
// empty sig forgets it.
func (st *Store) AckGPDrift(ctx context.Context, variantID int64, fact, sig string, personID int64) error {
	if sig == "" {
		_, err := st.db.Exec(ctx, `DELETE FROM gp_drift_ack WHERE variant_id = $1 AND fact = $2`, variantID, fact)
		return err
	}
	_, err := st.db.Exec(ctx, `
		INSERT INTO gp_drift_ack (variant_id, fact, sig, person_id) VALUES ($1, $2, $3, $4)
		ON CONFLICT (variant_id, fact) DO UPDATE SET sig = EXCLUDED.sig, person_id = EXCLUDED.person_id, created_at = now()`,
		variantID, fact, sig, personID)
	return err
}

// SongGPSourceFileVersion is an earlier GP file of a variant, replaced by
// a newer upload at ReplacedAt.
type SongGPSourceFileVersion struct {
	ID         int64
	Filename   string
	ByteSize   int64
	SHA256     string
	CreatedAt  time.Time
	ReplacedAt time.Time
}

// ListSongGPSourceFileVersions lists a variant's earlier GP files, newest
// first (metadata only).
func (st *Store) ListSongGPSourceFileVersions(ctx context.Context, variantID int64) ([]SongGPSourceFileVersion, error) {
	rows, err := st.db.Query(ctx, `
		SELECT id, filename, byte_size, sha256, created_at, replaced_at
		FROM song_gp_source_file_version WHERE variant_id = $1
		ORDER BY replaced_at DESC, id DESC`, variantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SongGPSourceFileVersion
	for rows.Next() {
		var v SongGPSourceFileVersion
		if err := rows.Scan(&v.ID, &v.Filename, &v.ByteSize, &v.SHA256, &v.CreatedAt, &v.ReplacedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetSongGPSourceFileVersionData loads one earlier GP file's bytes; the
// variant id guards against reading another song's file by its id.
func (st *Store) GetSongGPSourceFileVersionData(ctx context.Context, variantID, versionID int64) ([]byte, string, string, error) {
	var data []byte
	var contentType, filename string
	err := st.db.QueryRow(ctx, `
		SELECT data, content_type, filename FROM song_gp_source_file_version
		WHERE id = $1 AND variant_id = $2`, versionID, variantID).Scan(&data, &contentType, &filename)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", "", ErrNotFound
	}
	return data, contentType, filename, err
}

// copySongMedia gives a fork its source's Guitar Pro file (current and
// earlier versions) and reference recordings with their markers and bar
// sync points, so a forked GP-imported song keeps everything the
// original had, not just its text.
func copySongMedia(ctx context.Context, tx pgx.Tx, fromID, toID int64) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO song_gp_source_file (variant_id, filename, content_type, byte_size, sha256, data, created_at, created_by_person_id, facts)
		SELECT $2, filename, content_type, byte_size, sha256, data, created_at, created_by_person_id, facts
		FROM song_gp_source_file WHERE variant_id = $1`, fromID, toID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO song_gp_source_file_version (variant_id, filename, content_type, byte_size, sha256, data, created_at, replaced_at, created_by_person_id, facts)
		SELECT $2, filename, content_type, byte_size, sha256, data, created_at, replaced_at, created_by_person_id, facts
		FROM song_gp_source_file_version WHERE variant_id = $1`, fromID, toID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM song_audio WHERE variant_id = $1`, fromID)
	if err != nil {
		return err
	}
	var audioIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		audioIDs = append(audioIDs, id)
	}
	rows.Close()
	for _, oldID := range audioIDs {
		var newID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO song_audio (variant_id, content_type, byte_size, sha256, source_url, data, created_at, created_by_person_id, category, content_kind)
			SELECT $2, content_type, byte_size, sha256, source_url, data, created_at, created_by_person_id, category, content_kind
			FROM song_audio WHERE id = $1 RETURNING id`, oldID, toID).Scan(&newID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO song_audio_markers (song_audio_id, section_index, time_seconds)
			SELECT $2, section_index, time_seconds FROM song_audio_markers WHERE song_audio_id = $1`, oldID, newID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO gp_bar_sync_point (song_audio_id, bar_index, time_seconds)
			SELECT $2, bar_index, time_seconds FROM gp_bar_sync_point WHERE song_audio_id = $1`, oldID, newID); err != nil {
			return err
		}
	}
	return nil
}

// RiffSong is a song whose text still holds notes copied in as Riff JSON
// ("score[Track]: {...}"), with its Guitar Pro file when it has one: what
// cmd/gpmigrate moves to "gp[Track]:" lines.
type RiffSong struct {
	ID       int64  `json:"id"`
	Version  string `json:"version"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Body     string `json:"body"`
	Filename string `json:"filename,omitempty"`
	File     []byte `json:"file,omitempty"`
}

// ListRiffSongs lists every song with a Riff JSON score line, and every
// song whose Guitar Pro file has no facts yet.
func (st *Store) ListRiffSongs(ctx context.Context) ([]RiffSong, error) {
	return st.listRiffSongs(ctx, `s.body ~ '(?n)^score\[[^]]*\]:\s*\{"' OR (g.data IS NOT NULL AND g.facts IS NULL)`)
}

// ListSongsForMigration lists the given songs, as ListRiffSongs does,
// whatever their text holds (gpmigrate dump --ids).
func (st *Store) ListSongsForMigration(ctx context.Context, ids []int64) ([]RiffSong, error) {
	return st.listRiffSongs(ctx, `s.id = ANY($1)`, ids)
}

func (st *Store) listRiffSongs(ctx context.Context, where string, args ...any) ([]RiffSong, error) {
	rows, err := st.db.Query(ctx, `
		SELECT s.id, s.version::text, coalesce(s.title, ''), coalesce(s.artist, ''), s.body,
		       coalesce(g.filename, ''), g.data
		FROM songs s LEFT JOIN song_gp_source_file g ON g.variant_id = s.id
		WHERE `+where+`
		ORDER BY s.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RiffSong
	for rows.Next() {
		var s RiffSong
		if err := rows.Scan(&s.ID, &s.Version, &s.Title, &s.Artist, &s.Body, &s.Filename, &s.File); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
