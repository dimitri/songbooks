package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"app/internal/mailer"
)

// Email is the identity (docs/accounts.md, migration 0135): these tests
// pin login by email only, adding people by email or by name, claiming
// a placeholder account with an email, and that no page shows a login
// name any more.

// setLegacyUsername gives personID an old-style username straight in the
// database (the app no longer writes one), to prove nothing shows it.
func setLegacyUsername(t *testing.T, personID int64, name string) {
	t.Helper()
	ctx := context.Background()
	conn := os.Getenv("SONGBOOKS_TEST_DATABASE_URL")
	if conn == "" {
		conn = "postgres://localhost/songbooks_test?sslmode=disable"
	}
	c, err := pgx.Connect(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)
	if _, err := c.Exec(ctx, `UPDATE httpapi_test.people SET username = $1 WHERE id = $2`, name, personID); err != nil {
		t.Fatal(err)
	}
}

func (e *inviteEnv) withPassword(id int64, password string) {
	e.t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.st.SetPassword(context.Background(), id, string(h)); err != nil {
		e.t.Fatal(err)
	}
}

func formLogin(e *inviteEnv, fields url.Values) *http.Response {
	e.t.Helper()
	w := e.do("POST", "/login", fields.Encode(), nil)
	return w.Result()
}

// TestLoginIsByEmailOnly: the password form takes an email (any case);
// a legacy username, an unknown email, a wrong password and no password
// set all get the very same refusal; the magic link answers the same
// page whether the email has an account or not.
func TestLoginIsByEmailOnly(t *testing.T) {
	e := newInviteEnv(t)
	e.h = NewServer(e.st, mailer.NewConsoleMailer(e.st), nil, "", "")
	ctx := context.Background()
	id, _ := e.person("Lena")
	email := "Lena" + e.sfx + "@example.test"
	e.withPassword(id, "correct horse")
	setLegacyUsername(t, id, "lena"+e.sfx)

	res := formLogin(e, url.Values{"mode": {"password"}, "email": {strings.ToUpper(email)}, "password": {"correct horse"}})
	if res.StatusCode != http.StatusFound || res.Header.Get("Location") != "/home" {
		t.Fatalf("email + password: %d %q", res.StatusCode, res.Header.Get("Location"))
	}
	var gotSession bool
	for _, c := range res.Cookies() {
		gotSession = gotSession || (c.Name == sessionCookie && c.Value != "")
	}
	if !gotSession {
		t.Error("email + password: no session cookie")
	}

	refusals := map[string]url.Values{
		"the legacy username":      {"mode": {"password"}, "email": {"lena" + e.sfx}, "password": {"correct horse"}},
		"the old identifier field": {"mode": {"password"}, "identifier": {email}, "password": {"correct horse"}},
		"a wrong password":         {"mode": {"password"}, "email": {email}, "password": {"wrong horse!"}},
		"an unknown email":         {"mode": {"password"}, "email": {"nobody" + e.sfx + "@example.test"}, "password": {"correct horse"}},
		"nothing typed":            {"mode": {"password"}, "password": {"correct horse"}},
	}
	var first string
	for what, form := range refusals {
		w := e.do("POST", "/login", form.Encode(), nil)
		if w.Code != http.StatusFound || w.Header().Get("Location") != "/login?badlogin=1" || len(w.Result().Cookies()) != 0 {
			t.Errorf("%s: %d %q, want the one refusal", what, w.Code, w.Header().Get("Location"))
		}
		if first == "" {
			first = w.Body.String() + "|"
		} else if w.Body.String()+"|" != first {
			t.Errorf("%s: the refusal's body differs: %q", what, w.Body)
		}
	}

	// The login page asks for an email, and says so when it refuses.
	page := e.do("GET", "/login?badlogin=1", "", nil).Body.String()
	if strings.Contains(page, "Username") || strings.Contains(page, `name="identifier"`) || !strings.Contains(page, `type="email" name="email"`) {
		t.Errorf("login page still asks for a username")
	}
	if !strings.Contains(page, "That email and password don") {
		t.Errorf("login page: the refusal doesn't speak of the email")
	}

	// The magic link: the same page either way, a mail only for an account.
	known := e.do("POST", "/login", url.Values{"email": {email}}.Encode(), nil)
	unknown := e.do("POST", "/login", url.Values{"email": {"nobody" + e.sfx + "@example.test"}}.Encode(), nil)
	if known.Code != unknown.Code || known.Body.String() != unknown.Body.String() {
		t.Errorf("magic link: known %d and unknown %d emails answer differently", known.Code, unknown.Code)
	}
	if _, err := e.st.GetPersonByEmail(ctx, strings.ToLower(email)); err != nil {
		t.Fatal(err)
	}

	// A password and no email: listed as locked out (and only that).
	bare, err := e.st.CreatePerson(ctx, "Bare "+e.sfx, "")
	if err != nil {
		t.Fatal(err)
	}
	e.withPassword(bare, "correct horse")
	locked, err := e.st.ListPasswordNoEmail(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var listed bool
	for _, p := range locked {
		listed = listed || p.ID == bare
		if p.ID == id {
			t.Errorf("an account with an email is listed as locked out")
		}
	}
	if !listed {
		t.Errorf("an account with a password and no email isn't listed as locked out")
	}
}

// TestAddMemberByEmailOrName: a band's core member adds an account by its
// email, or someone they already play with by picking them; an unknown
// email is refused (a link is how a newcomer joins), and so is a person
// they don't know. The same for a community.
func TestAddMemberByEmailOrName(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	byEmailID, _ := e.person("ByEmail")
	mateID, _ := e.person("Mate")
	strangerID, _ := e.person("Stranger")
	bandID, err := e.st.CreateBand(ctx, "Email Band "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	otherBand, err := e.st.CreateBand(ctx, "Other Band "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddBandMember(ctx, otherBand, mateID, "core", coreID); err != nil {
		t.Fatal(err)
	}
	b := fmt.Sprintf("/api/bands/%d/members", bandID)
	member := func(band, id int64) bool {
		ok, err := e.st.IsCurrentBandMember(ctx, band, id)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}

	if w := e.do("POST", b, `{"email":" ByEmail`+e.sfx+`@Example.test ","status":"guest"}`, core); w.Code != http.StatusOK || !member(bandID, byEmailID) {
		t.Errorf("adding by email: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", b, `{"email":"nobody`+e.sfx+`@example.test","status":"guest"}`, core); w.Code != http.StatusNotFound {
		t.Errorf("an unknown email: %d, want 404", w.Code)
	}
	if w := e.do("POST", b, `{"email":"Stranger`+e.sfx+`","status":"guest"}`, core); w.Code != http.StatusBadRequest {
		t.Errorf("a name where the email goes: %d, want 400", w.Code)
	}
	if w := e.do("POST", b, `{"username":"Stranger`+e.sfx+`","status":"guest"}`, core); w.Code != http.StatusBadRequest {
		t.Errorf("the old username field: %d, want 400", w.Code)
	}

	// The members page offers Mate (they share Other Band), not Stranger,
	// and not who's in the band already.
	page := e.do("GET", fmt.Sprintf("/bands/%d/members", bandID), "", core).Body.String()
	if !strings.Contains(page, fmt.Sprintf(`<option value="%d">Mate · Other Band %s</option>`, mateID, e.sfx)) {
		t.Errorf("members page: Mate isn't offered")
	}
	if strings.Contains(page, fmt.Sprintf(`<option value="%d">`, strangerID)) || strings.Contains(page, fmt.Sprintf(`<option value="%d">`, byEmailID)) {
		t.Errorf("members page: offers a stranger or a current member")
	}
	if strings.Contains(page, `id="add-username"`) || !strings.Contains(page, `id="add-email"`) {
		t.Errorf("members page: still a username field")
	}
	if w := e.do("POST", b, fmt.Sprintf(`{"personId":%d,"status":"guest"}`, strangerID), core); w.Code != http.StatusNotFound || member(bandID, strangerID) {
		t.Errorf("adding a stranger by id: %d, want 404", w.Code)
	}
	if w := e.do("POST", b, fmt.Sprintf(`{"personId":%d,"status":"guest"}`, mateID), core); w.Code != http.StatusOK || !member(bandID, mateID) {
		t.Errorf("adding a bandmate by name: %d %s", w.Code, w.Body)
	}

	// A community: by email too.
	communityID, err := e.st.CreateCommunity(ctx, "Email Jam "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	c := fmt.Sprintf("/api/communities/%d/members", communityID)
	if w := e.do("POST", c, `{"email":"Stranger`+e.sfx+`@example.test","role":"member"}`, core); w.Code != http.StatusOK {
		t.Errorf("adding to a community by email: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", c, `{"email":"nobody`+e.sfx+`@example.test","role":"member"}`, core); w.Code != http.StatusNotFound {
		t.Errorf("a community, an unknown email: %d, want 404", w.Code)
	}
	if ok, err := e.st.IsCurrentCommunityMember(ctx, communityID, strangerID); err != nil || !ok {
		t.Errorf("not a community member after the add: %v %v", ok, err)
	}
}

// TestClaimPlaceholderByEmail: a placeholder (shell) account added by
// name is claimed with an email and a password, no username; the account
// then logs in with that email.
func TestClaimPlaceholderByEmail(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	bandID, err := e.st.CreateBand(ctx, "Claim Band "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	w := e.do("POST", fmt.Sprintf("/api/bands/%d/personal-invites", bandID), `{"displayName":"Roland","status":"core"}`, core)
	if w.Code != http.StatusOK {
		t.Fatalf("adding Roland by name: %d %s", w.Code, w.Body)
	}
	shells, err := e.st.ListShellPeopleForBand(ctx, bandID)
	if err != nil || len(shells) != 1 || shells[0].OpenInviteToken == "" {
		t.Fatalf("Roland's placeholder: %+v %v", shells, err)
	}
	roland, token := shells[0].PersonID, shells[0].OpenInviteToken

	page := e.do("GET", "/invite/"+token, "", nil).Body.String()
	if strings.Contains(page, `name="username"`) || !strings.Contains(page, `name="email"`) || !strings.Contains(page, "Welcome, Roland") {
		t.Fatalf("the claim page still asks for a username, or not for an email")
	}

	email := "roland" + e.sfx + "@example.test"
	form := url.Values{"email": {strings.ToUpper(email)}, "password": {"long enough"}, "confirm": {"long enough"}}
	if w := e.do("POST", "/invite/"+token, form.Encode(), nil); w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), "/welcome") {
		t.Fatalf("claiming: %d %q %s", w.Code, w.Header().Get("Location"), w.Body)
	}
	p, err := e.st.GetPersonByEmail(ctx, email)
	if err != nil || p.ID != roland || p.DisplayName != "Roland" {
		t.Fatalf("after the claim: %+v %v", p, err)
	}
	if ok, _ := e.st.IsCurrentBandMember(ctx, bandID, roland); !ok {
		t.Errorf("Roland lost his place in the band")
	}
	res := formLogin(e, url.Values{"mode": {"password"}, "email": {email}, "password": {"long enough"}})
	if res.Header.Get("Location") != "/home" {
		t.Errorf("Roland can't log in with his email: %q", res.Header.Get("Location"))
	}

	// Another placeholder can't take an email that's an account already.
	w = e.do("POST", fmt.Sprintf("/api/bands/%d/personal-invites", bandID), `{"displayName":"Twin","status":"guest"}`, core)
	if w.Code != http.StatusOK {
		t.Fatal(w.Body)
	}
	shells, _ = e.st.ListShellPeopleForBand(ctx, bandID)
	if len(shells) != 1 {
		t.Fatalf("Twin's placeholder: %+v", shells)
	}
	w = e.do("POST", "/invite/"+shells[0].OpenInviteToken, form.Encode(), nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "that email is already registered") {
		t.Errorf("claiming with a taken email: %d", w.Code)
	}
}

// TestNoUsernameOnPages renders the main pages a username used to show
// on (profile, members, admin, stats, songbooks, proposals) for someone
// who still has one in the database, and finds it on none of them.
func TestNoUsernameOnPages(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	handle := "zzhandle" + e.sfx
	meID, me := e.person("Mona")
	if err := e.st.SetAdmin(ctx, meID, true); err != nil {
		t.Fatal(err)
	}
	mateID, _ := e.person("Matt")
	setLegacyUsername(t, meID, handle)
	setLegacyUsername(t, mateID, handle+"mate")

	bandID, err := e.st.CreateBand(ctx, "Handle Band "+e.sfx, meID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddBandMember(ctx, bandID, mateID, "core", meID); err != nil {
		t.Fatal(err)
	}
	if v, err := e.st.CreateBandVariant(ctx, bandID, "Handle Song", "Someone", meID); err == nil {
		_ = e.st.AddBandCatalogEntry(ctx, bandID, v, meID)
	} else {
		t.Fatal(err)
	}
	communityID, err := e.st.CreateCommunity(ctx, "Handle Jam "+e.sfx, meID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddCommunityMember(ctx, communityID, mateID, "member", meID); err != nil {
		t.Fatal(err)
	}
	if v, err := e.st.CreateCommunityVariant(ctx, communityID, "Jam Song", "Someone", meID); err == nil {
		_ = e.st.AddCommunityCatalogEntry(ctx, communityID, v, meID)
	} else {
		t.Fatal(err)
	}
	eventID, err := e.st.CreateEvent(ctx, "community", communityID, "jam", "Handle Night", "", "", time.Now().Add(48*time.Hour), nil, "members", meID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.AddEventManager(ctx, eventID, mateID, meID); err != nil {
		t.Fatal(err)
	}

	pages := []string{
		"/home", "/profile", "/bands",
		fmt.Sprintf("/bands/%d", bandID), fmt.Sprintf("/bands/%d/members", bandID),
		fmt.Sprintf("/bands/%d/catalog", bandID), fmt.Sprintf("/bands/%d/catalog/vote", bandID),
		fmt.Sprintf("/communities/%d", communityID), fmt.Sprintf("/communities/%d/members", communityID),
		fmt.Sprintf("/communities/%d/admin", communityID), fmt.Sprintf("/communities/%d/songbooks/default", communityID),
		fmt.Sprintf("/communities/%d/admin/stats", communityID),
		fmt.Sprintf("/communities/%d/admin/stats/members/%d", communityID, mateID),
		fmt.Sprintf("/events/%d", eventID), fmt.Sprintf("/events/%d/manage", eventID),
		"/admin", fmt.Sprintf("/admin/people/%d", mateID),
	}
	for _, path := range pages {
		w := e.do("GET", path, "", me)
		if w.Code != http.StatusOK {
			t.Errorf("%s: %d", path, w.Code)
			continue
		}
		body := w.Body.String()
		if strings.Contains(body, handle) {
			t.Errorf("%s shows a username", path)
		}
		if strings.Contains(body, "@"+handle) || strings.Contains(body, ">Username<") {
			t.Errorf("%s shows an @name or a Username column", path)
		}
	}
	// The names are there instead.
	if page := e.do("GET", fmt.Sprintf("/bands/%d/catalog", bandID), "", me).Body.String(); !strings.Contains(page, "added by Mona") {
		t.Errorf("band catalog: no \"added by Mona\"")
	}
	if page := e.do("GET", fmt.Sprintf("/events/%d/manage", eventID), "", me).Body.String(); !strings.Contains(page, "<span>Matt</span>") {
		t.Errorf("event managers: Matt's name missing")
	}
	if page := e.do("GET", "/admin", "", me).Body.String(); !strings.Contains(page, "matt"+e.sfx+"@example.test") {
		t.Errorf("admin: the people list doesn't show emails")
	}
}
