package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/store"
)

// No login email reaches anyone yet (the console mailer), so every way
// to make an account or claim a placeholder needs a password
// (password_rule.go, docs/accounts.md), event registration included.

// signUpPath is one way in for someone without an account: post form
// (plus email, password and confirm) to path.
type signUpPath struct {
	name string
	path string
	form url.Values
}

func (e *inviteEnv) signUpPaths(t *testing.T) []signUpPath {
	ctx := context.Background()
	coreID, core := e.person("Core")
	band, err := e.st.CreateBand(ctx, "Password Band "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	jam, err := e.st.CreateCommunity(ctx, "Password Jam "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	var paths []signUpPath

	// A personal link: a band's placeholder, and a community's.
	_, bandTok := e.addByName("band", band, "Pat", core)
	paths = append(paths, signUpPath{"band personal link", "/invite/" + bandTok, url.Values{}})
	_, jamTok := e.addByName("community", jam, "Kim", core)
	paths = append(paths, signUpPath{"community personal link", "/invite/" + jamTok, url.Values{}})

	// A site admin's plain invite.
	plain, err := e.st.CreatePlainInvite(ctx, coreID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, signUpPath{"plain invite", "/invite/" + plain, url.Values{"display_name": {"Plain"}}})

	// A band's open link, a community's.
	link := func(path, body string) string {
		w := e.do("POST", path, body, core)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		var res struct{ URL string }
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		return res.URL
	}
	paths = append(paths, signUpPath{"band open link", link(fmt.Sprintf("/api/bands/%d/band-link", band), `{"status":"guest"}`), url.Values{"display_name": {"Opal"}, "slot_id": {"unlisted"}}})
	paths = append(paths, signUpPath{"community link", link(fmt.Sprintf("/api/communities/%d/community-link", jam), `{"role":"member"}`), url.Values{"display_name": {"Cleo"}, "slot_id": {"unlisted"}}})

	// A community's invite with names to pick: a name, and "someone else".
	_, roster, err := e.st.CreateGroupInvite(ctx, "community", jam, coreID, []store.GroupInviteSlotInput{{DisplayName: "Rory"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	view, err := e.st.GetGroupInviteByToken(ctx, roster)
	if err != nil || len(view.Slots) != 1 {
		t.Fatalf("the roster: %+v %v", view, err)
	}
	paths = append(paths, signUpPath{"community roster, a name", "/join/" + roster, url.Values{"display_name": {"Rory"}, "slot_id": {fmt.Sprint(view.Slots[0].ID)}}})
	paths = append(paths, signUpPath{"community roster, someone else", "/join/" + roster, url.Values{"display_name": {"Sid"}, "slot_id": {"unlisted"}}})

	// Registering for an event.
	eventID, err := e.st.CreateEvent(ctx, "community", jam, "jam", "Password Night", "", "", time.Now().Add(48*time.Hour), nil, "members", coreID)
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, signUpPath{"event registration", fmt.Sprintf("/events/%d/register", eventID), url.Values{"display_name": {"Eve"}}})
	return paths
}

// TestEverySignUpNeedsAPassword: each way in refuses no password, a short
// one, one too long for bcrypt and a mistyped confirmation, making no
// account; it takes a good one, and the account logs in with it.
func TestEverySignUpNeedsAPassword(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	for i, p := range e.signUpPaths(t) {
		email := fmt.Sprintf("signup%d-%s@example.test", i, e.sfx)
		post := func(password, confirm string) (int, string) {
			form := url.Values{}
			for k, v := range p.form {
				form[k] = v
			}
			form.Set("email", email)
			form.Set("password", password)
			form.Set("confirm", confirm)
			w := e.do("POST", p.path, form.Encode(), nil)
			return w.Code, w.Body.String()
		}
		for _, bad := range []struct{ password, confirm, reason string }{
			{"", "", "a password is required"},
			{"short", "short", "at least 8 characters"},
			{strings.Repeat("x", 73), strings.Repeat("x", 73), "too long"},
			{"long enough", "long enuogh", "didn't match"},
		} {
			code, body := post(bad.password, bad.confirm)
			if code == http.StatusFound {
				t.Errorf("%s: took password %q / %q", p.name, bad.password, bad.confirm)
			}
			if !strings.Contains(strings.ReplaceAll(body, "&#39;", "'"), bad.reason) {
				t.Errorf("%s, password %q: the answer doesn't say %q: %d %.200s", p.name, bad.password, bad.reason, code, body)
			}
			if _, err := e.st.GetPersonByEmail(ctx, email); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("%s, password %q: an account was made (%v)", p.name, bad.password, err)
			}
		}
		if code, body := post("long enough", "long enough"); code != http.StatusFound {
			t.Fatalf("%s: a good password: %d %.300s", p.name, code, body)
		}
		login := formLogin(e, url.Values{"mode": {"password"}, "email": {email}, "password": {"long enough"}})
		if loc := login.Header.Get("Location"); loc != "/home" {
			t.Errorf("%s: logging in with the new password: sent to %q", p.name, loc)
		}
	}
}

// TestEventRegistrationAsksForAPassword: the form has a required password
// and its confirmation, and says nothing about an emailed link instead.
func TestEventRegistrationAsksForAPassword(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, _ := e.person("Host")
	jam, err := e.st.CreateCommunity(ctx, "Form Jam "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	eventID, err := e.st.CreateEvent(ctx, "community", jam, "jam", "Form Night", "", "", time.Now().Add(48*time.Hour), nil, "members", coreID)
	if err != nil {
		t.Fatal(err)
	}
	page := e.do("GET", fmt.Sprintf("/events/%d", eventID), "", nil).Body.String()
	for _, want := range []string{`id="reg-password" name="password"`, `name="confirm"`, `data-match="reg-password" required`} {
		if !strings.Contains(page, want) {
			t.Errorf("the registration form lacks %q", want)
		}
	}
	if !strings.Contains(page, `aria-describedby="reg-password-hint" required minlength="8"`) {
		t.Errorf("the password isn't required, 8 characters at least")
	}
	for _, unwanted := range []string{"Leave it empty", "a link we send you", "(optional)</span></label>\n        <p class=\"field-hint\" id=\"reg-password-hint\""} {
		if strings.Contains(page, unwanted) {
			t.Errorf("the registration form still says %q", unwanted)
		}
	}
}

// TestLoginPutsThePasswordFirst: with no mailer that delivers, the login
// page leads with the password; the emailed link is still there, last.
func TestLoginPutsThePasswordFirst(t *testing.T) {
	e := newInviteEnv(t)
	page := e.do("GET", "/login", "", nil).Body.String()
	pw, passkey, link := strings.Index(page, `id="password-login-form"`), strings.Index(page, `id="passkey-login"`), strings.Index(page, `id="email-link"`)
	if pw < 0 || passkey < 0 || link < 0 || !(pw < passkey && passkey < link) {
		t.Errorf("the login page's order: password %d, passkey %d, emailed link %d", pw, passkey, link)
	}
	if strings.Contains(page, `id="webauthn-login-btn" class="primary"`) {
		t.Errorf("the passkey button is still the primary one")
	}
	if strings.Contains(page, "Get a login link by email below") {
		t.Errorf("a forgotten password still points to an emailed link")
	}
}

// TestAdminListsAccountsWithoutAPassword: an account with an email but
// no password nor passkey (event registration made them) is listed in
// the admin page's "can't log in" warning; one with a password, or a
// passkey, isn't.
func TestAdminListsAccountsWithoutAPassword(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	adminID, admin := e.person("Admin")
	if err := e.st.SetAdmin(ctx, adminID, true); err != nil {
		t.Fatal(err)
	}
	e.withPassword(adminID, "admin password")
	lockedID, _ := e.person("Locked Out")
	withPw, _ := e.person("Has Password")
	e.withPassword(withPw, "a password")
	withKey, _ := e.person("Has Passkey")
	if _, err := e.st.AddWebAuthnCredential(ctx, withKey, []byte("cred-"+e.sfx), []byte("key"), "none", []string{}, 0, false, false, true, true, "", "test"); err != nil {
		t.Fatal(err)
	}

	locked, err := e.st.ListEmailNoPassword(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[int64]bool{}
	for _, p := range locked {
		ids[p.ID] = true
	}
	if !ids[lockedID] || ids[withPw] || ids[withKey] || ids[adminID] {
		t.Errorf("ListEmailNoPassword: %+v (want %d, not %d, %d, %d)", locked, lockedID, withPw, withKey, adminID)
	}

	page := e.do("GET", "/admin", "", admin).Body.String()
	i := strings.Index(page, "but no password")
	if i < 0 {
		t.Fatalf("the admin page has no warning about accounts without a password")
	}
	block := page[i:]
	if end := strings.Index(block, "</div>"); end > 0 {
		block = block[:end]
	}
	if !strings.Contains(block, fmt.Sprintf(`href="/admin/people/%d"`, lockedID)) {
		t.Errorf("the warning doesn't list the locked-out account")
	}
	for _, id := range []int64{withPw, withKey} {
		if strings.Contains(block, fmt.Sprintf(`href="/admin/people/%d"`, id)) {
			t.Errorf("the warning lists %d, who can log in", id)
		}
	}
}

// TestNamelessBandLinkGone: a band's nameless one-off link can't be made
// any more, and one made before isn't taken: invites are by name (a
// placeholder's personal link) or by the band's open link.
func TestNamelessBandLinkGone(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	band, err := e.st.CreateBand(ctx, "One-off Band "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	if w := e.do("POST", fmt.Sprintf("/api/bands/%d/invites", band), url.Values{"status": {"guest"}}.Encode(), core); w.Code == http.StatusOK {
		t.Errorf("a one-off link was made: %s", w.Body)
	}
	page := e.do("GET", fmt.Sprintf("/bands/%d/members", band), "", core).Body.String()
	for _, unwanted := range []string{"Create invite link", "one-off link", `/api/bands/${bandId}/invites`} {
		if strings.Contains(page, unwanted) {
			t.Errorf("the members page still has %q", unwanted)
		}
	}

	// One made before the change, still in the table.
	tok := "old-one-off-" + e.sfx
	testExec(t, `INSERT INTO invites (token, band_id, band_status, expires_at) VALUES ($1, $2, 'guest', now() + interval '1 day')`, tok, band)
	if w := e.do("GET", "/invite/"+tok, "", nil); w.Code != http.StatusNotFound {
		t.Errorf("the old one-off link's page: %d", w.Code)
	}
	form := url.Values{"display_name": {"Late"}, "email": {"late" + e.sfx + "@example.test"}, "password": {"long enough"}, "confirm": {"long enough"}}
	if w := e.do("POST", "/invite/"+tok, form.Encode(), nil); w.Code != http.StatusNotFound {
		t.Errorf("joining with the old one-off link: %d", w.Code)
	}
	otherID, other := e.person("Other")
	if w := e.do("POST", "/invite/"+tok, "", other); w.Code != http.StatusNotFound {
		t.Errorf("joining with the old one-off link, signed in: %d", w.Code)
	}
	if ok, _ := e.st.IsCurrentBandMember(ctx, band, otherID); ok {
		t.Errorf("the old one-off link let someone in")
	}
	if _, err := e.st.AcceptInvite(ctx, tok, otherID, "Other"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("AcceptInvite with the old one-off link: %v", err)
	}
}

// testExec runs one statement on the test database, in the schema
// newTestStore uses: a row as an older version left it.
func testExec(t *testing.T, sql string, args ...any) {
	t.Helper()
	ctx := context.Background()
	conn := os.Getenv("SONGBOOKS_TEST_DATABASE_URL")
	if conn == "" {
		conn = "postgres://localhost/songbooks_test?sslmode=disable"
	}
	if strings.Contains(conn, "?") {
		conn += "&search_path=httpapi_test"
	} else {
		conn += "?search_path=httpapi_test"
	}
	db, err := pgx.Connect(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	if _, err := db.Exec(ctx, sql, args...); err != nil {
		t.Fatal(err)
	}
}
