package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"app/internal/store"
)

// TestUsedPersonalLink: a personal link, once taken, stays (used) and
// keeps opening for its person until it expires (link_devices_test.go
// has the rest); once expired, it answers 410 with a page that says it
// was used to join the band and offers to log in, never the email;
// someone signed in goes to the band when they're in it, else home with
// a notice; it can't be taken a second time; the admin sees it used.
func TestUsedPersonalLink(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	bandName := "Used Link Band " + e.sfx
	bandID, err := e.st.CreateBand(ctx, bandName, coreID)
	if err != nil {
		t.Fatal(err)
	}
	frank, token := e.addByName("band", bandID, "Frank", core)
	frankEmail := "frank" + e.sfx + "@example.test"
	form := url.Values{"email": {frankEmail}, "password": {"long enough"}, "confirm": {"long enough"}}
	if w := e.do("POST", "/invite/"+token, form.Encode(), nil); w.Code != http.StatusFound {
		t.Fatalf("taking the link: %d %s", w.Code, w.Body)
	}

	// The admin: used, by Frank, still open for him, in the list and on
	// his page.
	if err := e.st.SetAdmin(ctx, coreID, true); err != nil {
		t.Fatal(err)
	}
	rows, err := e.st.ListPlainInvites(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Token == token {
			found = true
			if r.UsedAt == nil || r.UsedByID != frank {
				t.Errorf("the list's row: used %v by %d, want by %d", r.UsedAt, r.UsedByID, frank)
			}
		}
	}
	if !found {
		t.Errorf("the used link isn't in the admin's list")
	}
	admin := e.do("GET", "/admin", "", core).Body.String()
	row := admin[strings.Index(admin, fmt.Sprintf(`personal: <a href="/admin/people/%d">`, frank)):]
	row = row[:strings.Index(row, "</tr>")]
	if !strings.Contains(row, "used ") || !strings.Contains(row, "still opens for them") || strings.Contains(row, "still open<") {
		t.Errorf("the admin's list doesn't show the link used, still opening: %s", row)
	}
	person := e.do("GET", fmt.Sprintf("/admin/people/%d", frank), "", core).Body.String()
	if !strings.Contains(person, "Joined with their invite link") || !strings.Contains(person, `id="person-invite-url"`) {
		t.Errorf("the admin's person page doesn't show the link used, still opening")
	}

	// Expired: the page, 410, the band named, Log in.
	testExec(t, `UPDATE invites SET expires_at = now() - interval '1 minute' WHERE token = $1`, token)
	w := e.do("GET", "/invite/"+token, "", nil)
	body := w.Body.String()
	if w.Code != http.StatusGone || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Errorf("the used link, expired: %d %q, want a 410 page", w.Code, w.Header().Get("Content-Type"))
	}
	// Not kept by the browser: once logged in, the same link must answer anew.
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("the used link's page: Cache-Control %q, want no-store", cc)
	}
	for _, want := range []string{"This link no longer works", "It has expired. You joined " + bandName + " with it.", "Log in to continue.", `action="/login"`, "Ask them for a new one."} {
		if !strings.Contains(body, want) {
			t.Errorf("the used link's page lacks %q", want)
		}
	}
	for _, unwanted := range []string{frankEmail, `name="email"`, `name="password"`, "Frank"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("the used link's page has %q", unwanted)
		}
	}
	// Its "Log in to join" too.
	if w := e.do("GET", "/invite/"+token+"/login", "", nil); w.Code != http.StatusGone || cookieNamed(w, pendingJoinCookie) != nil {
		t.Errorf("the used link's log in: %d, want its page, no pending join", w.Code)
	}

	// Signed in: Frank goes to the band; someone else home, told.
	if w := e.do("GET", "/invite/"+token, "", e.cookieOf(frank)); w.Code != http.StatusSeeOther || w.Header().Get("Location") != fmt.Sprintf("/bands/%d", bandID) {
		t.Errorf("Frank, signed in: %d %q, want the band", w.Code, w.Header().Get("Location"))
	}
	otherID, other := e.person("Other")
	w = e.do("GET", "/invite/"+token, "", other)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/home?link=dead" {
		t.Errorf("someone else, signed in: %d %q, want home with the notice", w.Code, w.Header().Get("Location"))
	}
	if home := e.do("GET", "/home?link=dead", "", other).Body.String(); !strings.Contains(home, "That link has already been used or is no longer valid.") {
		t.Errorf("home doesn't show the notice")
	}
	if home := e.do("GET", "/home", "", other).Body.String(); strings.Contains(home, "That link has already been used") {
		t.Errorf("home shows the notice without the query")
	}

	// Never taken twice: a new account, someone signed in, the store.
	again := url.Values{"email": {"thief" + e.sfx + "@example.test"}, "password": {"long enough"}, "confirm": {"long enough"}}
	if w := e.do("POST", "/invite/"+token, again.Encode(), nil); w.Code != http.StatusGone {
		t.Errorf("taking the used link again: %d, want 410", w.Code)
	}
	if w := e.do("POST", "/invite/"+token, "", other); w.Code != http.StatusSeeOther {
		t.Errorf("taking the used link signed in: %d", w.Code)
	}
	if ok, _ := e.st.IsCurrentBandMember(ctx, bandID, otherID); ok {
		t.Errorf("the used link let someone else in")
	}
	if _, err := e.st.ClaimPersonalInvite(ctx, token, "thief2"+e.sfx+"@example.test", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ClaimPersonalInvite with a used link: %v", err)
	}
	if err := e.st.ClaimPersonalInviteAs(ctx, token, otherID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ClaimPersonalInviteAs with a used link: %v", err)
	}
	if _, err := e.st.GetPersonalInvite(ctx, token); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetPersonalInvite with a used link: %v", err)
	}
	if p, _ := e.st.GetPersonByID(ctx, frank); p == nil || p.Email != frankEmail {
		t.Errorf("Frank's account changed: %+v", p)
	}
	if had, err := e.st.RevokePersonalInvites(ctx, frank); err != nil || had {
		t.Errorf("revoking an expired link: %v %v, want nothing to revoke", had, err)
	}
}

