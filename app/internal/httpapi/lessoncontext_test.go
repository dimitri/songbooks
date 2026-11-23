package httpapi

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"app/internal/store"
)

// TestLessonCourseContext: a lesson opened from no particular place is
// opened in its course (?songbook=<course>), for whoever may open that
// course: the first one, the lowest id first, when it's in two; a member
// of neither, or a song opened from a set list, a songbook, a band…,
// keeps the address it has. Its own database
// (SONGBOOKS_HTTPAPI_TEST_DATABASE_URL, a scratch one), skipped without.
func TestLessonCourseContext(t *testing.T) {
	url := os.Getenv("SONGBOOKS_HTTPAPI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set SONGBOOKS_HTTPAPI_TEST_DATABASE_URL to a scratch database to run this")
	}
	ctx := context.Background()
	st, err := store.New(ctx, url)
	if err != nil {
		t.Skipf("no test database at %s: %v", url, err)
	}
	defer st.Close()
	sfx := fmt.Sprint(time.Now().UnixNano())
	person := func(name string) *store.Person {
		id, err := st.CreatePerson(ctx, name+sfx, name, name+sfx+"@example.com")
		if err != nil {
			t.Fatal(err)
		}
		p, err := st.GetPersonByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	teacher, student, stranger, other := person("Teacher"), person("Student"), person("Stranger"), person("Other")
	course := func(owner *store.Person, name string, members ...*store.Person) (int64, int64) {
		c, err := st.CreateCommunity(ctx, name+sfx, owner.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range members {
			if _, err := st.AddCommunityMember(ctx, c, m.ID, "member", owner.ID); err != nil {
				t.Fatal(err)
			}
		}
		sb, err := st.CreateSongbook(ctx, "community", c, name, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetSongbookKind(ctx, sb, store.SongbookKindCourse); err != nil {
			t.Fatal(err)
		}
		return c, sb
	}
	_, first := course(teacher, "Méthode", student)
	_, second := course(other, "Autre méthode", stranger)
	lesson, err := st.CreateVariant(ctx, store.OwnerPerson, teacher.ID, "Le bend d'un ton", "", teacher.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, sb := range []int64{first, second} {
		if err := st.AddToSongbook(ctx, sb, lesson, teacher.ID); err != nil {
			t.Fatal(err)
		}
		five := 5
		l, err := st.CreateLesson(ctx, sb, &five, "Le bend d'un ton")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetSongLesson(ctx, sb, lesson, &l); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{store: st}
	opened := func(as *store.Person, query string) string {
		r := httptest.NewRequest("GET", "/song/x"+query, nil)
		r = r.WithContext(context.WithValue(r.Context(), personCtxKey, as))
		return s.withCourseContext(r, lesson).URL.Query().Get("songbook")
	}
	if got := opened(student, ""); got != fmt.Sprint(first) {
		t.Errorf("the student opening the lesson: songbook %q, want its course %d", got, first)
	}
	if got := opened(teacher, "?view=practice"); got != fmt.Sprint(first) {
		t.Errorf("the teacher: songbook %q, want %d", got, first)
	}
	// In two courses: the one this viewer may open.
	if got := opened(stranger, ""); got != fmt.Sprint(second) {
		t.Errorf("a member of the other course only: songbook %q, want %d", got, second)
	}
	nobody := person("Nobody")
	if got := opened(nobody, ""); got != "" {
		t.Errorf("a member of neither course got %q", got)
	}
	for _, q := range []string{"?setlist=3", "?band=2", "?event=4", "?community=5", "?embed=playlist", "?songbook=99"} {
		if got := opened(student, q); q != "?songbook=99" && got != "" || q == "?songbook=99" && got != "99" {
			t.Errorf("opened with %s: songbook %q", q, got)
		}
	}
	// The page's course lesson: its title (and video) for the student; for
	// a member of neither, a lesson still, without them.
	data := map[string]any{"GPFileName": "a.gp"}
	r := httptest.NewRequest("GET", "/song/x", nil).WithContext(context.WithValue(ctx, personCtxKey, student))
	if l := s.songLessonFor(r, lesson, data); l == nil || l.Title != "Le bend d'un ton" {
		t.Errorf("the student's course lesson: %+v", l)
	}
	r = httptest.NewRequest("GET", "/song/x", nil).WithContext(context.WithValue(ctx, personCtxKey, nobody))
	if l := s.songLessonFor(r, lesson, data); l == nil || l.Title != "" || l.VideoID != "" {
		t.Errorf("a stranger's course lesson: %+v", l)
	}
}
