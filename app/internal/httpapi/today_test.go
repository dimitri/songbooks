package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app/internal/setlist"
	"app/internal/store"
)

func TestTodayDay(t *testing.T) {
	loc := time.FixedZone("Paris", 3600)
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, loc) // a Friday
	cases := []struct {
		at     time.Time
		allDay bool
		en, fr string
	}{
		{time.Date(2026, 11, 20, 20, 30, 0, 0, loc), false, "tonight", "ce soir"},
		{time.Date(2026, 11, 20, 12, 0, 0, 0, loc), false, "today", "aujourd’hui"},
		{time.Date(2026, 11, 20, 0, 0, 0, 0, loc), true, "today", "aujourd’hui"},
		{time.Date(2026, 11, 21, 10, 0, 0, 0, loc), false, "tomorrow", "demain"},
		{time.Date(2026, 11, 24, 18, 0, 0, 0, loc), false, "Tuesday", "mardi"},
		{time.Date(2026, 12, 5, 9, 0, 0, 0, loc), false, "Sat 5 Dec", "sam. 5 déc."},
	}
	for _, c := range cases {
		if got := todayDay("en", c.at, now, c.allDay); got != c.en {
			t.Errorf("en %v: got %q, want %q", c.at, got, c.en)
		}
		if got := todayDay("fr", c.at, now, c.allDay); got != c.fr {
			t.Errorf("fr %v: got %q, want %q", c.at, got, c.fr)
		}
	}
	if got := todayWhen("fr", time.Date(2026, 11, 21, 10, 0, 0, 0, loc), now); got != "demain 10:00" {
		t.Errorf("todayWhen: %q", got)
	}
	if got := endOfWeek(now); got.Weekday() != time.Sunday || got.Day() != 22 {
		t.Errorf("endOfWeek: %v", got)
	}
	sunday := time.Date(2026, 11, 22, 8, 0, 0, 0, loc)
	if got := endOfWeek(sunday); got.Day() != 22 {
		t.Errorf("endOfWeek on a Sunday: %v", got)
	}
}

func TestSortTodayCards(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC) // a Friday
	cards := []todayCard{
		{Kind: "student", At: endOfWeek(now)},
		{Kind: "coach", At: now.Add(25 * time.Hour)},
		{Kind: "jam", At: now.Add(11 * time.Hour)},
		{Kind: "band", At: now.Add(11 * time.Hour)},
	}
	sortTodayCards(cards)
	var got []string
	for _, c := range cards {
		got = append(got, c.Kind)
	}
	if want := "band jam coach student"; strings.Join(got, " ") != want {
		t.Fatalf("order: %v, want %s", got, want)
	}
}

