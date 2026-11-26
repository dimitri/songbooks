package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/brand"
)

// A band's albums and live versions made from what it has, as cmd/albums
// runs it (docs/albums-and-live-versions.md §9): its albums and their
// tracks from a mapping (Other Lies': cmd/albums/otherlies.json) and/or
// the masters of its recordings folder (Albums/N - Title/), its songs
// renamed to the release's printed titles where the mapping says so, the
// live versions of the songs its set lists play — made from the songs as
// they are now (their charts carry the band's live edits: the live
// version keeps them, chart, tempo and sync; the set list lines, their
// notes and click-track notes move to it), the album song then taking
// the album's tempo when the master's name (or the mapping) says it —
// and the band's set lists set to use them. MapAlbums does it all in one
// transaction and rolls it back unless told to apply it: the dry run's
// report is what --apply does.

// AlbumsMapping is what cmd/albums reads: a band and its releases.
type AlbumsMapping struct {
	Band       int64  `json:"band"`
	ExpectBand string `json:"expectBand"` // the band's name, checked
	// LiveVersions: "setlists" (the songs the band's set lists play), or
	// the ids.
	LiveVersions    json.RawMessage `json:"liveVersions"`
	SetlistVersions string          `json:"setlistVersions"` // "live", "song", "" to leave it
	SkipMasters     []string        `json:"skipMasters"`     // masters left out, by title
	// FromFolder makes an album of each Albums/N - Title/ folder the
	// mapping doesn't name.
	FromFolder bool            `json:"fromFolder"`
	Albums     []MappingAlbum `json:"albums"`
}

// MappingAlbum is one release of the mapping.
type MappingAlbum struct {
	Title      string   `json:"title"`
	Kind       string   `json:"kind"`
	ReleasedOn string   `json:"releasedOn"`
	Label      string   `json:"label"`
	Credits    string   `json:"credits"`
	Licence    string   `json:"licence"`
	Links      []string `json:"links"`
	Artwork    string   `json:"artwork"` // fetched once by cmd/albums --artwork
	Folder     string   `json:"folder"`  // its masters: band_recordings.path
	Tracks     []MappingTrack `json:"tracks"`
}

// MappingTrack is one track: its song (its id, and its title as the band
// has it, checked), its printed title, rename to rename the song to it.
type MappingTrack struct {
	Song      int64  `json:"song"`
	SongTitle string `json:"songTitle"`
	Title     string `json:"title"`
	Rename    bool   `json:"rename"`
	Duration  string `json:"duration"` // "3:50"
	Tempo     int    `json:"tempo"`
	ISRC      string `json:"isrc"`
	Link      string `json:"link"`
}

// AlbumsOptions are MapAlbums' choices.
type AlbumsOptions struct {
	Apply bool
	// ActorID is who the changes are recorded as (revisions, created_by).
	ActorID int64
	// IgnoreTitles goes on when a song's title isn't the mapping's
	// songTitle (a copy of the band's data under other names): the song
	// is not renamed.
	IgnoreTitles bool
	// Artwork is each album's artwork (by title), fetched and prepared by
	// the caller; an album missing here keeps what it has.
	Artwork map[string]brand.LogoImage
}

// AlbumsReport is what MapAlbums did (or would do), line by line.
type AlbumsReport struct {
	Lines    []string
	Warnings []string
	Albums, Tracks, Renamed, Live, Tempos, SetlistLines int
}

func (r *AlbumsReport) add(format string, a ...any)  { r.Lines = append(r.Lines, fmt.Sprintf(format, a...)) }
func (r *AlbumsReport) warn(format string, a ...any) { r.Warnings = append(r.Warnings, fmt.Sprintf(format, a...)) }

var albumFolder = regexp.MustCompile(`^Albums?/(\d+)\s*-\s*(.+)$`)

type mapSong struct {
	id         int64
	title      string
	body       string
	version    int
	live       bool
	forkedFrom *int64
}

