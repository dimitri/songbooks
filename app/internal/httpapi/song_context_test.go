package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"app/internal/setlist"
	"app/internal/store"
)

// TestSongContextAccess: a public song opened with a place the viewer may
// not open (another person's songbook, a student's, another band's set
// list, a band or a community they're not in, an event of a community
// they're not in) shows nothing of that place: not its name, not the
// song's position, not the previous and next songs, not the key the song
// has there; the page is the song opened from nowhere in particular. Who
// may open the place sees all of it. A lesson's course is still its own
// rule: a context dropped falls back to it, for who may open it.
func TestSongContextAccess(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	person := func(name string) *store.Person {
		t.Helper()
		id, err := st.CreatePerson(ctx, name, strings.ToLower(name)+"@example.com")
		must(err)
		p, err := st.GetPersonByID(ctx, id)
		must(err)
		return p
	}
	owner, outsider, bandie, coach, student := person("Dimitri"), person("Outsider"), person("Bandie"), person("Coach"), person("Student")
	song := func(kind store.OwnerKind, id int64, title string, by int64) int64 {
		t.Helper()
		v, err := st.CreateVariant(ctx, kind, id, title, "", by)
		must(err)
		return v
	}

	// The public song, between two private ones in its owner's songbook,
	// where it has a key of its own.
	pub := song(store.OwnerPerson, owner.ID, "Open Song", owner.ID)
	must(st.SetSongPublic(ctx, owner.ID, pub, true))
	ownerBook, err := st.CreateSongbook(ctx, "person", owner.ID, "Les morceaux de Dimitri", false)
	must(err)
	for _, v := range []int64{song(store.OwnerPerson, owner.ID, "Secret Prev Title", owner.ID), pub, song(store.OwnerPerson, owner.ID, "Secret Next Title", owner.ID)} {
		must(st.AddToSongbook(ctx, ownerBook, v, owner.ID))
	}
	must(st.SetEntrySemitones(ctx, ownerBook, pub, 3))

	// A band's set list with it, between two of the band's songs.
	band, err := st.CreateBand(ctx, "Les Voisins", bandie.ID)
	must(err)
	sl, err := st.CreateSetlist(ctx, "band", "Set secret des Voisins", "", nil, &band, nil, bandie.ID)
	must(err)
	entries := []setlist.Entry{{VariantID: song(store.OwnerBand, band, "Voisins Prev", bandie.ID)}, {VariantID: pub}, {VariantID: song(store.OwnerBand, band, "Voisins Next", bandie.ID)}}
	_, err = st.SaveSetlistByID(ctx, sl, &setlist.Setlist{Sets: []setlist.Set{{Entries: entries}}}, "Set secret des Voisins", "", "", bandie.ID)
	must(err)

	// A coaching community: an event's program with it, a student's
	// songbook with it in the student's key, a course with it as a lesson.
	studio, err := st.CreateCommunity(ctx, "Studio Clé", coach.ID)
	must(err)
	must(st.SetCommunityKind(ctx, studio, "coaching"))
	_, err = st.AddCommunityMember(ctx, studio, student.ID, "member", coach.ID)
	must(err)
	ev, err := st.CreateEvent(ctx, "community", studio, "jam", "Audition de printemps", "", "", time.Now().AddDate(0, 0, 7), nil, "members", coach.ID)
	must(err)
	must(st.AddProgramSongs(ctx, ev, []int64{song(store.OwnerCommunity, studio, "Programme Prev", coach.ID), pub, song(store.OwnerCommunity, studio, "Programme Next", coach.ID)}))
	studentBook, err := st.StudentSongbook(ctx, studio, student.ID, "Carnet de l'élève", coach.ID)
	must(err)
	must(st.AddToSongbook(ctx, studentBook, pub, coach.ID))
	must(st.SetEntrySemitones(ctx, studentBook, pub, 2))
	course, err := st.CreateSongbook(ctx, "community", studio, "Cours du studio", false)
	must(err)
	must(st.SetSongbookKind(ctx, course, store.SongbookKindCourse))
	must(st.AddToSongbook(ctx, course, pub, coach.ID))
	five := 5
	lesson, err := st.CreateLesson(ctx, course, &five, "Leçon cinq")
	must(err)
	must(st.SetSongLesson(ctx, course, pub, &lesson))

	page := func(as *store.Person, query string) string {
		t.Helper()
		w := httptest.NewRecorder()
		NewServer(st, nil, as, "", "").ServeHTTP(w, httptest.NewRequest("GET", "/song/"+itoa(pub)+query, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("/song/%d%s as %s: %d", pub, query, as.DisplayName, w.Code)
		}
		return w.Body.String()
	}
	courseLink := "/songbooks/" + itoa(course) + "/lessons/" + itoa(lesson)
	type want struct {
		show, hide []string
	}
	place := []string{"song-crumb-pos"}
	cases := []struct {
		name  string
		as    *store.Person
		query string
		want
	}{
		{"the owner, from their songbook", owner, "?songbook=" + itoa(ownerBook),
			want{show: append([]string{"Les morceaux de Dimitri", "Secret Prev Title", "Secret Next Title", `data-key-offset="3"`}, place...)}},
		{"an outsider, with the owner's songbook", outsider, "?songbook=" + itoa(ownerBook),
			want{hide: append([]string{"Les morceaux de Dimitri", "Secret Prev Title", "Secret Next Title", "data-key-offset", "songbooksPracticeContext"}, place...)}},
		{"a band member, from the band's set list", bandie, "?setlist=" + itoa(sl),
			want{show: append([]string{"Set secret des Voisins", "Voisins Prev", "Voisins Next"}, place...)}},
		{"an outsider, with the band's set list", outsider, "?setlist=" + itoa(sl),
			want{hide: append([]string{"Set secret des Voisins", "Voisins Prev", "Voisins Next", "/setlists/" + itoa(sl)}, place...)}},
		{"an outsider, with the band's set list's gig view", outsider, "?setlist=" + itoa(sl) + "&gig=1",
			want{hide: append([]string{"Set secret des Voisins", "Voisins Prev", "Voisins Next", "/setlists/" + itoa(sl)}, place...)}},
		{"a band member, from the band", bandie, "?band=" + itoa(band),
			want{show: []string{"Les Voisins", "/bands/" + itoa(band) + "/catalog"}}},
		{"an outsider, with the band", outsider, "?band=" + itoa(band),
			want{hide: []string{"Les Voisins", "/bands/" + itoa(band) + "/catalog"}}},
		{"a band member, with the owner's songbook and the band", bandie, "?songbook=" + itoa(ownerBook) + "&band=" + itoa(band),
			want{show: []string{"Les Voisins"}, hide: append([]string{"Les morceaux de Dimitri", "Secret Prev Title", "Secret Next Title", "data-key-offset", "?songbook=" + itoa(ownerBook)}, place...)}},
		{"an outsider, with the owner's songbook and the band", outsider, "?songbook=" + itoa(ownerBook) + "&band=" + itoa(band),
			want{hide: append([]string{"Les morceaux de Dimitri", "Secret Prev Title", "Les Voisins", "data-key-offset"}, place...)}},
		{"a student, from the event's program", student, "?event=" + itoa(ev),
			want{show: append([]string{"Audition de printemps", "Programme Prev", "Programme Next", "data-key-offset"}, place...)}},
		{"an outsider, with the event", outsider, "?event=" + itoa(ev),
			want{hide: append([]string{"Audition de printemps", "Programme Prev", "Programme Next", "data-key-offset", "data-coaching"}, place...)}},
		{"a band member, with the event", bandie, "?event=" + itoa(ev),
			want{hide: append([]string{"Audition de printemps", "Programme Prev"}, place...)}},
		{"the student, from their songbook", student, "?songbook=" + itoa(studentBook),
			want{show: []string{"Carnet de l&#39;élève", `data-key-offset="2"`}}},
		{"the coach, from the student's songbook", coach, "?songbook=" + itoa(studentBook),
			want{show: []string{"Carnet de l&#39;élève", `data-key-offset="2"`}}},
		{"an outsider, with the student's songbook", outsider, "?songbook=" + itoa(studentBook),
			want{hide: []string{"Carnet de l", "data-key-offset", "data-key-for", "data-coaching"}}},
		{"a band member, with the student's songbook", bandie, "?songbook=" + itoa(studentBook),
			want{hide: []string{"Carnet de l", "data-key-offset"}}},
		{"an outsider, with the community", outsider, "?community=" + itoa(studio),
			want{hide: []string{"/communities/" + itoa(studio), "data-coaching"}}},
		// The course: the student opening it with a songbook they can't
		// open is in the course, as from nowhere; an outsider isn't.
		{"the student, with the owner's songbook: the course", student, "?songbook=" + itoa(ownerBook),
			want{show: []string{courseLink, "Cours du studio"}, hide: []string{"Les morceaux de Dimitri", "Secret Prev Title"}}},
		{"an outsider, from nowhere: no course", outsider, "",
			want{hide: []string{courseLink, "Cours du studio"}}},
		{"an outsider, with the course", outsider, "?songbook=" + itoa(course),
			want{hide: []string{courseLink, "Cours du studio", "Leçon cinq"}}},
	}
	for _, c := range cases {
		body := page(c.as, c.query)
		for _, s := range c.show {
			if !strings.Contains(body, s) {
				t.Errorf("%s (%s): no %q on the page", c.name, c.query, s)
			}
		}
		for _, s := range c.hide {
			if strings.Contains(body, s) {
				t.Errorf("%s (%s): %q on the page", c.name, c.query, s)
			}
		}
	}

	// Home's "Recently": a song's link keeps only the places the person
	// may still open; the visit is recorded so.
	s := &Server{store: st}
	stored := url.Values{"songbook": {itoa(ownerBook)}, "band": {itoa(band)}, "setlist": {itoa(sl)}, "gig": {"1"}}.Encode()
	if got := s.openableSongQuery(ctx, outsider, stored); got != "" {
		t.Errorf("an outsider's recent song keeps %q", got)
	}
	if got, _ := url.ParseQuery(s.openableSongQuery(ctx, bandie, stored)); got.Get("songbook") != "" || got.Get("band") != itoa(band) || got.Get("setlist") != itoa(sl) || got.Get("gig") != "1" {
		t.Errorf("a band member's recent song keeps %v", got)
	}
	if got := s.openableSongQuery(ctx, owner, "songbook="+itoa(ownerBook)); got != "songbook="+itoa(ownerBook) {
		t.Errorf("the owner's recent song keeps %q", got)
	}
	page(outsider, "?songbook="+itoa(ownerBook)+"&setlist="+itoa(sl))
	recent, err := st.ListRecentSongs(ctx, outsider.ID, 1)
	must(err)
	if len(recent) != 1 || recent[0].Query != "" {
		t.Errorf("the outsider's visit was recorded with %+v", recent)
	}
}
