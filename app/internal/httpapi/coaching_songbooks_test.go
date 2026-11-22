package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app/internal/store"
)

func TestKeyPill(t *testing.T) {
	for _, c := range []struct {
		key  string
		n    int
		want string
	}{
		{"D", -3, "B (−3)"},
		{"Am", 2, "Bm (+2)"},
		{"", -2, "−2"},
		{"D", 0, ""},
	} {
		if got := keyPill(c.key, c.n); got != c.want {
			t.Errorf("keyPill(%q, %d) = %q, want %q", c.key, c.n, got, c.want)
		}
	}
}

// TestSortBookCards: the coach's cards by the next lesson, the soonest
// first; then those with none, students before groups, by name.
func TestSortBookCards(t *testing.T) {
	at := func(h int) *time.Time {
		x := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC).Add(time.Duration(h) * time.Hour)
		return &x
	}
	cards := []coachBookCard{
		{Kind: "group", Name: "Mardi"},
		{Kind: "student", Name: "Zoé"},
		{Kind: "student", Name: "Ada", Next: at(30)},
		{Kind: "group", Name: "Jeudi", Next: at(10)},
		{Kind: "student", Name: "Émile"},
	}
	sortBookCards(cards)
	var got []string
	for _, c := range cards {
		got = append(got, c.Name)
	}
	if strings.Join(got, ",") != "Jeudi,Ada,Émile,Zoé,Mardi" {
		t.Fatalf("order: %v", got)
	}
}

// TestTransposedBody: a song for a PDF in a singer's key, its chords and
// its key: header moved; as it is when its key can't be told.
func TestTransposedBody(t *testing.T) {
	got := transposedBody("title: Song\nkey: D\n\n[V]\n[D]la la [A]la\n", -3)
	if !strings.Contains(got, "key: B") || !strings.Contains(got, "[B]la la [F#]la") {
		t.Fatalf("transposed: %q", got)
	}
	if got := transposedBody("title: Words\n\nla la\n", 2); got != "title: Words\n\nla la\n" {
		t.Fatalf("no chords: %q", got)
	}
}

