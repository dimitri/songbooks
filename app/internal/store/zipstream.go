package store

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// ZipStream is a zip of stored files, made as it is read: the files are
// stored as they are (no compression, PDFs and MP3s don't shrink), so its
// size and every byte's place are known before reading any of them, and
// it seeks — http.ServeContent answers range requests from it, and a large
// download resumes.
type ZipStream struct {
	st       *Store
	ctx      context.Context
	segs     []zipSegment
	size     int64
	off      int64
	ETag     string
	Modified time.Time

	cur    io.ReadSeeker // the file being read, segment curSeg
	curSeg int
}

// zipSegment is a run of the zip: bytes of the zip's own (lit), or a
// stored file's.
type zipSegment struct {
	off, size int64
	lit       []byte
	file      *SongbookFile
}

// segmentWriter receives what zip.Writer writes: its own headers are kept,
// a file's bytes (zeros, see NewZipStream) only counted.
type segmentWriter struct {
	segs []zipSegment
	off  int64
	file *SongbookFile
}

func (w *segmentWriter) Write(p []byte) (int, error) {
	n := int64(len(p))
	last := len(w.segs) - 1
	switch {
	case w.file != nil && last >= 0 && w.segs[last].file == w.file:
		w.segs[last].size += n
	case w.file != nil:
		w.segs = append(w.segs, zipSegment{off: w.off, size: n, file: w.file})
	case last >= 0 && w.segs[last].file == nil:
		w.segs[last].lit = append(w.segs[last].lit, p...)
		w.segs[last].size += n
	default:
		w.segs = append(w.segs, zipSegment{off: w.off, size: n, lit: append([]byte(nil), p...)})
	}
	w.off += n
	return len(p), nil
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// NewZipStream lays out a zip of files, each at its path less the folder
// prefix (the folder the zip is made of, "" for all).
func (st *Store) NewZipStream(ctx context.Context, files []SongbookFile, prefix string) (*ZipStream, error) {
	files = append([]SongbookFile(nil), files...)
	sort.SliceStable(files, func(i, j int) bool { return files[i].FullPath() < files[j].FullPath() })
	sw := &segmentWriter{}
	zw := zip.NewWriter(sw)
	h := sha256.New()
	var modified time.Time
	for i := range files {
		f := &files[i]
		name := f.FullPath()
		if prefix != "" {
			name = strings.TrimPrefix(name, prefix+"/")
		}
		fh := &zip.FileHeader{
			Name:               name,
			Method:             zip.Store,
			CRC32:              f.CRC32,
			CompressedSize64:   uint64(f.ByteSize),
			UncompressedSize64: uint64(f.ByteSize),
		}
		fh.SetMode(0o644)
		fh.SetModTime(f.ModifiedAt) //nolint:staticcheck // CreateRaw doesn't set the MS-DOS time from Modified
		if !isASCII(name) && utf8.ValidString(name) {
			fh.Flags |= 0x800 // the name is UTF-8
		}
		fmt.Fprintf(h, "%s\x00%s\x00%d\n", name, f.SHA256, f.ModifiedAt.Unix())
		if f.ModifiedAt.After(modified) {
			modified = f.ModifiedAt
		}
		w, err := zw.CreateRaw(fh)
		if err != nil {
			return nil, err
		}
		if err := zw.Flush(); err != nil {
			return nil, err
		}
		// zip.Writer counts what goes through it to place what follows:
		// the file's bytes are only counted here, and read from the
		// database when the zip is.
		sw.file = f
		if _, err := io.CopyN(w, zeroReader{}, f.ByteSize); err != nil {
			return nil, err
		}
		if err := zw.Flush(); err != nil {
			return nil, err
		}
		sw.file = nil
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return &ZipStream{st: st, ctx: ctx, segs: sw.segs, size: sw.off,
		ETag: `"zip-` + hex.EncodeToString(h.Sum(nil))[:32] + `"`, Modified: modified}, nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// Size is the zip's size in bytes.
func (z *ZipStream) Size() int64 { return z.size }

func (z *ZipStream) Read(p []byte) (int, error) {
	if z.off >= z.size {
		return 0, io.EOF
	}
	i := sort.Search(len(z.segs), func(i int) bool { return z.segs[i].off+z.segs[i].size > z.off })
	seg := z.segs[i]
	at := z.off - seg.off
	want := min(int64(len(p)), seg.size-at)
	var n int
	if seg.file == nil {
		n = copy(p[:want], seg.lit[at:])
	} else {
		if z.cur == nil || z.curSeg != i {
			z.cur, z.curSeg = z.st.OpenSongbookFile(z.ctx, *seg.file), i
		}
		r := z.cur
		if _, err := r.Seek(at, io.SeekStart); err != nil {
			return 0, err
		}
		var err error
		if n, err = io.ReadFull(r, p[:want]); err != nil {
			return n, err
		}
	}
	z.off += int64(n)
	return n, nil
}

func (z *ZipStream) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += z.off
	case io.SeekEnd:
		offset += z.size
	default:
		return 0, errors.New("bad whence")
	}
	if offset < 0 {
		return 0, errors.New("negative position")
	}
	z.off = offset
	return offset, nil
}
