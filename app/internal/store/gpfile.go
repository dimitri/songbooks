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

// SetSongGPSourceFile replaces (or creates) a variant's GP source file —
// one per variant, so re-running the import wizard with a new upload
// overwrites the one already there.
func (st *Store) SetSongGPSourceFile(ctx context.Context, variantID int64, filename, contentType string, data []byte, sha256 string, createdByPersonID int64) error {
	_, err := st.db.Exec(ctx, `
		INSERT INTO song_gp_source_file (variant_id, filename, content_type, byte_size, sha256, data, created_by_person_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (variant_id) DO UPDATE SET
			filename = EXCLUDED.filename, content_type = EXCLUDED.content_type,
			byte_size = EXCLUDED.byte_size, sha256 = EXCLUDED.sha256,
			data = EXCLUDED.data, created_at = now(), created_by_person_id = EXCLUDED.created_by_person_id`,
		variantID, filename, contentType, len(data), sha256, data, createdByPersonID)
	return err
}
