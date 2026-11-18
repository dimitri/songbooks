package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/i18n"
)

// A band's recordings (docs/band-recordings.md, migration 0075): the files
// of the folder the band shares on pCloud, indexed and never stored. What
// a recording is comes from where it is and what it's called, the way
// bands file them:
//
//	Repetes/2025-03-13/07 - Plan Chicago Blues - Take 2 - 2025-03-13.mp3
//	Concerts/2025-06-14/03 - Killing Me Softly - 2025-06-14.mp3
//
// a rehearsal (or a concert, an audition...) on a date, the song played
// seventh that day, its second take. A band's corrections fix what its
// names get wrong; each member rates recordings, by half stars; and the
// band picks, for each song, the recording its playlist plays.

// BandRecordingSource is the folder a band shares its recordings in.
type BandRecordingSource struct {
	ID       int64
	BandID   int64
	Provider string
	Link     string
	Code     string
	APIHost  string
	Name     string
	SyncedAt *time.Time
}

// pcloudLink is a pCloud public link's code and the API host serving it:
// e.pcloud.link is the European one, u.pcloud.link the American.
func pcloudLink(link string) (code, apiHost string, err error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Host == "" {
		return "", "", errors.New("that isn't a link")
	}
	code = u.Query().Get("code")
	if code == "" {
		return "", "", errors.New("that isn't a pCloud shared link (no code= in it)")
	}
	switch {
	case strings.HasPrefix(u.Host, "e.") || strings.Contains(u.Host, "e.pcloud"):
		return code, "eapi.pcloud.com", nil
	case strings.Contains(u.Host, "pcloud"):
		return code, "api.pcloud.com", nil
	}
	return "", "", errors.New("that isn't a pCloud link")
}

// pcloudItem is a file or folder of a pCloud listing.
type pcloudItem struct {
	Name        string       `json:"name"`
	IsFolder    bool         `json:"isfolder"`
	FileID      int64        `json:"fileid"`
	Size        int64        `json:"size"`
	ContentType string       `json:"contenttype"`
	Created     string       `json:"created"`
	Modified    string       `json:"modified"`
	Contents    []pcloudItem `json:"contents"`
}