// TestCoachingSongbooks: the Songbooks section of a coaching community,
// both views, and "Ask for it": the student sees their songbook with each
// song's key and the repertoire; asks for a song for themselves only, of
// the repertoire only; the coach is told, sees the request and the
// students' cards (the soonest lesson first), adds it in a key; another
// student sees and does none of it.
func TestCoachingSongbooks(t *testing.T) {
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
	coach := person("coachuser", "Cora Coach")
	stan := person("stanuser", "Stan Student")
	olga := person("olgauser", "Olga Other")
	c, err := st.CreateCommunity(ctx, "Voice", coach)
	must(err)
	must(st.SetCommunityKind(ctx, c, "coaching"))
	for _, p := range []int64{stan, olga} {
		_, err := st.AddCommunityMember(ctx, c, p, "member", coach)
		must(err)
	}
	song := func(title, body string) int64 {
		t.Helper()
		id, err := st.CreateVariant(ctx, store.OwnerCommunity, c, title, "", coach)
		must(err)
		if body != "" {
			_, err = st.SaveVariantRaw(id, "title: "+title+"\n"+body, "", coach)
			must(err)
		}
		return id
	}
	alpha := song("Alpha", "key: D\n\n[D]la [A]la\n")
	bravo := song("Bravo", "key: G\n\n[G]la\n")
	charlie := song("Charlie", "")
	outside, err := st.CreateVariant(ctx, store.OwnerPerson, coach, "Outside", "", coach)
	must(err)
	rep, err := st.GetDefaultSongbook(ctx, "community", c)
	must(err)
	for _, v := range []int64{alpha, bravo, charlie} {
		must(st.AddToSongbook(ctx, rep, v, coach))
	}
	stanBook, err := st.StudentSongbook(ctx, c, stan, "Stan’s songs", coach)
	must(err)
	must(st.AddToSongbook(ctx, stanBook, alpha, coach))
	must(st.SetEntrySemitones(ctx, stanBook, alpha, -3))
	must(st.AddToSongbook(ctx, stanBook, charlie, coach))
	olgaBook, err := st.StudentSongbook(ctx, c, olga, "Olga’s songs", coach)
	must(err)
	must(st.AddToSongbook(ctx, olgaBook, bravo, coach))
	must(st.SetEntrySemitones(ctx, olgaBook, bravo, 4))
	// Olga's lesson is tomorrow, Stan has none: Olga's card comes first.
	soon := time.Now().Add(24 * time.Hour)
	end := soon.Add(time.Hour)
	ev, err := st.CreateEvent(ctx, "community", c, "lesson", "Olga", "", "", soon, &end, "members", coach)
	must(err)
	must(st.RegisterForEvent(ctx, ev, olga, 0))

	tokens := map[int64]string{}
	for _, p := range []int64{coach, stan, olga} {
		tok, err := st.CreateSession(ctx, p)
		must(err)
		tokens[p] = tok
	}
	do := func(who int64, method, path, body string) (int, string) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		r.Header.Set("Accept-Language", "en")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: tokens[who]})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	page := fmt.Sprintf("/communities/%d/songbooks", c)
	asks := fmt.Sprintf("/api/communities/%d/song-requests", c)
	notes := func(p int64, kind string) []store.Notification {
		t.Helper()
		all, err := st.ListNotifications(ctx, p, false)
		must(err)
		var out []store.Notification
		for _, n := range all {
			if n.Kind == kind {
				out = append(out, n)
			}
		}
		return out
	}

	// The student: their songbook first, each song's key; the repertoire,
	// what's theirs already and what to ask for; nobody else's.
	code, body := do(stan, "GET", page, "")
	if code != http.StatusOK {
		t.Fatalf("student's page: %d", code)
	}
	for _, want := range []string{"Your songbook with Cora Coach", "Stan’s songs", "B (−3)", `<span class="cbook-key">original</span>`,
		"Cora’s repertoire", "in your songbook", `class="cbook-ask"`, "export-songbook.pdf?keys=songbook"} {
		if !strings.Contains(body, want) {
			t.Errorf("student's page: no %q", want)
		}
	}
	for _, not := range []string{"Olga’s songs", "B (&#43;4)", `class="cbook-card`, `class="cbook-request`} {
		if strings.Contains(body, not) {
			t.Errorf("student's page shows %q", not)
		}
	}
	if strings.Index(body, "cbook-mine") > strings.Index(body, "cbook-rep-list") {
		t.Error("the repertoire comes before the student's songbook")
	}

	// Asking: a song of the repertoire, for themselves; once while it waits.
	if code, _ := do(stan, "POST", asks, fmt.Sprintf(`{"variantId":%d}`, bravo)); code != http.StatusOK {
		t.Fatalf("ask: %d", code)
	}
	if code, _ := do(stan, "POST", asks, fmt.Sprintf(`{"variantId":%d}`, bravo)); code != http.StatusOK {
		t.Fatalf("ask again: %d", code)
	}
	told := notes(coach, "song_request")
	if len(told) != 1 || !strings.Contains(told[0].Body, "Stan Student") || !strings.Contains(told[0].Body, "Bravo") {
		t.Fatalf("the coach's notifications: %+v", told)
	}
	if code, _ := do(stan, "POST", asks, fmt.Sprintf(`{"variantId":%d}`, outside)); code != http.StatusNotFound {
		t.Errorf("asking for a song outside the repertoire: %d", code)
	}
	if code, _ := do(stan, "POST", asks, fmt.Sprintf(`{"variantId":%d}`, alpha)); code != http.StatusConflict {
		t.Errorf("asking for a song already theirs: %d", code)
	}
	if code, _ := do(coach, "POST", asks, fmt.Sprintf(`{"variantId":%d}`, bravo)); code != http.StatusForbidden {
		t.Errorf("the coach asking: %d", code)
	}
	if _, body := do(stan, "GET", page, ""); !strings.Contains(body, `cbook-state is-asked`) {
		t.Error("the row doesn't say Asked")
	}
	pending, err := st.PendingSongRequests(ctx, c)
	must(err)
	if len(pending) != 1 || pending[0].PersonID != stan {
		t.Fatalf("pending: %+v", pending)
	}
	req := pending[0].ID
	if !strings.Contains(told[0].Link, fmt.Sprintf("request=%d", req)) {
		t.Errorf("the notification's link: %q", told[0].Link)
	}

	// Another student: sees nothing of it, acts on none of it.
	_, body = do(olga, "GET", page, "")
	if strings.Contains(body, "Stan") || strings.Contains(body, "B (−3)") || !strings.Contains(body, "B (&#43;4)") {
		t.Error("another student's page shows the first one's songbook, or not their own")
	}
	add := fmt.Sprintf("/api/communities/%d/song-requests/%d/add", c, req)
	decline := fmt.Sprintf("/api/communities/%d/song-requests/%d/decline", c, req)
	for _, who := range []int64{olga, stan} {
		if code, _ := do(who, "POST", add, `{"semitones":2}`); code != http.StatusForbidden {
			t.Errorf("a student adding: %d", code)
		}
		if code, _ := do(who, "POST", decline, ""); code != http.StatusForbidden {
			t.Errorf("a student declining: %d", code)
		}
	}

	// The coach: the request, a card per student, Olga's (a lesson
	// tomorrow) before Stan's (none).
	code, body = do(coach, "GET", page+fmt.Sprintf("?request=%d", req), "")
	if code != http.StatusOK {
		t.Fatalf("coach's page: %d", code)
	}
	for _, want := range []string{fmt.Sprintf(`id="request-%d"`, req), "is-open", "Your students’ songbooks", "cbook-rep-coach", `class="cbook-add-to"`} {
		if !strings.Contains(body, want) {
			t.Errorf("coach's page: no %q", want)
		}
	}
	o, s := strings.Index(body, `data-name="Olga Other"`), strings.Index(body, `data-name="Stan Student"`)
	if o < 0 || s < 0 || o > s {
		t.Errorf("cards: Olga at %d, Stan at %d", o, s)
	}
	if strings.Contains(body, `class="cbook-ask"`) {
		t.Error("the coach is offered Ask for it")
	}

	// Added in a key: in Stan's songbook, the request answered, the
	// coach's notification read, Stan told.
	if code, b := do(coach, "POST", add, `{"semitones":2}`); code != http.StatusOK {
		t.Fatalf("add: %d %s", code, b)
	}
	if n, _ := st.EntrySemitones(ctx, stanBook, bravo); n != 2 {
		t.Errorf("Bravo in Stan's songbook: %d", n)
	}
	if q, _ := st.GetSongRequest(ctx, req); q.Status != store.SongRequestAdded {
		t.Errorf("request: %q", q.Status)
	}
	if told := notes(coach, "song_request"); len(told) != 1 || !told[0].IsRead() {
		t.Errorf("the coach's notification isn't read: %+v", told)
	}
	if got := notes(stan, "song_request_added"); len(got) != 1 || !strings.Contains(got[0].Body, "Bravo") {
		t.Errorf("Stan's notification: %+v", got)
	}
	if code, _ := do(coach, "POST", add, `{"semitones":2}`); code != http.StatusConflict {
		t.Errorf("adding twice: %d", code)
	}
	if _, body := do(stan, "GET", page, ""); !strings.Contains(body, "A (&#43;2)") {
		t.Error("the student's page doesn't show the song added in their key")
	}
	if n, _ := st.EntrySemitones(ctx, olgaBook, bravo); n != 4 {
		t.Errorf("Olga's key moved: %d", n)
	}

	// The PDF in the student's keys is theirs only.
	pdf := fmt.Sprintf("/songbooks/%d/export-songbook.pdf?keys=songbook", stanBook)
	if code, _ := do(olga, "GET", pdf, ""); code != http.StatusNotFound {
		t.Errorf("another student's PDF: %d", code)
	}
}
