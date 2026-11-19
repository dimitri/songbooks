package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"app/internal/store"
)

// TestOrderByCalendar: the coming week's lessons first, the soonest
// first; the others as they were.
func TestOrderByCalendar(t *testing.T) {
	at := func(h int) *time.Time { x := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC).Add(time.Duration(h) * time.Hour); return &x }
	ts := []coachKeyTarget{
		{KeyTarget: store.KeyTarget{Name: "Ada"}},
		{KeyTarget: store.KeyTarget{Name: "Bea"}, NextSession: at(50)},
		{KeyTarget: store.KeyTarget{Name: "Cid"}},
		{KeyTarget: store.KeyTarget{Name: "Dee"}, NextSession: at(10)},
		{KeyTarget: store.KeyTarget{Name: "Group", Kind: "group"}, NextSession: at(30)},
	}
	orderByCalendar(ts)
	var got []string
	for _, x := range ts {
		got = append(got, x.Name)
	}
	if strings.Join(got, ",") != "Dee,Group,Bea,Ada,Cid" {
		t.Fatalf("order: %v", got)
	}
}

// TestCoachKeysPermissions runs the song page's key endpoints and the
// community's player option against a real database: only the
// community's coach gives a student a key, a student reads none of the
// others', the coming week's lessons come first; only the staff turn the
// player for singers on. Its own database (SONGBOOKS_HTTPAPI_TEST_DATABASE_URL,
// a scratch one: rows are added, never removed), skipped without it.
func TestCoachKeysPermissions(t *testing.T) {
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
	coach, poppy, george := person("Coach"), person("Poppy"), person("George")
	c, err := st.CreateCommunity(ctx, "Voice "+sfx, coach.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetCommunityKind(ctx, c, "coaching"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []*store.Person{poppy, george} {
		if _, err := st.AddCommunityMember(ctx, c, p.ID, "member", coach.ID); err != nil {
			t.Fatal(err)
		}
	}
	song, err := st.CreateVariant(ctx, store.OwnerPerson, coach.ID, "Song", "", coach.ID)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := st.StudentSongbook(ctx, c, poppy.ID, "Poppy’s songs", coach.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddToSongbook(ctx, sb, song, coach.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.SetEntrySemitones(ctx, sb, song, -1); err != nil {
		t.Fatal(err)
	}
	soon := time.Now().Add(48 * time.Hour)
	end := soon.Add(time.Hour)
	ev, err := st.CreateEvent(ctx, "community", c, "lesson", "George", "", "", soon, &end, "members", coach.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RegisterForEvent(ctx, ev, george.ID, 0); err != nil {
		t.Fatal(err)
	}

	s := &Server{store: st}
	call := func(as *store.Person, h http.HandlerFunc, method, path, body string, pathVals map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		for k, v := range pathVals {
			r.SetPathValue(k, v)
		}
		r = r.WithContext(context.WithValue(r.Context(), personCtxKey, as))
		w := httptest.NewRecorder()
		h(w, r)
		return w
	}
	songID := map[string]string{"id": fmt.Sprint(song)}
	keys := func(as *store.Person) []coachKeyTarget {
		w := call(as, s.handleGetCoachKeys, "GET", "/api/songs/x/coach-keys?lang=fr", "", songID)
		if w.Code != http.StatusOK {
			t.Fatalf("GET coach-keys: %d %s", w.Code, w.Body)
		}
		var d struct{ Targets []coachKeyTarget }
		if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		return d.Targets
	}

	// The coach: George first (a lesson in two days, its date said), then Poppy in her key.
	ts := keys(coach)
	if len(ts) != 2 || ts[0].PersonID != george.ID || ts[0].NextSession == nil || ts[0].NextSessionLabel == "" || ts[1].PersonID != poppy.ID || ts[1].Semitones != -1 || ts[1].NextSession != nil {
		t.Fatalf("the coach's list: %+v", ts)
	}
	// A student: nobody's key to give.
	if ts := keys(poppy); len(ts) != 0 {
		t.Fatalf("a student sees %+v", ts)
	}
	post := func(as *store.Person, who int64, n int) int {
		body := fmt.Sprintf(`{"communityId":%d,"personId":%d,"semitones":%d}`, c, who, n)
		return call(as, s.handlePostCoachKey, "POST", "/api/songs/x/coach-keys", body, songID).Code
	}
	if code := post(poppy, george.ID, 3); code != http.StatusForbidden {
		t.Fatalf("a student gave a key: %d", code)
	}
	if code := post(poppy, poppy.ID, 3); code != http.StatusForbidden {
		t.Fatalf("a student set their own key: %d", code)
	}
	if code := post(coach, george.ID, 2); code != http.StatusOK {
		t.Fatalf("the coach: %d", code)
	}
	if _, n, ok, _ := st.StudentKeyOf(ctx, george.ID, song); !ok || n != 2 {
		t.Fatalf("George's key: %d %v", n, ok)
	}

	// The player for singers: the staff's to turn on, not a student's.
	put := func(as *store.Person, on bool) int {
		h := s.requireCommunityPrivilege(store.PrivManageSettings, s.handlePutCommunityVocalSlider)
		s.devAutoLogin = as // requireAuth's way in, for this call
		defer func() { s.devAutoLogin = nil }()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("PUT", "/api/communities/x/vocal-slider", strings.NewReader(fmt.Sprintf(`{"on":%v}`, on)))
		r.SetPathValue("id", fmt.Sprint(c))
		h(w, r)
		return w.Code
	}
	if code := put(poppy, true); code != http.StatusForbidden {
		t.Fatalf("a student turned it on: %d", code)
	}
	if on, _ := st.CommunityPlayerVocalSlider(ctx, c); on {
		t.Fatal("on after a student's try")
	}
	if code := put(coach, true); code != http.StatusOK {
		t.Fatalf("the coach: %d", code)
	}
	if on, _ := st.CommunityPlayerVocalSlider(ctx, c); !on {
		t.Fatal("not on")
	}

	// The song page's context: the student's songbook, the repertoire's
	// community; another community's songbook without it says no.
	data := map[string]any{"SongbookRef": sb}
	r := httptest.NewRequest("GET", "/song/x", nil).WithContext(context.WithValue(ctx, personCtxKey, poppy))
	if id := s.vocalSliderCommunity(r, data, nil, song); id != c {
		t.Fatalf("from Poppy's songbook: %d", id)
	}
	if id := s.vocalSliderCommunity(r, map[string]any{}, nil, song); id != c {
		t.Fatalf("Poppy, from nowhere: %d", id)
	}
	other, _ := st.CreateCommunity(ctx, "Jam "+sfx, coach.ID)
	if id := s.vocalSliderCommunity(r, map[string]any{"CommunityRef": other}, nil, song); id != 0 {
		t.Fatalf("from a community without it: %d", id)
	}
	outsider := person("Outsider")
	r = httptest.NewRequest("GET", "/song/x", nil).WithContext(context.WithValue(ctx, personCtxKey, outsider))
	if id := s.vocalSliderCommunity(r, map[string]any{}, nil, song); id != 0 {
		t.Fatalf("an outsider, from nowhere: %d", id)
	}
}
