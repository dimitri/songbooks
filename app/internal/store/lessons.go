package store

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// A course is a songbook whose songs are grouped in lessons (migration
// 0073, docs/courses.md). The songbook stays the product: access, sharing,
// downloads and stats are the songbook's; a lesson only groups its songs
// and files, in the author's order, with a text of its own.

const (
	SongbookKindSongbook = "songbook"
	SongbookKindCourse   = "course"
	SongbookKindStudent  = "student"
)

// Lesson is one lesson of a course: the author's number (nil when it has
// none), a title, a text, and its songs in order.
type Lesson struct {
	ID         int64
	SongbookID int64
	Number     *int
	Title      string
	Body       string
	VideoURL   string // the lesson's video; "" for its songs' (migration 0074)
	Songs      []LessonSong
}

// LessonSong is one song of a lesson, or of a course outside any lesson.
type LessonSong struct {
	VariantID int64
	Title     string
	Artist    string
}

// Label is how the lesson is named in a list: "Lesson 3 · Title".
func (l Lesson) Label() string {
	if l.Number == nil {
		return l.Title
	}
	return fmt.Sprintf("Lesson %d · %s", *l.Number, l.Title)
}

// Paragraphs is the lesson's text, a paragraph per blank-line-separated
// block.
func (l Lesson) Paragraphs() []string {
	var out []string
	for _, p := range strings.Split(strings.ReplaceAll(l.Body, "\r\n", "\n"), "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// SetSongbookKind makes songbookID a course or a plain songbook. Its
// lessons are kept either way.
func (st *Store) SetSongbookKind(ctx context.Context, songbookID int64, kind string) error {
	if kind != SongbookKindSongbook && kind != SongbookKindCourse {
		return fmt.Errorf("invalid songbook kind %q", kind)
	}
	_, err := st.db.Exec(ctx, `UPDATE songbooks SET kind = $2 WHERE id = $1`, songbookID, kind)
	return err
}

const lessonOrder = `l.number NULLS LAST, l.id`

// ListLessons lists songbookID's lessons in order, each with its songs,
// and the songbook's songs that are in no lesson (by title).
func (st *Store) ListLessons(ctx context.Context, songbookID int64) ([]Lesson, []LessonSong, error) {
	rows, err := st.db.Query(ctx, `
		SELECT l.id, l.songbook_id, l.number, l.title, l.body, l.video_url FROM lessons l
		WHERE l.songbook_id = $1 ORDER BY `+lessonOrder, songbookID)
	if err != nil {
		return nil, nil, err
	}
	var lessons []Lesson
	at := map[int64]int{}
	for rows.Next() {
		var l Lesson
		if err := rows.Scan(&l.ID, &l.SongbookID, &l.Number, &l.Title, &l.Body, &l.VideoURL); err != nil {
			rows.Close()
			return nil, nil, err
		}
		at[l.ID] = len(lessons)
		lessons = append(lessons, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	rows, err = st.db.Query(ctx, `
		SELECT se.lesson_id, s.id, s.title, s.artist
		FROM songbook_entries se JOIN songs s ON s.id = se.variant_id
		WHERE se.songbook_id = $1
		ORDER BY se.lesson_id NULLS LAST, se.lesson_position, lower(s.title), s.id`, songbookID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var loose []LessonSong
	for rows.Next() {
		var lessonID *int64
		var s LessonSong
		if err := rows.Scan(&lessonID, &s.VariantID, &s.Title, &s.Artist); err != nil {
			return nil, nil, err
		}
		if lessonID == nil {
			loose = append(loose, s)
			continue
		}
		if i, ok := at[*lessonID]; ok {
			lessons[i].Songs = append(lessons[i].Songs, s)
		}
	}
	return lessons, loose, rows.Err()
}

// GetLesson is one lesson, with its songs.
func (st *Store) GetLesson(ctx context.Context, id int64) (*Lesson, error) {
	var l Lesson
	err := st.db.QueryRow(ctx, `SELECT id, songbook_id, number, title, body, video_url FROM lessons WHERE id = $1`, id).
		Scan(&l.ID, &l.SongbookID, &l.Number, &l.Title, &l.Body, &l.VideoURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such lesson %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	rows, err := st.db.Query(ctx, `
		SELECT s.id, s.title, s.artist
		FROM songbook_entries se JOIN songs s ON s.id = se.variant_id
		WHERE se.lesson_id = $1 ORDER BY se.lesson_position, lower(s.title), s.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s LessonSong
		if err := rows.Scan(&s.VariantID, &s.Title, &s.Artist); err != nil {
			return nil, err
		}
		l.Songs = append(l.Songs, s)
	}
	return &l, rows.Err()
}

// LessonOfSong is the lesson variantID is in, in songbookID; nil when
// it's in none.
func (st *Store) LessonOfSong(ctx context.Context, songbookID, variantID int64) (*Lesson, error) {
	var id *int64
	err := st.db.QueryRow(ctx, `SELECT lesson_id FROM songbook_entries WHERE songbook_id = $1 AND variant_id = $2`,
		songbookID, variantID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && id == nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return st.GetLesson(ctx, *id)
}

// CreateLesson adds a lesson to songbookID.
func (st *Store) CreateLesson(ctx context.Context, songbookID int64, number *int, title string) (int64, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return 0, errors.New("a lesson needs a title")
	}
	var id int64
	err := st.db.QueryRow(ctx, `INSERT INTO lessons (songbook_id, number, title) VALUES ($1, $2, $3) RETURNING id`,
		songbookID, number, title).Scan(&id)
	return id, err
}

// UpdateLesson changes a lesson's number, title, text and video.
func (st *Store) UpdateLesson(ctx context.Context, id int64, number *int, title, body, videoURL string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("a lesson needs a title")
	}
	_, err := st.db.Exec(ctx, `UPDATE lessons SET number = $2, title = $3, body = $4, video_url = $5 WHERE id = $1`,
		id, number, title, strings.TrimSpace(body), strings.TrimSpace(videoURL))
	return err
}

// DeleteLesson removes a lesson; its songs and files stay in the course,
// in no lesson.
func (st *Store) DeleteLesson(ctx context.Context, id int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM lessons WHERE id = $1`, id)
	return err
}

// lessonInSongbook checks that lessonID is one of songbookID's lessons.
func (st *Store) lessonInSongbook(ctx context.Context, songbookID, lessonID int64) error {
	var ok bool
	if err := st.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM lessons WHERE id = $1 AND songbook_id = $2)`,
		lessonID, songbookID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("lesson %d isn't in songbook %d: %w", lessonID, songbookID, ErrNotFound)
	}
	return nil
}

// SetSongLesson puts variantID, a song of songbookID, in lessonID (last),
// or in no lesson (nil).
func (st *Store) SetSongLesson(ctx context.Context, songbookID, variantID int64, lessonID *int64) error {
	if lessonID != nil {
		if err := st.lessonInSongbook(ctx, songbookID, *lessonID); err != nil {
			return err
		}
	}
	tag, err := st.db.Exec(ctx, `
		UPDATE songbook_entries SET lesson_id = $3,
		       lesson_position = coalesce((SELECT max(lesson_position) + 1 FROM songbook_entries WHERE lesson_id = $3), 0)
		WHERE songbook_id = $1 AND variant_id = $2 AND lesson_id IS DISTINCT FROM $3`, songbookID, variantID, lessonID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if in, err := st.SongbookHasVariant(ctx, songbookID, variantID); err != nil {
			return err
		} else if !in {
			return fmt.Errorf("song %d isn't in songbook %d: %w", variantID, songbookID, ErrNotFound)
		}
	}
	return nil
}

// MoveLessonSong moves variantID one place up (-1) or down (+1) in its
// lesson.
func (st *Store) MoveLessonSong(ctx context.Context, lessonID, variantID int64, by int) error {
	l, err := st.GetLesson(ctx, lessonID)
	if err != nil {
		return err
	}
	i := -1
	for k, s := range l.Songs {
		if s.VariantID == variantID {
			i = k
		}
	}
	j := i + by
	if i < 0 || j < 0 || j >= len(l.Songs) {
		return nil
	}
	l.Songs[i], l.Songs[j] = l.Songs[j], l.Songs[i]
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for k, s := range l.Songs {
		if _, err := tx.Exec(ctx, `UPDATE songbook_entries SET lesson_position = $3 WHERE lesson_id = $1 AND variant_id = $2`,
			lessonID, s.VariantID, k); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// SetFileLesson puts a file of a course in lessonID (nil: back with its
// song's lesson, or none).
func (st *Store) SetFileLesson(ctx context.Context, fileID int64, lessonID *int64) error {
	f, err := st.GetSongbookFile(ctx, fileID)
	if err != nil {
		return err
	}
	if lessonID != nil {
		if err := st.lessonInSongbook(ctx, f.SongbookID, *lessonID); err != nil {
			return err
		}
	}
	_, err = st.db.Exec(ctx, `UPDATE songbook_files SET lesson_id = $2 WHERE id = $1`, fileID, lessonID)
	return err
}

// LessonsFromFiles makes a course's lessons from its files, as an author
// numbers them: a lesson per number a top folder ("07_Plan Chicago Blues/")
// or else a file name ("07_Plan Chicago Blues PB.mp3") starts with, named
// after the folder, or else its Guitar Pro files. Each lesson gets its
// files' songs, and the files that go with no song. A lesson already
// there with that number is kept as it is, and a song or file already in
// a lesson stays there: running it again only adds what's new.
func (st *Store) LessonsFromFiles(ctx context.Context, songbookID int64) (created, placed int, err error) {
	files, err := st.ListSongbookFiles(ctx, []int64{songbookID})
	if err != nil {
		return 0, 0, err
	}
	type group struct {
		folder   string
		gp, rest []string
		songs    []int64
		loose    []int64
		key      map[int64]string // a song's name, as its files have it
	}
	groups := map[int]*group{}
	for _, f := range files {
		if f.Kind == "book" {
			continue
		}
		top, _, _ := strings.Cut(f.Path, "/")
		num, folder := lessonNumber(top), ""
		if num != "" {
			folder = lessonTitle(top)
		} else {
			num = lessonNumber(f.Filename)
		}
		n, err := strconv.Atoi(num)
		if err != nil {
			continue
		}
		g := groups[n]
		if g == nil {
			g = &group{key: map[int64]string{}}
			groups[n] = g
		}
		if g.folder == "" {
			g.folder = folder
		}
		if f.Kind == "gp" {
			g.gp = append(g.gp, lessonTitle(f.Filename))
		} else {
			g.rest = append(g.rest, lessonTitle(f.Filename))
		}
		if f.VariantID != nil {
			if !containsID(g.songs, *f.VariantID) {
				g.songs = append(g.songs, *f.VariantID)
			}
			if k := g.key[*f.VariantID]; k == "" || f.Kind == "gp" {
				g.key[*f.VariantID] = matchKey(lessonTitle(f.Filename))
			}
		} else {
			g.loose = append(g.loose, f.ID)
		}
	}
	nums := make([]int, 0, len(groups))
	for n, g := range groups {
		nums = append(nums, n)
		sort.SliceStable(g.songs, func(i, j int) bool { return g.key[g.songs[i]] < g.key[g.songs[j]] })
	}
	sort.Ints(nums)

	existing := map[int]int64{}
	rows, err := st.db.Query(ctx, `SELECT number, id FROM lessons WHERE songbook_id = $1 AND number IS NOT NULL ORDER BY id DESC`, songbookID)
	if err != nil {
		return 0, 0, err
	}
	for rows.Next() {
		var n int
		var id int64
		if err := rows.Scan(&n, &id); err != nil {
			rows.Close()
			return 0, 0, err
		}
		existing[n] = id
	}
	rows.Close()

	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)
	for _, n := range nums {
		g := groups[n]
		id, ok := existing[n]
		if !ok {
			title := g.folder
			if title == "" {
				title = commonTitle(g.gp)
			}
			if title == "" {
				title = commonTitle(g.rest)
			}
			if title == "" {
				title = fmt.Sprintf("Lesson %d", n)
			}
			// The song's own title when it says the same with its accents
			// ("Hommage a SRV": "Hommage à SRV"), not in capitals.
			for _, v := range g.songs {
				var song string
				if err := tx.QueryRow(ctx, `SELECT title FROM songs WHERE id = $1`, v).Scan(&song); err != nil {
					return 0, 0, err
				}
				if matchKey(song) == matchKey(title) && song != strings.ToUpper(song) {
					title = song
				}
			}
			if err := tx.QueryRow(ctx, `INSERT INTO lessons (songbook_id, number, title) VALUES ($1, $2, $3) RETURNING id`,
				songbookID, n, title).Scan(&id); err != nil {
				return 0, 0, err
			}
			created++
		}
		for _, v := range g.songs {
			tag, err := tx.Exec(ctx, `
				UPDATE songbook_entries SET lesson_id = $3,
				       lesson_position = coalesce((SELECT max(lesson_position) + 1 FROM songbook_entries WHERE lesson_id = $3), 0)
				WHERE songbook_id = $1 AND variant_id = $2 AND lesson_id IS NULL`, songbookID, v, id)
			if err != nil {
				return 0, 0, err
			}
			placed += int(tag.RowsAffected())
		}
		for _, fid := range g.loose {
			tag, err := tx.Exec(ctx, `UPDATE songbook_files SET lesson_id = $2 WHERE id = $1 AND lesson_id IS NULL`, fid, id)
			if err != nil {
				return 0, 0, err
			}
			placed += int(tag.RowsAffected())
		}
	}
	return created, placed, tx.Commit(ctx)
}

func containsID(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// lessonTitle is a lesson's name from an author's folder or file name:
// no extension, lesson number, underscores, or backing track's "PB".
func lessonTitle(name string) string {
	name = strings.TrimSpace(fileStem(path.Base(name)))
	name = strings.Trim(name, "_- ")
	words := strings.Fields(strings.ReplaceAll(name, "_", " "))
	for len(words) > 0 {
		last := strings.ToLower(words[len(words)-1])
		if last != "pb" && last != "playback" {
			break
		}
		words = words[:len(words)-1]
	}
	return strings.Join(words, " ")
}

// commonTitle is what names share: the one name, or the words they all
// start with ("Plan psychédélique 1", "… 2": "Plan psychédélique").
func commonTitle(names []string) string {
	if len(names) == 0 {
		return ""
	}
	common := strings.Fields(names[0])
	for _, n := range names[1:] {
		w := strings.Fields(n)
		k := 0
		for k < len(common) && k < len(w) && strings.EqualFold(common[k], w[k]) {
			k++
		}
		common = common[:k]
	}
	if len(common) == 0 {
		return names[0]
	}
	return strings.Join(common, " ")
}

// LessonProgress is where someone is with one lesson: started once
// they've opened one of its songs or files, done when they said so.
type LessonProgress struct {
	Started bool
	Done    bool
	Last    *time.Time // when they last opened something of it
}

// CourseProgress is personID's progress in songbookID's lessons, by
// lesson id.
func (st *Store) CourseProgress(ctx context.Context, songbookID, personID int64) (map[int64]LessonProgress, error) {
	rows, err := st.db.Query(ctx, `
		SELECT l.id,
		       EXISTS(SELECT 1 FROM lesson_progress p WHERE p.lesson_id = l.id AND p.person_id = $2),
		       greatest(
		         (SELECT max(v.visited_at) FROM songbook_entries se JOIN song_visits v ON v.song_id = se.variant_id AND v.person_id = $2
		          WHERE se.lesson_id = l.id),
		         (SELECT max(d.at) FROM songbook_downloads d
		            JOIN songbook_files f ON f.id = d.file_id
		            LEFT JOIN songbook_entries fe ON fe.songbook_id = f.songbook_id AND fe.variant_id = f.variant_id
		          WHERE d.person_id = $2 AND coalesce(f.lesson_id, fe.lesson_id) = l.id))
		FROM lessons l WHERE l.songbook_id = $1`, songbookID, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]LessonProgress{}
	for rows.Next() {
		var id int64
		var p LessonProgress
		if err := rows.Scan(&id, &p.Done, &p.Last); err != nil {
			return nil, err
		}
		p.Started = p.Done || p.Last != nil
		out[id] = p
	}
	return out, rows.Err()
}

// SetLessonDone records that personID has done lessonID, or not.
func (st *Store) SetLessonDone(ctx context.Context, lessonID, personID int64, done bool) error {
	var err error
	if done {
		_, err = st.db.Exec(ctx, `INSERT INTO lesson_progress (lesson_id, person_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, lessonID, personID)
	} else {
		_, err = st.db.Exec(ctx, `DELETE FROM lesson_progress WHERE lesson_id = $1 AND person_id = $2`, lessonID, personID)
	}
	return err
}

// courseOrder is a course's songs as ListSongsOrdered gives them: a group
// per lesson, then the songs in none. ok is false for a songbook that
// isn't a course, or a course with no lessons yet.
func (st *Store) courseOrder(ctx context.Context, songbookID int64) (groups []SongGroup, ok bool, err error) {
	var kind string
	if err := st.db.QueryRow(ctx, `SELECT kind FROM songbooks WHERE id = $1`, songbookID).Scan(&kind); err != nil {
		return nil, false, err
	}
	if kind != SongbookKindCourse {
		return nil, false, nil
	}
	lessons, loose, err := st.ListLessons(ctx, songbookID)
	if err != nil || len(lessons) == 0 {
		return nil, false, err
	}
	for _, l := range lessons {
		g := SongGroup{Label: l.Label()}
		for _, s := range l.Songs {
			g.VariantIDs = append(g.VariantIDs, s.VariantID)
		}
		if len(g.VariantIDs) > 0 {
			groups = append(groups, g)
		}
	}
	if len(loose) > 0 {
		g := SongGroup{Label: "Not in a lesson"}
		for _, s := range loose {
			g.VariantIDs = append(g.VariantIDs, s.VariantID)
		}
		groups = append(groups, g)
	}
	return groups, true, nil
}

// ShelfCounts is how many songbooks and how many courses a band or a
// community ("band", "community") has: what its shelf is called
// (webassets' shelfLabel).
func (st *Store) ShelfCounts(ctx context.Context, ownerKind string, id int64) (songbooks, courses int, err error) {
	err = st.db.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE kind <> 'course'), count(*) FILTER (WHERE kind = 'course')
		  FROM songbooks
		 WHERE owner_kind = $1 AND (owner_band_id = $2 OR owner_community_id = $2)`, ownerKind, id).Scan(&songbooks, &courses)
	return songbooks, courses, err
}
