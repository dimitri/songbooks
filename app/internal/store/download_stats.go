package store

import (
	"context"
	"time"
)

// RecordSongbookDownload notes that personID downloaded (or opened,
// played, or took as a zip) a songbook's file, for the community's stats.
// The same thing again within half a minute is the same download (a
// player asking twice).
func (st *Store) RecordSongbookDownload(ctx context.Context, communityID, songbookID int64, fileID *int64, personID int64, what, name string, size int64) error {
	_, err := st.db.Exec(ctx, `
		INSERT INTO songbook_downloads (community_id, songbook_id, file_id, person_id, what, name, byte_size)
		SELECT $1, $2, $3, $4, $5, $6, $7
		WHERE NOT EXISTS (
			SELECT 1 FROM songbook_downloads
			WHERE person_id = $4 AND songbook_id = $2 AND file_id IS NOT DISTINCT FROM $3 AND what = $5 AND name = $6
			  AND at > now() - interval '30 seconds')`,
		communityID, songbookID, fileID, personID, what, name, size)
	return err
}

// DownloadCounts is how often something was downloaded, opened, played or
// taken as a zip, by how many people, last when.
type DownloadCounts struct {
	Downloads, Opens, Plays, Zips int
	People                        int
	Last                          *time.Time
}

// Total is every download, of any kind.
func (c DownloadCounts) Total() int { return c.Downloads + c.Opens + c.Plays + c.Zips }

// FileStats is a file's downloads.
type FileStats struct {
	FileID     int64
	SongbookID int64
	Path       string
	Filename   string
	SongTitle  string
	VariantID  *int64
	DownloadCounts
}

// ZipStats is the downloads of a songbook, or of one of its folders, as a zip.
type ZipStats struct {
	SongbookID int64
	Name       string
	DownloadCounts
}

// SongStats is a song of a community's songbooks: its page's views and
// its files' downloads.
type SongStats struct {
	SongbookID int64
	VariantID  int64
	Title      string
	Views      int
	Viewers    int
	LastViewed *time.Time
	Downloads  int // of its files, every kind
}

// MemberStats is what one member viewed and downloaded.
type MemberStats struct {
	PersonID     int64
	Username     string
	DisplayName  string
	Role         string
	Songs        int // songs of the community's songbooks viewed
	Views        int
	LessonsDone  int // course lessons they said they've done
	Files        int // distinct files downloaded, opened or played
	Downloads    int // every download, opening, playing, zip
	Zips         int
	LastActivity *time.Time
}

// CommunityStats is a community's stats page.
type CommunityStats struct {
	Members []MemberStats
	Files   []FileStats
	Zips    []ZipStats
	Songs   []SongStats
	Lessons []CourseLessonStats // the lessons of its courses
	Totals  DownloadCounts
	Views   int
}

// CourseLessonStats is a lesson of a course: its songs' views, who said
// they've done it, its files' downloads.
type CourseLessonStats struct {
	SongbookID int64
	LessonID   int64
	Number     *int
	Title      string
	Songs      int
	Views      int
	Viewers    int
	Done       int
	Downloads  int
}

// Label is the lesson's name as its course lists it, in lang.
func (l CourseLessonStats) Label(lang string) string {
	return Lesson{Number: l.Number, Title: l.Title}.Label(lang)
}

// communitySongsCTE is the songs of a community's songbooks.
const communitySongsCTE = `
	community_songs AS (
		SELECT DISTINCT se.songbook_id, se.variant_id
		FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
		WHERE sb.owner_kind = 'community' AND sb.owner_community_id = $1
	)`

const countsCols = `
	count(*) FILTER (WHERE d.what = 'download'), count(*) FILTER (WHERE d.what = 'open'),
	count(*) FILTER (WHERE d.what = 'play'), count(*) FILTER (WHERE d.what = 'zip'),
	count(DISTINCT d.person_id), max(d.at)`

