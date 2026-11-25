package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"app/internal/store"
)

// A personal link keeps opening for its person until it expires
// (returning_link.go): the first use makes the account; on another
// device, signed out, the account's password (or passkey) brings them
// back in, never the link alone; signed in as them, straight to the
// band; signed in as someone else, whose link it is.

// joinWithLink adds name to bandID by name, as core, takes the link
// with email and password, and returns the person and the link.
func (e *inviteEnv) joinWithLink(bandID int64, name, email, password string, core *http.Cookie) (int64, string) {
	e.t.Helper()
	id, token := e.addByName("band", bandID, name, core)
	form := url.Values{"email": {email}, "password": {password}, "confirm": {password}}
	w := e.do("POST", "/invite/"+token, form.Encode(), nil)
	if w.Code != http.StatusFound || cookieNamed(w, sessionCookie) == nil {
		e.t.Fatalf("taking the link: %d %s", w.Code, w.Body)
	}
	return id, token
}

// TestPersonalLinkOnAnotherDevice: the whole flow.
func TestPersonalLinkOnAnotherDevice(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	bandName := "Mercy " + e.sfx
	bandID, err := e.st.CreateBand(ctx, bandName, coreID)
	if err != nil {
		t.Fatal(err)
	}
	email := "guillaume" + e.sfx + "@gmail.com"
	gID, token := e.joinWithLink(bandID, "Guillaume", email, "first device", core)
	band := fmt.Sprintf("/bands/%d", bandID)

	// Another device, signed out: "Welcome back", the password, the
	// email masked; no session from the link alone; never cached.
	w := e.do("GET", "/invite/"+token, "", nil)
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("the link again, signed out: %d", w.Code)
	}
	for _, want := range []string{"Welcome back, Guillaume", "Continue as Guillaume", "g•••@gmail.com", "Your link to " + bandName + ".",
		`name="password"`, `autocomplete="current-password"`, `id="link-return-passkey"`, "/api/webauthn/login/finish?link=" + token,
		`href="/login"`, "Not you?"} {
		if !strings.Contains(body, want) {
			t.Errorf("the welcome back page lacks %q", want)
		}
	}
	for _, unwanted := range []string{email, "guillaume" + e.sfx, `name="email"`, `name="confirm"`, "Create account"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("the welcome back page has %q", unwanted)
		}
	}
	if cookieNamed(w, sessionCookie) != nil {
		t.Errorf("opening the link set a session")
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", cc)
	}
	// Its "Log in to join" is this page.
	if w := e.do("GET", "/invite/"+token+"/login", "", nil); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/invite/"+token || cookieNamed(w, pendingJoinCookie) != nil {
		t.Errorf("the link's log in: %d %q", w.Code, w.Header().Get("Location"))
	}

	// A wrong password: the login page's one answer, no session.
	w = e.do("POST", "/invite/"+token, url.Values{"password": {"not it"}}.Encode(), nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "That email and password don&#39;t match.") || cookieNamed(w, sessionCookie) != nil {
		t.Errorf("a wrong password: %d, session %v", w.Code, cookieNamed(w, sessionCookie))
	}
	if strings.Contains(w.Body.String(), email) {
		t.Errorf("the wrong password page shows the email")
	}
	// The first use's form, again: no account made, nothing changed.
	thief := url.Values{"email": {"thief" + e.sfx + "@example.test"}, "password": {"stolen pass"}, "confirm": {"stolen pass"}}
	if w := e.do("POST", "/invite/"+token, thief.Encode(), nil); w.Code != http.StatusOK || cookieNamed(w, sessionCookie) != nil {
		t.Errorf("the sign-up form on a used link: %d", w.Code)
	}
	if _, err := e.st.GetPersonByEmail(ctx, "thief"+e.sfx+"@example.test"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the used link made an account: %v", err)
	}

	// The right password: in, to the band, counted; the email typed
	// along is ignored, the password stays.
	right := url.Values{"password": {"first device"}, "email": {"new" + e.sfx + "@example.test"}, "confirm": {"first device"}}
	w = e.do("POST", "/invite/"+token, right.Encode(), nil)
	sess := cookieNamed(w, sessionCookie)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != band || sess == nil {
		t.Fatalf("the right password: %d %q", w.Code, w.Header().Get("Location"))
	}
	if p, err := e.st.GetSession(ctx, sess.Value); err != nil || p.ID != gID {
		t.Errorf("the session isn't Guillaume's: %v", err)
	}
	if p, _ := e.st.GetPersonByID(ctx, gID); p == nil || p.Email != email {
		t.Errorf("the account's email changed: %+v", p)
	}
	if l, err := e.st.ReturningLinkFor(ctx, gID); err != nil || l == nil || l.Token != token || l.Signins != 1 || l.LastSignin == nil || l.UsedAt == nil {
		t.Errorf("the link's sign-ins: %+v %v", l, err)
	}
	// Claiming twice stays impossible.
	if _, err := e.st.ClaimPersonalInvite(ctx, token, "again"+e.sfx+"@example.test", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ClaimPersonalInvite on a used link: %v", err)
	}

	// Signed in as Guillaume: straight to the band.
	if w := e.do("GET", "/invite/"+token, "", sess); w.Code != http.StatusSeeOther || w.Header().Get("Location") != band {
		t.Errorf("signed in as him: %d %q", w.Code, w.Header().Get("Location"))
	}

	// Signed in as someone else: whose link it is, Log out (back to the
	// link) or Go home; no merge, by GET or POST.
	otherID, other := e.person("Marc")
	w = e.do("GET", "/invite/"+token, "", other)
	body = w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, "This link is Guillaume’s") || !strings.Contains(body, "You’re signed in as Marc.") ||
		!strings.Contains(body, `action="/logout"`) || !strings.Contains(body, `value="/invite/`+token+`"`) || !strings.Contains(body, `action="/home"`) {
		t.Errorf("signed in as someone else: %d", w.Code)
	}
	if strings.Contains(body, `name="password"`) || strings.Contains(body, email) {
		t.Errorf("the someone-else page asks for a password or shows the email")
	}
	if w := e.do("POST", "/invite/"+token, url.Values{"password": {"first device"}}.Encode(), other); w.Code != http.StatusOK || cookieNamed(w, sessionCookie) != nil {
		t.Errorf("posting as someone else: %d", w.Code)
	}
	if ok, _ := e.st.IsCurrentBandMember(ctx, bandID, otherID); ok {
		t.Errorf("someone else got into the band")
	}
	if _, err := e.st.GetPersonByID(ctx, gID); err != nil {
		t.Errorf("Guillaume's account is gone: %v", err)
	}
	// Log out comes back to the link; only to an /invite/ link.
	if w := e.do("POST", "/logout", url.Values{"next": {"/invite/" + token}}.Encode(), other); w.Header().Get("Location") != "/invite/"+token {
		t.Errorf("log out back to the link: %q", w.Header().Get("Location"))
	}
	for _, next := range []string{"//evil.example/x", "/invite/a/../../admin", "https://evil.example/invite/x", "/invite/x?y"} {
		if w := e.do("POST", "/logout", url.Values{"next": {next}}.Encode(), nil); w.Header().Get("Location") != "/login" {
			t.Errorf("log out with next %q: %q, want /login", next, w.Header().Get("Location"))
		}
	}

	// Revoked: the dead link's page, 410, Log in.
	if w := e.do("DELETE", fmt.Sprintf("/api/people/%d/personal-invites", gID), "", core); w.Code != http.StatusOK {
		t.Fatalf("revoking: %d %s", w.Code, w.Body)
	}
	w = e.do("GET", "/invite/"+token, "", nil)
	if w.Code != http.StatusGone || !strings.Contains(w.Body.String(), "It has expired. You joined "+bandName+" with it.") || !strings.Contains(w.Body.String(), `action="/login"`) {
		t.Errorf("the revoked link: %d", w.Code)
	}
	if w := e.do("POST", "/invite/"+token, url.Values{"password": {"first device"}}.Encode(), nil); w.Code != http.StatusGone || cookieNamed(w, sessionCookie) != nil {
		t.Errorf("the revoked link's password: %d", w.Code)
	}
	if w := e.do("GET", "/invite/"+token, "", sess); w.Code != http.StatusSeeOther || w.Header().Get("Location") != band {
		t.Errorf("the revoked link, signed in as him: %d %q", w.Code, w.Header().Get("Location"))
	}
}

