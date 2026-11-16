package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"mime"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// FileChunkSize is the size of one stored piece of a file (migration
// 0070): small enough to stream a large PDF a piece at a time.
const FileChunkSize = 1 << 20

// SongbookFile is one file of a songbook's download area: the book's PDF,
// an original Guitar Pro file, a backing track... in a folder (Path, ""
// at the top) of the songbook's files. VariantID, when set, is the song of
// the songbook it goes with.
type SongbookFile struct {
	ID          int64
	SongbookID  int64
	VariantID   *int64
	SongTitle   string // the song it goes with, if any
	Kind        string // "book", "gp", "audio" or "other"
	Path        string
	Filename    string
	Title       string
	ContentType string
	BlobID      int64
	ByteSize    int64
	SHA256      string
	CRC32       uint32
	ModifiedAt  time.Time
	CreatedAt   time.Time
}

// FullPath is the file's place among the songbook's files: "folder/name".
func (f SongbookFile) FullPath() string {
	return path.Join(f.Path, f.Filename)
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
	return lessonNumber(f.Filename)
}

func lessonNumber(name string) string {
	n := 0
	for n < len(name) && n < 3 && name[n] >= '0' && name[n] <= '9' {
		n++
	}
	if n == 0 || n == len(name) || (name[n] != '_' && name[n] != ' ') {
		return ""
	}
	if s := strings.TrimLeft(name[:n], "0"); s != "" {
		return s
	}
	return "0"
}

// PutFileBlob stores the bytes read from r once: the same content stored
// before is not stored again, and its blob is returned.
func (st *Store) PutFileBlob(ctx context.Context, r io.Reader) (id int64, size int64, err error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)
	// The hash is only known once the content is read: the blob has none
	// until then.
	if err := tx.QueryRow(ctx, `INSERT INTO file_blobs DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		return 0, 0, err
	}
	h, c := sha256.New(), crc32.NewIEEE()
	buf := make([]byte, FileChunkSize)
	for n := 0; ; n++ {
		k, rerr := io.ReadFull(r, buf)
		if k > 0 {
			h.Write(buf[:k])
			c.Write(buf[:k])
			size += int64(k)
			if _, err := tx.Exec(ctx, `INSERT INTO file_blob_chunks (blob_id, n, data) VALUES ($1, $2, $3)`, id, n, buf[:k]); err != nil {
				return 0, 0, err
			}
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			return 0, 0, rerr
		}
	}
	if size == 0 {
		return 0, 0, errors.New("the file is empty")
	}
	sum := hex.EncodeToString(h.Sum(nil))
	var existing int64
	err = tx.QueryRow(ctx, `SELECT id FROM file_blobs WHERE sha256 = $1`, sum).Scan(&existing)
	if err == nil {
		return existing, size, nil // the deferred rollback drops this copy
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE file_blobs SET sha256 = $2, byte_size = $3, crc32 = $4 WHERE id = $1`, id, sum, size, int64(c.Sum32())); err != nil {
		if isUniqueViolation(err) { // the same content, stored meanwhile
			if err := st.db.QueryRow(ctx, `SELECT id FROM file_blobs WHERE sha256 = $1`, sum).Scan(&existing); err == nil {
				return existing, size, nil
			}
		}
		return 0, 0, err
	}
	return id, size, tx.Commit(ctx)
}

// dropBlobIfUnused removes a blob no file points at any more.
func dropBlobIfUnused(ctx context.Context, q dbtx, blobID int64) error {
	_, err := q.Exec(ctx, `
		DELETE FROM file_blobs b WHERE b.id = $1
		  AND NOT EXISTS (SELECT 1 FROM songbook_files f WHERE f.blob_id = b.id)`, blobID)
	return err
}

// NewSongbookFile is what AddSongbookFile needs to know of a file, besides
// its bytes.
type NewSongbookFile struct {
	SongbookID  int64
	VariantID   *int64
	Kind        string // FileKind's, when ""
	Path        string // its folder, "" at the top
	Filename    string
	Title       string
	ModifiedAt  time.Time // now, when zero
	CreatedByID int64
}

// What AddSongbookFile did.
const (
	FileAdded     = "added"
	FileReplaced  = "replaced"
	FileUnchanged = "unchanged"
)