// pcloudGet fetches a pCloud API URL; tests replace it.
var pcloudGet = func(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

// fetchPCloudTree lists a pCloud public link's folder, its subfolders'
// contents included.
func fetchPCloudTree(ctx context.Context, apiHost, code string) (*pcloudItem, error) {
	res, err := pcloudGet(ctx, "https://"+apiHost+"/showpublink?code="+url.QueryEscape(code))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var out struct {
		Result   int        `json:"result"`
		Error    string     `json:"error"`
		Metadata pcloudItem `json:"metadata"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("pCloud: %w", err)
	}
	if out.Result != 0 {
		return nil, fmt.Errorf("pCloud: %s", out.Error)
	}
	return &out.Metadata, nil
}

// SetBandRecordingSource sets the folder bandID shares its recordings in,
// from its pCloud public link, checking pCloud answers for it.
func (st *Store) SetBandRecordingSource(ctx context.Context, bandID int64, link string, personID int64) (*BandRecordingSource, error) {
	code, host, err := pcloudLink(link)
	if err != nil {
		return nil, err
	}
	tree, err := fetchPCloudTree(ctx, host, code)
	if err != nil {
		return nil, err
	}
	_, err = st.db.Exec(ctx, `
		INSERT INTO band_recording_sources (band_id, provider, link, code, api_host, name, created_by_person_id)
		VALUES ($1, 'pcloud', $2, $3, $4, $5, $6)
		ON CONFLICT (band_id) DO UPDATE SET link = EXCLUDED.link, code = EXCLUDED.code, api_host = EXCLUDED.api_host,
			name = EXCLUDED.name, synced_at = NULL`,
		bandID, strings.TrimSpace(link), code, host, tree.Name, nullID(personID))
	if err != nil {
		return nil, err
	}
	src, err := st.GetBandRecordingSource(ctx, bandID)
	if err != nil {
		return nil, err
	}
	// A new folder: its files, not the last one's.
	if _, err := st.db.Exec(ctx, `DELETE FROM band_recordings WHERE band_id = $1 AND source_id <> $2`, bandID, src.ID); err != nil {
		return nil, err
	}
	return src, st.applyRecordingTree(ctx, src, tree)
}

func nullID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

// GetBandRecordingSource is bandID's recordings folder; ErrNotFound when
// it has none.
func (st *Store) GetBandRecordingSource(ctx context.Context, bandID int64) (*BandRecordingSource, error) {
	var s BandRecordingSource
	err := st.db.QueryRow(ctx, `
		SELECT id, band_id, provider, link, code, api_host, name, synced_at FROM band_recording_sources WHERE band_id = $1`, bandID).
		Scan(&s.ID, &s.BandID, &s.Provider, &s.Link, &s.Code, &s.APIHost, &s.Name, &s.SyncedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("band %d has no recordings folder: %w", bandID, ErrNotFound)
	}
	return &s, err
}

// SyncBandRecordings lists bandID's folder again: new files are added,
// changed ones updated, and those gone marked so (their stars kept, in
// case they come back).
func (st *Store) SyncBandRecordings(ctx context.Context, bandID int64) error {
	src, err := st.GetBandRecordingSource(ctx, bandID)
	if err != nil {
		return err
	}
	tree, err := fetchPCloudTree(ctx, src.APIHost, src.Code)
	if err != nil {
		return err
	}
	return st.applyRecordingTree(ctx, src, tree)
}

// mediaExts are the files a band's recordings are.
var mediaExts = map[string]bool{"mp3": true, "wav": true, "m4a": true, "aif": true, "aiff": true, "flac": true, "ogg": true, "mp4": true, "mov": true}

// stemDirs hold a recording's parts, not recordings: a DAW's session.
var stemDirs = regexp.MustCompile(`(?i)^(audio files|session file backups|media|bounced files|.*\.(band|logicx|ptx))$`)

// parsedRecording is what a recording's place and name say.
type parsedRecording struct {
	Kind       string
	FolderDate string
	FileDate   string
	Position   *int
	Take       *int
	Title      string
	Raw        bool // a recorder's own name: no song to look for
}

var (
	dateAtStart = regexp.MustCompile(`^(\d{4})-(\d{2})(?:-(\d{2}))?`)
	isoDate     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	takeToken   = regexp.MustCompile(`(?i)^take\s*(\d+)$`)
	numToken    = regexp.MustCompile(`^\d{1,3}$`)
	leadNum     = regexp.MustCompile(`^(\d{1,3})\s*[-_.]?\s+`)
	songFolder  = regexp.MustCompile(`^\d{1,3}\s*-\s*.+\s-\s*\d{4}-\d{2}-\d{2}$`)
)

// recordingKind is what a band's top folder holds, by its name.
func recordingKind(top string) string {
	k := matchKey(top)
	switch {
	case strings.HasPrefix(k, "repet") || strings.HasPrefix(k, "rehears") || strings.HasPrefix(k, "practi") || strings.HasPrefix(k, "repe"):
		return "rehearsal"
	case strings.HasPrefix(k, "concert") || strings.HasPrefix(k, "live") || strings.HasPrefix(k, "gig"):
		return "concert"
	case strings.HasPrefix(k, "audition"):
		return "audition"
	case strings.HasPrefix(k, "enregistrement") || strings.HasPrefix(k, "studio") || strings.HasPrefix(k, "recording"):
		return "studio"
	case strings.HasPrefix(k, "album"):
		return "album"
	case strings.HasPrefix(k, "set list") || strings.HasPrefix(k, "setlist"):
		return "setlist"
	}
	return "other"
}

// parseRecording reads what a file's folders (from the shared one) and
// name say, its name with the band's templates (recording_names.go); ok
// is false for a file that isn't a recording, or a part of one (a DAW
// session's tracks, a take's stems).
func parseRecording(folders []string, name, band string, tmpls []*nameTemplate) (p parsedRecording, ok bool) {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	if !mediaExts[ext] || strings.HasPrefix(name, "._") {
		return p, false
	}
	for _, f := range folders {
		if stemDirs.MatchString(f) {
			return p, false
		}
	}
	if n := len(folders); n > 0 && songFolder.MatchString(folders[n-1]) {
		return p, false // "01 - Mottainai - 2025-03-03/": that take's stems
	}
	if len(folders) > 0 {
		p.Kind = recordingKind(folders[0])
	} else {
		p.Kind = "other"
	}
	for i := len(folders) - 1; i >= 0; i-- {
		if m := dateAtStart.FindStringSubmatch(folders[i]); m != nil {
			p.FolderDate = m[0]
			break
		}
	}
	stem := strings.TrimSpace(strings.TrimSuffix(name, path.Ext(name)))
	if rawName.MatchString(stem) {
		p.Raw, p.Title = true, stem
		return p, true
	}
	// The band's name says nothing: "Other Lies - Ultrablanc - Skins".
	bandKey := matchKey(band)
	var kept []string
	for _, tok := range strings.Split(stem, " - ") {
		if bandKey == "" || matchKey(tok) != bandKey {
			kept = append(kept, tok)
		}
	}
	stem = strings.Join(kept, " - ")
	video := ext == "mp4" || ext == "mov"
	if r, ok := readName(stem, tmpls); ok {
		p.Position, p.Take, p.Title, p.FileDate = r.Position, r.Take, r.Title, r.Date
		if p.Title == "" {
			p.Title = stem
		}
		// A video is a song's only when its name numbers it ("07 - Gone"):
		// else a trailer, a stream, a clip, never asked about.
		p.Raw = video && p.Position == nil
		return p, true
	}
	if video {
		p.Raw, p.Title = true, stem
		return p, true
	}
	if m := leadNum.FindStringSubmatch(stem); m != nil {
		n, _ := strconv.Atoi(m[1])
		p.Position = &n
		stem = stem[len(m[0]):]
	}
	for _, tok := range strings.Split(stem, " - ") {
		tok = strings.Trim(strings.TrimSpace(tok), "-_ ")
		switch {
		case tok == "":
		case isoDate.MatchString(tok):
			if p.FileDate == "" {
				p.FileDate = tok
			}
		case takeToken.MatchString(tok):
			n, _ := strconv.Atoi(takeToken.FindStringSubmatch(tok)[1])
			p.Take = &n
		case numToken.MatchString(tok):
			if p.Position == nil {
				n, _ := strconv.Atoi(tok)
				p.Position = &n
			}
		case p.Title == "":
			p.Title = tok
		}
	}
	if p.Title == "" {
		p.Title = stem
	}
	return p, true
}

// applyRecordingTree records a listing of src's folder.
func (st *Store) applyRecordingTree(ctx context.Context, src *BandRecordingSource, tree *pcloudItem) error {
	var band string
	var own []string
	if err := st.db.QueryRow(ctx, `SELECT b.name, s.name_templates FROM bands b JOIN band_recording_sources s ON s.band_id = b.id WHERE s.id = $1`, src.ID).Scan(&band, &own); err != nil {
		return err
	}
	tmpls := templatesFor(own)
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE band_recordings SET gone = true WHERE source_id = $1`, src.ID); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	var walk func(it pcloudItem, folders []string)
	walk = func(it pcloudItem, folders []string) {
		for _, c := range it.Contents {
			if c.IsFolder {
				walk(c, append(append([]string{}, folders...), c.Name))
				continue
			}
			p, ok := parseRecording(folders, c.Name, band, tmpls)
			if !ok {
				continue
			}
			batch.Queue(`
				INSERT INTO band_recordings (source_id, band_id, file_id, path, name, size, content_type, created, modified,
				                             kind, folder_date, file_date, position, title, take, raw)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
				ON CONFLICT (source_id, file_id) DO UPDATE SET path = EXCLUDED.path, name = EXCLUDED.name, size = EXCLUDED.size,
					content_type = EXCLUDED.content_type, created = EXCLUDED.created, modified = EXCLUDED.modified, kind = EXCLUDED.kind,
					folder_date = EXCLUDED.folder_date, file_date = EXCLUDED.file_date, position = EXCLUDED.position,
					title = EXCLUDED.title, take = EXCLUDED.take, raw = EXCLUDED.raw, gone = false`,
				src.ID, src.BandID, c.FileID, strings.Join(folders, "/"), c.Name, c.Size, c.ContentType,
				pcloudTime(c.Created), pcloudTime(c.Modified), p.Kind, p.FolderDate, p.FileDate, p.Position, p.Title, p.Take, p.Raw)
		}
	}
	walk(*tree, nil)
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE band_recording_sources SET synced_at = now(), name = $2 WHERE id = $1`, src.ID, tree.Name); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return st.ResolveBandRecordings(ctx, src.BandID)
}

func pcloudTime(s string) *time.Time {
	t, err := time.Parse(time.RFC1123Z, s)
	if err != nil {
		return nil
	}
	return &t
}

// bandSong is a song of a band's, as recordings are matched to it.
type bandSong struct {
	ID    int64
	Title string
	Key   string
}

// bandSongs is bandID's repertoire: its default songbook, and its own songs.
func (st *Store) bandSongs(ctx context.Context, bandID int64) ([]bandSong, error) {
	rows, err := st.db.Query(ctx, `
		SELECT DISTINCT s.id, s.title FROM songs s
		WHERE s.owner_band_id = $1
		   OR s.id IN (SELECT se.variant_id FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
		               WHERE sb.owner_kind = 'band' AND sb.owner_band_id = $1)`, bandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []bandSong
	for rows.Next() {
		var s bandSong
		if err := rows.Scan(&s.ID, &s.Title); err != nil {
			return nil, err
		}
		s.Key = songKey(s.Title)
		out = append(out, s)
	}
	return out, rows.Err()
}

// songKey is a title as recordings are matched by it: no accents,
// punctuation, apostrophes ("I'd" and "Id" alike) or parenthesis.
var parenthesis = regexp.MustCompile(`\(.*?\)`)

func songKey(s string) string {
	s = parenthesis.ReplaceAllString(s, " ")
	s = strings.NewReplacer("'", "", "’", "", "`", "").Replace(s)
	return matchKey(s)
}

// matchBandSong is the band's song a recording's title names: the same words,
// or nearly (a letter or two off, one name starting the other).
func matchBandSong(title string, songs []bandSong) *int64 {
	k := songKey(title)
	if k == "" {
		return nil
	}
	var best *int64
	bestScore := 0.0
	for i, s := range songs {
		if s.Key == "" {
			continue
		}
		score := 0.0
		switch {
		case s.Key == k:
			score = 1
		case squash(s.Key) == squash(k):
			score = 0.95 // "Teens4Today" for "Teens For Today"
		case strings.HasPrefix(s.Key, k+" ") || strings.HasPrefix(k, s.Key+" "):
			score = 0.9
		default:
			score = max(similarity(k, s.Key), similarity(squash(k), squash(s.Key)))
		}
		if score > bestScore {
			bestScore, best = score, &songs[i].ID
		}
	}
	if bestScore < 0.82 {
		return nil
	}
	return best
}

// squash is a title key without its spaces, with the numbers that stand
// for words as the words: "teens4today" and "teens for today" alike.
var digitWords = strings.NewReplacer("4", "for", "2", "to", "8", "ate")

func squash(key string) string {
	return digitWords.Replace(strings.ReplaceAll(key, " ", ""))
}

// matchRecording is the band's song a recording is of: its title's, else
// another part of its name's ("1 - Thierry - Colors": an audition, the
// candidate first), else the song whose every word its name has
// ("Other Lies-Ultrablanc-Colors_of_Thunder_115_Mix.v8").
func matchRecording(title, name string, songs []bandSong) *int64 {
	if v := matchBandSong(title, songs); v != nil {
		return v
	}
	stem := strings.TrimSuffix(name, path.Ext(name))
	for _, tok := range strings.Split(stem, " - ") {
		tok = strings.TrimSpace(tok)
		if tok == "" || tok == title || isoDate.MatchString(tok) || takeToken.MatchString(tok) || numToken.MatchString(tok) {
			continue
		}
		if v := matchBandSong(tok, songs); v != nil {
			return v
		}
	}
	words := map[string]bool{}
	for _, w := range strings.Fields(songKey(strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(stem))) {
		words[w] = true
	}
	var best *int64
	bestLen := 0
	for i, s := range songs {
		ws := strings.Fields(s.Key)
		n := 0
		for _, w := range ws {
			if w == "the" || w == "a" || w == "le" || w == "la" || w == "les" {
				continue
			}
			if !words[w] && !words[w+"s"] {
				n = -1
				break
			}
			n += len(w)
		}
		if n > bestLen && n >= 5 {
			best, bestLen = &songs[i].ID, n
		}
	}
	return best
}

// similarity is 1 less the edit distance over the longer length.
func similarity(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 || len(rb) == 0 {
		return 0
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return 1 - float64(prev[len(rb)])/float64(max(len(ra), len(rb)))
}

// ResolveBandRecordings gives each of bandID's recordings its session's
// date and its song, the band's corrections applied first: called after
// a sync, and after a correction.
func (st *Store) ResolveBandRecordings(ctx context.Context, bandID int64) error {
	fixes, err := st.bandFixes(ctx, bandID)
	if err != nil {
		return err
	}
	songs, err := st.bandSongs(ctx, bandID)
	if err != nil {
		return err
	}
	rows, err := st.db.Query(ctx, `SELECT id, file_id, name, folder_date, file_date, title, raw, session_date, variant_id FROM band_recordings WHERE band_id = $1`, bandID)
	if err != nil {
		return err
	}
	type upd struct {
		id      int64
		date    *time.Time
		variant *int64
	}
	var updates []upd
	for rows.Next() {
		var id, fileID int64
		var name, folderDate, fileDate, title string
		var raw bool
		var curDate *time.Time
		var curVariant *int64
		if err := rows.Scan(&id, &fileID, &name, &folderDate, &fileDate, &title, &raw, &curDate, &curVariant); err != nil {
			rows.Close()
			return err
		}
		date := sessionDate(folderDate, fileDate, fixes["date"])
		// The file's own correction first, then its title's; a recorder's
		// own name has no song unless it's said.
		var variant *int64
		if to, ok := fixes["file"][strconv.FormatInt(fileID, 10)]; ok {
			if v, err := strconv.ParseInt(to, 10, 64); err == nil {
				variant = &v
			}
		} else if to, ok := fixes["title"][songKey(title)]; ok && !raw {
			if v, err := strconv.ParseInt(to, 10, 64); err == nil {
				variant = &v
			}
		} else if !raw {
			variant = matchRecording(title, name, songs)
		}
		if !sameDate(date, curDate) || !sameID(variant, curVariant) {
			updates = append(updates, upd{id, date, variant})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, u := range updates {
		batch.Queue(`UPDATE band_recordings SET session_date = $2, variant_id = $3 WHERE id = $1`, u.id, u.date, u.variant)
	}
	return st.db.SendBatch(ctx, batch).Close()
}

func sameDate(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Format("2006-01-02") == b.Format("2006-01-02")
}

func sameID(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// sessionDate is a recording's session date: its folder's, corrected, else
// its name's; a month alone ("2023-12") is its first day.
func sessionDate(folderDate, fileDate string, fixes map[string]string) *time.Time {
	d := folderDate
	if d == "" {
		d = fileDate
	}
	if to, ok := fixes[d]; ok {
		d = to
	}
	if len(d) == 7 {
		d += "-01"
	}
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return nil
	}
	return &t
}

// bandFixes is bandID's corrections, by kind then the value corrected
// (a title's songKey).
func (st *Store) bandFixes(ctx context.Context, bandID int64) (map[string]map[string]string, error) {
	out := map[string]map[string]string{"date": {}, "title": {}, "file": {}}
	rows, err := st.db.Query(ctx, `SELECT kind, from_value, to_value FROM band_recording_fixes WHERE band_id = $1`, bandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, from, to string
		if err := rows.Scan(&kind, &from, &to); err != nil {
			return nil, err
		}
		out[kind][from] = to
	}
	return out, rows.Err()
}

// RecordingFix is one of a band's corrections.
type RecordingFix struct {
	ID        int64
	Kind      string // "date", "title" or "file"
	From, To  string
	SongTitle string // a title's or a file's: the song it names ("" for a file that's none)
	FileName  string // a file's: its folder and name
	Files     int    // the recordings it applies to
}

// ListRecordingFixes is bandID's corrections.
func (st *Store) ListRecordingFixes(ctx context.Context, bandID int64) ([]RecordingFix, error) {
	rows, err := st.db.Query(ctx, `
		SELECT f.id, f.kind, f.from_value, f.to_value, coalesce(s.title, ''),
		       coalesce((SELECT r.path || '/' || r.name FROM band_recordings r WHERE f.kind = 'file' AND r.band_id = f.band_id AND r.file_id::text = f.from_value LIMIT 1), ''),
		       (SELECT count(*) FROM band_recordings r WHERE r.band_id = f.band_id AND NOT r.gone AND
		          CASE WHEN f.kind = 'date' THEN coalesce(nullif(r.folder_date, ''), r.file_date) = f.from_value
		               ELSE false END)
		FROM band_recording_fixes f
		LEFT JOIN songs s ON f.kind IN ('title', 'file') AND s.id::text = f.to_value
		WHERE f.band_id = $1 ORDER BY f.kind, f.from_value`, bandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecordingFix
	for rows.Next() {
		var f RecordingFix
		if err := rows.Scan(&f.ID, &f.Kind, &f.From, &f.To, &f.SongTitle, &f.FileName, &f.Files); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SetRecordingFix adds (or changes) one of bandID's corrections: a date
// ("2024-05-26" is "2025-05-26"; the same date to say it's right), or a
// title (as its recordings name it) that's a song of the band's.
func (st *Store) SetRecordingFix(ctx context.Context, bandID int64, kind, from, to string, personID int64) error {
	switch kind {
	case "date":
		if !dateAtStart.MatchString(from) {
			return errors.New("the date to correct is a date")
		}
		if _, err := time.Parse("2006-01-02", to); err != nil {
			return errors.New("the right date is a date: 2025-05-26")
		}
	case "title":
		from = songKey(from)
		id, err := strconv.ParseInt(to, 10, 64)
		if err != nil || from == "" {
			return errors.New("a title goes to one of the band's songs")
		}
		songs, err := st.bandSongs(ctx, bandID)
		if err != nil {
			return err
		}
		found := false
		for _, s := range songs {
			found = found || s.ID == id
		}
		if !found {
			return fmt.Errorf("song %d isn't the band's: %w", id, ErrNotFound)
		}
	case "file":
		// A file of the band's folder, to one of its songs or "none".
		var n int
		if err := st.db.QueryRow(ctx, `SELECT count(*) FROM band_recordings WHERE band_id = $1 AND file_id::text = $2`, bandID, from).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("no file %s in the band's folder: %w", from, ErrNotFound)
		}
		if to != "none" {
			id, err := strconv.ParseInt(to, 10, 64)
			if err != nil {
				return errors.New("a file goes to one of the band's songs, or none")
			}
			songs, err := st.bandSongs(ctx, bandID)
			if err != nil {
				return err
			}
			found := false
			for _, s := range songs {
				found = found || s.ID == id
			}
			if !found {
				return fmt.Errorf("song %d isn't the band's: %w", id, ErrNotFound)
			}
		}
	default:
		return fmt.Errorf("invalid correction kind %q", kind)
	}
	if _, err := st.db.Exec(ctx, `
		INSERT INTO band_recording_fixes (band_id, kind, from_value, to_value, created_by_person_id) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (band_id, kind, from_value) DO UPDATE SET to_value = EXCLUDED.to_value`,
		bandID, kind, from, to, nullID(personID)); err != nil {
		return err
	}
	return st.ResolveBandRecordings(ctx, bandID)
}

// DeleteRecordingFix removes one of bandID's corrections.
func (st *Store) DeleteRecordingFix(ctx context.Context, bandID, fixID int64) error {
	if _, err := st.db.Exec(ctx, `DELETE FROM band_recording_fixes WHERE id = $1 AND band_id = $2`, fixID, bandID); err != nil {
		return err
	}
	return st.ResolveBandRecordings(ctx, bandID)
}

// DateQuestion is a session whose files' names don't give its folder's
// date: one of them is likely a typo ("2015-01-27" in 2025-01-27/, a
// folder dated a year early), for the band to say which.
type DateQuestion struct {
	Kind       string
	FolderDate string
	Path       string
	FileDates  []string // the other dates its files' names give, most first
	Created    string   // when its files went up, the earliest
	Files      int
	Likely     string // the date nearest to when they went up, within a month
}

// ListDateQuestions is bandID's sessions whose dates disagree, not yet
// answered by a correction.
func (st *Store) ListDateQuestions(ctx context.Context, bandID int64) ([]DateQuestion, error) {
	fixes, err := st.bandFixes(ctx, bandID)
	if err != nil {
		return nil, err
	}
	rows, err := st.db.Query(ctx, `
		SELECT kind, folder_date, min(path), file_date, count(*), min(created)
		FROM band_recordings
		WHERE band_id = $1 AND NOT gone AND folder_date <> '' AND file_date <> '' AND file_date <> folder_date
		GROUP BY kind, folder_date, file_date
		ORDER BY folder_date, count(*) DESC`, bandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	at := map[string]int{}
	var out []DateQuestion
	for rows.Next() {
		var kind, folderDate, p, fileDate string
		var n int
		var created *time.Time
		if err := rows.Scan(&kind, &folderDate, &p, &fileDate, &n, &created); err != nil {
			return nil, err
		}
		if _, answered := fixes["date"][folderDate]; answered {
			continue
		}
		i, ok := at[kind+folderDate]
		if !ok {
			i = len(out)
			at[kind+folderDate] = i
			q := DateQuestion{Kind: kind, FolderDate: folderDate, Path: p}
			if created != nil {
				q.Created = created.Format("2006-01-02")
			}
			out = append(out, q)
		}
		out[i].FileDates = append(out[i].FileDates, fileDate)
		out[i].Files += n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, q := range out {
		up, err := time.Parse("2006-01-02", q.Created)
		if err != nil {
			continue
		}
		best := 31 * 24 * time.Hour
		for _, d := range append([]string{q.FolderDate}, q.FileDates...) {
			t, err := time.Parse("2006-01-02", d)
			if err != nil {
				continue
			}
			if gap := up.Sub(t); gap >= 0 && gap < best {
				best, out[i].Likely = gap, d
			}
		}
	}
	return out, nil
}

// BandRecording is one recording, as a session or a song lists it.
type BandRecording struct {
	ID          int64
	FileID      int64
	Kind        string
	Path        string
	Name        string
	Size        int64
	SessionDate *time.Time
	Position    *int
	Title       string
	Take        *int
	VariantID   *int64
	SongTitle   string
	Stars       float64 // the members' average; 0: unrated
	Ratings     int
	MyStars     float64
	Reference   bool // the band's chosen recording of its song
	Gone        bool
}

// Ext is the recording's file type: "MP3".
func (r BandRecording) Ext() string {
	return strings.ToUpper(strings.TrimPrefix(path.Ext(r.Name), "."))
}

// IsVideo reports whether the recording is a video.
func (r BandRecording) IsVideo() bool { e := r.Ext(); return e == "MP4" || e == "MOV" }

const bandRecordingCols = `r.id, r.file_id, r.kind, r.path, r.name, r.size, r.session_date, r.position, r.title, r.take,
	r.variant_id, coalesce(s.title, ''),
	coalesce((SELECT avg(stars)::float8 FROM band_recording_ratings x WHERE x.recording_id = r.id), 0),
	(SELECT count(*) FROM band_recording_ratings x WHERE x.recording_id = r.id),
	coalesce((SELECT stars::float8 FROM band_recording_ratings x WHERE x.recording_id = r.id AND x.person_id = $2), 0),
	EXISTS(SELECT 1 FROM band_song_recordings c WHERE c.recording_id = r.id), r.gone`

func scanBandRecording(row pgx.Row) (BandRecording, error) {
	var r BandRecording
	err := row.Scan(&r.ID, &r.FileID, &r.Kind, &r.Path, &r.Name, &r.Size, &r.SessionDate, &r.Position, &r.Title, &r.Take,
		&r.VariantID, &r.SongTitle, &r.Stars, &r.Ratings, &r.MyStars, &r.Reference, &r.Gone)
	return r, err
}

func (st *Store) queryBandRecordings(ctx context.Context, where string, args ...any) ([]BandRecording, error) {
	rows, err := st.db.Query(ctx, `SELECT `+bandRecordingCols+` FROM band_recordings r LEFT JOIN songs s ON s.id = r.variant_id WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BandRecording
	for rows.Next() {
		r, err := scanBandRecording(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecordingSession is a day a band recorded: a rehearsal, a concert.
type RecordingSession struct {
	Date       time.Time
	Recordings int
	Songs      int // distinct songs recognised
	Stars      float64
	Folder     string
}

// ListRecordingSessions is bandID's sessions of a kind ("rehearsal",
// "concert"...), the latest first.
func (st *Store) ListRecordingSessions(ctx context.Context, bandID int64, kind string) ([]RecordingSession, error) {
	rows, err := st.db.Query(ctx, `
		SELECT r.session_date, count(*), count(DISTINCT r.variant_id),
		       coalesce((SELECT avg(x.stars)::float8 FROM band_recording_ratings x JOIN band_recordings y ON y.id = x.recording_id
		                 WHERE y.band_id = $1 AND y.kind = $2 AND y.session_date = r.session_date AND NOT y.gone), 0),
		       min(r.path)
		FROM band_recordings r
		WHERE r.band_id = $1 AND r.kind = $2 AND NOT r.gone AND r.session_date IS NOT NULL
		GROUP BY r.session_date ORDER BY r.session_date DESC`, bandID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecordingSession
	for rows.Next() {
		var s RecordingSession
		if err := rows.Scan(&s.Date, &s.Recordings, &s.Songs, &s.Stars, &s.Folder); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SessionRecordings is a session's recordings, in the order they were
// played, personID's stars with each.
func (st *Store) SessionRecordings(ctx context.Context, bandID, personID int64, kind string, date time.Time) ([]BandRecording, error) {
	return st.queryBandRecordings(ctx, `r.band_id = $1 AND r.kind = $3 AND r.session_date = $4 AND NOT r.gone
		ORDER BY r.position NULLS LAST, r.path, r.name`, bandID, personID, kind, date)
}

// SongRecordings is a band's recordings of a song, the best rated first,
// then the latest.
func (st *Store) SongRecordings(ctx context.Context, bandID, personID, variantID int64) ([]BandRecording, error) {
	return st.queryBandRecordings(ctx, `r.band_id = $1 AND r.variant_id = $3 AND NOT r.gone
		ORDER BY 13 DESC, r.session_date DESC NULLS LAST, r.take NULLS FIRST`, bandID, personID, variantID)
}

// GetBandRecording is one of bandID's recordings.
func (st *Store) GetBandRecording(ctx context.Context, bandID, personID, id int64) (BandRecording, error) {
	recs, err := st.queryBandRecordings(ctx, `r.band_id = $1 AND r.id = $3`, bandID, personID, id)
	if err != nil {
		return BandRecording{}, err
	}
	if len(recs) == 0 {
		return BandRecording{}, fmt.Errorf("no such recording %d: %w", id, ErrNotFound)
	}
	return recs[0], nil
}

// UnmatchedTitles is the titles bandID's song recordings give that name
// none of its songs, the most frequent first.
func (st *Store) UnmatchedTitles(ctx context.Context, bandID int64) ([]TitleCount, error) {
	rows, err := st.db.Query(ctx, `
		SELECT title, count(*), max(path) FROM band_recordings
		WHERE band_id = $1 AND NOT gone AND NOT raw AND variant_id IS NULL AND position IS NOT NULL AND kind IN ('rehearsal', 'concert', 'audition')
		  AND file_id::text NOT IN (SELECT from_value FROM band_recording_fixes WHERE band_id = $1 AND kind = 'file')
		GROUP BY title ORDER BY count(*) DESC, title`, bandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TitleCount
	for rows.Next() {
		var t TitleCount
		if err := rows.Scan(&t.Title, &t.Count, &t.Example); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TitleCount is a title and how many recordings give it.
type TitleCount struct {
	Title   string
	Count   int
	Example string // a folder it's in
}

// RateRecording sets personID's stars for a recording of bandID's: ½ to
// 5, by halves; 0 takes them back.
func (st *Store) RateRecording(ctx context.Context, bandID, recordingID, personID int64, stars float64) error {
	var ok bool
	if err := st.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM band_recordings WHERE id = $1 AND band_id = $2)`, recordingID, bandID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no such recording %d: %w", recordingID, ErrNotFound)
	}
	if stars == 0 {
		_, err := st.db.Exec(ctx, `DELETE FROM band_recording_ratings WHERE recording_id = $1 AND person_id = $2`, recordingID, personID)
		return err
	}
	if stars < 0.5 || stars > 5 || stars*2 != float64(int(stars*2)) {
		return errors.New("stars go from ½ to 5, by halves")
	}
	_, err := st.db.Exec(ctx, `
		INSERT INTO band_recording_ratings (recording_id, person_id, stars) VALUES ($1, $2, $3)
		ON CONFLICT (recording_id, person_id) DO UPDATE SET stars = EXCLUDED.stars, rated_at = now()`, recordingID, personID, stars)
	return err
}

// SetSongRecording makes a recording the band's reference for its song,
// the one its playlist plays; recordingID 0 takes it back.
func (st *Store) SetSongRecording(ctx context.Context, bandID, variantID, recordingID, personID int64) error {
	if recordingID == 0 {
		_, err := st.db.Exec(ctx, `DELETE FROM band_song_recordings WHERE band_id = $1 AND variant_id = $2`, bandID, variantID)
		return err
	}
	var ok bool
	if err := st.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM band_recordings WHERE id = $1 AND band_id = $2 AND variant_id = $3)`,
		recordingID, bandID, variantID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("recording %d isn't one of the band's for song %d: %w", recordingID, variantID, ErrNotFound)
	}
	_, err := st.db.Exec(ctx, `
		INSERT INTO band_song_recordings (band_id, variant_id, recording_id, chosen_by_person_id) VALUES ($1, $2, $3, $4)
		ON CONFLICT (band_id, variant_id) DO UPDATE SET recording_id = EXCLUDED.recording_id,
			chosen_by_person_id = EXCLUDED.chosen_by_person_id, chosen_at = now()`,
		bandID, variantID, recordingID, nullID(personID))
	return err
}

// RemoteRecording is what a browser needs to play a recording straight
// from the provider: a pCloud public link's code and file.
type RemoteRecording struct {
	RecordingID int64
	APIHost     string
	Code        string
	FileID      int64
	// Where it's from (Label): its kind, date and take.
	Kind string
	Date *time.Time
	Take *int
}

// Label says where the recording is from, in lang: "Rehearsal 13 Mar
// 2025, take 2".
func (r RemoteRecording) Label(lang string) string {
	return recordingLabel(lang, r.Kind, r.Date, r.Take)
}

// ChosenSongRecording is the recording bandID chose for a song, ready to
// play; nil when it chose none.
func (st *Store) ChosenSongRecording(ctx context.Context, bandID, variantID int64) (*RemoteRecording, error) {
	var r RemoteRecording
	err := st.db.QueryRow(ctx, `
		SELECT r.id, s.api_host, s.code, r.file_id, r.kind, r.session_date, r.take
		FROM band_song_recordings c JOIN band_recordings r ON r.id = c.recording_id
		JOIN band_recording_sources s ON s.id = r.source_id
		WHERE c.band_id = $1 AND c.variant_id = $2 AND NOT r.gone`, bandID, variantID).
		Scan(&r.RecordingID, &r.APIHost, &r.Code, &r.FileID, &r.Kind, &r.Date, &r.Take)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// recordingLabel says where a recording is from, in lang.
func recordingLabel(lang, kind string, date *time.Time, take *int) string {
	label := map[string]string{"rehearsal": "Rehearsal", "concert": "Concert", "audition": "Audition", "studio": "Studio",
		"album": "Album", "setlist": "Set list"}[kind]
	if label == "" {
		label = "Recording"
	}
	label = i18n.T(lang, label)
	if date != nil {
		label += " " + i18n.Date(lang, *date, "2 Jan 2006")
	}
	if take != nil {
		label += i18n.T(lang, ", take %d", *take)
	}
	return label
}

// SongPracticeStats is how much a band rehearsed one of its songs.
type SongPracticeStats struct {
	VariantID  int64
	Title      string
	Sessions   int // rehearsals it was played at
	Recordings int
	Last       *time.Time
	Stars      float64 // its recordings' average
	Concerts   int
	Chosen     bool // the band chose its recording
}

// BandPracticeStats is each of bandID's songs, recorded or not, with how
// often and when last it was rehearsed.
func (st *Store) BandPracticeStats(ctx context.Context, bandID int64) ([]SongPracticeStats, error) {
	songs, err := st.bandSongs(ctx, bandID)
	if err != nil {
		return nil, err
	}
	rows, err := st.db.Query(ctx, `
		SELECT r.variant_id,
		       count(DISTINCT r.session_date) FILTER (WHERE r.kind = 'rehearsal'),
		       count(DISTINCT r.id) FILTER (WHERE r.kind = 'rehearsal'),
		       max(r.session_date) FILTER (WHERE r.kind = 'rehearsal'),
		       coalesce(avg(x.stars)::float8, 0),
		       count(DISTINCT r.session_date) FILTER (WHERE r.kind = 'concert'),
		       EXISTS(SELECT 1 FROM band_song_recordings c WHERE c.band_id = $1 AND c.variant_id = r.variant_id)
		FROM band_recordings r LEFT JOIN band_recording_ratings x ON x.recording_id = r.id
		WHERE r.band_id = $1 AND r.variant_id IS NOT NULL AND NOT r.gone
		GROUP BY r.variant_id`, bandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[int64]SongPracticeStats{}
	for rows.Next() {
		var s SongPracticeStats
		if err := rows.Scan(&s.VariantID, &s.Sessions, &s.Recordings, &s.Last, &s.Stars, &s.Concerts, &s.Chosen); err != nil {
			return nil, err
		}
		byID[s.VariantID] = s
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]SongPracticeStats, 0, len(songs))
	for _, song := range songs {
		s := byID[song.ID]
		s.VariantID, s.Title = song.ID, song.Title
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Sessions != out[j].Sessions {
			return out[i].Sessions > out[j].Sessions
		}
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out, nil
}
