package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// De-duplication (docs/ownership-model.md §7): among public songs, the
// same title and artist once normalised (songIdentity: case, accents,
// "The", "&"/"and"), or the same reference video, are duplicates. The
// Big Songbook's staff (the site's admins) review them and merge one into
// the other: everything that pointed at the one merged — songbook
// entries (their keys kept), set list lines, band recordings, notes,
// practice, versions' forked_from_id — points at the one kept
// (repointSong); the merged song goes, its revisions kept under its id
// and listed on the kept one's history (song_merges, migration 0116).

// DuplicateSong is one song of a group of duplicates, with what a merge
// would move.
type DuplicateSong struct {
	ID           int64
	WorkID       int64
	Title        string
	Artist       string
	VersionName  string
	OwnerName    string
	Catalogue    bool
	ReferenceURL string
	VideoID      string
	Version      int
	CreatedAt    time.Time
	// What points at it: songbook entries, set lists listing it, band
	// recordings, versions made from it, its recordings and Guitar Pro
	// file.
	Entries    int
	Setlists   int
	Recordings int
	Versions   int
	Media      int
}

// DuplicateGroup is songs found to be the same: Reasons "title" (the same
// title and artist), "video" (the same reference video), or both.
type DuplicateGroup struct {
	Reasons []string
	Title   string // the first song's "Title — Artist"
	VideoID string // for "video"
	Songs   []DuplicateSong
	// KeepID: the one to keep, proposed — the one most pointed at
	// (songbook entries, set lists, recordings, versions), the oldest of
	// equals.
	KeepID int64
}

func (g *DuplicateGroup) proposeKeep() {
	best := -1
	for _, s := range g.Songs {
		if n := s.Entries + s.Setlists + s.Recordings + s.Versions; n > best {
			best, g.KeepID = n, s.ID
		}
	}
}