// MapAlbums applies m to its band (or, without opts.Apply, reports what it
// would do and changes nothing).
func (st *Store) MapAlbums(ctx context.Context, m AlbumsMapping, opts AlbumsOptions) (*AlbumsReport, error) {
	rep := &AlbumsReport{}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var bandName string
	if err := tx.QueryRow(ctx, `SELECT name FROM bands WHERE id = $1`, m.Band).Scan(&bandName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("no band %d", m.Band)
		}
		return nil, err
	}
	if m.ExpectBand != "" && !strings.EqualFold(m.ExpectBand, bandName) {
		if !opts.IgnoreTitles {
			return nil, fmt.Errorf("band %d is %q, the mapping is %q's (--ignore-titles to go on)", m.Band, bandName, m.ExpectBand)
		}
		rep.warn("band %d is %q, the mapping is %q's", m.Band, bandName, m.ExpectBand)
	}
	rep.add("Band %d, %s", m.Band, bandName)

	songs := map[int64]*mapSong{}
	rows, err := tx.Query(ctx, `SELECT id, title, body, version, live, forked_from_id FROM songs WHERE owner_band_id = $1`, m.Band)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s mapSong
		if err := rows.Scan(&s.id, &s.title, &s.body, &s.version, &s.live, &s.forkedFrom); err != nil {
			rows.Close()
			return nil, err
		}
		songs[s.id] = &s
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	masters, err := st.AlbumMasters(ctx, m.Band)
	if err != nil {
		return nil, err
	}
	byFolder := map[string][]BandRecordingChoice{}
	for _, r := range masters {
		byFolder[r.Path] = append(byFolder[r.Path], r)
	}
	skip := func(name string) bool {
		for _, s := range m.SkipMasters {
			if s != "" && strings.Contains(strings.ToLower(name), strings.ToLower(s)) {
				return true
			}
		}
		return false
	}

	albums := m.Albums
	if m.FromFolder {
		named := map[string]bool{}
		for _, a := range albums {
			named[a.Folder] = true
		}
		var folders []string
		for f := range byFolder {
			if albumFolder.MatchString(f) && !named[f] {
				folders = append(folders, f)
			}
		}
		sort.Strings(folders)
		for _, f := range folders {
			title := strings.TrimSpace(albumFolder.FindStringSubmatch(f)[2])
			albums = append(albums, MappingAlbum{Title: title, Kind: "album", Folder: f})
		}
	}

	// tempoOf is each song's album tempo, from its track (the mapping,
	// else its master's name).
	tempoOf := map[int64]int{}
	tempoFrom := map[int64]string{}
	renames := map[int64]string{}
	for _, a := range albums {
		if a.Kind == "" {
			a.Kind = "album"
		}
		folderMasters := byFolder[a.Folder]
		if a.Folder != "" && len(folderMasters) == 0 {
			rep.warn("%s: no master in %q", a.Title, a.Folder)
		}
		masterAt := map[int]BandRecordingChoice{}
		var skipped []string
		for _, r := range folderMasters {
			pos := 0
			if r.Position != nil {
				pos = *r.Position
			}
			if p, _, _ := MasterFacts(r.Name); p != 0 {
				pos = p
			}
			if pos == 0 {
				rep.warn("%s: master %q has no track number", a.Title, r.Name)
				continue
			}
			if skip(r.Name) {
				skipped = append(skipped, fmt.Sprintf("  master %d %q left out (skipMasters)", pos, r.Name))
				continue
			}
			masterAt[pos] = r
		}
		tracks := a.Tracks
		if len(tracks) == 0 && len(masterAt) > 0 {
			// From the folder alone: one track per numbered master, its
			// song the one the recordings matched.
			var nums []int
			for n := range masterAt {
				nums = append(nums, n)
			}
			sort.Ints(nums)
			for _, n := range nums {
				r := masterAt[n]
				t := MappingTrack{Title: recordingTitleOf(r.Name)}
				var v *int64
				_ = tx.QueryRow(ctx, `SELECT variant_id FROM band_recordings WHERE id = $1`, r.ID).Scan(&v)
				if v != nil {
					if s := songs[*v]; s != nil && !s.live {
						t.Song, t.SongTitle, t.Title = s.id, s.title, s.title
					}
				}
				tracks = append(tracks, t)
			}
		}
		in := AlbumInput{Title: a.Title, Kind: a.Kind, ReleasedOn: a.ReleasedOn, Label: a.Label, Credits: a.Credits, Licence: a.Licence}
		for _, l := range a.Links {
			in.Links = append(in.Links, AlbumLink{URL: l})
		}
		for i, t := range tracks {
			pos := i + 1
			ti := AlbumTrackInput{Disc: 1, SongID: t.Song, Title: t.Title, Tempo: t.Tempo, ISRC: t.ISRC, LinkURL: t.Link}
			if t.Song != 0 {
				s := songs[t.Song]
				switch {
				case s == nil:
					return nil, fmt.Errorf("%s, track %d: song %d isn't band %d's", a.Title, pos, t.Song, m.Band)
				case s.live:
					return nil, fmt.Errorf("%s, track %d: song %d is a live version", a.Title, pos, t.Song)
				case t.SongTitle != "" && !strings.EqualFold(strings.TrimSpace(t.SongTitle), strings.TrimSpace(s.title)) &&
					!(t.Rename && strings.EqualFold(strings.TrimSpace(t.Title), strings.TrimSpace(s.title))): // renamed by a run before
					if !opts.IgnoreTitles {
						return nil, fmt.Errorf("%s, track %d: song %d is %q, the mapping says %q (--ignore-titles to go on)", a.Title, pos, t.Song, s.title, t.SongTitle)
					}
					rep.warn("%s, track %d: song %d is %q, the mapping says %q: not renamed", a.Title, pos, t.Song, s.title, t.SongTitle)
				default:
					if t.Rename && t.Title != "" && t.Title != s.title {
						renames[s.id] = t.Title
					}
				}
				if ti.Title == "" {
					ti.Title = s.title
				}
			}
			if t.Duration != "" {
				d, err := parseDuration(t.Duration)
				if err != nil {
					return nil, fmt.Errorf("%s, track %d: %w", a.Title, pos, err)
				}
				ti.DurationS = d
			}
			master := ""
			if r, ok := masterAt[pos]; ok {
				ti.RecordingID = r.ID
				master = r.Name
				_, tempo, isrc := MasterFacts(r.Name)
				if ti.Tempo == 0 && tempo != 0 {
					ti.Tempo = tempo
				}
				if ti.ISRC == "" {
					ti.ISRC = isrc
				}
				delete(masterAt, pos)
			}
			if t.Song != 0 && ti.Tempo != 0 && a.Kind != "single" {
				if _, set := tempoOf[t.Song]; !set {
					tempoOf[t.Song] = ti.Tempo
					tempoFrom[t.Song] = a.Title
					if master != "" {
						tempoFrom[t.Song] = "master " + master
					}
				}
			}
			in.Tracks = append(in.Tracks, ti)
		}
		for pos, r := range masterAt {
			rep.warn("%s: master %d %q has no track in the mapping: not added", a.Title, pos, r.Name)
		}
		id, made, err := st.upsertAlbumTx(ctx, tx, m.Band, opts.ActorID, in)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", a.Title, err)
		}
		verb := "update"
		if made {
			verb = "create"
		}
		rep.Albums++
		art := "no artwork"
		if img, ok := opts.Artwork[a.Title]; ok {
			if err := setAlbumCoverTx(ctx, tx, id, img, opts.ActorID); err != nil {
				return nil, err
			}
			art = fmt.Sprintf("artwork %dx%d (%d KB)", img.Width, img.Height, len(img.Data)/1024)
		} else if a.Artwork != "" {
			art = "artwork not fetched (run with --artwork)"
		}
		date := a.ReleasedOn
		if date == "" {
			date = "no date"
		}
		tracksWord := "tracks"
		if len(in.Tracks) == 1 {
			tracksWord = "track"
		}
		rep.add("%s %s %q (%s, %s), %d %s, %s", verb, a.Kind, a.Title, date, strings.Join(nonEmptyStrings(a.Label, a.Licence), ", "), len(in.Tracks), tracksWord, art)
		for i, t := range in.Tracks {
			rep.Tracks++
			var bits []string
			if t.SongID != 0 {
				bits = append(bits, fmt.Sprintf("song %d", t.SongID))
			} else {
				bits = append(bits, "no song")
			}
			if t.DurationS > 0 {
				bits = append(bits, fmt.Sprintf("%d:%02d", t.DurationS/60, t.DurationS%60))
			}
			if t.Tempo > 0 {
				bits = append(bits, fmt.Sprintf("%d bpm", t.Tempo))
			}
			if t.ISRC != "" {
				bits = append(bits, t.ISRC)
			}
			if t.RecordingID != 0 {
				bits = append(bits, fmt.Sprintf("master #%d", t.RecordingID))
			} else if a.Folder != "" {
				bits = append(bits, "no master")
			}
			rep.add("  %2d. %s — %s", i+1, t.Title, strings.Join(bits, " · "))
		}
		rep.Lines = append(rep.Lines, skipped...)
	}

	// The printed titles.
	var renamed []int64
	for id := range renames {
		renamed = append(renamed, id)
	}
	sort.Slice(renamed, func(i, j int) bool { return renamed[i] < renamed[j] })
	for _, id := range renamed {
		s := songs[id]
		to := renames[id]
		body := setHeader(s.body, "title", to)
		if _, err := saveSongTx(ctx, tx, SongSave{SongID: id, Title: to, Artist: songArtist(ctx, tx, id), ReferenceURL: songRef(ctx, tx, id), Body: body,
			ActorID: opts.ActorID, Summary: "The release's title: " + to}); err != nil {
			return nil, fmt.Errorf("renaming song %d: %w", id, err)
		}
		// Its work too, when it's only the band's.
		if _, err := tx.Exec(ctx, `
			UPDATE works w SET title = $2 FROM songs s
			WHERE s.id = $1 AND w.id = s.work_id
			  AND NOT EXISTS (SELECT 1 FROM songs o WHERE o.work_id = w.id AND o.owner_band_id IS DISTINCT FROM s.owner_band_id)
			  AND NOT EXISTS (SELECT 1 FROM works x WHERE x.id <> w.id AND x.norm_title = lower(btrim($2)) AND x.norm_artist = w.norm_artist)`,
			id, to); err != nil {
			return nil, err
		}
		rep.add("rename song %d %q → %q", id, s.title, to)
		s.title, s.body = to, body
		rep.Renamed++
	}

	// The live versions.
	liveIDs, err := liveSongsOf(ctx, tx, m, songs, rep)
	if err != nil {
		return nil, err
	}
	for _, id := range liveIDs {
		s := songs[id]
		var have int64
		_ = tx.QueryRow(ctx, `SELECT id FROM songs WHERE forked_from_id = $1 AND live AND owner_band_id = $2`, id, m.Band).Scan(&have)
		if have != 0 {
			rep.add("live version of %q (%d): there already (%d)", s.title, id, have)
			continue
		}
		var lines int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM setlist_items i JOIN setlists sl ON sl.id = i.setlist_id
			WHERE i.variant_id = $1 AND sl.owner_band_id = $2`, id, m.Band).Scan(&lines); err != nil {
			return nil, err
		}
		liveTempo := headerValue(s.body, "tempo")
		liveID, err := st.makeLiveVersionTx(ctx, tx, m.Band, id, opts.ActorID, "Live", true)
		if err != nil {
			return nil, fmt.Errorf("live version of song %d: %w", id, err)
		}
		rep.Live++
		rep.SetlistLines += lines
		tempoNote := ""
		if album, ok := tempoOf[id]; ok {
			if strconv.Itoa(album) == liveTempo {
				tempoNote = fmt.Sprintf("album tempo %d, the same as live", album)
			} else {
				body := setHeader(s.body, "tempo", strconv.Itoa(album))
				if _, err := saveSongTx(ctx, tx, SongSave{SongID: id, Title: s.title, Artist: songArtist(ctx, tx, id), ReferenceURL: songRef(ctx, tx, id), Body: body,
					ActorID: opts.ActorID, Summary: fmt.Sprintf("The album's tempo: %d (from %s); the live tempo is the live version's", album, tempoFrom[id])}); err != nil {
					return nil, fmt.Errorf("album tempo of song %d: %w", id, err)
				}
				s.body = body
				rep.Tempos++
				tempoNote = fmt.Sprintf("album song's tempo %s → %d (from %s)", orDash(liveTempo), album, tempoFrom[id])
			}
		} else {
			if liveTempo == "" {
				tempoNote = "album tempo unknown: the album song has no tempo either"
			} else {
				tempoNote = fmt.Sprintf("album tempo unknown: the album song keeps %s bpm, as live", liveTempo)
			}
		}
		rep.add("live version of %q (%d) → song %d, live tempo %s, %d set list line(s) moved; %s", s.title, id, liveID, orDash(liveTempo), lines, tempoNote)
	}

	// The band's setting.
	if m.SetlistVersions != "" {
		var cur string
		if err := tx.QueryRow(ctx, `SELECT setlist_versions FROM bands WHERE id = $1`, m.Band).Scan(&cur); err != nil {
			return nil, err
		}
		if cur != m.SetlistVersions {
			if _, err := tx.Exec(ctx, `UPDATE bands SET setlist_versions = $2 WHERE id = $1`, m.Band, m.SetlistVersions); err != nil {
				return nil, err
			}
			rep.add("set lists use: %s → %s", cur, m.SetlistVersions)
		} else {
			rep.add("set lists use: %s already", cur)
		}
	}
	if opts.Apply {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
	}
	return rep, nil
}

// liveSongsOf is the songs to make live versions of: the mapping's ids,
// or the band's own songs its set lists play ("setlists").
func liveSongsOf(ctx context.Context, tx pgx.Tx, m AlbumsMapping, songs map[int64]*mapSong, rep *AlbumsReport) ([]int64, error) {
	raw := strings.TrimSpace(string(m.LiveVersions))
	if raw == "" || raw == "null" {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT CASE WHEN s.live THEN s.forked_from_id ELSE s.id END
		FROM setlist_items i JOIN setlists sl ON sl.id = i.setlist_id JOIN songs s ON s.id = i.variant_id
		WHERE sl.owner_band_id = $1 AND s.owner_band_id = $1`, m.Band)
	if err != nil {
		return nil, err
	}
	played, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	var ids []int64
	var word string
	if err := json.Unmarshal(m.LiveVersions, &word); err == nil {
		if word != "setlists" {
			return nil, fmt.Errorf(`liveVersions is "setlists" or a list of song ids, not %q`, word)
		}
		ids = played
	} else if err := json.Unmarshal(m.LiveVersions, &ids); err != nil {
		return nil, fmt.Errorf("liveVersions: %w", err)
	} else {
		listed := map[int64]bool{}
		for _, id := range ids {
			listed[id] = true
		}
		for _, id := range played {
			if !listed[id] {
				rep.warn("song %d is in a set list but not in liveVersions: no live version made", id)
			}
			delete(listed, id)
		}
		for id := range listed {
			rep.warn("song %d is in liveVersions but in no set list", id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if s := songs[id]; s == nil || s.live {
			return nil, fmt.Errorf("liveVersions: song %d isn't one of the band's album songs", id)
		}
	}
	return ids, nil
}

// upsertAlbumTx makes the band's album in (or, one of that title and kind
// being there, updates it and replaces its tracks).
func (st *Store) upsertAlbumTx(ctx context.Context, tx pgx.Tx, bandID, actorID int64, in AlbumInput) (int64, bool, error) {
	on, year, err := in.clean()
	if err != nil {
		return 0, false, err
	}
	links, _ := json.Marshal(in.Links)
	var by *int64
	if actorID > 0 {
		by = &actorID
	}
	var id int64
	err = tx.QueryRow(ctx, `SELECT id FROM albums WHERE band_id = $1 AND lower(title) = lower($2) AND kind = $3`, bandID, in.Title, in.Kind).Scan(&id)
	made := false
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := tx.QueryRow(ctx, `
			INSERT INTO albums (band_id, title, kind, released_on, release_year, label, credits, licence, links, created_by_person_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
			bandID, in.Title, in.Kind, on, year, in.Label, in.Credits, in.Licence, links, by).Scan(&id); err != nil {
			return 0, false, err
		}
		made = true
	case err != nil:
		return 0, false, err
	default:
		if _, err := tx.Exec(ctx, `
			UPDATE albums SET title = $2, released_on = coalesce($3, released_on), release_year = coalesce($4, release_year),
			  label = CASE WHEN $5 = '' THEN label ELSE $5 END, credits = CASE WHEN $6 = '' THEN credits ELSE $6 END,
			  licence = CASE WHEN $7 = '' THEN licence ELSE $7 END, links = CASE WHEN $8::jsonb = '[]'::jsonb THEN links ELSE $8 END
			WHERE id = $1`, id, in.Title, on, year, in.Label, in.Credits, in.Licence, links); err != nil {
			return 0, false, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM album_tracks WHERE album_id = $1`, id); err != nil {
			return 0, false, err
		}
	}
	if err := writeAlbumTracks(ctx, tx, bandID, id, in.Tracks); err != nil {
		return 0, false, err
	}
	return id, made, nil
}

