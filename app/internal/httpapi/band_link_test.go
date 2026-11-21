package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/store"
)

// newTestStore opens the Postgres test database, same as the store
// package's own tests (SONGBOOKS_TEST_DATABASE_URL, else a local
// songbooks_test), and skips when there's none. Its tables live in
// their own schema, httpapi_test: the store tests empty the public one
// while `go test ./...` runs this package alongside. Nothing is emptied
// here: callers make their own uniquely named people and bands.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	connString := os.Getenv("SONGBOOKS_TEST_DATABASE_URL")
	if connString == "" {
		connString = "postgres://localhost/songbooks_test?sslmode=disable"
	}
	conn, err := pgx.Connect(ctx, connString)
	if err != nil {
		t.Skipf("no test database available at %s (set SONGBOOKS_TEST_DATABASE_URL to run these tests): %v", connString, err)
	}
	_, err = conn.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS httpapi_test`)
	conn.Close(ctx)
	if err != nil {
		t.Fatalf("creating the httpapi_test schema: %v", err)
	}
	switch {
	case !strings.Contains(connString, "://"):
		connString += " search_path=httpapi_test"
	case strings.Contains(connString, "?"):
		connString += "&search_path=httpapi_test"
	default:
		connString += "?search_path=httpapi_test"
	}
	st, err := store.New(ctx, connString)
	if err != nil {
		t.Fatalf("opening the test database: %v", err)
	}
	t.Cleanup(st.Close)
	return st
}

// TestBandLink: a band member makes the band's open link (one per
// status), a stranger opens it with no names to pick, creates an account
// and is in the band with the link's status; someone already signed in
// joins the same way; once closed, the link lets nobody else in.
func TestBandLink(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	h := NewServer(st, nil, nil, "", "")
	sfx := fmt.Sprint(time.Now().UnixNano())

	person := func(name string) (int64, *http.Cookie) {
		t.Helper()
		id, err := st.CreatePerson(ctx, name+sfx, name, name+sfx+"@example.test")
		if err != nil {
			t.Fatal(err)
		}
		tok, err := st.CreateSession(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return id, &http.Cookie{Name: sessionCookie, Value: tok}
	}
	do := func(method, path, ctype, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if ctype != "" {
			r.Header.Set("Content-Type", ctype)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	type link struct {
		ID     int64  `json:"id"`
		URL    string `json:"url"`
		Status string `json:"status"`
	}
	getLink := func(bandID int64, status string, cookie *http.Cookie) link {
		t.Helper()
		w := do("POST", fmt.Sprintf("/api/bands/%d/band-link", bandID), "application/json", `{"status":"`+status+`"}`, cookie)
		if w.Code != http.StatusOK {
			t.Fatalf("band-link %s: %d %s", status, w.Code, w.Body)
		}
		var l link
		if err := json.Unmarshal(w.Body.Bytes(), &l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	statusIn := func(bandID, personID int64) string {
		t.Helper()
		members, err := st.ListBandMembers(ctx, bandID, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range members {
			if m.PersonID == personID {
				return m.Status
			}
		}
		return ""
	}

	ownerID, owner := person("Owner")
	bandID, err := st.CreateBand(ctx, "Link Band "+sfx, ownerID)
	if err != nil {
		t.Fatal(err)
	}

	core := getLink(bandID, "core", owner)
	if core.Status != "core" || !strings.HasPrefix(core.URL, "/join/") {
		t.Fatalf("core link: %+v", core)
	}
	if again := getLink(bandID, "core", owner); again.ID != core.ID || again.URL != core.URL {
		t.Errorf("asking twice for a core link made a second one: %+v then %+v", core, again)
	}
	guest := getLink(bandID, "guest", owner)
	if guest.ID == core.ID {
		t.Errorf("the guest link is the core link")
	}
	if w := do("POST", fmt.Sprintf("/api/bands/%d/band-link", bandID), "application/json", `{"status":"boss"}`, owner); w.Code != http.StatusBadRequest {
		t.Errorf("status boss: %d", w.Code)
	}
	_, outsider := person("Outsider")
	if w := do("POST", fmt.Sprintf("/api/bands/%d/band-link", bandID), "application/json", `{"status":"guest"}`, outsider); w.Code != http.StatusForbidden {
		t.Errorf("a non-member made a band link: %d", w.Code)
	}

	// The open link's page: no roster to pick a name from.
	w := do("GET", core.URL, "", "", nil)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `type="radio"`) || !strings.Contains(w.Body.String(), "you’re in the band") {
		t.Fatalf("join page: %d\n%s", w.Code, w.Body)
	}

	// A stranger creates an account through it.
	form := url.Values{
		"slot_id": {"unlisted"}, "username": {"newbie" + sfx}, "display_name": {"Newbie"},
		"email": {"newbie" + sfx + "@example.test"}, "password": {"long enough"}, "confirm": {"long enough"},
	}
	w = do("POST", core.URL, "application/x-www-form-urlencoded", form.Encode(), nil)
	wantDest := fmt.Sprintf("/welcome?next=%s", url.QueryEscape(fmt.Sprintf("/bands/%d", bandID)))
	if w.Code != http.StatusFound || w.Header().Get("Location") != wantDest {
		t.Fatalf("joining with a new account: %d %q %s", w.Code, w.Header().Get("Location"), w.Body)
	}
	newbie, err := st.GetPersonByUsername(ctx, "newbie"+sfx)
	if err != nil {
		t.Fatal(err)
	}
	if got := statusIn(bandID, newbie.ID); got != "core" {
		t.Errorf("joined through the core link as %q", got)
	}

	// Someone already signed in joins through the guest link.
	friendID, friend := person("Friend")
	w = do("POST", guest.URL, "application/x-www-form-urlencoded", "slot_id=unlisted", friend)
	if w.Code != http.StatusFound || w.Header().Get("Location") != fmt.Sprintf("/bands/%d", bandID) {
		t.Fatalf("joining signed in: %d %q %s", w.Code, w.Header().Get("Location"), w.Body)
	}
	if got := statusIn(bandID, friendID); got != "guest" {
		t.Errorf("joined through the guest link as %q", got)
	}

	// The members page lists both links and who joined with each.
	w = do("GET", fmt.Sprintf("/bands/%d/members", bandID), "", "", owner)
	page := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(page, core.URL) || !strings.Contains(page, guest.URL) {
		t.Fatalf("members page: %d, links missing", w.Code)
	}
	if !strings.Contains(page, "Joined with it: Newbie") || !strings.Contains(page, "Joined with it: Friend") {
		t.Errorf("members page doesn't say who joined with each link")
	}

	// Closed: nobody else gets in, and the next core link is a new one.
	w = do("POST", fmt.Sprintf("/api/bands/%d/group-invites/%d/close", bandID, core.ID), "", "", owner)
	if w.Code != http.StatusOK {
		t.Fatalf("closing: %d %s", w.Code, w.Body)
	}
	form.Set("username", "late"+sfx)
	form.Set("email", "late"+sfx+"@example.test")
	if w = do("POST", core.URL, "application/x-www-form-urlencoded", form.Encode(), nil); w.Code != http.StatusGone {
		t.Errorf("joining through a closed link: %d", w.Code)
	}
	if _, err := st.GetPersonByUsername(ctx, "late"+sfx); err == nil {
		t.Errorf("a closed link still made an account")
	}
	if w = do("GET", core.URL, "", "", nil); !strings.Contains(w.Body.String(), "has been closed") {
		t.Errorf("closed link's page doesn't say so")
	}
	if fresh := getLink(bandID, "core", owner); fresh.ID == core.ID {
		t.Errorf("a closed link was handed out again")
	}
}
