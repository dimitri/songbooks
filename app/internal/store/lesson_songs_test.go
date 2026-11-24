package store

import (
	"context"
	"testing"
)

// A song is a course's lesson when it's placed in one of a course's
// lessons; not in a plain songbook, not left out of every lesson, not
// once the songbook is a songbook again.
func TestCourseLessonsOfSong(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, err := st.CreatePerson(ctx, "Teacher", "teacher@example.com")
	if err != nil {
		t.Fatal(err)
	}
	sb, err := st.GetDefaultSongbook(ctx, "person", me)
	if err != nil {
		t.Fatal(err)
	}
	song := func(title string) int64 {
		id, err := st.CreateVariant(ctx, OwnerPerson, me, title, "", me)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AddToSongbook(ctx, sb, id, me); err != nil {
			t.Fatal(err)
		}
		return id
	}
	lessonSong, loose := song("Le bend d'un ton"), song("Pas dans un cours")
	is := func(v int64) bool {
		refs, err := st.CourseLessonsOfSong(ctx, v)
		if err != nil {
			t.Fatal(err)
		}
		return len(refs) > 0
	}
	if is(lessonSong) {
		t.Fatalf("a plain songbook's song is a lesson")
	}
	if err := st.SetSongbookKind(ctx, sb, SongbookKindCourse); err != nil {
		t.Fatal(err)
	}
	five := 5
	l, err := st.CreateLesson(ctx, sb, &five, "Le bend d'un ton")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSongLesson(ctx, sb, lessonSong, &l); err != nil {
		t.Fatal(err)
	}
	if got, err := st.CourseLessonsOfSong(ctx, lessonSong); err != nil || len(got) != 1 || got[0] != (CourseLessonRef{SongbookID: sb, LessonID: l}) {
		t.Errorf("a course's lesson's song: %+v, %v", got, err)
	}
	if is(loose) {
		t.Errorf("a course's song in no lesson is a lesson")
	}
	if err := st.SetSongbookKind(ctx, sb, SongbookKindSongbook); err == nil && is(lessonSong) {
		t.Errorf("a songbook again, its songs are still lessons")
	}
}
