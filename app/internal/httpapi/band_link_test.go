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
		id, err := st.CreatePerson(ctx, name, name+sfx+"@example.test")
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
		"slot_id": {"unlisted"}, "display_name": {"Newbie"},
		"email": {"newbie" + sfx + "@example.test"}, "password": {"long enough"}, "confirm": {"long enough"},
	}
	w = do("POST", core.URL, "application/x-www-form-urlencoded", form.Encode(), nil)
	wantDest := fmt.Sprintf("/welcome?next=%s", url.QueryEscape(fmt.Sprintf("/bands/%d", bandID)))
	if w.Code != http.StatusFound || w.Header().Get("Location") != wantDest {
		t.Fatalf("joining with a new account: %d %q %s", w.Code, w.Header().Get("Location"), w.Body)
	}
	newbie, err := st.GetPersonByEmail(ctx, "newbie"+sfx+"@example.test")
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
	form.Set("email", "late"+sfx+"@example.test")
	if w = do("POST", core.URL, "application/x-www-form-urlencoded", form.Encode(), nil); w.Code != http.StatusGone {
		t.Errorf("joining through a closed link: %d", w.Code)
	}
	if _, err := st.GetPersonByEmail(ctx, "late"+sfx+"@example.test"); err == nil {
		t.Errorf("a closed link still made an account")
	}
	if w = do("GET", core.URL, "", "", nil); !strings.Contains(w.Body.String(), "has been closed") {
		t.Errorf("closed link's page doesn't say so")
	}
	if fresh := getLink(bandID, "core", owner); fresh.ID == core.ID {
		t.Errorf("a closed link was handed out again")
	}
}

// inviteEnv is a test database and the app's handler, with helpers to
// make uniquely named people and to call the app as one of them.
type inviteEnv struct {
	t   *testing.T
	st  *store.Store
	h   http.Handler
	sfx string
}

func newInviteEnv(t *testing.T) *inviteEnv {
	st := newTestStore(t)
	return &inviteEnv{t: t, st: st, h: NewServer(st, nil, nil, "", ""), sfx: fmt.Sprint(time.Now().UnixNano())}
}

