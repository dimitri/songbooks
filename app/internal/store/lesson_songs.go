package store

import "context"

// CourseLessonRef is a course lesson a song is placed in.
type CourseLessonRef struct {
	SongbookID, LessonID int64
}

// CourseLessonsOfSong are the course lessons a song is placed in (a
// songbook of kind "course", one of its lessons), the lowest course id
// first; none for a song in no course. A song in one makes its page a
// lesson's (httpapi/lessonview.go), with its file, whichever way it's
// opened; the first the viewer may open gives it its course's context.
func (st *Store) CourseLessonsOfSong(ctx context.Context, variantID int64) ([]CourseLessonRef, error) {
	rows, err := st.db.Query(ctx, `
		SELECT e.songbook_id, e.lesson_id FROM songbook_entries e JOIN songbooks sb ON sb.id = e.songbook_id
		WHERE e.variant_id = $1 AND e.lesson_id IS NOT NULL AND sb.kind = $2
		ORDER BY e.songbook_id, e.lesson_id`, variantID, SongbookKindCourse)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CourseLessonRef
	for rows.Next() {
		var c CourseLessonRef
		if err := rows.Scan(&c.SongbookID, &c.LessonID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
