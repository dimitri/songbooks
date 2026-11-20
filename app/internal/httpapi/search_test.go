package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app/internal/store"
)

// TestSearchEndpoints: /api/search and /search find only what the
// viewer may see, the current space's first; the commands follow the
// viewer's roles; /new/… lands in the right space.
func TestSearchEndpoints(t *testing.T) {
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
	me := person("me", "Mia Me")
	other := person("other", "Otto Other")
	coach := person("coach", "Cora Coach")

	mine, err := st.CreateBand(ctx, "Velvet Hour", me)
	must(err)
	theirs, err := st.CreateBand(ctx, "Velvet Underground", other)
	must(err)
	_, err = st.CreateVariant(ctx, store.OwnerCatalog, 0, "Velvet Alpha", "", me)
	must(err)
	bandSong, err := st.CreateVariant(ctx, store.OwnerBand, mine, "Velvet Zulu", "", me)
	must(err)
	_, err = st.CreateVariant(ctx, store.OwnerBand, theirs, "Velvet Secret", "", other)
	must(err)
	_, err = st.CreateVariant(ctx, store.OwnerPerson, other, "Velvet Diary", "", other)
	must(err)
	studio, err := st.CreateCommunity(ctx, "Studio", coach)
	must(err)
	must(st.SetCommunityKind(ctx, studio, "coaching"))
	_, err = st.AddCommunityMember(ctx, studio, me, "member", coach)
	must(err)
	vera := person("vera", "Vera Velvet")
	_, err = st.AddCommunityMember(ctx, studio, vera, "member", coach)
	must(err)
	_, err = st.CreateEvent(ctx, "community", studio, "lesson", "Velvet Lesson", "", "", time.Now().Add(24*time.Hour), nil, "members", coach)
	must(err)

	tokens := map[int64]string{}
	for _, p := range []int64{me, coach} {
		tok, err := st.CreateSession(ctx, p)
		must(err)
		tokens[p] = tok
	}
	do := func(who int64, path string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		if who != 0 {
			r.AddCookie(&http.Cookie{Name: sessionCookie, Value: tokens[who]})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	type result struct {
		Commands []searchItem  `json:"commands"`
		Groups   []searchGroup `json:"groups"`
	}
	search := func(who int64, q, space string) (result, string) {
		t.Helper()
		w := do(who, "/api/search?q="+q+"&space="+space)
		if w.Code != http.StatusOK {
			t.Fatalf("/api/search: %d %s", w.Code, w.Body.String())
		}
		var res result
		must(json.Unmarshal(w.Body.Bytes(), &res))
		return res, w.Body.String()
	}
	hasCommand := func(res result, url string) bool {
		for _, c := range res.Commands {
			if c.URL == url {
				return true
			}
		}
		return false
	}

	if w := do(0, "/api/search?q=velvet"); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", w.Code)
	}

	res, body := search(me, "velvet", "")
	for _, want := range []string{"Velvet Alpha", "Velvet Zulu", "Velvet Hour"} {
		if !strings.Contains(body, want) {
			t.Errorf("me: no %q in %s", want, body)
		}
	}
	for _, nope := range []string{"Velvet Secret", "Velvet Diary", "Velvet Underground", "Velvet Lesson", "Vera Velvet"} {
		if strings.Contains(body, nope) {
			t.Errorf("me: %q found", nope)
		}
	}
	// Nothing typed: every command the viewer has.
	res, _ = search(me, "", "")
	if hasCommand(res, "/new/lesson") || !hasCommand(res, "/new/setlist") || !hasCommand(res, "/tuner") {
		t.Errorf("me: commands %+v", res.Commands)
	}

	// From the band's pages: its song first, marked as here.
	res, _ = search(me, "velvet", fmt.Sprintf("band:%d", mine))
	if len(res.Groups) == 0 || res.Groups[0].Kind != "song" || res.Groups[0].Items[0].URL != fmt.Sprintf("/song/%d?band=%d", bandSong, mine) || !res.Groups[0].Items[0].Here {
		t.Errorf("band space: %+v", res.Groups)
	}

	// The coach: their student, the lesson, and "New lesson".
	res, body = search(coach, "velvet", "")
	for _, want := range []string{"Vera Velvet", "Velvet Lesson"} {
		if !strings.Contains(body, want) {
			t.Errorf("coach: no %q in %s", want, body)
		}
	}
	if strings.Contains(body, "Velvet Hour") {
		t.Errorf("coach: %s", body)
	}
	if res, _ = search(coach, "", ""); !hasCommand(res, "/new/lesson") {
		t.Errorf("coach: commands %+v", res.Commands)
	}
	// Commands match by name, accents aside.
	res, _ = search(coach, "metro", "")
	if len(res.Commands) != 1 || res.Commands[0].URL != "/metronome" {
		t.Errorf("metro: %+v", res.Commands)
	}

	// /new/…: the right place, or nothing.
	if w := do(coach, "/new/lesson"); w.Code != http.StatusFound || w.Header().Get("Location") != fmt.Sprintf("/communities/%d/lessons#new", studio) {
		t.Errorf("coach /new/lesson: %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := do(me, "/new/lesson"); w.Code != http.StatusNotFound {
		t.Errorf("me /new/lesson: %d", w.Code)
	}
	if w := do(me, fmt.Sprintf("/new/setlist?space=band:%d", mine)); w.Header().Get("Location") != fmt.Sprintf("/setlists#new-band-%d", mine) {
		t.Errorf("me /new/setlist in my band: %s", w.Header().Get("Location"))
	}
	if w := do(me, fmt.Sprintf("/new/setlist?space=band:%d", theirs)); w.Header().Get("Location") != "/setlists#new" {
		t.Errorf("me /new/setlist in their band: %s", w.Header().Get("Location"))
	}

	// The page: the same finds.
	w := do(me, "/search?q=velvet")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Velvet Zulu") || strings.Contains(w.Body.String(), "Velvet Secret") {
		t.Errorf("/search: %d", w.Code)
	}
}
