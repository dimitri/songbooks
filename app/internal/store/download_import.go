package store

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// ImportedFile is what ImportSongbookZip did with one entry of the zip.
type ImportedFile struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	FileID    int64  `json:"id,omitempty"`
	SongID    *int64 `json:"song,omitempty"`
	SongTitle string `json:"songTitle,omitempty"`
	Status    string `json:"status"` // FileAdded, FileReplaced, FileUnchanged, or "skipped"
	Note      string `json:"note,omitempty"`
}

// ImportSongbookZip adds the files of a zip to a songbook's download area,
// in folder into, keeping the zip's own folders (less a top folder named
// as the zip itself, the way a "Compress" makes one). Each file goes with
// the song of the songbook it's for, when that can be told: a Guitar Pro
// file the song was made from (or is named after), then the other files
// of its folder or of its lesson number. What a computer adds to a zip
// (__MACOSX, .DS_Store) is left out.
func (st *Store) ImportSongbookZip(ctx context.Context, songbookID int64, zr *zip.Reader, zipName, into string, actorID int64) ([]ImportedFile, error) {
	into = CleanFilePath(into)
	type entry struct {
		f         *zip.File
		dir, name string
		sha       string // Guitar Pro files only, for matching
	}
	var entries []entry
	var out []ImportedFile
	for _, f := range zr.File {
		name := zipEntryName(f)
		dir, base := path.Split(name)
		dir = CleanFilePath(dir)
		switch {
		case f.FileInfo().IsDir(), base == "":
			continue
		case strings.HasPrefix(name, "__MACOSX/"), strings.HasPrefix(base, "._"),
			base == ".DS_Store", strings.EqualFold(base, "Thumbs.db"), strings.EqualFold(base, "desktop.ini"):
			continue
		case strings.EqualFold(filepath.Ext(base), ".zip"):
			out = append(out, ImportedFile{Path: dir, Name: base, Size: int64(f.UncompressedSize64), Status: "skipped",
				Note: "a zip in the zip: add it on its own"})
			continue
		}
		entries = append(entries, entry{f: f, dir: dir, name: base})
	}
	// A zip of one folder named as the zip: its files, not the folder.
	if root := strings.TrimSuffix(norm.NFC.String(path.Base(zipName)), filepath.Ext(zipName)); root != "" {
		all := len(entries) > 0
		for _, e := range entries {
			if e.dir != root && !strings.HasPrefix(e.dir, root+"/") {
				all = false
				break
			}
		}
		if all {
			for i := range entries {
				entries[i].dir = strings.TrimPrefix(strings.TrimPrefix(entries[i].dir, root), "/")
			}
		}
	}

	songs, err := st.songbookSongsForMatching(ctx, songbookID)
	if err != nil {
		return nil, err
	}
	var toMatch []matchEntry
	for i, e := range entries {
		if FileKind(e.name, false) == "gp" {
			rc, err := e.f.Open()
			if err != nil {
				return nil, err
			}
			h := sha256.New()
			_, err = io.Copy(h, rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			entries[i].sha = hex.EncodeToString(h.Sum(nil))
		}
		toMatch = append(toMatch, matchEntry{Dir: e.dir, Name: e.name, SHA: entries[i].sha})
	}
	matched := matchFilesToSongs(toMatch, songs)
	titles := map[int64]string{}
	for _, s := range songs {
		titles[s.ID] = s.Title
	}

	for i, e := range entries {
		rc, err := e.f.Open()
		if err != nil {
			return nil, err
		}
		dir := path.Join(into, e.dir)
		if dir == "." {
			dir = ""
		}
		id, status, err := st.AddSongbookFile(ctx, NewSongbookFile{
			SongbookID: songbookID, VariantID: matched[i], Path: dir, Filename: e.name,
			ModifiedAt: e.f.Modified, CreatedByID: actorID,
		}, rc)
		rc.Close()
		res := ImportedFile{Path: dir, Name: e.name, Size: int64(e.f.UncompressedSize64), FileID: id, SongID: matched[i], Status: status}
		if matched[i] != nil {
			res.SongTitle = titles[*matched[i]]
		}
		if err != nil {
			res.Status, res.Note = "skipped", err.Error()
		}
		out = append(out, res)
	}
	return out, nil
}

// zipEntryName is an entry's name as UTF-8 (NFC, as typed): a zip made
// without saying its names are UTF-8 has them in the old DOS code page,
// or in UTF-8 anyway (macOS, decomposed).
func zipEntryName(f *zip.File) string {
	name := f.Name
	if !utf8.ValidString(name) {
		if s, err := charmap.CodePage437.NewDecoder().String(name); err == nil {
			name = s
		}
	}
	return norm.NFC.String(strings.ReplaceAll(name, "\\", "/"))
}

// matchSong is a song of the songbook, as a file is matched to it.
type matchSong struct {
	ID     int64
	Title  string
	GPName string   // the Guitar Pro file it has
	GPSHAs []string // that file's hash, and its earlier ones'
}

// matchEntry is a file to match: its folder, name, and for a Guitar Pro
// file its hash.
type matchEntry struct {
	Dir, Name, SHA string
}

func (st *Store) songbookSongsForMatching(ctx context.Context, songbookID int64) ([]matchSong, error) {
	rows, err := st.db.Query(ctx, `
		SELECT s.id, s.title, coalesce(g.filename, ''),
		       array_remove(array_append(
		         coalesce((SELECT array_agg(v.sha256) FROM song_gp_source_file_version v WHERE v.variant_id = s.id), '{}'),
		         g.sha256), NULL)
		FROM songbook_entries se
		JOIN songs s ON s.id = se.variant_id
		LEFT JOIN song_gp_source_file g ON g.variant_id = s.id
		WHERE se.songbook_id = $1
		ORDER BY s.id`, songbookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []matchSong
	for rows.Next() {
		var s matchSong
		if err := rows.Scan(&s.ID, &s.Title, &s.GPName, &s.GPSHAs); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// matchFilesToSongs tells which song each file goes with, nil for none:
//
//  1. a Guitar Pro file a song was made from (same bytes), or named as
//     the song or as its Guitar Pro file;
//  2. any other file named so;
//  3. a file in the folder of the Guitar Pro files of one song;
//  4. a file with the lesson number of matched Guitar Pro files (the
//     first of them, by name, when there are several).
func matchFilesToSongs(entries []matchEntry, songs []matchSong) []*int64 {
	bySHA := map[string]int64{}
	byName := map[string]int64{}
	for _, s := range songs {
		for _, sha := range s.GPSHAs {
			bySHA[sha] = s.ID
		}
		if k := matchKey(s.Title); k != "" {
			byName[k] = s.ID
		}
		if k := matchKey(fileStem(s.GPName)); k != "" {
			if _, taken := byName[k]; !taken {
				byName[k] = s.ID
			}
		}
	}
	out := make([]*int64, len(entries))
	set := func(i int, id int64) { out[i] = &id }
	// The Guitar Pro files first: they say where the rest go.
	type found struct {
		name string
		id   int64
	}
	byDir := map[string]map[int64]bool{}
	byNumber := map[string][]found{}
	for i, e := range entries {
		if FileKind(e.Name, false) != "gp" {
			continue
		}
		id, ok := bySHA[e.SHA]
		if !ok {
			id, ok = byName[matchKey(fileStem(e.Name))]
		}
		if !ok {
			continue
		}
		set(i, id)
		if byDir[e.Dir] == nil {
			byDir[e.Dir] = map[int64]bool{}
		}
		byDir[e.Dir][id] = true
		if n := lessonNumber(e.Name); n != "" {
			byNumber[n] = append(byNumber[n], found{e.Name, id})
		}
	}
	for i, e := range entries {
		if out[i] != nil {
			continue
		}
		if id, ok := byName[matchKey(fileStem(e.Name))]; ok {
			set(i, id)
			continue
		}
		if ids := byDir[e.Dir]; e.Dir != "" && len(ids) == 1 {
			for id := range ids {
				set(i, id)
			}
			continue
		}
		if fs := byNumber[lessonNumber(e.Name)]; len(fs) > 0 {
			sort.SliceStable(fs, func(a, b int) bool { return fs[a].name < fs[b].name })
			set(i, fs[0].id)
		}
	}
	return out
}

// fileStem is a file name without its extension(s) — "x.gp.wav" is "x" —
// and its lesson number.
func fileStem(name string) string {
	for ext := filepath.Ext(name); ext != "" && len(ext) <= 5; ext = filepath.Ext(name) {
		name = strings.TrimSuffix(name, ext)
	}
	if lessonNumber(name) != "" {
		name = strings.TrimLeft(name, "0123456789")
	}
	return name
}

var stripMarks = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// matchKey is a name as it's compared: lower case, no accents, words of
// letters and digits only, less a backing track's "PB" at the end.
func matchKey(s string) string {
	s, _, _ = transform.String(stripMarks, strings.ToLower(s))
	words := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for len(words) > 0 && (words[len(words)-1] == "pb" || words[len(words)-1] == "playback") {
		words = words[:len(words)-1]
	}
	return strings.Join(words, " ")
}
