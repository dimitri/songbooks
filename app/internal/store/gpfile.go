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
}

// GetSongGPSourceFileMeta loads a variant's GP source file metadata
// (without its bytes) — used by pages that just need to know whether a
// download link should show, and what filename to offer.
func (st *Store) GetSongGPSourceFileMeta(ctx context.Context, variantID int64) (*SongGPSourceFile, error) {
	var f SongGPSourceFile
	err := st.db.QueryRow(ctx, `
		SELECT variant_id, filename, content_type, byte_size, sha256, created_at, created_by_person_id
		FROM song_gp_source_file WHERE variant_id = $1`, variantID).
		Scan(&f.VariantID, &f.Filename, &f.ContentType, &f.ByteSize, &f.SHA256, &f.CreatedAt, &f.CreatedByPersonID)
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
// nothing.
func (st *Store) SetSongGPSourceFile(ctx context.Context, variantID int64, filename, contentType string, data []byte, sha256 string, createdByPersonID int64) error {
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
		INSERT INTO song_gp_source_file_version (variant_id, filename, content_type, byte_size, sha256, data, created_at, created_by_person_id)
		SELECT variant_id, filename, content_type, byte_size, sha256, data, created_at, created_by_person_id
		FROM song_gp_source_file WHERE variant_id = $1 AND sha256 <> $2`, variantID, sha256); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO song_gp_source_file (variant_id, filename, content_type, byte_size, sha256, data, created_by_person_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (variant_id) DO UPDATE SET
			filename = EXCLUDED.filename, content_type = EXCLUDED.content_type,
			byte_size = EXCLUDED.byte_size, sha256 = EXCLUDED.sha256,
			data = EXCLUDED.data, created_at = now(), created_by_person_id = EXCLUDED.created_by_person_id
		WHERE song_gp_source_file.sha256 <> EXCLUDED.sha256`,
		variantID, filename, contentType, len(data), sha256, data, creator); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
		INSERT INTO song_gp_source_file (variant_id, filename, content_type, byte_size, sha256, data, created_at, created_by_person_id)
		SELECT $2, filename, content_type, byte_size, sha256, data, created_at, created_by_person_id
		FROM song_gp_source_file WHERE variant_id = $1`, fromID, toID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO song_gp_source_file_version (variant_id, filename, content_type, byte_size, sha256, data, created_at, replaced_at, created_by_person_id)
		SELECT $2, filename, content_type, byte_size, sha256, data, created_at, replaced_at, created_by_person_id
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
