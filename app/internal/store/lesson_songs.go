package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// CourseLessonOfSong is the course lesson a song is placed in (a songbook
// of kind "course", one of its lessons; the first, should it be in
// several), whichever way the song is opened; nil for none. It makes a
// song's page a lesson's (httpapi/lessonview.go), with its file.
func (st *Store) CourseLessonOfSong(ctx context.Context, variantID int64) (*Lesson, error) {
	var id int64
	err := st.db.QueryRow(ctx, `
		SELECT e.lesson_id FROM songbook_entries e JOIN songbooks sb ON sb.id = e.songbook_id
		WHERE e.variant_id = $1 AND e.lesson_id IS NOT NULL AND sb.kind = $2
		ORDER BY sb.id, e.lesson_id LIMIT 1`,
		variantID, SongbookKindCourse).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return st.GetLesson(ctx, id)
}