// TestPersonalLinkPasswordLimit: wrong passwords through links are
// limited per account; the right one too once over the limit.
func TestPersonalLinkPasswordLimit(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	bandID, err := e.st.CreateBand(ctx, "Limit Band "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	gID, token := e.joinWithLink(bandID, "Lena", "lena"+e.sfx+"@example.test", "right password", core)
	defer linkSignInTries.reset(fmt.Sprint(gID))
	for i := 0; i < linkSignInTries.max; i++ {
		if w := e.do("POST", "/invite/"+token, url.Values{"password": {"wrong"}}.Encode(), nil); w.Code != http.StatusOK {
			t.Fatalf("wrong password %d: %d", i+1, w.Code)
		}
	}
	w := e.do("POST", "/invite/"+token, url.Values{"password": {"right password"}}.Encode(), nil)
	if w.Code != http.StatusTooManyRequests || cookieNamed(w, sessionCookie) != nil || !strings.Contains(w.Body.String(), "Too many tries.") {
		t.Errorf("over the limit: %d", w.Code)
	}
	// The login page itself isn't this limit's.
	linkSignInTries.reset(fmt.Sprint(gID))
	if w := e.do("POST", "/invite/"+token, url.Values{"password": {"right password"}}.Encode(), nil); w.Code != http.StatusSeeOther {
		t.Errorf("after the window: %d", w.Code)
	}
}

// TestReturningLinkOnRosters: a band's core sees a joined member's link
// on the roster (Copy, New link, Revoke, "used to join on … still opens
// for … until …"); "Get link" for someone who joined since gives that
// same link; New link replaces it; a guest can't revoke it; a
// community's admin page lists it.
func TestReturningLinkOnRosters(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	bandID, err := e.st.CreateBand(ctx, "Roster Band "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	gID, token := e.joinWithLink(bandID, "Guy", "guy"+e.sfx+"@example.test", "a password", core)

	page := e.do("GET", fmt.Sprintf("/bands/%d/members", bandID), "", core).Body.String()
	row := page[strings.Index(page, fmt.Sprintf(`data-person-id="%d" data-name="Guy"`, gID)):]
	row = row[:strings.Index(row, `class="roster-row-actions"`)]
	for _, want := range []string{`data-url="/invite/` + token + `"`, "used to join on", "still opens for Guy until", "revoke-link-btn", `data-joined="1"`} {
		if !strings.Contains(row, want) {
			t.Errorf("the roster row lacks %q", want)
		}
	}

	// Get link (no replace): the same link.
	link := func(path string, cookie *http.Cookie) (int, string) {
		w := e.do("POST", path, "", cookie)
		var res struct{ URL string }
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		return w.Code, strings.TrimPrefix(res.URL, "/invite/")
	}
	if code, got := link(fmt.Sprintf("/api/bands/%d/members/%d/invite-link", bandID, gID), core); code != http.StatusOK || got != token {
		t.Errorf("Get link for someone who joined: %d %q, want the same link", code, got)
	}
	if code, got := link(fmt.Sprintf("/api/people/%d/personal-invites", gID), core); code != http.StatusOK || got != token {
		t.Errorf("Get invite link for someone who joined: %d %q, want the same link", code, got)
	}
	// New link: another, the old one ends.
	code, renewed := link(fmt.Sprintf("/api/bands/%d/members/%d/invite-link?replace=1", bandID, gID), core)
	if code != http.StatusOK || renewed == "" || renewed == token {
		t.Fatalf("New link: %d", code)
	}
	if w := e.do("GET", "/invite/"+token, "", nil); w.Code != http.StatusGone {
		t.Errorf("the replaced link: %d, want 410", w.Code)
	}
	if w := e.do("GET", "/invite/"+renewed, "", nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Welcome back, Guy") {
		t.Errorf("the new link: %d", w.Code)
	}
	// Opened by Guy signed in: his band.
	if w := e.do("GET", "/invite/"+renewed, "", e.cookieOf(gID)); w.Code != http.StatusSeeOther || w.Header().Get("Location") != fmt.Sprintf("/bands/%d", bandID) {
		t.Errorf("the new link, signed in: %d %q", w.Code, w.Header().Get("Location"))
	}

	// A guest can't revoke it; core can; then there's nothing to get.
	guestID, guest := e.person("Guest")
	if _, err := e.st.AddBandMember(ctx, bandID, guestID, "guest", coreID); err != nil {
		t.Fatal(err)
	}
	if w := e.do("DELETE", fmt.Sprintf("/api/people/%d/personal-invites", gID), "", guest); w.Code != http.StatusForbidden {
		t.Errorf("a guest revoking: %d, want 403", w.Code)
	}
	if strings.Contains(e.do("GET", fmt.Sprintf("/bands/%d/members", bandID), "", guest).Body.String(), "/invite/"+renewed) {
		t.Errorf("a guest sees the link on the roster")
	}
	if w := e.do("DELETE", fmt.Sprintf("/api/people/%d/personal-invites", gID), "", core); w.Code != http.StatusOK {
		t.Errorf("core revoking: %d", w.Code)
	}
	if code, _ := link(fmt.Sprintf("/api/bands/%d/members/%d/invite-link", bandID, gID), core); code != http.StatusConflict {
		t.Errorf("Get link once revoked: %d, want 409", code)
	}

	// A community's admin page.
	commID, err := e.st.CreateCommunity(ctx, "Roster Community "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	cID, cToken := e.addByName("community", commID, "Cleo", core)
	form := url.Values{"email": {"cleo" + e.sfx + "@example.test"}, "password": {"a password"}, "confirm": {"a password"}}
	if w := e.do("POST", "/invite/"+cToken, form.Encode(), nil); w.Code != http.StatusFound {
		t.Fatalf("Cleo joining: %d", w.Code)
	}
	admin := e.do("GET", fmt.Sprintf("/communities/%d/admin", commID), "", core).Body.String()
	if !strings.Contains(admin, "Joined, link still open") || !strings.Contains(admin, `data-url="/invite/`+cToken+`"`) || !strings.Contains(admin, fmt.Sprintf(`data-person-id="%d" data-name="Cleo"`, cID)) {
		t.Errorf("the community admin page doesn't list Cleo's link")
	}
}

// TestPersonalLinkFirstUse: the first use is as before, and says the
// link keeps working; the link's page for someone signed in who hasn't
// taken it is still "Join as".
func TestPersonalLinkFirstUse(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	bandID, err := e.st.CreateBand(ctx, "First Use Band "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	_, token := e.addByName("band", bandID, "Nina", core)
	body := e.do("GET", "/invite/"+token, "", nil).Body.String()
	if !strings.Contains(body, `name="email"`) || !strings.Contains(body, `name="confirm"`) || !strings.Contains(body, "Keep this link: until") {
		t.Errorf("the first use's page")
	}
	if _, err := e.st.GetReturningLink(ctx, token); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a link not taken yet is a link for coming back: %v", err)
	}
}

func TestMaskEmail(t *testing.T) {
	for in, want := range map[string]string{
		"guillaume@gmail.com":    "g•••@gmail.com",
		"G.Dupont@Outlook.FR":    "G•••@Outlook.FR",
		"guillaume@dupont.fr":    "g•••@d•••.fr",
		"élise@mercy-band.co.uk": "é•••@m•••.uk",
		"x@localhost":            "x•••@l•••",
		"":                       "",
		"not-an-email":           "",
	} {
		if got := maskEmail(in); got != want {
			t.Errorf("maskEmail(%q) = %q, want %q", in, got, want)
		}
	}
}
