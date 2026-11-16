package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// FileChunkSize is the size of one stored piece of a songbook file
// (migration 0070): small enough to stream a large PDF a piece at a time.
const FileChunkSize = 1 << 20

// SongbookFile is one file of a songbook's download area: the book's PDF,
// an original Guitar Pro file, a backing track... VariantID, when set,
// is the song of the songbook it goes with.
type SongbookFile struct {
	ID          int64
	SongbookID  int64
	VariantID   *int64
	SongTitle   string // the song it goes with, if any
	Kind        string // "book", "gp", "audio" or "other"
	Filename    string
	Title       string
	ContentType string
	ByteSize    int64
	SHA256      string
	CreatedAt   time.Time
}

// Label is the file's title, or when it has none its file name without
// the extension, the lesson number and an author's underscores.
func (f SongbookFile) Label() string {
	if f.Title != "" {
		return f.Title
	}
	name := strings.TrimSuffix(f.Filename, filepath.Ext(f.Filename))
	if n := f.Number(); n != "" {
		name = strings.TrimLeft(name, "0123456789")
	}
	return strings.Trim(name, "_ ")
}

// Ext is the file's extension, as a label: "PDF".
func (f SongbookFile) Ext() string {
	return strings.ToUpper(strings.TrimPrefix(filepath.Ext(f.Filename), "."))
}

// Number is the lesson number an author's file name starts with ("07_…"),
// or "".
func (f SongbookFile) Number() string {
	n := 0
	for n < len(f.Filename) && n < 3 && f.Filename[n] >= '0' && f.Filename[n] <= '9' {
		n++
	}
	if n == 0 || n == len(f.Filename) || (f.Filename[n] != '_' && f.Filename[n] != ' ') {
		return ""
	}
	return strings.TrimLeft(f.Filename[:n], "0")
}

// AddSongbookFile stores a file in songbookID's download area, reading it
// from r. The same file added twice to a songbook is kept once: existed is
// then true, and id is the copy already there.
func (st *Store) AddSongbookFile(ctx context.Context, songbookID int64, variantID *int64, kind, filename, title, contentType string, r io.Reader, createdByPersonID int64) (id int64, existed bool, err error) {
	var creator *int64
	if createdByPersonID != 0 {
		creator = &createdByPersonID
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)
	// The hash is only known once the file is read: hold the row under a
	// placeholder until then.
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return 0, false, err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO songbook_files (songbook_id, variant_id, kind, filename, title, content_type, byte_size, sha256, created_by_person_id)
		VALUES ($1, $2, $3, $4, $5, $6, 0, $7, $8) RETURNING id`,
		songbookID, variantID, kind, filename, title, contentType, "pending-"+hex.EncodeToString(nonce[:]), creator).Scan(&id); err != nil {
		return 0, false, err
	}
	h := sha256.New()
	buf := make([]byte, FileChunkSize)
	var size int64
	for n := 0; ; n++ {
		k, rerr := io.ReadFull(r, buf)
		if k > 0 {
			h.Write(buf[:k])
			size += int64(k)
			if _, err := tx.Exec(ctx, `INSERT INTO songbook_file_chunks (file_id, n, data) VALUES ($1, $2, $3)`, id, n, buf[:k]); err != nil {
				return 0, false, err
			}
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			return 0, false, rerr
		}
	}
	if size == 0 {
		return 0, false, fmt.Errorf("%s is empty", filename)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	var existing int64
	err = tx.QueryRow(ctx, `SELECT id FROM songbook_files WHERE songbook_id = $1 AND sha256 = $2`, songbookID, sum).Scan(&existing)
	if err == nil {
		return existing, true, nil // the deferred rollback drops this copy
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE songbook_files SET byte_size = $2, sha256 = $3 WHERE id = $1`, id, size, sum); err != nil {
		return 0, false, err
	}
	return id, false, tx.Commit(ctx)
}

const songbookFileCols = `f.id, f.songbook_id, f.variant_id, coalesce(s.title, ''), f.kind, f.filename, f.title,
	f.content_type, f.byte_size, f.sha256, f.created_at`