func setAlbumCoverTx(ctx context.Context, tx pgx.Tx, albumID int64, img brand.LogoImage, by int64) error {
	var byp *int64
	if by > 0 {
		byp = &by
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO album_covers (album_id, content_type, width, height, byte_size, sha256, data, created_by_person_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (album_id) DO UPDATE SET content_type = EXCLUDED.content_type, width = EXCLUDED.width,
		  height = EXCLUDED.height, byte_size = EXCLUDED.byte_size, sha256 = EXCLUDED.sha256, data = EXCLUDED.data,
		  created_at = now(), created_by_person_id = EXCLUDED.created_by_person_id`,
		albumID, img.ContentType, img.Width, img.Height, len(img.Data), img.SHA256, img.Data, byp)
	return err
}

func songArtist(ctx context.Context, tx pgx.Tx, id int64) string {
	var a string
	_ = tx.QueryRow(ctx, `SELECT artist FROM songs WHERE id = $1`, id).Scan(&a)
	return a
}

func songRef(ctx context.Context, tx pgx.Tx, id int64) string {
	var a string
	_ = tx.QueryRow(ctx, `SELECT reference_url FROM songs WHERE id = $1`, id).Scan(&a)
	return a
}

// headerLine is a chart's "key: value" line, at the top of its text.
func headerLine(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?mi)^` + regexp.QuoteMeta(key) + `:[ \t]*(.*)$`)
}

// headerValue is the chart's header value for key, "" when none.
func headerValue(body, key string) string {
	head, _ := chartHead(body)
	if m := headerLine(key).FindStringSubmatch(head); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// setHeader sets the chart's header key to value: its line rewritten
// (its alignment kept), or added after the header's last line.
func setHeader(body, key, value string) string {
	head, rest := chartHead(body)
	re := headerLine(key)
	if loc := re.FindStringSubmatchIndex(head); loc != nil {
		return head[:loc[2]] + value + head[loc[3]:] + rest
	}
	if head != "" && !strings.HasSuffix(head, "\n") {
		head += "\n"
	}
	return head + key + ": " + value + "\n" + rest
}

// chartHead splits a chart's text into its header (the "key: value"
// lines it starts with) and the rest.
func chartHead(body string) (string, string) {
	i := 0
	for i < len(body) {
		j := strings.IndexByte(body[i:], '\n')
		line := body[i:]
		if j >= 0 {
			line = body[i : i+j]
		}
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "[") || !strings.Contains(t, ":") {
			break
		}
		if j < 0 {
			i = len(body)
			break
		}
		i += j + 1
	}
	return body[:i], body[i:]
}

func parseDuration(s string) (int, error) {
	s = strings.TrimSpace(s)
	if m, sec, ok := strings.Cut(s, ":"); ok {
		mi, err1 := strconv.Atoi(m)
		si, err2 := strconv.Atoi(sec)
		if err1 == nil && err2 == nil && si < 60 {
			return mi*60 + si, nil
		}
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return int(d.Seconds()), nil
	}
	return 0, fmt.Errorf("a duration reads 3:50, not %q", s)
}

// recordingTitleOf is a master's name without its number, its band, its
// album, its mix and its extension: "03 Other lies-UltraBlanc-Bones_90_Mix.v5
// FR-9W1-25-34056.wav" is "Bones".
func recordingTitleOf(name string) string {
	stem := strings.TrimSuffix(name, pathExt(name))
	stem = masterISRC.ReplaceAllString(stem, "")
	parts := regexp.MustCompile(`\s+-\s+|-`).Split(stem, -1)
	best := strings.TrimSpace(parts[len(parts)-1])
	best = regexp.MustCompile(`^\d{1,2}\s+`).ReplaceAllString(best, "")
	if i := strings.IndexByte(best, '_'); i > 0 {
		best = best[:i]
	}
	return strings.TrimSpace(best)
}

func nonEmptyStrings(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{"no label"}
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
