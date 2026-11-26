package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/brand"
)

// Albums (docs/albums-and-live-versions.md, migration 0146): a band's
// releases — an album, an EP, a single, a live album — with their facts,
// their uploaded artwork, their links and their ordered tracks. A track
// is one of the band's songs as printed on the release, its master a file
// of the band's recordings folder, streamed through the app (nothing
// copied, nothing analysed). The band's members see its albums; a public
// album, everyone signed in. Whoever may edit the band's songbook
// (edit_songbook) edits its albums; publish makes one public.

// Album kinds.
var albumKinds = map[string]bool{"album": true, "ep": true, "single": true, "live": true}

// AlbumLink is one of an album's links: Bandcamp, Spotify, …
type AlbumLink struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

// Label is the link's name as a page shows it: "Bandcamp", or its site.
func (l AlbumLink) Label() string {
	switch l.Kind {
	case "bandcamp":
		return "Bandcamp"
	case "spotify":
		return "Spotify"
	case "apple", "applemusic", "apple-music":
		return "Apple Music"
	case "deezer":
		return "Deezer"
	case "youtube":
		return "YouTube"
	case "soundcloud":
		return "SoundCloud"
	}
	if u, err := url.Parse(l.URL); err == nil && u.Host != "" {
		return strings.TrimPrefix(u.Host, "www.")
	}
	return l.URL
}

// LinkKindOf guesses a link's kind from its address.
func LinkKindOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "other"
	}
	h := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	switch {
	case strings.HasSuffix(h, "bandcamp.com"):
		return "bandcamp"
	case strings.HasSuffix(h, "spotify.com"):
		return "spotify"
	case strings.HasSuffix(h, "music.apple.com"), strings.HasSuffix(h, "itunes.apple.com"):
		return "apple"
	case strings.HasSuffix(h, "deezer.com"):
		return "deezer"
	case strings.HasSuffix(h, "youtube.com"), h == "youtu.be":
		return "youtube"
	case strings.HasSuffix(h, "soundcloud.com"):
		return "soundcloud"
	}
	return "other"
}

// Album is a band's release.
type Album struct {
	ID          int64
	BandID      int64
	BandName    string
	Title       string
	Kind        string
	ReleasedOn  *time.Time
	ReleaseYear int // released_on's year, else the year alone; 0: unknown
	Label       string
	Credits     string
	Licence     string
	Links       []AlbumLink
	Public      bool
	CoverSHA    string // "" when it has no artwork
	Tracks      []AlbumTrack
}

// CoverPath is where its artwork is served, "" when it has none.
func (a *Album) CoverPath() string {
	if a == nil || a.CoverSHA == "" {
		return ""
	}
	return AlbumCoverPath(a.ID, a.CoverSHA)
}

// AlbumCoverPath is album id's artwork address: its hash is in it, so it
// is cached for good and a new artwork is a new address.
func AlbumCoverPath(albumID int64, sha string) string {
	if len(sha) > 16 {
		sha = sha[:16]
	}
	return fmt.Sprintf("/album-covers/%d/%s.jpg", albumID, sha)
}

// Seconds is the album's length, the tracks with a duration summed.
func (a *Album) Seconds() int {
	n := 0
	for _, t := range a.Tracks {
		if t.DurationS != nil {
			n += *t.DurationS
		}
	}
	return n
}

// AlbumTrack is one track of an album.
type AlbumTrack struct {
	ID          int64
	Disc        int
	Position    int
	SongID      *int64
	Title       string // as printed on the release
	DurationS   *int
	Tempo       *int
	ISRC        string
	RecordingID *int64 // its master in the band's folder
	LinkURL     string

	// Read with it: the song's own title, whether it's public, its live
	// version (0: none); the master's file name and folder.
	SongTitle     string
	SongPublic    bool
	LiveID        int64
	RecordingName string
	RecordingPath string
}

// Duration is "3:50", "" when unknown.
func (t AlbumTrack) Duration() string {
	if t.DurationS == nil {
		return ""
	}
	return fmt.Sprintf("%d:%02d", *t.DurationS/60, *t.DurationS%60)
}

