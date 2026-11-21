package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	qrcode "github.com/skip2/go-qrcode"

	"app/internal/mailer"
	"app/internal/store"
)

// These run against a real Postgres, like internal/store's tests: point
// SONGBOOKS_TEST_DATABASE_URL at a scratch database (migrated on open).
// They make their own uniquely named people and touch nothing else.
func loginLinkTestServer(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()
	conn := os.Getenv("SONGBOOKS_TEST_DATABASE_URL")
	if conn == "" {
		conn = "postgres://localhost/songbooks_test?sslmode=disable"
	}
	st, err := store.New(context.Background(), conn)
	if err != nil {
		t.Skipf("no test database available at %s (set SONGBOOKS_TEST_DATABASE_URL to run these tests): %v", conn, err)
	}
	t.Cleanup(st.Close)
	return st, NewServer(st, mailer.NewConsoleMailer(st), nil, "", "")
}

type testPerson struct {
	ID      int64
	Name    string
	Email   string
	Session string
}

func newTestPerson(t *testing.T, st *store.Store, admin bool) testPerson {
	t.Helper()
	ctx := context.Background()
	b := make([]byte, 6)
	rand.Read(b)
	suffix := hex.EncodeToString(b)
	p := testPerson{Name: "Test " + suffix, Email: "ll-" + suffix + "@example.test"}
	id, err := st.CreatePerson(ctx, "ll-"+suffix, p.Name, p.Email)
	if err != nil {
		t.Fatal(err)
	}
	p.ID = id
	if admin {
		if err := st.SetAdmin(ctx, id, true); err != nil {
			t.Fatal(err)
		}
	}
	if p.Session, err = st.CreateSession(ctx, id); err != nil {
		t.Fatal(err)
	}
	return p
}

func do(t *testing.T, h http.Handler, as testPerson, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Host = "songbooks.example.test"
	if as.Session != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: as.Session})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAdminLoginLink(t *testing.T) {
	st, h := loginLinkTestServer(t)
	ctx := context.Background()
	admin := newTestPerson(t, st, true)
	member := newTestPerson(t, st, false)
	path := fmt.Sprintf("/api/admin/people/%d/login-link", member.ID)

	t.Run("admin gets a link for that person", func(t *testing.T) {
		rec := do(t, h, admin, "POST", path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		var got loginLinkResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(got.URL)
		if err != nil || u.Host != "songbooks.example.test" || u.Path != "/login/confirm" || u.Query().Get("token") != got.Token {
			t.Fatalf("link %q is not this server's /login/confirm?token=%s", got.URL, got.Token)
		}
		state, err := st.GetLoginTokenState(ctx, got.Token)
		if err != nil {
			t.Fatal(err)
		}
		if state.PersonID != member.ID || state.Used {
			t.Fatalf("token row: %+v, want person %d unused", state, member.ID)
		}
		if got.Name != member.Name || got.IsAdmin || !got.HasEmail || got.SecondsLeft != 900 {
			t.Errorf("response: %+v", got)
		}
		if !strings.HasPrefix(got.QRSVG, "<svg") || strings.Contains(got.QRSVG, got.Token) {
			t.Errorf("qrSvg should be an SVG drawing, not text: %.80q", got.QRSVG)
		}

		// Status: unused, then used once the link has logged someone in.
		statusBody := fmt.Sprintf(`{"token":%q}`, got.Token)
		rec = do(t, h, admin, "POST", path+"/status", statusBody)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"used":false`) {
			t.Fatalf("status before use: %d %s", rec.Code, rec.Body)
		}
		rec = do(t, h, testPerson{}, "GET", "/login/confirm?token="+url.QueryEscape(got.Token), "")
		if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Set-Cookie"), sessionCookie+"=") {
			t.Fatalf("the link didn't log in: %d %v", rec.Code, rec.Header())
		}
		rec = do(t, h, admin, "POST", path+"/status", statusBody)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"used":true`) {
			t.Fatalf("status after use: %d %s", rec.Code, rec.Body)
		}
		// Someone else's token isn't this person's link.
		other := newTestPerson(t, st, false)
		rec = do(t, h, admin, "POST", fmt.Sprintf("/api/admin/people/%d/login-link/status", other.ID), statusBody)
		if rec.Code != http.StatusNotFound {
			t.Errorf("status of another person's token: %d", rec.Code)
		}
	})

	t.Run("non-admin gets 403", func(t *testing.T) {
		for _, p := range []string{path, path + "/email", path + "/status"} {
			if rec := do(t, h, member, "POST", p, ""); rec.Code != http.StatusForbidden {
				t.Errorf("%s as a non-admin: %d", p, rec.Code)
			}
		}
		if rec := do(t, h, testPerson{}, "POST", path, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("logged out: %d", rec.Code)
		}
	})

	t.Run("unknown person 404, shell person 409", func(t *testing.T) {
		if rec := do(t, h, admin, "POST", "/api/admin/people/999999999/login-link", ""); rec.Code != http.StatusNotFound {
			t.Errorf("unknown person: %d", rec.Code)
		}
		shellID, err := st.CreateShellPerson(ctx, "Not Joined Yet")
		if err != nil {
			t.Fatal(err)
		}
		if rec := do(t, h, admin, "POST", fmt.Sprintf("/api/admin/people/%d/login-link", shellID), ""); rec.Code != http.StatusConflict {
			t.Errorf("shell person: %d", rec.Code)
		}
	})

	t.Run("cross-site request refused", func(t *testing.T) {
		req := httptest.NewRequest("POST", path, nil)
		req.Host = "songbooks.example.test"
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: admin.Session})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("cross-site: %d", rec.Code)
		}
	})

	t.Run("email goes through the mailer and ends the link on screen", func(t *testing.T) {
		rec := do(t, h, admin, "POST", path, "")
		var shown loginLinkResponse
		json.Unmarshal(rec.Body.Bytes(), &shown)

		rec = do(t, h, admin, "POST", path+"/email", fmt.Sprintf(`{"revoke":%q}`, shown.Token))
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		if state, _ := st.GetLoginTokenState(ctx, shown.Token); state == nil || !state.Used {
			t.Errorf("the link on screen should stop working: %+v", state)
		}
		out, err := st.ListOutbox(ctx, 50)
		if err != nil {
			t.Fatal(err)
		}
		var body string
		for _, e := range out {
			if e.To == member.Email {
				body = e.Body
				break
			}
		}
		if body == "" {
			t.Fatalf("no email to %s in the outbox", member.Email)
		}
		m := regexp.MustCompile(`/login/confirm\?token=([A-Za-z0-9_-]+)`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("no login link in the email: %q", body)
		}
		state, err := st.GetLoginTokenState(ctx, m[1])
		if err != nil || state.PersonID != member.ID || state.Used {
			t.Fatalf("emailed token: %+v %v", state, err)
		}

		// Nobody to email.
		noEmailID, err := st.CreatePerson(ctx, "ll-noemail-"+strconv.FormatInt(member.ID, 10), "No Email", "")
		if err != nil {
			t.Fatal(err)
		}
		if rec := do(t, h, admin, "POST", fmt.Sprintf("/api/admin/people/%d/login-link/email", noEmailID), ""); rec.Code != http.StatusConflict {
			t.Errorf("no email address: %d", rec.Code)
		}
	})
}