func scanSongbookFile(row pgx.Row) (SongbookFile, error) {
	var f SongbookFile
	err := row.Scan(&f.ID, &f.SongbookID, &f.VariantID, &f.SongTitle, &f.Kind, &f.Filename, &f.Title,
		&f.ContentType, &f.ByteSize, &f.SHA256, &f.CreatedAt)
	return f, err
}

// ListSongbookFiles lists the download area of each of songbookIDs: the
// book files first, then by file name (an author's own numbering).
func (st *Store) ListSongbookFiles(ctx context.Context, songbookIDs []int64) ([]SongbookFile, error) {
	rows, err := st.db.Query(ctx, `
		SELECT `+songbookFileCols+`
		FROM songbook_files f LEFT JOIN songs s ON s.id = f.variant_id
		WHERE f.songbook_id = ANY($1)
		ORDER BY f.songbook_id, f.kind <> 'book', f.filename COLLATE "C", f.id`, songbookIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SongbookFile
	for rows.Next() {
		f, err := scanSongbookFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetSongbookFile is one file of a download area, without its bytes.
func (st *Store) GetSongbookFile(ctx context.Context, id int64) (SongbookFile, error) {
	f, err := scanSongbookFile(st.db.QueryRow(ctx, `
		SELECT `+songbookFileCols+`
		FROM songbook_files f LEFT JOIN songs s ON s.id = f.variant_id
		WHERE f.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return f, fmt.Errorf("no such file %d: %w", id, ErrNotFound)
	}
	return f, err
}

// SongbookHasVariant reports whether variantID is in songbookID.
func (st *Store) SongbookHasVariant(ctx context.Context, songbookID, variantID int64) (bool, error) {
	var ok bool
	err := st.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM songbook_entries WHERE songbook_id = $1 AND variant_id = $2)`,
		songbookID, variantID).Scan(&ok)
	return ok, err
}

// DeleteSongbookFile removes a file from its download area.
func (st *Store) DeleteSongbookFile(ctx context.Context, id int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM songbook_files WHERE id = $1`, id)
	return err
}

// OpenSongbookFile reads a stored file a chunk at a time; it seeks, so
// http.ServeContent can answer range requests from it.
func (st *Store) OpenSongbookFile(ctx context.Context, f SongbookFile) io.ReadSeeker {
	return &fileChunkReader{st: st, ctx: ctx, id: f.ID, size: f.ByteSize, n: -1}
}

type fileChunkReader struct {
	st    *Store
	ctx   context.Context
	id    int64
	size  int64
	off   int64
	n     int // the chunk in buf, -1 for none
	chunk []byte
}

func (r *fileChunkReader) Read(p []byte) (int, error) {
	if r.off >= r.size {
		return 0, io.EOF
	}
	n := int(r.off / FileChunkSize)
	if n != r.n {
		if err := r.st.db.QueryRow(r.ctx, `SELECT data FROM songbook_file_chunks WHERE file_id = $1 AND n = $2`, r.id, n).Scan(&r.chunk); err != nil {
			return 0, err
		}
		r.n = n
	}
	at := int(r.off - int64(n)*FileChunkSize)
	if at >= len(r.chunk) {
		return 0, io.ErrUnexpectedEOF
	}
	k := copy(p, r.chunk[at:])
	r.off += int64(k)
	return k, nil
}

func (r *fileChunkReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += r.off
	case io.SeekEnd:
		offset += r.size
	default:
		return 0, errors.New("bad whence")
	}
	if offset < 0 {
		return 0, errors.New("negative position")
	}
	r.off = offset
	return offset, nil
}

// FileKind sorts a download by its name: a PDF that goes with no song is
// the book itself.
func FileKind(name string, withSong bool) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".gp", ".gpx", ".gp5", ".gp4", ".gp3":
		return "gp"
	case ".mp3", ".wav", ".m4a", ".ogg", ".flac", ".aac":
		return "audio"
	case ".pdf":
		if !withSong {
			return "book"
		}
	}
	return "other"
}

// FileContentType is the type a download is served with.
func FileContentType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".gp", ".gpx", ".gp5", ".gp4", ".gp3":
		return "application/octet-stream"
	case ".mp3":
		return "audio/mpeg"
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}