// FindDuplicates lists the duplicates among public songs, oldest song
// first in each group, the groups by title.
func (st *Store) FindDuplicates(ctx context.Context) ([]DuplicateGroup, error) {
	rows, err := st.db.Query(ctx, `
		SELECT s.id, s.work_id, s.title, s.artist, s.version_name,
		       coalesce(b.name, CASE WHEN c.kind = 'global' THEN '' ELSE c.name END, p.display_name, ''),
		       coalesce(c.kind = 'global', false), s.reference_url, s.version, s.created_at,
		       (SELECT count(*) FROM songbook_entries se WHERE se.variant_id = s.id),
		       (SELECT count(DISTINCT si.setlist_id) FROM setlist_items si WHERE si.variant_id = s.id),
		       (SELECT count(*) FROM band_recordings br WHERE br.variant_id = s.id),
		       (SELECT count(*) FROM songs v WHERE v.forked_from_id = s.id),
		       (SELECT count(*) FROM song_audio a WHERE a.variant_id = s.id)
		         + (SELECT count(*) FROM song_gp_source_file g WHERE g.variant_id = s.id)
		FROM songs s
		LEFT JOIN bands b ON b.id = s.owner_band_id
		LEFT JOIN communities c ON c.id = s.owner_community_id
		LEFT JOIN people p ON p.id = s.owner_person_id
		WHERE s.public
		ORDER BY s.id`)
	if err != nil {
		return nil, err
	}
	var songs []DuplicateSong
	for rows.Next() {
		var d DuplicateSong
		if err := rows.Scan(&d.ID, &d.WorkID, &d.Title, &d.Artist, &d.VersionName, &d.OwnerName, &d.Catalogue,
			&d.ReferenceURL, &d.Version, &d.CreatedAt, &d.Entries, &d.Setlists, &d.Recordings, &d.Versions, &d.Media); err != nil {
			rows.Close()
			return nil, err
		}
		d.VideoID = YouTubeID(d.ReferenceURL)
		songs = append(songs, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return groupDuplicates(songs), nil
}

// groupDuplicates groups songs (oldest first) by identity, then by video;
// a video group of the same songs as a title group is that group's second
// reason.
func groupDuplicates(songs []DuplicateSong) []DuplicateGroup {
	byIdentity := map[string][]DuplicateSong{}
	byVideo := map[string][]DuplicateSong{}
	var identities, videos []string
	for _, s := range songs {
		k := songIdentity(s.Title, s.Artist)
		if s.VersionName != "" {
			k += "|" + normIdentityPart(s.VersionName)
		}
		if byIdentity[k] == nil {
			identities = append(identities, k)
		}
		byIdentity[k] = append(byIdentity[k], s)
		if s.VideoID != "" {
			if byVideo[s.VideoID] == nil {
				videos = append(videos, s.VideoID)
			}
			byVideo[s.VideoID] = append(byVideo[s.VideoID], s)
		}
	}
	ids := func(ss []DuplicateSong) string {
		var b strings.Builder
		for _, s := range ss {
			b.WriteString(strconv.FormatInt(s.ID, 10) + ",")
		}
		return b.String()
	}
	var out []DuplicateGroup
	bySet := map[string]int{}
	for _, k := range identities {
		for _, ss := range sameRecording(byIdentity[k]) {
			if len(ss) < 2 {
				continue
			}
			if _, seen := bySet[ids(ss)]; seen {
				continue
			}
			bySet[ids(ss)] = len(out)
			out = append(out, DuplicateGroup{Reasons: []string{"title"}, Title: ss[0].Title + " — " + ss[0].Artist, Songs: ss})
		}
	}
	for _, v := range videos {
		ss := byVideo[v]
		if len(ss) < 2 {
			continue
		}
		if i, ok := bySet[ids(ss)]; ok {
			out[i].Reasons = append(out[i].Reasons, "video")
			out[i].VideoID = v
			continue
		}
		out = append(out, DuplicateGroup{Reasons: []string{"video"}, VideoID: v, Title: ss[0].Title + " — " + ss[0].Artist, Songs: ss})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	for i := range out {
		out[i].proposeKeep()
	}
	return out
}

// sameRecording splits songs of the same title and artist by their
// reference video: two songs following two different recordings (the
// album version and a live one, the catalogue's "another recording"
// version: docs/ownership-model.md, "The catalogue: open edits") aren't
// duplicates. Each video's songs go with the songs that have none; songs
// none of which has a video stay one group.
func sameRecording(ss []DuplicateSong) [][]DuplicateSong {
	var none []DuplicateSong
	byVideo := map[string][]DuplicateSong{}
	var videos []string
	for _, s := range ss {
		if s.VideoID == "" {
			none = append(none, s)
			continue
		}
		if byVideo[s.VideoID] == nil {
			videos = append(videos, s.VideoID)
		}
		byVideo[s.VideoID] = append(byVideo[s.VideoID], s)
	}
	if len(videos) <= 1 {
		return [][]DuplicateSong{ss}
	}
	var out [][]DuplicateSong
	for _, v := range videos {
		g := append(append([]DuplicateSong{}, none...), byVideo[v]...)
		sort.Slice(g, func(i, j int) bool { return g[i].ID < g[j].ID })
		out = append(out, g)
	}
	return out
}

// DuplicateReasons says why keep and drop are duplicates ("title",
// "video"), nil when they aren't. The same title and artist following
// two different recordings isn't a duplicate.
func duplicateReasons(a, b DuplicateSong) []string {
	var r []string
	if songIdentity(a.Title, a.Artist) == songIdentity(b.Title, b.Artist) &&
		normIdentityPart(a.VersionName) == normIdentityPart(b.VersionName) &&
		(a.VideoID == "" || b.VideoID == "" || a.VideoID == b.VideoID) {
		r = append(r, "title")
	}
	if a.VideoID != "" && a.VideoID == b.VideoID {
		r = append(r, "video")
	}
	return r
}

// MergeReport is what merging Drop into Keep moves (or would, DryRun).
type MergeReport struct {
	Keep, Drop DuplicateSong
	Reasons    []string
	Moved      RepointCounts
	// MediaDropped: the merged song's recordings and Guitar Pro file of a
	// kind the kept song has its own of: they go with it.
	MediaDropped int
	DryRun       bool
}

// ErrNotDuplicates: the two songs aren't duplicates (§7) — not both
// public, or neither the same title and artist nor the same video.
var ErrNotDuplicates = errors.New("these songs aren't duplicates")

// MergeSongs merges the public song dropID into keepID (§7), for a site
// admin (ErrForbidden otherwise): repointSong, the merged song's last
// state recorded as a revision under its id, a song_merges row, the
// merged song deleted. With dryRun it does all of it and rolls back: the
// report is exactly what a merge would do.
func (st *Store) MergeSongs(ctx context.Context, actorID, keepID, dropID int64, dryRun bool) (*MergeReport, error) {
	var admin bool
	if err := st.db.QueryRow(ctx, `SELECT coalesce((SELECT is_admin FROM people WHERE id = $1), false)`, actorID).Scan(&admin); err != nil {
		return nil, err
	}
	if !admin {
		return nil, fmt.Errorf("only The Big Songbook's staff merge songs: %w", ErrForbidden)
	}
	if keepID == dropID {
		return nil, ErrNotDuplicates
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	r := &MergeReport{DryRun: dryRun}
	for _, x := range []struct {
		id  int64
		out *DuplicateSong
	}{{keepID, &r.Keep}, {dropID, &r.Drop}} {
		var public bool
		err := tx.QueryRow(ctx, `
			SELECT s.id, s.work_id, s.title, s.artist, s.version_name,
			       coalesce(b.name, CASE WHEN c.kind = 'global' THEN '' ELSE c.name END, p.display_name, ''),
			       coalesce(c.kind = 'global', false), s.reference_url, s.version, s.created_at, s.public
			FROM songs s
			LEFT JOIN bands b ON b.id = s.owner_band_id
			LEFT JOIN communities c ON c.id = s.owner_community_id
			LEFT JOIN people p ON p.id = s.owner_person_id
			WHERE s.id = $1 FOR UPDATE OF s`, x.id).
			Scan(&x.out.ID, &x.out.WorkID, &x.out.Title, &x.out.Artist, &x.out.VersionName, &x.out.OwnerName,
				&x.out.Catalogue, &x.out.ReferenceURL, &x.out.Version, &x.out.CreatedAt, &public)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("no such song %d: %w", x.id, ErrNotFound)
		}
		if err != nil {
			return nil, err
		}
		if !public {
			return nil, ErrNotDuplicates
		}
		x.out.VideoID = YouTubeID(x.out.ReferenceURL)
	}
	r.Reasons = duplicateReasons(r.Keep, r.Drop)
	if len(r.Reasons) == 0 {
		return nil, ErrNotDuplicates
	}

	// The merged song's history: its revisions stay under its id; its
	// last state is one more.
	var body string
	if err := tx.QueryRow(ctx, `SELECT body FROM songs WHERE id = $1`, dropID).Scan(&body); err != nil {
		return nil, err
	}
	actor := &actorID
	if err := st.RecordRevision(ctx, tx, "variant", strconv.FormatInt(dropID, 10), actor, VariantSnapshot{
		Title: r.Drop.Title, Artist: r.Drop.Artist, ReferenceURL: r.Drop.ReferenceURL, Body: body,
	}); err != nil {
		return nil, err
	}
	// Its recording fit is its own chart's, not the kept one's.
	if _, err := tx.Exec(ctx, `DELETE FROM song_recording_fit WHERE song_id = $1`, dropID); err != nil {
		return nil, err
	}
	moved, err := st.repointSong(ctx, tx, dropID, keepID, actor)
	if err != nil {
		return nil, err
	}
	r.Moved = moved
	if err := tx.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM song_audio WHERE variant_id = $1)
		     + (SELECT count(*) FROM song_gp_source_file WHERE variant_id = $1)`, dropID).Scan(&r.MediaDropped); err != nil {
		return nil, err
	}
	movedJSON, err := json.Marshal(moved)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO song_merges (merged_song_id, into_song_id, title, artist, version_name, owner_name, reason, moved, merged_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		dropID, keepID, r.Drop.Title, r.Drop.Artist, r.Drop.VersionName, r.Drop.OwnerName,
		strings.Join(r.Reasons, ","), movedJSON, actorID); err != nil {
		return nil, err
	}
	// A song merged into the one merged now: into the kept one.
	if _, err := tx.Exec(ctx, `UPDATE song_merges SET into_song_id = $2 WHERE into_song_id = $1`, dropID, keepID); err != nil {
		return nil, err
	}
	if err := deleteSong(ctx, tx, dropID); err != nil {
		return nil, err
	}
	if dryRun {
		return r, nil // rolled back
	}
	return r, tx.Commit(ctx)
}

// SongMerge is a song merged into another (song_merges).
type SongMerge struct {
	MergedSongID int64
	IntoSongID   int64
	Title        string
	Artist       string
	VersionName  string
	OwnerName    string
	Reason       string
	MergedBy     string
	MergedAt     time.Time
}

// MergedInto lists the songs merged into songID, the latest first.
func (st *Store) MergedInto(ctx context.Context, songID int64) ([]SongMerge, error) {
	rows, err := st.db.Query(ctx, `
		SELECT m.merged_song_id, m.into_song_id, m.title, m.artist, m.version_name, m.owner_name, m.reason,
		       coalesce(p.display_name, ''), m.merged_at
		FROM song_merges m LEFT JOIN people p ON p.id = m.merged_by
		WHERE m.into_song_id = $1
		ORDER BY m.merged_at DESC, m.merged_song_id`, songID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SongMerge
	for rows.Next() {
		var m SongMerge
		if err := rows.Scan(&m.MergedSongID, &m.IntoSongID, &m.Title, &m.Artist, &m.VersionName, &m.OwnerName, &m.Reason,
			&m.MergedBy, &m.MergedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MergedSongTarget is the song a merged song went into (0 when songID
// wasn't merged).
func (st *Store) MergedSongTarget(ctx context.Context, songID int64) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx, `SELECT into_song_id FROM song_merges WHERE merged_song_id = $1`, songID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}
