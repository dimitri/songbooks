package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"app/internal/store"
)

// Invites are by name, joining is by email (docs/accounts.md): someone
// added by name gets a personal link, and whoever follows it — or any
// other link — joins with their email, as a new account or by logging in
// to the one they have; someone signed in just joins.

// doCookies is inviteEnv.do with any number of cookies.
func (e *inviteEnv) doCookies(method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	e.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

// cookieNamed is the cookie a response sets under name, nil without one.
func cookieNamed(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// addByName adds a placeholder by name to a band (with an instrument) or
// a community, as core, and returns their id and personal link's token.
func (e *inviteEnv) addByName(kind string, groupID int64, name string, core *http.Cookie) (int64, string) {
	e.t.Helper()
	body := fmt.Sprintf(`{"displayName":%q,"instrument":"drums","status":"guest"}`, name)
	if kind == "community" {
		body = fmt.Sprintf(`{"displayName":%q,"role":"member"}`, name)
	}
	plural := map[string]string{"band": "bands", "community": "communities"}[kind]
	w := e.do("POST", fmt.Sprintf("/api/%s/%d/personal-invites", plural, groupID), body, core)
	if w.Code != http.StatusOK {
		e.t.Fatalf("adding %s by name: %d %s", name, w.Code, w.Body)
	}
	var res struct {
		PersonID int64  `json:"personId"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		e.t.Fatal(err)
	}
	return res.PersonID, strings.TrimPrefix(res.URL, "/invite/")
}

// logInToJoin follows a page's "Log in to join" (path), logs in with
// email and password, and lands on /home, as the login page does; it
// returns where /home sends them.
func (e *inviteEnv) logInToJoin(path, email, password string) string {
	e.t.Helper()
	w := e.do("GET", path, "", nil)
	pending := cookieNamed(w, pendingJoinCookie)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/login" || pending == nil {
		e.t.Fatalf("%s: %d %q, no pending join", path, w.Code, w.Header().Get("Location"))
	}
	login := e.doCookies("POST", "/login", url.Values{"mode": {"password"}, "email": {email}, "password": {password}}.Encode(), pending)
	session := cookieNamed(login, sessionCookie)
	if login.Header().Get("Location") != "/home" || session == nil {
		e.t.Fatalf("logging in: %q", login.Header().Get("Location"))
	}
	home := e.doCookies("GET", "/home", "", pending, session)
	if c := cookieNamed(home, pendingJoinCookie); c == nil || c.MaxAge >= 0 {
		e.t.Errorf("the pending join isn't cleared")
	}
	return home.Header().Get("Location")
}

// TestInviteByNameJoinNewAccount: added by name to a community, the
// person opens their link, which asks for an email (no name: theirs is
// on the roster) and offers to log in instead; they make an account,
// which is the placeholder, now with that email.
func TestInviteByNameJoinNewAccount(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	jam, err := e.st.CreateCommunity(ctx, "Name Jam "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	nina, token := e.addByName("community", jam, "Nina", core)

	page := e.do("GET", "/invite/"+token, "", nil).Body.String()
	for _, want := range []string{"Welcome, Nina", `name="email"`, `action="/invite/` + token + `/login"`, "Log in to join"} {
		if !strings.Contains(page, want) {
			t.Errorf("the link's page lacks %q", want)
		}
	}
	if strings.Contains(page, `name="display_name"`) {
		t.Errorf("the link's page asks for a name")
	}
	email := "nina" + e.sfx + "@example.test"
	form := url.Values{"email": {email}, "password": {"long enough"}, "confirm": {"long enough"}}
	if w := e.do("POST", "/invite/"+token, form.Encode(), nil); w.Code != http.StatusFound || cookieNamed(w, sessionCookie) == nil {
		t.Fatalf("joining: %d %s", w.Code, w.Body)
	}
	p, err := e.st.GetPersonByEmail(ctx, email)
	if err != nil || p.ID != nina || p.DisplayName != "Nina" {
		t.Fatalf("the account: %+v %v", p, err)
	}
	if ok, _ := e.st.IsCurrentCommunityMember(ctx, jam, nina); !ok {
		t.Errorf("Nina isn't in the community")
	}
}

// TestInviteByNameJoinExistingAccount: someone added by name who has an
// account takes their link's place with it — signed in already (asked
// "Join as …?" first), or by logging in from the link's page — and the
// placeholder merges into their account; an email that has an account
// isn't taken for a new one.
func TestInviteByNameJoinExistingAccount(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	bandID, err := e.st.CreateBand(ctx, "Name Band "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}

	// Signed in.
	ritaID, rita := e.person("Rita")
	shell, token := e.addByName("band", bandID, "Reet", core)
	page := e.do("GET", "/invite/"+token, "", rita).Body.String()
	if !strings.Contains(page, "<strong>Rita</strong>") || strings.Contains(page, `name="email"`) || !strings.Contains(page, `action="/logout"`) {
		t.Errorf("signed in: the page doesn't ask to join as Rita, or still asks for an email")
	}
	w := e.do("POST", "/invite/"+token, "", rita)
	if w.Code != http.StatusFound || w.Header().Get("Location") != fmt.Sprintf("/bands/%d", bandID) {
		t.Fatalf("joining as Rita: %d %q %s", w.Code, w.Header().Get("Location"), w.Body)
	}
	if ok, _ := e.st.IsCurrentBandMember(ctx, bandID, ritaID); !ok {
		t.Errorf("Rita isn't in the band")
	}
	if _, err := e.st.GetPersonByID(ctx, shell); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the placeholder is still there: %v", err)
	}
	ins, _ := e.st.ListPersonInstruments(ctx, ritaID)
	if len(ins) != 1 || ins[0].Instrument != "drums" {
		t.Errorf("Rita's instruments: %+v", ins)
	}
	// Opened again signed out: it still opens for Rita (returning_link.go).
	if w := e.do("GET", "/invite/"+token, "", nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Welcome back, Rita") {
		t.Errorf("the used link: %d, want Rita's welcome back", w.Code)
	}

	// Not signed in: a new account with an email that has one is
	// refused, pointing at "Log in to join"; logging in joins.
	samID, _ := e.person("Sam")
	samEmail := "Sam" + e.sfx + "@example.test"
	e.withPassword(samID, "correct horse")
	shell, token = e.addByName("band", bandID, "Samuel", core)
	form := url.Values{"email": {samEmail}, "password": {"long enough"}, "confirm": {"long enough"}}
	if w := e.do("POST", "/invite/"+token, form.Encode(), nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "that email is already registered") {
		t.Errorf("a new account with Sam's email: %d", w.Code)
	}
	if dest := e.logInToJoin("/invite/"+token+"/login", samEmail, "correct horse"); dest != fmt.Sprintf("/bands/%d", bandID) {
		t.Errorf("after logging in: sent to %q", dest)
	}
	if ok, _ := e.st.IsCurrentBandMember(ctx, bandID, samID); !ok {
		t.Errorf("Sam isn't in the band")
	}
	if _, err := e.st.GetPersonByID(ctx, shell); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Samuel's placeholder is still there: %v", err)
	}
}

// TestEveryLinkOffersLogInToJoin: a band's open link and an event's
// registration (a personal link: TestInviteByNameJoinExistingAccount)
// each offer someone with an account to log in, and land them in (or
// back on the event, to register).
func TestEveryLinkOffersLogInToJoin(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	bandID, err := e.st.CreateBand(ctx, "Link Band "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	ollieID, _ := e.person("Ollie")
	ollieEmail := "Ollie" + e.sfx + "@example.test"
	e.withPassword(ollieID, "correct horse")

	// A band's open link.
	other := bandID
	w := e.do("POST", fmt.Sprintf("/api/bands/%d/band-link", other), `{"status":"guest"}`, core)
	if w.Code != http.StatusOK {
		t.Fatalf("an open link: %d %s", w.Code, w.Body)
	}
	var link struct{ URL string }
	if err := json.Unmarshal(w.Body.Bytes(), &link); err != nil {
		t.Fatal(err)
	}
	if page := e.do("GET", link.URL, "", nil).Body.String(); !strings.Contains(page, "Log in to join") || !strings.Contains(page, `name="email"`) {
		t.Errorf("the open link's page: no email, or no log in to join")
	}
	if dest := e.logInToJoin(link.URL+"/login", ollieEmail, "correct horse"); dest != fmt.Sprintf("/bands/%d", other) {
		t.Errorf("open link, after logging in: sent to %q", dest)
	}

	// An event: back to it, to register.
	jam, err := e.st.CreateCommunity(ctx, "Event Jam "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	eventID, err := e.st.CreateEvent(ctx, "community", jam, "jam", "Open Night", "", "", time.Now().Add(48*time.Hour), nil, "members", coreID)
	if err != nil {
		t.Fatal(err)
	}
	if page := e.do("GET", fmt.Sprintf("/events/%d", eventID), "", nil).Body.String(); !strings.Contains(page, fmt.Sprintf(`action="/events/%d/login"`, eventID)) {
		t.Errorf("the event page offers no log in to register")
	}
	if dest := e.logInToJoin(fmt.Sprintf("/events/%d/login", eventID), ollieEmail, "correct horse"); dest != fmt.Sprintf("/events/%d", eventID) {
		t.Errorf("event, after logging in: sent to %q", dest)
	}
}