// FormatDuration is a length in seconds as "37 min" or "3:50".
func FormatDuration(s int, long bool) string {
	if long {
		if s >= 3600 {
			return fmt.Sprintf("%d h %02d", s/3600, s%3600/60)
		}
		return fmt.Sprintf("%d min", (s+30)/60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// songAlbumSQL is the title of the first album song alias is on (a live
// version: its album song's), the albums before the singles, "" when
// none: what pages that showed works.album show now.
func songAlbumSQL(alias string) string {
	return `coalesce((SELECT a.title FROM album_tracks t JOIN albums a ON a.id = t.album_id
		WHERE t.song_id = CASE WHEN ` + alias + `.live THEN ` + alias + `.forked_from_id ELSE ` + alias + `.id END
		ORDER BY a.kind = 'single', a.released_on NULLS LAST, a.id LIMIT 1), '')`
}

const albumCols = `a.id, a.band_id, b.name, a.title, a.kind, a.released_on, coalesce(extract(year FROM a.released_on)::int, a.release_year, 0),
	a.label, a.credits, a.licence, a.links, a.public, coalesce(c.sha256, '')`

const albumFrom = `albums a JOIN bands b ON b.id = a.band_id LEFT JOIN album_covers c ON c.album_id = a.id`

// albumOrder: the albums (and EPs, live albums), newest first, then the
// singles, newest first.
const albumOrder = `a.kind = 'single', coalesce(a.released_on, make_date(coalesce(a.release_year, 1900), 1, 1)) DESC, lower(a.title)`

func scanAlbum(row pgx.Row) (*Album, error) {
	var a Album
	var links []byte
	if err := row.Scan(&a.ID, &a.BandID, &a.BandName, &a.Title, &a.Kind, &a.ReleasedOn, &a.ReleaseYear,
		&a.Label, &a.Credits, &a.Licence, &links, &a.Public, &a.CoverSHA); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(links, &a.Links)
	return &a, nil
}

// BandAlbums is bandID's releases with their tracks: the albums newest
// first, then the singles.
func (st *Store) BandAlbums(ctx context.Context, bandID int64) ([]*Album, error) {
	rows, err := st.db.Query(ctx, `SELECT `+albumCols+` FROM `+albumFrom+` WHERE a.band_id = $1 ORDER BY `+albumOrder, bandID)
	if err != nil {
		return nil, err
	}
	var out []*Album
	for rows.Next() {
		a, err := scanAlbum(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := st.loadTracks(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAlbum is one album with its tracks; ErrNotFound when there's none.
func (st *Store) GetAlbum(ctx context.Context, albumID int64) (*Album, error) {
	a, err := scanAlbum(st.db.QueryRow(ctx, `SELECT `+albumCols+` FROM `+albumFrom+` WHERE a.id = $1`, albumID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such album %d: %w", albumID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	if err := st.loadTracks(ctx, []*Album{a}); err != nil {
		return nil, err
	}
	return a, nil
}

func (st *Store) loadTracks(ctx context.Context, albums []*Album) error {
	if len(albums) == 0 {
		return nil
	}
	ids := make([]int64, len(albums))
	byID := map[int64]*Album{}
	for i, a := range albums {
		ids[i] = a.ID
		byID[a.ID] = a
	}
	rows, err := st.db.Query(ctx, `
		SELECT t.album_id, t.id, t.disc, t.position, t.song_id, t.title, t.duration_s, t.tempo, t.isrc, t.recording_id, t.link_url,
		       coalesce(s.title, ''), coalesce(s.public, false),
		       coalesce((SELECT l.id FROM songs l WHERE l.forked_from_id = s.id AND l.live AND l.owner_band_id = a.band_id), 0),
		       coalesce(r.name, ''), coalesce(r.path, '')
		FROM album_tracks t JOIN albums a ON a.id = t.album_id
		LEFT JOIN songs s ON s.id = t.song_id
		LEFT JOIN band_recordings r ON r.id = t.recording_id AND NOT r.gone
		WHERE t.album_id = ANY($1)
		ORDER BY t.album_id, t.disc, t.position`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var albumID int64
		var t AlbumTrack
		if err := rows.Scan(&albumID, &t.ID, &t.Disc, &t.Position, &t.SongID, &t.Title, &t.DurationS, &t.Tempo, &t.ISRC, &t.RecordingID, &t.LinkURL,
			&t.SongTitle, &t.SongPublic, &t.LiveID, &t.RecordingName, &t.RecordingPath); err != nil {
			return err
		}
		if t.RecordingName == "" {
			t.RecordingID = nil // gone from the folder
		}
		byID[albumID].Tracks = append(byID[albumID].Tracks, t)
	}
	return rows.Err()
}

// AlbumRights is what one person may do with an album.
type AlbumRights struct {
	See     bool // the band's current members; anyone signed in when it's public
	Edit    bool // edit_songbook (a site admin too)
	Publish bool // publish (a site admin too)
}

// AlbumRightsFor is personID's rights on album (one of BandAlbums' or
// GetAlbum's).
func (st *Store) AlbumRightsFor(ctx context.Context, personID int64, a *Album) (AlbumRights, error) {
	var r AlbumRights
	var member, admin bool
	err := st.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM band_members WHERE band_id = $1 AND person_id = $2 AND upper_inf(validity)),
		       coalesce((SELECT is_admin FROM people WHERE id = $2), false),
		       has_band_privilege($1, $2, 'edit_songbook'), has_band_privilege($1, $2, 'publish')`,
		a.BandID, personID).Scan(&member, &admin, &r.Edit, &r.Publish)
	if err != nil {
		return r, err
	}
	if admin {
		r.Edit, r.Publish = true, true
	}
	r.See = personID > 0 && (member || admin || a.Public)
	return r, nil
}

// AlbumInput is an album's fields and tracks, as the editor saves them.
type AlbumInput struct {
	Title       string            `json:"title"`
	Kind        string            `json:"kind"`
	ReleasedOn  string            `json:"releasedOn"` // 2006-01-02, or ""
	ReleaseYear int               `json:"releaseYear"`
	Label       string            `json:"label"`
	Credits     string            `json:"credits"`
	Licence     string            `json:"licence"`
	Links       []AlbumLink       `json:"links"`
	Tracks      []AlbumTrackInput `json:"tracks"`
	KeepTracks  bool              `json:"-"` // SaveAlbum: leave the tracks as they are
}

// AlbumTrackInput is one track, in the album's order (disc 0 is disc 1).
type AlbumTrackInput struct {
	Disc        int    `json:"disc"`
	SongID      int64  `json:"songId"`
	Title       string `json:"title"`
	DurationS   int    `json:"durationS"`
	Tempo       int    `json:"tempo"`
	ISRC        string `json:"isrc"`
	RecordingID int64  `json:"recordingId"`
	LinkURL     string `json:"linkUrl"`
}

var isrcRe = regexp.MustCompile(`^[A-Z]{2}-?[A-Z0-9]{3}-?\d{2}-?\d{5}$`)

// clean checks in and tidies it: ErrInvalid says what's wrong.
func (in *AlbumInput) clean() (*time.Time, *int, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || len(in.Title) > 200 {
		return nil, nil, fmt.Errorf("an album needs a title, 200 characters at most: %w", ErrInvalid)
	}
	if in.Kind == "" {
		in.Kind = "album"
	}
	if !albumKinds[in.Kind] {
		return nil, nil, fmt.Errorf("no album kind %q: %w", in.Kind, ErrInvalid)
	}
	var on *time.Time
	if s := strings.TrimSpace(in.ReleasedOn); s != "" {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			return nil, nil, fmt.Errorf("a release date reads 2026-04-10: %w", ErrInvalid)
		}
		on = &d
	}
	var year *int
	if in.ReleaseYear != 0 {
		if in.ReleaseYear < 1900 || in.ReleaseYear > 2200 {
			return nil, nil, fmt.Errorf("no release year %d: %w", in.ReleaseYear, ErrInvalid)
		}
		year = &in.ReleaseYear
	}
	in.Label, in.Credits, in.Licence = strings.TrimSpace(in.Label), strings.TrimSpace(in.Credits), strings.TrimSpace(in.Licence)
	if len(in.Label) > 200 || len(in.Credits) > 4000 || len(in.Licence) > 200 {
		return nil, nil, fmt.Errorf("the label, credits or licence is too long: %w", ErrInvalid)
	}
	links := in.Links[:0]
	for _, l := range in.Links {
		l.Kind, l.URL = strings.ToLower(strings.TrimSpace(l.Kind)), strings.TrimSpace(l.URL)
		if l.URL == "" {
			continue
		}
		if u, err := url.Parse(l.URL); err != nil || u.Scheme != "https" || u.Host == "" {
			return nil, nil, fmt.Errorf("a link is an https address: %q: %w", l.URL, ErrInvalid)
		}
		if l.Kind == "" {
			l.Kind = LinkKindOf(l.URL)
		}
		links = append(links, l)
	}
	in.Links = links
	if len(in.Links) > 20 || len(in.Tracks) > 200 {
		return nil, nil, fmt.Errorf("too many links or tracks: %w", ErrInvalid)
	}
	for i := range in.Tracks {
		t := &in.Tracks[i]
		t.Title = strings.TrimSpace(t.Title)
		t.ISRC = strings.ToUpper(strings.TrimSpace(t.ISRC))
		t.LinkURL = strings.TrimSpace(t.LinkURL)
		if t.Disc <= 0 {
			t.Disc = 1
		}
		if t.Title == "" && t.SongID == 0 {
			return nil, nil, fmt.Errorf("track %d has neither a title nor a song: %w", i+1, ErrInvalid)
		}
		if t.ISRC != "" && !isrcRe.MatchString(t.ISRC) {
			return nil, nil, fmt.Errorf("track %d: %q isn't an ISRC: %w", i+1, t.ISRC, ErrInvalid)
		}
		if t.DurationS < 0 || t.DurationS > 6*3600 || (t.Tempo != 0 && (t.Tempo < 20 || t.Tempo > 400)) {
			return nil, nil, fmt.Errorf("track %d: its duration or tempo: %w", i+1, ErrInvalid)
		}
		if t.LinkURL != "" {
			if u, err := url.Parse(t.LinkURL); err != nil || u.Scheme != "https" || u.Host == "" {
				return nil, nil, fmt.Errorf("track %d: a link is an https address: %w", i+1, ErrInvalid)
			}
		}
	}
	return on, year, nil
}

func (st *Store) mayEditBandAlbums(ctx context.Context, q dbtx, personID, bandID int64) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT has_band_privilege($1, $2, 'edit_songbook') OR coalesce((SELECT is_admin FROM people WHERE id = $2), false)`,
		bandID, personID).Scan(&ok)
	return ok, err
}

// CreateAlbum makes bandID's new album for personID (edit_songbook), its
// tracks in.Tracks; ErrForbidden, ErrInvalid, ErrConflict (the band has
// an album of that title and kind already).
func (st *Store) CreateAlbum(ctx context.Context, personID, bandID int64, in AlbumInput) (int64, error) {
	if ok, err := st.mayEditBandAlbums(ctx, st.db, personID, bandID); err != nil {
		return 0, err
	} else if !ok {
		return 0, fmt.Errorf("person %d may not edit band %d's albums: %w", personID, bandID, ErrForbidden)
	}
	on, year, err := in.clean()
	if err != nil {
		return 0, err
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	links, _ := json.Marshal(in.Links)
	var by *int64
	if personID > 0 {
		by = &personID
	}
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO albums (band_id, title, kind, released_on, release_year, label, credits, licence, links, created_by_person_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (band_id, lower(title), kind) DO NOTHING RETURNING id`,
		bandID, in.Title, in.Kind, on, year, in.Label, in.Credits, in.Licence, links, by).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("band %d has a %s named %q already: %w", bandID, in.Kind, in.Title, ErrConflict)
	}
	if err != nil {
		return 0, err
	}
	if err := writeAlbumTracks(ctx, tx, bandID, id, in.Tracks); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// SaveAlbum saves album's fields and, unless in.KeepTracks, its tracks
// (in.Tracks, in order: the positions are theirs), for personID.
func (st *Store) SaveAlbum(ctx context.Context, personID, albumID int64, in AlbumInput) error {
	a, err := st.GetAlbum(ctx, albumID)
	if err != nil {
		return err
	}
	if ok, err := st.mayEditBandAlbums(ctx, st.db, personID, a.BandID); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("person %d may not edit album %d: %w", personID, albumID, ErrForbidden)
	}
	on, year, err := in.clean()
	if err != nil {
		return err
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	links, _ := json.Marshal(in.Links)
	var clash bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM albums WHERE band_id = $1 AND lower(title) = lower($2) AND kind = $3 AND id <> $4)`,
		a.BandID, in.Title, in.Kind, albumID).Scan(&clash); err != nil {
		return err
	}
	if clash {
		return fmt.Errorf("the band has a %s named %q already: %w", in.Kind, in.Title, ErrConflict)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE albums SET title = $2, kind = $3, released_on = $4, release_year = $5, label = $6, credits = $7, licence = $8, links = $9
		WHERE id = $1`, albumID, in.Title, in.Kind, on, year, in.Label, in.Credits, in.Licence, links); err != nil {
		return err
	}
	if !in.KeepTracks {
		if _, err := tx.Exec(ctx, `DELETE FROM album_tracks WHERE album_id = $1`, albumID); err != nil {
			return err
		}
		if err := writeAlbumTracks(ctx, tx, a.BandID, albumID, in.Tracks); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// writeAlbumTracks inserts tracks, numbered in their order within each
// disc. A track's song is one of the band's (its own, or one its
// songbooks hold), never a live version; its master, a file of the band's
// folder. A track with no title takes its song's.
func writeAlbumTracks(ctx context.Context, tx pgx.Tx, bandID, albumID int64, tracks []AlbumTrackInput) error {
	pos := map[int]int{}
	for i, t := range tracks {
		var song, rec *int64
		var dur, tempo *int
		if t.SongID != 0 {
			var title string
			var live bool
			err := tx.QueryRow(ctx, `
				SELECT s.title, s.live FROM songs s
				WHERE s.id = $1 AND (s.owner_band_id = $2 OR s.id IN (
				  SELECT se.variant_id FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id WHERE sb.owner_band_id = $2))`,
				t.SongID, bandID).Scan(&title, &live)
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("track %d: song %d isn't one of the band's: %w", i+1, t.SongID, ErrInvalid)
			}
			if err != nil {
				return err
			}
			if live {
				return fmt.Errorf("track %d: song %d is a live version; an album holds the album song: %w", i+1, t.SongID, ErrInvalid)
			}
			if t.Title == "" {
				t.Title = title
			}
			song = &t.SongID
		}
		if t.RecordingID != 0 {
			var ok bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM band_recordings WHERE id = $1 AND band_id = $2)`, t.RecordingID, bandID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("track %d: recording %d isn't in the band's folder: %w", i+1, t.RecordingID, ErrInvalid)
			}
			rec = &t.RecordingID
		}
		if t.DurationS > 0 {
			dur = &t.DurationS
		}
		if t.Tempo > 0 {
			tempo = &t.Tempo
		}
		pos[t.Disc]++
		if _, err := tx.Exec(ctx, `
			INSERT INTO album_tracks (album_id, disc, position, song_id, title, duration_s, tempo, isrc, recording_id, link_url)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			albumID, t.Disc, pos[t.Disc], song, t.Title, dur, tempo, t.ISRC, rec, t.LinkURL); err != nil {
			return err
		}
	}
	return nil
}

// DeleteAlbum removes an album (its tracks and artwork; the songs stay).
func (st *Store) DeleteAlbum(ctx context.Context, personID, albumID int64) error {
	a, err := st.GetAlbum(ctx, albumID)
	if err != nil {
		return err
	}
	if ok, err := st.mayEditBandAlbums(ctx, st.db, personID, a.BandID); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("person %d may not delete album %d: %w", personID, albumID, ErrForbidden)
	}
	_, err = st.db.Exec(ctx, `DELETE FROM albums WHERE id = $1`, albumID)
	return err
}

// SetAlbumPublic makes an album public (everyone signed in sees its page,
// hears its masters) or the band's again, for personID (publish). With
// songs, its tracks' songs go public too — the album songs, never their
// live versions, nor a closed songbook's song. Making it private again
// leaves its songs as they are.
func (st *Store) SetAlbumPublic(ctx context.Context, personID, albumID int64, public, songs bool) (int, error) {
	a, err := st.GetAlbum(ctx, albumID)
	if err != nil {
		return 0, err
	}
	r, err := st.AlbumRightsFor(ctx, personID, a)
	if err != nil {
		return 0, err
	}
	if !r.Publish {
		return 0, fmt.Errorf("person %d may not publish album %d: %w", personID, albumID, ErrForbidden)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE albums SET public = $2 WHERE id = $1`, albumID, public); err != nil {
		return 0, err
	}
	n := 0
	if public && songs {
		tag, err := tx.Exec(ctx, `
			UPDATE songs s SET public = true FROM album_tracks t
			WHERE t.album_id = $1 AND s.id = t.song_id AND s.owner_band_id = $2 AND NOT s.live AND NOT s.public
			  AND NOT song_closed(s.id)`, albumID, a.BandID)
		if err != nil {
			return 0, err
		}
		n = int(tag.RowsAffected())
	}
	return n, tx.Commit(ctx)
}

// SetAlbumCover keeps an album's uploaded artwork (brand.PrepareArtwork),
// in place of the one it had.
func (st *Store) SetAlbumCover(ctx context.Context, albumID int64, img brand.LogoImage, byPersonID int64) error {
	var by *int64
	if byPersonID != 0 {
		by = &byPersonID
	}
	tag, err := st.db.Exec(ctx, `
		INSERT INTO album_covers (album_id, content_type, width, height, byte_size, sha256, data, created_by_person_id)
		SELECT id, $2, $3, $4, $5, $6, $7, $8 FROM albums WHERE id = $1
		ON CONFLICT (album_id) DO UPDATE SET content_type = EXCLUDED.content_type, width = EXCLUDED.width,
		  height = EXCLUDED.height, byte_size = EXCLUDED.byte_size, sha256 = EXCLUDED.sha256, data = EXCLUDED.data,
		  created_at = now(), created_by_person_id = EXCLUDED.created_by_person_id`,
		albumID, img.ContentType, img.Width, img.Height, len(img.Data), img.SHA256, img.Data, by)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such album %d: %w", albumID, ErrNotFound)
	}
	return nil
}

// DeleteAlbumCover removes an album's artwork, if any.
func (st *Store) DeleteAlbumCover(ctx context.Context, albumID int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM album_covers WHERE album_id = $1`, albumID)
	return err
}

// GetAlbumCover is an album's artwork, to serve (BrandLogo's shape);
// ErrNotFound when it has none.
func (st *Store) GetAlbumCover(ctx context.Context, albumID int64) (BrandLogo, error) {
	var l BrandLogo
	err := st.db.QueryRow(ctx, `SELECT data, content_type, sha256, created_at FROM album_covers WHERE album_id = $1`, albumID).
		Scan(&l.Data, &l.ContentType, &l.SHA256, &l.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return BrandLogo{}, ErrNotFound
	}
	return l, err
}

// SongAlbum is one release a song is on, as the song's page names it:
// "From Ultrablanc (2026), track 3".
type SongAlbum struct {
	AlbumID    int64
	BandID     int64
	Title      string
	Kind       string
	Year       int
	ReleasedOn *time.Time
	Disc       int
	Position   int
	Public     bool
	CoverSHA   string
}

// CoverPath is the album's artwork address, "" when it has none.
func (a SongAlbum) CoverPath() string {
	if a.CoverSHA == "" {
		return ""
	}
	return AlbumCoverPath(a.AlbumID, a.CoverSHA)
}

// AlbumsOfSong is the releases songID is on (a live version: its album
// song's), the albums before the singles, oldest first.
func (st *Store) AlbumsOfSong(ctx context.Context, songID int64) ([]SongAlbum, error) {
	rows, err := st.db.Query(ctx, `
		SELECT a.id, a.band_id, a.title, a.kind, coalesce(extract(year FROM a.released_on)::int, a.release_year, 0), a.released_on,
		       t.disc, t.position, a.public, coalesce(c.sha256, '')
		FROM songs s
		JOIN album_tracks t ON t.song_id = CASE WHEN s.live THEN s.forked_from_id ELSE s.id END
		JOIN albums a ON a.id = t.album_id
		LEFT JOIN album_covers c ON c.album_id = a.id
		WHERE s.id = $1
		ORDER BY a.kind = 'single', a.released_on NULLS LAST, a.release_year NULLS LAST, a.id`, songID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SongAlbum
	for rows.Next() {
		var a SongAlbum
		if err := rows.Scan(&a.AlbumID, &a.BandID, &a.Title, &a.Kind, &a.Year, &a.ReleasedOn, &a.Disc, &a.Position, &a.Public, &a.CoverSHA); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AlbumCoversOfSongs maps each of songIDs on an album with artwork (a
// live version: through its album song) to that artwork's address, its
// album's title beside it: a song's cover is its album's.
func (st *Store) AlbumCoversOfSongs(ctx context.Context, songIDs []int64) (map[int64][2]string, error) {
	out := map[int64][2]string{}
	if len(songIDs) == 0 {
		return out, nil
	}
	rows, err := st.db.Query(ctx, `
		SELECT DISTINCT ON (s.id) s.id, a.id, a.title, c.sha256
		FROM songs s
		JOIN album_tracks t ON t.song_id = CASE WHEN s.live THEN s.forked_from_id ELSE s.id END
		JOIN albums a ON a.id = t.album_id
		JOIN album_covers c ON c.album_id = a.id
		WHERE s.id = ANY($1)
		ORDER BY s.id, a.kind = 'single', a.released_on NULLS LAST, a.id`, songIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var song, album int64
		var title, sha string
		if err := rows.Scan(&song, &album, &title, &sha); err != nil {
			return nil, err
		}
		out[song] = [2]string{AlbumCoverPath(album, sha), title}
	}
	return out, rows.Err()
}

// BandRecordingChoice is a file of a band's folder an album track may
// take as its master.
type BandRecordingChoice struct {
	ID       int64  `json:"id"`
	Path     string `json:"path"`
	Name     string `json:"name"`
	Position *int   `json:"position,omitempty"`
}

// AlbumMasters is bandID's files of kind album (the Albums folder), by
// folder and name: the masters an album's tracks are picked from.
func (st *Store) AlbumMasters(ctx context.Context, bandID int64) ([]BandRecordingChoice, error) {
	rows, err := st.db.Query(ctx, `
		SELECT id, path, name, position FROM band_recordings
		WHERE band_id = $1 AND kind = 'album' AND NOT gone ORDER BY path, name`, bandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BandRecordingChoice
	for rows.Next() {
		var c BandRecordingChoice
		if err := rows.Scan(&c.ID, &c.Path, &c.Name, &c.Position); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Master file names say a track's tempo and ISRC: "03 Other lies-UltraBlanc-
// Bones_90_Mix.v5 FR-9W1-25-34056.wav".
var (
	masterTempo = regexp.MustCompile(`[_ ](\d{2,3})[_ ](?:bpm|mix|master|v\d)?`)
	masterISRC  = regexp.MustCompile(`\b([A-Z]{2}-?[A-Z0-9]{3}-?\d{2}-?\d{5})\b`)
	masterNum   = regexp.MustCompile(`(?:^|\s-\s)(\d{1,2})(?:\s|\s?-\s|_)`)
)

// MasterFacts reads what a master's file name says: its track number, its
// tempo, its ISRC (0, 0, "" when it doesn't).
func MasterFacts(name string) (position, tempo int, isrc string) {
	stem := strings.TrimSuffix(name, pathExt(name))
	if m := masterNum.FindStringSubmatch(stem); m != nil {
		position, _ = strconv.Atoi(m[1])
	}
	lower := strings.ToLower(stem)
	if m := masterTempo.FindStringSubmatch(lower); m != nil {
		if n, _ := strconv.Atoi(m[1]); n >= 40 && n <= 260 {
			tempo = n
		}
	}
	if m := masterISRC.FindStringSubmatch(stem); m != nil {
		isrc = m[1]
	}
	return position, tempo, isrc
}

func pathExt(name string) string {
	if i := strings.LastIndexByte(name, '.'); i > 0 && len(name)-i <= 5 {
		return name[i:]
	}
	return ""
}

// AlbumSongs maps each of bandID's songs on one of its albums to the
// albums it's on (BandAlbums' order) and its track: the songbook by
// album. Songs are the album songs (never live versions).
func AlbumSongs(albums []*Album) map[int64][]*Album {
	out := map[int64][]*Album{}
	for _, a := range albums {
		for _, t := range a.Tracks {
			if t.SongID != nil {
				out[*t.SongID] = append(out[*t.SongID], a)
			}
		}
	}
	return out
}

// Live versions (docs/albums-and-live-versions.md §4): the way the band
// plays a song live is a version of the album song (forked_from_id),
// marked live, named "Live": its own chart, tempo, reference, click and
// sync. A band's set lists may use them (bands.setlist_versions).

// LiveVersionsOf maps each of songIDs that has a live version of its
// owner's to it.
func (st *Store) LiveVersionsOf(ctx context.Context, songIDs []int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	if len(songIDs) == 0 {
		return out, nil
	}
	rows, err := st.db.Query(ctx, `
		SELECT s.id, l.id FROM songs s JOIN songs l ON l.forked_from_id = s.id AND l.live
		  AND l.owner_kind = s.owner_kind
		  AND coalesce(l.owner_person_id, l.owner_band_id, l.owner_community_id) = coalesce(s.owner_person_id, s.owner_band_id, s.owner_community_id)
		WHERE s.id = ANY($1)`, songIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s, l int64
		if err := rows.Scan(&s, &l); err != nil {
			return nil, err
		}
		out[s] = l
	}
	return out, rows.Err()
}

// LiveOf is what songIDs are: each live version mapped to its album song.
func (st *Store) LiveOf(ctx context.Context, songIDs []int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	if len(songIDs) == 0 {
		return out, nil
	}
	rows, err := st.db.Query(ctx, `SELECT id, forked_from_id FROM songs WHERE id = ANY($1) AND live AND forked_from_id IS NOT NULL`, songIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l, s int64
		if err := rows.Scan(&l, &s); err != nil {
			return nil, err
		}
		out[l] = s
	}
	return out, rows.Err()
}

// LiveVersionOptions are MakeLiveVersion's choices.
type LiveVersionOptions struct {
	// Name is the version's name ("" is "Live").
	Name string
	// MoveSetlists points the owner's set list lines playing the song at
	// the live version, their notes and click-track notes with them (a
	// revision for each set list).
	MoveSetlists bool
}

// MakeLiveVersion makes the live version of songID, a band's own song,
// for personID (edit_songbook): a version of it marked live, named
// "Live", its chart, reference and media copied as they are (the click,
// the sync), beside it in the band's songbooks (the same key); the
// band's concert and set-list recordings of the song, and its chosen one
// when it's one of those, go to it. ErrForbidden; ErrInvalid for a song
// that's no band's, a live version itself; ErrConflict when it has one.
func (st *Store) MakeLiveVersion(ctx context.Context, personID, songID int64, opt LiveVersionOptions) (int64, error) {
	name := strings.TrimSpace(opt.Name)
	if name == "" {
		name = "Live"
	}
	if len(name) > 80 {
		return 0, fmt.Errorf("a version name is 80 characters at most: %w", ErrInvalid)
	}
	var bandID *int64
	var live bool
	err := st.db.QueryRow(ctx, `SELECT owner_band_id, live FROM songs WHERE id = $1`, songID).Scan(&bandID, &live)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("no such song %d: %w", songID, ErrNotFound)
	}
	if err != nil {
		return 0, err
	}
	if bandID == nil {
		return 0, fmt.Errorf("song %d isn't a band's own song: %w", songID, ErrInvalid)
	}
	if live {
		return 0, fmt.Errorf("song %d is a live version already: %w", songID, ErrInvalid)
	}
	band := *bandID
	if ok, err := st.mayEditBandAlbums(ctx, st.db, personID, band); err != nil {
		return 0, err
	} else if !ok {
		return 0, fmt.Errorf("person %d may not make band %d's versions: %w", personID, band, ErrForbidden)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	liveID, err := st.makeLiveVersionTx(ctx, tx, band, songID, personID, name, opt.MoveSetlists)
	if err != nil {
		return 0, err
	}
	return liveID, tx.Commit(ctx)
}

// makeLiveVersionTx is MakeLiveVersion's work in tx, its checks made.
func (st *Store) makeLiveVersionTx(ctx context.Context, tx pgx.Tx, band, songID, personID int64, name string, moveSetlists bool) (int64, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM songs WHERE forked_from_id = $1 AND live AND owner_band_id = $2)`, songID, band).Scan(&exists); err != nil {
		return 0, err
	}
	if exists {
		return 0, fmt.Errorf("song %d has a live version already: %w", songID, ErrConflict)
	}
	var by *int64
	if personID > 0 {
		by = &personID
	}
	var liveID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO songs (title, artist, reference_url, body, work_id, version_name, owner_band_id, forked_from_id, created_by_person_id,
		                   base_body, base_version, base_synced_at, public, live)
		SELECT title, artist, reference_url, body, work_id, $3, $2, id, $4, body, version, now(), false, true
		FROM songs WHERE id = $1
		RETURNING id`, songID, band, name, by).Scan(&liveID); err != nil {
		return 0, err
	}
	if err := copySongMedia(ctx, tx, songID, liveID); err != nil {
		return 0, err
	}
	// Beside it in the band's songbooks, the same key and lesson.
	rows, err := tx.Query(ctx, `
		SELECT se.songbook_id FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
		WHERE se.variant_id = $1 AND sb.owner_band_id = $2 ORDER BY sb.id`, songID, band)
	if err != nil {
		return 0, err
	}
	books, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return 0, err
	}
	for _, sb := range books {
		if _, err := tx.Exec(ctx, `
			INSERT INTO songbook_entries (songbook_id, variant_id, added_by_person_id, lesson_id, lesson_position, semitones)
			SELECT songbook_id, $3, $4, lesson_id, lesson_position, semitones
			FROM songbook_entries WHERE songbook_id = $1 AND variant_id = $2
			ON CONFLICT (songbook_id, variant_id) DO NOTHING`, sb, songID, liveID, by); err != nil {
			return 0, err
		}
		if err := insertInSongOrder(ctx, tx, sb, songID, liveID); err != nil {
			return 0, err
		}
	}
	// The band's recordings of it from its concerts and its set lists are
	// the live version's (recordings matching: liveKinds); its chosen
	// recording too, when it's one of those.
	if _, err := tx.Exec(ctx, `
		UPDATE band_recordings SET variant_id = $3 WHERE band_id = $1 AND variant_id = $2 AND kind = ANY($4)`,
		band, songID, liveID, liveRecordingKinds); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE band_song_recordings c SET variant_id = $3 FROM band_recordings r
		WHERE c.band_id = $1 AND c.variant_id = $2 AND r.id = c.recording_id AND r.kind = ANY($4)`,
		band, songID, liveID, liveRecordingKinds); err != nil {
		return 0, err
	}
	if moveSetlists {
		if err := st.moveSetlistsToLive(ctx, tx, band, songID, liveID, personID); err != nil {
			return 0, err
		}
	}
	return liveID, nil
}

// liveRecordingKinds are the kinds of a band's recordings that are of a
// song's live version when it has one: its concerts and its set list's
// folder. Rehearsals and the album's masters stay with the album song.
var liveRecordingKinds = []string{"concert", "setlist"}

// moveSetlistsToLive points band's set list lines playing song at its
// live version (a revision each), their click-track notes with them.
func (st *Store) moveSetlistsToLive(ctx context.Context, tx pgx.Tx, band, song, live, personID int64) error {
	var actor *int64
	if personID > 0 {
		actor = &personID
	}
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT i.setlist_id FROM setlist_items i JOIN setlists sl ON sl.id = i.setlist_id
		WHERE i.variant_id = $1 AND sl.owner_band_id = $2 ORDER BY 1`, song, band)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := moveSetlistLines(ctx, st, tx, id, map[int64]int64{song: live}, actor); err != nil {
			return err
		}
	}
	return nil
}

// moveSetlistLines points setlistID's lines from each key of to at its
// value, the click-track notes with them, in one revision of the set list.
func moveSetlistLines(ctx context.Context, st *Store, tx pgx.Tx, setlistID int64, to map[int64]int64, actor *int64) error {
	snap, _, ok, err := setlistSnapshot(ctx, tx, setlistID)
	if err != nil || !ok {
		return err
	}
	if err := st.RecordRevision(ctx, tx, "setlist", strconv.FormatInt(setlistID, 10), actor, snap); err != nil {
		return err
	}
	for from, dst := range to {
		if _, err := tx.Exec(ctx, `UPDATE setlist_items SET variant_id = $3 WHERE setlist_id = $1 AND variant_id = $2`, setlistID, from, dst); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE setlist_clicktrack_notes SET variant_id = $3 WHERE setlist_id = $1 AND variant_id = $2
			  AND NOT EXISTS (SELECT 1 FROM setlist_clicktrack_notes x WHERE x.setlist_id = $1 AND x.variant_id = $3)`,
			setlistID, from, dst); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE setlists SET version = version + 1 WHERE id = $1`, setlistID)
	return err
}

// UseLiveVersions points every line of setlistID (a band's) whose song
// has a live version at it, in one change of the set list's history, for
// personID (who may edit the set list: the caller checks). It answers
// how many lines moved.
func (st *Store) UseLiveVersions(ctx context.Context, personID, setlistID int64) (int, error) {
	meta, err := st.GetSetlistMeta(ctx, setlistID)
	if err != nil {
		return 0, err
	}
	if meta.OwnerBandID == nil {
		return 0, fmt.Errorf("set list %d isn't a band's: %w", setlistID, ErrInvalid)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT i.variant_id, l.id FROM setlist_items i
		JOIN songs l ON l.forked_from_id = i.variant_id AND l.live AND l.owner_band_id = $2
		WHERE i.setlist_id = $1`, setlistID, *meta.OwnerBandID)
	if err != nil {
		return 0, err
	}
	to := map[int64]int64{}
	for rows.Next() {
		var s, l int64
		if err := rows.Scan(&s, &l); err != nil {
			rows.Close()
			return 0, err
		}
		to[s] = l
	}
	rows.Close()
	if len(to) == 0 {
		return 0, nil
	}
	var n int
	from := make([]int64, 0, len(to))
	for s := range to {
		from = append(from, s)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM setlist_items WHERE setlist_id = $1 AND variant_id = ANY($2)`, setlistID, from).Scan(&n); err != nil {
		return 0, err
	}
	actor := &personID
	if err := moveSetlistLines(ctx, st, tx, setlistID, to, actor); err != nil {
		return 0, err
	}
	return n, tx.Commit(ctx)
}

// BandSetlistVersions is what bandID's set lists use: "song" (the song
// itself) or "live" (its live version when there is one).
func (st *Store) BandSetlistVersions(ctx context.Context, bandID int64) (string, error) {
	var v string
	err := st.db.QueryRow(ctx, `SELECT setlist_versions FROM bands WHERE id = $1`, bandID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("no such band %d: %w", bandID, ErrNotFound)
	}
	return v, err
}

// SetBandSetlistVersions sets it ("song" or "live").
func (st *Store) SetBandSetlistVersions(ctx context.Context, bandID int64, v string) error {
	if v != "song" && v != "live" {
		return fmt.Errorf("set lists use %q: %w", v, ErrInvalid)
	}
	tag, err := st.db.Exec(ctx, `UPDATE bands SET setlist_versions = $2 WHERE id = $1`, bandID, v)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such band %d: %w", bandID, ErrNotFound)
	}
	return nil
}

// LiveForNewLines is, for a band whose set lists use live versions, the
// live version each of songIDs (lines a save adds) plays instead; empty
// for a band that uses the songs themselves.
func (st *Store) LiveForNewLines(ctx context.Context, bandID int64, songIDs []int64) (map[int64]int64, error) {
	v, err := st.BandSetlistVersions(ctx, bandID)
	if err != nil || v != "live" {
		return map[int64]int64{}, err
	}
	all, err := st.LiveVersionsOf(ctx, songIDs)
	if err != nil {
		return nil, err
	}
	out := map[int64]int64{}
	if len(all) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(all))
	for _, l := range all {
		ids = append(ids, l)
	}
	rows, err := st.db.Query(ctx, `SELECT id FROM songs WHERE id = ANY($1) AND owner_band_id = $2`, ids, bandID)
	if err != nil {
		return nil, err
	}
	ok, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	mine := map[int64]bool{}
	for _, id := range ok {
		mine[id] = true
	}
	for s, l := range all {
		if mine[l] {
			out[s] = l
		}
	}
	return out, nil
}

// HasLiveVersions reports whether bandID has any live version.
func (st *Store) HasLiveVersions(ctx context.Context, bandID int64) (bool, error) {
	var ok bool
	err := st.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM songs WHERE owner_band_id = $1 AND live)`, bandID).Scan(&ok)
	return ok, err
}

// BandHasAlbums reports whether bandID has any album.
func (st *Store) BandHasAlbums(ctx context.Context, bandID int64) (bool, error) {
	var ok bool
	err := st.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM albums WHERE band_id = $1)`, bandID).Scan(&ok)
	return ok, err
}

// bandLiveVersions maps each of bandID's songs with a live version to it.
func (st *Store) bandLiveVersions(ctx context.Context, bandID int64) (map[int64]int64, error) {
	rows, err := st.db.Query(ctx, `SELECT forked_from_id, id FROM songs WHERE owner_band_id = $1 AND live AND forked_from_id IS NOT NULL`, bandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var s, l int64
		if err := rows.Scan(&s, &l); err != nil {
			return nil, err
		}
		out[s] = l
	}
	return out, rows.Err()
}

// SongOwnerBand is the band that owns songID, 0 when no band does.
func (st *Store) SongOwnerBand(ctx context.Context, songID int64) (int64, error) {
	var band *int64
	err := st.db.QueryRow(ctx, `SELECT owner_band_id FROM songs WHERE id = $1`, songID).Scan(&band)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("no such song %d: %w", songID, ErrNotFound)
	}
	if err != nil || band == nil {
		return 0, err
	}
	return *band, nil
}