// AddSongbookFile stores a file in a songbook's download area, reading it
// from r. A file of the same name in the same folder is replaced; the
// same content again changes nothing (but which song it goes with, when
// given).
func (st *Store) AddSongbookFile(ctx context.Context, nf NewSongbookFile, r io.Reader) (id int64, status string, err error) {
	nf.Path = CleanFilePath(nf.Path)
	if nf.Filename == "" || strings.ContainsAny(nf.Filename, "/\\") {
		return 0, "", fmt.Errorf("invalid file name %q", nf.Filename)
	}
	if nf.Kind == "" {
		nf.Kind = FileKind(nf.Filename, nf.VariantID != nil)
	}
	if nf.ModifiedAt.IsZero() {
		nf.ModifiedAt = time.Now()
	}
	var creator *int64
	if nf.CreatedByID != 0 {
		creator = &nf.CreatedByID
	}
	blobID, _, err := st.PutFileBlob(ctx, r)
	if err != nil {
		return 0, "", fmt.Errorf("%s: %w", nf.Filename, err)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback(ctx)
	var oldBlob int64
	err = tx.QueryRow(ctx, `SELECT id, blob_id FROM songbook_files WHERE songbook_id = $1 AND path = $2 AND filename = $3 FOR UPDATE`,
		nf.SongbookID, nf.Path, nf.Filename).Scan(&id, &oldBlob)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		status = FileAdded
		err = tx.QueryRow(ctx, `
			INSERT INTO songbook_files (songbook_id, variant_id, kind, path, filename, title, content_type, blob_id, modified_at, created_by_person_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
			nf.SongbookID, nf.VariantID, nf.Kind, nf.Path, nf.Filename, nf.Title, FileContentType(nf.Filename), blobID, nf.ModifiedAt, creator).Scan(&id)
	case err != nil:
	case oldBlob == blobID:
		status = FileUnchanged
		if nf.VariantID != nil {
			_, err = tx.Exec(ctx, `UPDATE songbook_files SET variant_id = $2, kind = $3 WHERE id = $1`, id, nf.VariantID, nf.Kind)
		}
	default:
		status = FileReplaced
		_, err = tx.Exec(ctx, `
			UPDATE songbook_files SET blob_id = $2, modified_at = $3, created_at = now(), created_by_person_id = $4,
			       variant_id = coalesce($5, variant_id), kind = CASE WHEN $5::bigint IS NULL THEN kind ELSE $6 END,
			       title = CASE WHEN $7 = '' THEN title ELSE $7 END
			WHERE id = $1`, id, blobID, nf.ModifiedAt, creator, nf.VariantID, nf.Kind, nf.Title)
		if err == nil {
			err = dropBlobIfUnused(ctx, tx, oldBlob)
		}
	}
	if err != nil {
		return 0, "", err
	}
	return id, status, tx.Commit(ctx)
}

const songbookFileCols = `f.id, f.songbook_id, f.variant_id, coalesce(s.title, ''), f.kind, f.path, f.filename, f.title,
	f.content_type, f.blob_id, b.byte_size, b.sha256, b.crc32, f.modified_at, f.created_at`

const songbookFileFrom = `songbook_files f JOIN file_blobs b ON b.id = f.blob_id LEFT JOIN songs s ON s.id = f.variant_id`

func scanSongbookFile(row pgx.Row) (SongbookFile, error) {
	var f SongbookFile
	var crc int64
	err := row.Scan(&f.ID, &f.SongbookID, &f.VariantID, &f.SongTitle, &f.Kind, &f.Path, &f.Filename, &f.Title,
		&f.ContentType, &f.BlobID, &f.ByteSize, &f.SHA256, &crc, &f.ModifiedAt, &f.CreatedAt)
	f.CRC32 = uint32(crc)
	return f, err
}

// ListSongbookFiles lists the download area of each of songbookIDs, by
// folder then file name (an author's own numbering).
func (st *Store) ListSongbookFiles(ctx context.Context, songbookIDs []int64) ([]SongbookFile, error) {
	rows, err := st.db.Query(ctx, `
		SELECT `+songbookFileCols+` FROM `+songbookFileFrom+`
		WHERE f.songbook_id = ANY($1)
		ORDER BY f.songbook_id, f.path COLLATE "C", f.filename COLLATE "C", f.id`, songbookIDs)
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
	f, err := scanSongbookFile(st.db.QueryRow(ctx, `SELECT `+songbookFileCols+` FROM `+songbookFileFrom+` WHERE f.id = $1`, id))
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

// SetSongbookFileSong says which song of its songbook a file goes with
// (nil: none), which may make a PDF the book or a lesson's.
func (st *Store) SetSongbookFileSong(ctx context.Context, id int64, variantID *int64) error {
	f, err := st.GetSongbookFile(ctx, id)
	if err != nil {
		return err
	}
	kind := f.Kind
	if kind == "book" || kind == "other" {
		kind = FileKind(f.Filename, variantID != nil)
	}
	_, err = st.db.Exec(ctx, `UPDATE songbook_files SET variant_id = $2, kind = $3 WHERE id = $1`, id, variantID, kind)
	return err
}

// DeleteSongbookFile removes a file from its download area.
func (st *Store) DeleteSongbookFile(ctx context.Context, id int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var blobID int64
	if err := tx.QueryRow(ctx, `DELETE FROM songbook_files WHERE id = $1 RETURNING blob_id`, id).Scan(&blobID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if err := dropBlobIfUnused(ctx, tx, blobID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// OpenSongbookFile reads a stored file a chunk at a time; it seeks, so
// http.ServeContent can answer range requests from it.
func (st *Store) OpenSongbookFile(ctx context.Context, f SongbookFile) io.ReadSeeker {
	return &fileChunkReader{st: st, ctx: ctx, id: f.BlobID, size: f.ByteSize, n: -1}
}

type fileChunkReader struct {
	st    *Store
	ctx   context.Context
	id    int64 // the blob
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
		if err := r.st.db.QueryRow(r.ctx, `SELECT data FROM file_blob_chunks WHERE blob_id = $1 AND n = $2`, r.id, n).Scan(&r.chunk); err != nil {
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

// CleanFilePath makes a folder path of a songbook's files from what a
// person or a zip says: slash-separated, no "." or "..", no leading or
// trailing slash; "" for the top.
func CleanFilePath(p string) string {
	var parts []string
	for _, part := range strings.Split(strings.ReplaceAll(p, "\\", "/"), "/") {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "/")
}

// FileKind sorts a download by its name: a PDF that goes with no song and
// isn't numbered as a lesson is the book itself.
func FileKind(name string, withSong bool) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".gp", ".gpx", ".gp5", ".gp4", ".gp3":
		return "gp"
	case ".mp3", ".wav", ".m4a", ".ogg", ".flac", ".aac", ".aif", ".aiff":
		return "audio"
	case ".pdf":
		if !withSong && lessonNumber(name) == "" {
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
	case ".wav":
		return "audio/wav"
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}