// GetCommunityStats gathers who viewed and downloaded what in communityID.
func (st *Store) GetCommunityStats(ctx context.Context, communityID int64) (*CommunityStats, error) {
	var out CommunityStats
	c := &out.Totals
	if err := st.db.QueryRow(ctx, `SELECT `+countsCols+` FROM songbook_downloads d WHERE d.community_id = $1`, communityID).
		Scan(&c.Downloads, &c.Opens, &c.Plays, &c.Zips, &c.People, &c.Last); err != nil {
		return nil, err
	}
	if err := st.db.QueryRow(ctx, `WITH `+communitySongsCTE+`
		SELECT coalesce(sum(v.views), 0) FROM song_visits v WHERE v.song_id IN (SELECT variant_id FROM community_songs)`, communityID).
		Scan(&out.Views); err != nil {
		return nil, err
	}

	rows, err := st.db.Query(ctx, `
		SELECT f.id, f.songbook_id, f.path, f.filename, coalesce(s.title, ''), f.variant_id, `+countsCols+`
		FROM songbook_files f
		JOIN songbooks sb ON sb.id = f.songbook_id
		LEFT JOIN songs s ON s.id = f.variant_id
		LEFT JOIN songbook_downloads d ON d.file_id = f.id
		WHERE sb.owner_kind = 'community' AND sb.owner_community_id = $1
		GROUP BY f.id, s.title
		ORDER BY count(d.id) DESC, f.songbook_id, f.path COLLATE "C", f.filename COLLATE "C"`, communityID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var f FileStats
		if err := rows.Scan(&f.FileID, &f.SongbookID, &f.Path, &f.Filename, &f.SongTitle, &f.VariantID,
			&f.Downloads, &f.Opens, &f.Plays, &f.Zips, &f.People, &f.Last); err != nil {
			rows.Close()
			return nil, err
		}
		out.Files = append(out.Files, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = st.db.Query(ctx, `
		SELECT d.songbook_id, d.name, `+countsCols+`
		FROM songbook_downloads d
		WHERE d.community_id = $1 AND d.what = 'zip' AND d.songbook_id IS NOT NULL
		GROUP BY d.songbook_id, d.name
		ORDER BY count(*) DESC, d.name`, communityID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var z ZipStats
		if err := rows.Scan(&z.SongbookID, &z.Name, &z.Downloads, &z.Opens, &z.Plays, &z.Zips, &z.People, &z.Last); err != nil {
			rows.Close()
			return nil, err
		}
		out.Zips = append(out.Zips, z)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = st.db.Query(ctx, `WITH `+communitySongsCTE+`
		SELECT cs.songbook_id, cs.variant_id, s.title,
		       coalesce((SELECT sum(v.views) FROM song_visits v WHERE v.song_id = cs.variant_id), 0),
		       (SELECT count(*) FROM song_visits v WHERE v.song_id = cs.variant_id),
		       (SELECT max(v.visited_at) FROM song_visits v WHERE v.song_id = cs.variant_id),
		       (SELECT count(*) FROM songbook_downloads d JOIN songbook_files f ON f.id = d.file_id
		         WHERE f.variant_id = cs.variant_id AND f.songbook_id = cs.songbook_id)
		FROM community_songs cs JOIN songs s ON s.id = cs.variant_id
		ORDER BY cs.songbook_id, s.title`, communityID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var l SongStats
		if err := rows.Scan(&l.SongbookID, &l.VariantID, &l.Title, &l.Views, &l.Viewers, &l.LastViewed, &l.Downloads); err != nil {
			rows.Close()
			return nil, err
		}
		out.Songs = append(out.Songs, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = st.db.Query(ctx, `
		SELECT l.songbook_id, l.id, l.number, l.title,
		       (SELECT count(*) FROM songbook_entries se WHERE se.lesson_id = l.id),
		       coalesce((SELECT sum(v.views) FROM songbook_entries se JOIN song_visits v ON v.song_id = se.variant_id WHERE se.lesson_id = l.id), 0),
		       (SELECT count(DISTINCT v.person_id) FROM songbook_entries se JOIN song_visits v ON v.song_id = se.variant_id WHERE se.lesson_id = l.id),
		       (SELECT count(*) FROM lesson_progress p WHERE p.lesson_id = l.id),
		       (SELECT count(*) FROM songbook_downloads d JOIN songbook_files f ON f.id = d.file_id
		          LEFT JOIN songbook_entries fe ON fe.songbook_id = f.songbook_id AND fe.variant_id = f.variant_id
		        WHERE coalesce(f.lesson_id, fe.lesson_id) = l.id)
		FROM lessons l JOIN songbooks sb ON sb.id = l.songbook_id
		WHERE sb.owner_kind = 'community' AND sb.owner_community_id = $1 AND sb.kind = 'course'
		ORDER BY l.songbook_id, l.number NULLS LAST, l.id`, communityID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var l CourseLessonStats
		if err := rows.Scan(&l.SongbookID, &l.LessonID, &l.Number, &l.Title, &l.Songs, &l.Views, &l.Viewers, &l.Done, &l.Downloads); err != nil {
			rows.Close()
			return nil, err
		}
		out.Lessons = append(out.Lessons, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = st.db.Query(ctx, `WITH `+communitySongsCTE+`,
		views AS (
			SELECT v.person_id, count(DISTINCT v.song_id) AS lessons, sum(v.views) AS views, max(v.visited_at) AS last
			FROM song_visits v WHERE v.song_id IN (SELECT variant_id FROM community_songs)
			GROUP BY v.person_id
		),
		dl AS (
			SELECT d.person_id, count(DISTINCT d.file_id) AS files, count(*) AS downloads,
			       count(*) FILTER (WHERE d.what = 'zip') AS zips, max(d.at) AS last
			FROM songbook_downloads d WHERE d.community_id = $1
			GROUP BY d.person_id
		),
		done AS (
			SELECT p.person_id, count(*) AS lessons, max(p.done_at) AS last
			FROM lesson_progress p JOIN lessons l ON l.id = p.lesson_id JOIN songbooks sb ON sb.id = l.songbook_id
			WHERE sb.owner_kind = 'community' AND sb.owner_community_id = $1
			GROUP BY p.person_id
		)
		SELECT p.id, p.username, p.display_name, cr.name,
		       coalesce(views.lessons, 0), coalesce(views.views, 0),
		       coalesce(dl.files, 0), coalesce(dl.downloads, 0), coalesce(dl.zips, 0), coalesce(done.lessons, 0),
		       greatest(views.last, dl.last, done.last)
		FROM community_members m
		JOIN people p ON p.id = m.person_id
		JOIN community_roles cr ON cr.id = m.role_id
		LEFT JOIN views ON views.person_id = p.id
		LEFT JOIN dl ON dl.person_id = p.id
		LEFT JOIN done ON done.person_id = p.id
		WHERE m.community_id = $1 AND upper_inf(m.validity)
		ORDER BY greatest(views.last, dl.last, done.last) DESC NULLS LAST, lower(p.display_name), p.username`, communityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m MemberStats
		if err := rows.Scan(&m.PersonID, &m.Username, &m.DisplayName, &m.Role, &m.Songs, &m.Views,
			&m.Files, &m.Downloads, &m.Zips, &m.LessonsDone, &m.LastActivity); err != nil {
			return nil, err
		}
		out.Members = append(out.Members, m)
	}
	return &out, rows.Err()
}

// MemberSongView is a song of the community a member viewed.
type MemberSongView struct {
	VariantID int64
	Title     string
	Views     int
	First     time.Time
	Last      time.Time
}

// MemberDownload is one download of a member's.
type MemberDownload struct {
	SongbookID int64
	FileID     *int64
	What       string
	Name       string
	ByteSize   int64
	At         time.Time
}

// GetMemberActivity is what personID viewed and downloaded in communityID,
// newest first (the last 500 downloads).
func (st *Store) GetMemberActivity(ctx context.Context, communityID, personID int64) ([]MemberSongView, []MemberDownload, error) {
	rows, err := st.db.Query(ctx, `WITH `+communitySongsCTE+`
		SELECT s.id, s.title, v.views, v.first_visited_at, v.visited_at
		FROM song_visits v JOIN songs s ON s.id = v.song_id
		WHERE v.person_id = $2 AND v.song_id IN (SELECT variant_id FROM community_songs)
		ORDER BY v.visited_at DESC`, communityID, personID)
	if err != nil {
		return nil, nil, err
	}
	var views []MemberSongView
	for rows.Next() {
		var v MemberSongView
		if err := rows.Scan(&v.VariantID, &v.Title, &v.Views, &v.First, &v.Last); err != nil {
			rows.Close()
			return nil, nil, err
		}
		views = append(views, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	rows, err = st.db.Query(ctx, `
		SELECT coalesce(d.songbook_id, 0), d.file_id, d.what, d.name, d.byte_size, d.at
		FROM songbook_downloads d
		WHERE d.community_id = $1 AND d.person_id = $2
		ORDER BY d.at DESC LIMIT 500`, communityID, personID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var downloads []MemberDownload
	for rows.Next() {
		var d MemberDownload
		if err := rows.Scan(&d.SongbookID, &d.FileID, &d.What, &d.Name, &d.ByteSize, &d.At); err != nil {
			return nil, nil, err
		}
		downloads = append(downloads, d)
	}
	return views, downloads, rows.Err()
}

// MemberLessonDone is a course lesson a member said they've done.
type MemberLessonDone struct {
	SongbookID int64
	LessonID   int64
	Number     *int // the lesson's, as Lesson's: a page names it
	Title      string
	At         time.Time
}

// GetMemberLessonsDone is the lessons of communityID's courses personID
// has done, the latest first.
func (st *Store) GetMemberLessonsDone(ctx context.Context, communityID, personID int64) ([]MemberLessonDone, error) {
	rows, err := st.db.Query(ctx, `
		SELECT l.songbook_id, l.id, l.number, l.title, p.done_at
		FROM lesson_progress p JOIN lessons l ON l.id = p.lesson_id JOIN songbooks sb ON sb.id = l.songbook_id
		WHERE sb.owner_kind = 'community' AND sb.owner_community_id = $1 AND p.person_id = $2
		ORDER BY p.done_at DESC`, communityID, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemberLessonDone
	for rows.Next() {
		var d MemberLessonDone
		if err := rows.Scan(&d.SongbookID, &d.LessonID, &d.Number, &d.Title, &d.At); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