// TestTodayCards: Accueil's first cards, for each role, from real data:
// each person sees their own and nothing of anyone else's.
func TestTodayCards(t *testing.T) {
	st, h := newTestServer(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	person := func(username, name string) int64 {
		t.Helper()
		id, err := st.CreatePerson(ctx, username, name, username+"@example.com")
		must(err)
		return id
	}
	coach := person("coach", "Cora Coach")
	stan := person("stan", "Stan Student")
	quinn := person("quinn", "Quinn Quiet")
	org := person("org", "Olga Organiser")
	jo := person("jo", "Jo Jammer")
	bea := person("bea", "Bea Bassist")
	outsider := person("outsider", "Otto Outsider")

	// A coaching studio: Stan has a lesson tomorrow and something to
	// practise, done once this week; Quinn hasn't been heard from.
	voice, err := st.CreateCommunity(ctx, "Studio Voix", coach)
	must(err)
	must(st.SetCommunityKind(ctx, voice, "coaching"))
	for _, p := range []int64{stan, quinn} {
		_, err = st.AddCommunityMember(ctx, voice, p, "member", coach)
		must(err)
	}
	_, err = st.CreateEvent(ctx, "community", voice, "lesson", "Not Stan's", "", "", time.Now().AddDate(0, 0, -40), nil, "members", coach)
	must(err)
	tomorrow := time.Now().AddDate(0, 0, 1)
	at := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 10, 0, 0, 0, time.Local)
	end := at.Add(time.Hour)
	lesson, err := st.CreateEvent(ctx, "community", voice, "lesson", "Lesson · Stan", "", "Studio", at, &end, "members", coach)
	must(err)
	must(st.RegisterForEvent(ctx, lesson, stan, 0))
	a, err := st.CreatePracticeAssignment(ctx, store.NewPracticeAssignment{CommunityID: voice, PersonID: stan, Title: "Lip trills", PerWeek: 5, By: coach})
	must(err)
	_, err = st.LogPractice(ctx, voice, stan, a, time.Now(), 300, "")
	must(err)

	// A jam community: Jo is a member, the jam is in two days.
	jamC, err := st.CreateCommunity(ctx, "Jam du Canal", org)
	must(err)
	_, err = st.AddCommunityMember(ctx, jamC, jo, "member", org)
	must(err)
	jam, err := st.CreateEvent(ctx, "community", jamC, "jam", "Friday jam", "", "Le Quai", time.Now().AddDate(0, 0, 2), nil, "members", org)
	must(err)

	// A band with a gig today, its set list two songs long.
	band, err := st.CreateBand(ctx, "Lanterns", bea)
	must(err)
	sl, err := st.CreateSetlist(ctx, "band", "Gig set", "", nil, &band, nil, bea)
	must(err)
	var entries []setlist.Entry
	for _, title := range []string{"One", "Two"} {
		v, err := st.CreateVariant(ctx, store.OwnerBand, band, title, "", bea)
		must(err)
		entries = append(entries, setlist.Entry{VariantID: v})
	}
	_, err = st.SaveSetlistByID(ctx, sl, &setlist.Setlist{Sets: []setlist.Set{{Entries: entries}}}, "Gig set", "", "", bea)
	must(err)
	today := time.Now()
	_, err = st.AddSetlistGig(ctx, sl, &today, "La Péniche", "")
	must(err)

	tokens := map[int64]string{}
	for _, p := range []int64{coach, stan, quinn, org, jo, bea, outsider} {
		tok, err := st.CreateSession(ctx, p)
		must(err)
		tokens[p] = tok
	}
	do := func(who int64, method, path string) (int, string) {
		t.Helper()
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Accept-Language", "en")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: tokens[who]})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	home := func(who int64) string {
		t.Helper()
		code, body := do(who, "GET", "/home")
		if code != http.StatusOK {
			t.Fatalf("GET /home: %d", code)
		}
		return body
	}
	has := func(name, body string, want ...string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("%s: no %q", name, w)
			}
		}
	}
	hasNot := func(name, body string, nope ...string) {
		t.Helper()
		for _, n := range nope {
			if strings.Contains(body, n) {
				t.Errorf("%s: %q shouldn't be there", name, n)
			}
		}
	}

	body := home(stan)
	has("student", body, "today-card kind-student", "Lip trills", "1/5 this week", "lesson ", "Studio Voix",
		fmt.Sprintf(`href="/communities/%d/students/%d#practice"`, voice, stan))
	hasNot("student", body, "kind-coach", "kind-jam", "kind-band", "not heard from")

	body = home(coach)
	has("coach", body, "today-card kind-coach", "Stan, ", "Prepare",
		fmt.Sprintf(`href="/communities/%d/students/%d"`, voice, stan))
	// Quinn joined today: not "not heard from" yet (store.QuietStudents'
	// own test covers the count).
	hasNot("coach", body, "kind-student", "Lip trills", "not heard from")

	body = home(quinn)
	hasNot("quiet student", body, "today-card")

	body = home(jo)
	has("jam", body, "today-card kind-jam", "Friday jam", fmt.Sprintf(`action="/events/%d/register"`, jam))
	if code, _ := do(jo, "POST", fmt.Sprintf("/events/%d/register", jam)); code != http.StatusFound {
		t.Fatalf("register: %d", code)
	}
	hasNot("jam, registered", home(jo), "today-card")
	hasNot("jam organiser", home(org), "today-card")

	body = home(bea)
	has("band", body, "today-card kind-band", "Gig set", "2 songs", "La Péniche", fmt.Sprintf(`href="/setlists/%d"`, sl), "Open the set list")

	hasNot("outsider", home(outsider), "today-card")
}