// TestUsedPersonalLinkSignedIn: taken by someone signed in (the
// placeholder merged into their account), the link stays used, on their
// account; opened again it opens for them, naming the community.
func TestUsedPersonalLinkSignedIn(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	communityName := "Used Link Community " + e.sfx
	communityID, err := e.st.CreateCommunity(ctx, communityName, coreID)
	if err != nil {
		t.Fatal(err)
	}
	_, token := e.addByName("community", communityID, "Reet", core)
	ritaID, rita := e.person("Rita")
	if w := e.do("POST", "/invite/"+token, "", rita); w.Code != http.StatusFound {
		t.Fatalf("joining as Rita: %d %s", w.Code, w.Body)
	}
	// It opens for Rita, to her community, until it expires.
	w := e.do("GET", "/invite/"+token, "", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Welcome back, Rita") || !strings.Contains(w.Body.String(), "Your link to "+communityName+".") {
		t.Errorf("the used link: %d, want Rita's welcome back naming the community", w.Code)
	}
	if w := e.do("GET", "/invite/"+token, "", rita); w.Code != http.StatusSeeOther || w.Header().Get("Location") != fmt.Sprintf("/communities/%d", communityID) {
		t.Errorf("Rita, signed in: %d %q, want the community", w.Code, w.Header().Get("Location"))
	}
	inv, err := e.st.GetPersonalInviteFor(ctx, ritaID)
	if err != nil || inv == nil || inv.Token != token || !inv.Used() {
		t.Errorf("Rita's used link: %+v %v", inv, err)
	}
}

// TestDeadLinkPages: an unknown link is a 404 page, an expired one and a
// used plain invite 410 pages; a closed band link a 410 page naming the
// band, and sends its members to the band.
func TestDeadLinkPages(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()

	for _, path := range []string{"/invite/no-such-link-" + e.sfx, "/join/no-such-link-" + e.sfx} {
		w := e.do("GET", path, "", nil)
		body := w.Body.String()
		if w.Code != http.StatusNotFound || !strings.Contains(body, "This link has already been used or is no longer valid.") ||
			!strings.Contains(body, "Already joined? Log in with your email and password (or your passkey).") || !strings.Contains(body, `action="/login"`) {
			t.Errorf("%s: %d, want the 404 page with Log in", path, w.Code)
		}
	}

	coreID, core := e.person("Core")
	shell, err := e.st.CreateShellPerson(ctx, "Late "+e.sfx)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := e.st.CreatePersonalInvite(ctx, shell, coreID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	testExec(t, `UPDATE invites SET expires_at = now() - interval '1 day' WHERE token = $1`, expired)
	if w := e.do("GET", "/invite/"+expired, "", nil); w.Code != http.StatusGone || !strings.Contains(w.Body.String(), "It has expired.") {
		t.Errorf("an expired link: %d", w.Code)
	}

	plain, err := e.st.CreatePlainInvite(ctx, coreID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	joinerID, _ := e.person("Joiner")
	if _, err := e.st.AcceptInvite(ctx, plain, joinerID, "Joiner"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AcceptInvite(ctx, plain, coreID, "Core"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a plain invite taken twice: %v", err)
	}
	if w := e.do("GET", "/invite/"+plain, "", nil); w.Code != http.StatusGone || !strings.Contains(w.Body.String(), "It was already used.") {
		t.Errorf("a used plain invite: %d", w.Code)
	}

	bandName := "Closed Link Band " + e.sfx
	bandID, err := e.st.CreateBand(ctx, bandName, coreID)
	if err != nil {
		t.Fatal(err)
	}
	var guestRole int64
	for _, r := range e.mustRoles(t, bandID) {
		if r.BuiltinKey == "guest" {
			guestRole = r.ID
		}
	}
	giID, link, err := e.st.CreateBandLink(ctx, bandID, coreID, guestRole)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.CloseGroupInvite(ctx, giID, "band", bandID); err != nil {
		t.Fatal(err)
	}
	if w := e.do("GET", "/join/"+link, "", nil); w.Code != http.StatusGone || !strings.Contains(w.Body.String(), "it no longer lets anyone join "+bandName+".") {
		t.Errorf("a closed band link: %d", w.Code)
	}
	if w := e.do("GET", "/join/"+link+"/login", "", nil); w.Code != http.StatusGone || cookieNamed(w, pendingJoinCookie) != nil {
		t.Errorf("a closed band link's log in: %d", w.Code)
	}
	if w := e.do("GET", "/join/"+link, "", core); w.Code != http.StatusSeeOther || w.Header().Get("Location") != fmt.Sprintf("/bands/%d", bandID) {
		t.Errorf("a member, signed in: %d %q, want the band", w.Code, w.Header().Get("Location"))
	}
	if w := e.do("GET", "/join/"+link, "", e.cookieOf(joinerID)); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/home?link=dead" {
		t.Errorf("someone else, signed in: %d %q, want home with the notice", w.Code, w.Header().Get("Location"))
	}
}
