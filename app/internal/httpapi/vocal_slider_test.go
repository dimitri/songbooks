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
	at := func(h int) *time.Time {
		x := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC).Add(time.Duration(h) * time.Hour)
		return &x
	}
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
// coaching community's player options against a real database: only the
// community's coach gives a student a key, a student reads none of the
// others'; the coming week's lessons come first only from the community,
// with its key per student on; only the staff turn the options on, for a
// coaching community only; where a song page was opened decides. Its own database (SONGBOOKS_HTTPAPI_TEST_DATABASE_URL,
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
		id, err := st.CreatePerson(ctx, name, name+sfx+"@example.com")
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
	// The community's song (its repertoire's): a community's songbooks,
	// a student's among them, hold its own songs and public ones, never
	// the coach's private song (store.LinkCheck; checked below).
	song, err := st.CreateCommunityVariant(ctx, c, "Song", "", coach.ID)
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
	keysFrom := func(as *store.Person, query string) []coachKeyTarget {
		w := call(as, s.handleGetCoachKeys, "GET", "/api/songs/x/coach-keys?lang=fr"+query, "", songID)
		if w.Code != http.StatusOK {
			t.Fatalf("GET coach-keys: %d %s", w.Code, w.Body)
		}
		var d struct{ Targets []coachKeyTarget }
		if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		return d.Targets
	}
	inCommunity := fmt.Sprintf("&community=%d", c)
	names := func(ts []coachKeyTarget) string {
		var out []string
		for _, x := range ts {
			out = append(out, x.Name)
		}
		return strings.Join(out, ",")
	}

	// The coach, as on main: by name, no calendar; the same from the
	// community's songbook while its key per student is off.
	for _, q := range []string{"", inCommunity} {
		ts := keysFrom(coach, q)
		if names(ts) != "George,Poppy" || ts[0].NextSession != nil || ts[1].Semitones != -1 {
			t.Fatalf("the coach's list (%q), the key per student off: %+v", q, ts)
		}
	}
	// The key per student on: from the community's songbook, George first
	// (a lesson in two days, its date said), then Poppy in her key;
	// anywhere else, as on main.
	if err := st.SetCommunityCoachStudentKeys(ctx, c, true); err != nil {
		t.Fatal(err)
	}
	ts := keysFrom(coach, inCommunity)
	if len(ts) != 2 || ts[0].PersonID != george.ID || ts[0].NextSession == nil || ts[0].NextSessionLabel == "" || ts[1].PersonID != poppy.ID || ts[1].Semitones != -1 || ts[1].NextSession != nil {
		t.Fatalf("the coach's list, from the community: %+v", ts)
	}
	if ts := keysFrom(coach, ""); names(ts) != "George,Poppy" || ts[0].NextSession != nil {
		t.Fatalf("the coach's list, from elsewhere: %+v", ts)
	}
	// A student: nobody's key to give, from anywhere.
	for _, q := range []string{"", inCommunity} {
		if ts := keysFrom(poppy, q); len(ts) != 0 {
			t.Fatalf("a student sees %+v", ts)
		}
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
	// The coach's own private song, in Poppy's songbook from before the
	// ownership model: George can't be given it (a 409 that says why),
	// the community's songbooks don't hold a person's private song.
	private, err := st.CreateVariant(ctx, store.OwnerPerson, coach.ID, "Mine", "", coach.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddToSongbook(ctx, sb, private, coach.ID); err != nil {
		t.Fatal(err)
	}
	w := call(coach, s.handlePostCoachKey, "POST", "/api/songs/x/coach-keys",
		fmt.Sprintf(`{"communityId":%d,"personId":%d,"semitones":1}`, c, george.ID), map[string]string{"id": fmt.Sprint(private)})
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"private"`) {
		t.Fatalf("the coach's private song for George: %d %s", w.Code, w.Body)
	}

	// The player options: the staff's to turn on, not a student's; and
	// for a coaching community only.
	put := func(as *store.Person, community int64, h http.HandlerFunc, on bool) int {
		h = s.requireCommunityPrivilege(store.PrivManageSettings, h)
		s.devAutoLogin = as // requireAuth's way in, for this call
		defer func() { s.devAutoLogin = nil }()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("PUT", "/api/communities/x/option", strings.NewReader(fmt.Sprintf(`{"on":%v}`, on)))
		r.SetPathValue("id", fmt.Sprint(community))
		h(w, r)
		return w.Code
	}
	for _, opt := range []struct {
		name string
		h    http.HandlerFunc
		get  func(store.PlayerOptions) bool
	}{
		{"vocal slider", s.handlePutCommunityVocalSlider, func(o store.PlayerOptions) bool { return o.VocalSlider }},
		{"key per student", s.handlePutCommunityCoachStudentKeys, func(o store.PlayerOptions) bool { return o.CoachStudentKeys }},
	} {
		if code := put(coach, c, opt.h, false); code != http.StatusOK {
			t.Fatalf("%s: the coach turned it off: %d", opt.name, code)
		}
		if code := put(poppy, c, opt.h, true); code != http.StatusForbidden {
			t.Fatalf("%s: a student turned it on: %d", opt.name, code)
		}
		if o, _ := st.CommunityPlayerOptions(ctx, c); opt.get(o) {
			t.Fatalf("%s: on after a student's try", opt.name)
		}
		if code := put(coach, c, opt.h, true); code != http.StatusOK {
			t.Fatalf("%s: the coach: %d", opt.name, code)
		}
		if o, _ := st.CommunityPlayerOptions(ctx, c); !opt.get(o) {
			t.Fatalf("%s: not on", opt.name)
		}
	}
	other, _ := st.CreateCommunity(ctx, "Jam "+sfx, coach.ID)
	if code := put(coach, other, s.handlePutCommunityCoachStudentKeys, true); code != http.StatusBadRequest {
		t.Fatalf("the key per student, on for a plain community: %d", code)
	}

	// Where the song was opened: the coaching community's songbooks (a
	// student's, its repertoire), its page, an event of its; not a band,
	// a set list, nowhere in particular, or a plain community.
	band, err := st.CreateBand(ctx, "Band "+sfx, coach.ID)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/song/x", nil).WithContext(context.WithValue(ctx, personCtxKey, poppy))
	for _, x := range []struct {
		what string
		data map[string]any
		want int64
	}{
		{"Poppy's songbook", map[string]any{"SongbookRef": sb}, c},
		{"the community's page", map[string]any{"CommunityRef": c}, c},
		{"an event of the community", map[string]any{"EventRef": ev}, c},
		{"a band", map[string]any{"BandRef": band}, 0},
		{"a band, with the songbook", map[string]any{"BandRef": band, "SongbookRef": sb}, 0},
		{"a set list", map[string]any{"SetlistRef": "1", "SongbookRef": sb}, 0},
		{"nowhere", map[string]any{}, 0},
		{"a plain community", map[string]any{"CommunityRef": other}, 0},
	} {
		if id := s.songCoachingContext(r, x.data); id != x.want {
			t.Errorf("the coaching context, from %s: %d (want %d)", x.what, id, x.want)
		}
	}
}