func (e *inviteEnv) person(name string) (int64, *http.Cookie) {
	e.t.Helper()
	ctx := context.Background()
	id, err := e.st.CreatePerson(ctx, name, name+e.sfx+"@example.test")
	if err != nil {
		e.t.Fatal(err)
	}
	tok, err := e.st.CreateSession(ctx, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return id, &http.Cookie{Name: sessionCookie, Value: tok}
}

func (e *inviteEnv) do(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	e.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if strings.HasPrefix(body, "{") {
		r.Header.Set("Content-Type", "application/json")
	} else if body != "" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

// TestBandInvitesAreCoreOnly: every way of inviting someone to a band,
// and closing a band link, is for core members; a guest gets 403 on
// each, and a core member gets through.
func TestBandInvitesAreCoreOnly(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	guestID, guest := e.person("Guest")
	someoneID, _ := e.person("Someone") // an account to add, one Core plays with
	bandID, err := e.st.CreateBand(ctx, "Core Band "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := e.st.CreateBand(ctx, "Elsewhere "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddBandMember(ctx, elsewhere, someoneID, "guest", coreID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddBandMember(ctx, bandID, guestID, "guest", coreID); err != nil {
		t.Fatal(err)
	}
	b := fmt.Sprintf("/api/bands/%d", bandID)

	// call makes the request as the guest (403 expected), then as the
	// core member (200 expected), and returns the core member's answer.
	call := func(name, method, path, body string) map[string]any {
		t.Helper()
		if w := e.do(method, path, body, guest); w.Code != http.StatusForbidden {
			t.Errorf("%s as a guest: %d %s", name, w.Code, w.Body)
		} else if !strings.Contains(w.Body.String(), "core members") {
			t.Errorf("%s as a guest: unclear 403 %q", name, w.Body)
		}
		w := e.do(method, path, body, core)
		if w.Code != http.StatusOK {
			t.Fatalf("%s as core: %d %s", name, w.Code, w.Body)
		}
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}

	call("add someone known", "POST", b+"/members", fmt.Sprintf(`{"personId":%d,"status":"guest"}`, someoneID))
	shell := call("personal invite", "POST", b+"/personal-invites", `{"displayName":"Placeholder","status":"guest"}`)
	shellID := int64(shell["personId"].(float64))
	call("new link for a placeholder", "POST", fmt.Sprintf("/api/people/%d/personal-invites", shellID), "")
	roster := call("roster link", "POST", b+"/group-invites", `{"slots":[{"displayName":"Ana"}]}`)
	gi := fmt.Sprintf("%s/group-invites/%d", b, int64(roster["id"].(float64)))
	slot := call("add a name", "POST", gi+"/slots", `{"displayName":"Bo"}`)
	sl := fmt.Sprintf("%s/slots/%d", gi, int64(slot["slotId"].(float64)))
	call("fix a name", "POST", sl, `{"displayName":"Bob"}`)
	call("remove a name", "DELETE", sl, "")
	call("close the roster link", "POST", gi+"/close", "")
	for _, status := range []string{"core", "guest"} {
		link := call(status+" band link", "POST", b+"/band-link", `{"status":"`+status+`"}`)
		call("close the "+status+" band link", "POST", fmt.Sprintf("%s/group-invites/%d/close", b, int64(link["id"].(float64))), "")
	}

	// The members page: a guest sees no way of inviting.
	page := e.do("GET", fmt.Sprintf("/bands/%d/members", bandID), "", guest).Body.String()
	for _, form := range []string{`id="roster-quick-add-form"`, `id="add-member-form"`, `id="band-link"`} {
		if strings.Contains(page, form) {
			t.Errorf("a guest's members page has %s", form)
		}
	}
	if !strings.Contains(page, "Your role doesn’t let you invite people.") {
		t.Errorf("a guest's members page doesn't say why")
	}
	page = e.do("GET", fmt.Sprintf("/bands/%d/members", bandID), "", core).Body.String()
	if !strings.Contains(page, `id="band-link"`) || !strings.Contains(page, `id="roster-quick-add-form"`) {
		t.Errorf("a core member's members page has no way of inviting")
	}
}

// TestCommunityOpenLinks: asking twice for a role's link gives the same
// link; the admin page lists the open links (role links and links with
// names) for staff who manage members, with who made them, who joined
// and who's still awaited; a new link for a community placeholder needs
// manage_members.
func TestCommunityOpenLinks(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	staffID, staff := e.person("Staffer")
	memberID, member := e.person("Plain")
	communityID, err := e.st.CreateCommunity(ctx, "Link Community "+e.sfx, staffID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddCommunityMember(ctx, communityID, memberID, "member", staffID); err != nil {
		t.Fatal(err)
	}
	c := fmt.Sprintf("/api/communities/%d", communityID)
	type link struct {
		ID  int64  `json:"id"`
		URL string `json:"url"`
	}
	get := func(path, body string) link {
		t.Helper()
		w := e.do("POST", path, body, staff)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		var l link
		_ = json.Unmarshal(w.Body.Bytes(), &l)
		return l
	}
	first := get(c+"/community-link", `{"role":"member"}`)
	if again := get(c+"/community-link", `{"role":"member"}`); again.ID != first.ID || first.ID == 0 {
		t.Errorf("asking twice for the member link: %+v then %+v", first, again)
	}
	if guestLink := get(c+"/community-link", `{"role":"guest"}`); guestLink.ID == first.ID {
		t.Errorf("the guest link is the member link")
	}
	named := get(c+"/group-invites", `{"slots":[{"displayName":"Awaited Name"}],"role":"member"}`)
	if again := get(c+"/community-link", `{"role":"member"}`); again.ID != first.ID {
		t.Errorf("a link with names was handed out as the member link")
	}

	// Someone joins through the member link.
	form := url.Values{
		"slot_id": {"unlisted"}, "display_name": {"Joiner Person"},
		"email": {"joiner" + e.sfx + "@example.test"}, "password": {"long enough"}, "confirm": {"long enough"},
	}
	if w := e.do("POST", first.URL, form.Encode(), nil); w.Code != http.StatusFound {
		t.Fatalf("joining: %d %s", w.Code, w.Body)
	}

	page := e.do("GET", fmt.Sprintf("/communities/%d/admin", communityID), "", staff).Body.String()
	for _, want := range []string{"Pending invites", "Open invite links", first.URL, named.URL, "Joined with it: Joiner Person", "Still waiting for: Awaited Name", "made by Staffer ·", "Link with names", "Open link"} {
		if !strings.Contains(page, want) {
			t.Errorf("admin page: missing %q", want)
		}
	}
	if w := e.do("POST", fmt.Sprintf("%s/group-invites/%d/close", c, named.ID), "", staff); w.Code != http.StatusOK {
		t.Fatalf("closing: %d", w.Code)
	}
	if page = e.do("GET", fmt.Sprintf("/communities/%d/admin", communityID), "", staff).Body.String(); strings.Contains(page, named.URL) {
		t.Errorf("a closed link is still listed")
	}

	// A community placeholder's new link: manage_members, not just any member.
	w := e.do("POST", c+"/personal-invites", `{"displayName":"Waiting One","role":"member"}`, staff)
	var shell struct {
		PersonID int64 `json:"personId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &shell); err != nil || shell.PersonID == 0 {
		t.Fatalf("community personal invite: %d %s", w.Code, w.Body)
	}
	p := fmt.Sprintf("/api/people/%d/personal-invites", shell.PersonID)
	if w := e.do("POST", p, "", member); w.Code != http.StatusForbidden {
		t.Errorf("a plain member made a new link for a placeholder: %d", w.Code)
	}
	if w := e.do("POST", p, "", staff); w.Code != http.StatusOK {
		t.Errorf("staff couldn't make a new link for a placeholder: %d %s", w.Code, w.Body)
	}

	// The placeholder is a row under "Invited by name": their name, their
	// role, who invited them, Copy link and QR code; never an internal
	// placeholder name, nor the link spelled out (the buttons carry it).
	page = e.do("GET", fmt.Sprintf("/communities/%d/admin", communityID), "", staff).Body.String()
	for _, want := range []string{"Invited by name", fmt.Sprintf(`class="roster-row invite-row" data-person-id="%d"`, shell.PersonID), ">Waiting One</strong>", "invited by Staffer ·", "link expires", "copy-link-btn", "qr-link-btn"} {
		if !strings.Contains(page, want) {
			t.Errorf("admin page: missing %q", want)
		}
	}
	for _, unwanted := range []string{">pending-", "(pending-", ">/invite/", ">/join/"} {
		if strings.Contains(page, unwanted) {
			t.Errorf("admin page: shows %q", unwanted)
		}
	}
}