// The members pages show the button to a site admin only, and never on
// a row for someone who hasn't joined yet.
func TestLoginLinkButtonOnBandMembersPage(t *testing.T) {
	st, h := loginLinkTestServer(t)
	ctx := context.Background()
	admin := newTestPerson(t, st, true)
	member := newTestPerson(t, st, false)
	bandID, err := st.CreateBand(ctx, "Login Link Test Band", member.ID)
	if err != nil {
		t.Fatal(err)
	}
	shellID, err := st.CreateShellPerson(ctx, "Pending Person")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBandMember(ctx, bandID, shellID, "core", member.ID); err != nil {
		t.Fatal(err)
	}
	page := fmt.Sprintf("/bands/%d/members", bandID)

	rec := do(t, h, admin, "GET", page, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin: %d", rec.Code)
	}
	html := rec.Body.String()
	if !strings.Contains(html, fmt.Sprintf(`class="login-link-btn" data-person-id="%d"`, member.ID)) {
		t.Error("admin should see a Login link button on the member's row")
	}
	if strings.Contains(html, fmt.Sprintf(`class="login-link-btn" data-person-id="%d"`, shellID)) {
		t.Error("no Login link for someone who hasn't joined yet")
	}
	if !strings.Contains(html, "/static/js/login-link.js") {
		t.Error("admin page should load login-link.js")
	}

	rec = do(t, h, member, "GET", page, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("member: %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "login-link") {
		t.Error("a non-admin member must not see the Login link button or script")
	}
}

// qrSVG draws exactly go-qrcode's own bitmap: read the path back into a
// grid and compare, module by module.
func TestQRSVG(t *testing.T) {
	data := "https://songbooks.example.test/login/confirm?token=abcDEF123_-xyz"
	svg, err := qrSVG(data)
	if err != nil {
		t.Fatal(err)
	}
	q, err := qrcode.New(data, qrcode.Medium)
	if err != nil {
		t.Fatal(err)
	}
	want := q.Bitmap()
	n := len(want)
	if !strings.Contains(svg, fmt.Sprintf(`viewBox="0 0 %d %d"`, n, n)) {
		t.Fatalf("viewBox should be the %d-module bitmap: %.120s", n, svg)
	}
	got := make([][]bool, n)
	for i := range got {
		got[i] = make([]bool, n)
	}
	runs := regexp.MustCompile(`M(\d+) (\d+)h(\d+)v1h-(\d+)z`).FindAllStringSubmatch(svg, -1)
	if len(runs) == 0 {
		t.Fatal("no modules drawn")
	}
	for _, r := range runs {
		x, _ := strconv.Atoi(r[1])
		y, _ := strconv.Atoi(r[2])
		w, _ := strconv.Atoi(r[3])
		for i := x; i < x+w; i++ {
			got[y][i] = true
		}
	}
	for y := range want {
		for x := range want[y] {
			if got[y][x] != want[y][x] {
				t.Fatalf("module (%d,%d): drew %v, want %v", x, y, got[y][x], want[y][x])
			}
		}
	}
	// Quiet zone: the outer rows are white.
	for x := 0; x < n; x++ {
		if got[0][x] || got[n-1][x] {
			t.Fatal("the border should be blank")
		}
	}
}
