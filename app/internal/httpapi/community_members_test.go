package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"app/internal/mailer"
	"app/internal/store"
)

// newTestServer is the whole app on a database of its own: a scratch
// database made next to SONGBOOKS_TEST_DATABASE_URL's (the store tests',
// which empty it as they run, possibly at the same time as these) and
// dropped at the end. Skipped without a test database.
func newTestServer(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()
	ctx := context.Background()
	connString := os.Getenv("SONGBOOKS_TEST_DATABASE_URL")
	if connString == "" {
		connString = "postgres://localhost/songbooks_test?sslmode=disable"
	}
	admin, err := pgx.Connect(ctx, connString)
	if err != nil {
		t.Skipf("no test database available at %s (set SONGBOOKS_TEST_DATABASE_URL to run these tests): %v", connString, err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	b := make([]byte, 6)
	rand.Read(b)
	name := "songbooks_httpapi_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Skipf("can't make a scratch database: %v", err)
	}
	t.Cleanup(func() {
		admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	u, err := url.Parse(connString)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	st, err := store.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st, NewServer(st, mailer.NewConsoleMailer(st), nil, "", "")
}

// TestCommunityMembersPage: the Members page and its setting, for each
// kind of visitor, with the list shown to admins only and to every
// member; and the admin actions, refused to whoever's role doesn't grant
// them.
func TestCommunityMembersPage(t *testing.T) {
	st, h := newTestServer(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	person := func(handle, name string) int64 {
		t.Helper()
		id, err := st.CreatePerson(ctx, name, handle+"@example.com")
		must(err)
		return id
	}
	siteAdmin := person("siteadmin", "Ada Admin")
	must(st.SetAdmin(ctx, siteAdmin, true))
	staff := person("staffuser", "Sam Staff")
	coordinator := person("coorduser", "Cleo Coordinator")
	member := person("memberuser", "Mia Member")
	outsider := person("outsideruser", "Otto Outsider")
	coach := person("coachuser", "Cora Coach")
	student := person("studentuser", "Stan Student")
	other := person("otheruser", "Olga Other")

	jam, err := st.CreateCommunity(ctx, "Jam", staff)
	must(err)
	_, err = st.CreateCommunityRole(ctx, jam, "Events coordinator", []string{store.PrivManageEvents})
	must(err)
	_, err = st.AddCommunityMember(ctx, jam, coordinator, "Events coordinator", staff)
	must(err)
	_, err = st.AddCommunityMember(ctx, jam, member, "member", staff)
	must(err)
	must(st.UpdateOwnInstruments(ctx, member, []string{"trombone"}, nil))
	shell, err := st.CreateShellPerson(ctx, "Carla Placeholder")
	must(err)
	_, err = st.AddCommunityMember(ctx, jam, shell, "member", staff)
	must(err)

	voice, err := st.CreateCommunity(ctx, "Voice", coach)
	must(err)
	must(st.SetCommunityKind(ctx, voice, "coaching"))
	for _, p := range []int64{student, other} {
		_, err = st.AddCommunityMember(ctx, voice, p, "member", coach)
		must(err)
	}
	// What the coach keeps about a student, never on this page.
	must(st.SetStudentNotes(ctx, voice, other, "PRIVATE-STUDENT-NOTES", coach))
	_, err = st.AddTeachingNote(ctx, voice, other, nil, "PRIVATE-TEACHING-NOTE", coach)
	must(err)

	tokens := map[int64]string{}
	for _, p := range []int64{siteAdmin, staff, coordinator, member, outsider, coach, student, other} {
		tok, err := st.CreateSession(ctx, p)
		must(err)
		tokens[p] = tok
	}
	const anonymous = int64(0)
	do := func(who int64, method, path, body string) (int, string) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if who != anonymous {
			r.AddCookie(&http.Cookie{Name: sessionCookie, Value: tokens[who]})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	membersPath := func(c int64) string { return fmt.Sprintf("/communities/%d/members", c) }
	membersLink := func(c int64) string { return fmt.Sprintf(`href="/communities/%d/members"`, c) }
	names := map[int64]string{siteAdmin: "site admin", staff: "staff", coordinator: "events coordinator", member: "member", outsider: "non-member", anonymous: "anonymous", coach: "coach", student: "student", other: "other student"}

	type want struct {
		code   int
		admin  bool // who hasn't joined yet
		manage bool // role selects, Remove
	}
	check := func(setting string, c int64, cases map[int64]want) {
		t.Helper()
		for who, w := range cases {
			code, body := do(who, "GET", membersPath(c), "")
			if code != w.code {
				t.Errorf("%s, %s: GET members %d, want %d", setting, names[who], code, w.code)
				continue
			}
			// The tab, on the community's overview.
			if who != anonymous && who != outsider {
				_, overview := do(who, "GET", fmt.Sprintf("/communities/%d", c), "")
				if strings.Contains(overview, membersLink(c)) != (w.code == http.StatusOK) {
					t.Errorf("%s, %s: Members tab shown: %v", setting, names[who], !(w.code == http.StatusOK))
				}
			}
			if code != http.StatusOK {
				continue
			}
			if !strings.Contains(body, "</html>") {
				t.Errorf("%s, %s: incomplete page", setting, names[who])
			}
			// No login handle or email for anyone, admins included
			// (migration 0135: there are no usernames).
			handle := map[int64]string{jam: "memberuser", voice: "otheruser"}[c]
			if strings.Contains(body, "("+handle+")") || strings.Contains(body, handle+"@example.com") {
				t.Errorf("%s, %s: a member's login handle or email shows", setting, names[who])
			}
			if strings.Contains(body, `class="roster-pending-badge"`) != (w.admin && c == jam) {
				t.Errorf("%s, %s: admin details shown: %v, want %v", setting, names[who], !w.admin, w.admin)
			}
			if strings.Contains(body, `class="member-role-select"`) != w.manage || strings.Contains(body, `class="member-remove-btn`) != w.manage {
				t.Errorf("%s, %s: role and remove controls shown: %v, want %v", setting, names[who], !w.manage, w.manage)
			}
			if strings.Contains(body, "PRIVATE-") {
				t.Errorf("%s, %s: a student's private notes on the page", setting, names[who])
			}
		}
	}

	// Admins only, the default.
	check("admins only", jam, map[int64]want{
		siteAdmin:   {200, true, true},
		staff:       {200, true, true},
		coordinator: {200, true, false},
		member:      {404, false, false},
		outsider:    {403, false, false},
		anonymous:   {302, false, false},
	})
	check("admins only", voice, map[int64]want{
		coach:   {200, true, true},
		student: {404, false, false},
	})

	// Who sees the list is a setting: its select on the Admin page, and
	// the Members page's link to it, only for a role granting
	// manage_settings (the events coordinator opens the Admin page for
	// its other sections).
	for who, settings := range map[int64]bool{siteAdmin: true, staff: true, coordinator: false} {
		code, admin := do(who, "GET", fmt.Sprintf("/communities/%d/admin", jam), "")
		if code != http.StatusOK {
			t.Errorf("%s: GET admin %d", names[who], code)
			continue
		}
		if strings.Contains(admin, `id="members-visibility-select"`) != settings {
			t.Errorf("%s: who-sees-the-list select shown: %v, want %v", names[who], !settings, settings)
		}
		if !strings.Contains(admin, membersLink(jam)) {
			t.Errorf("%s: no link from the Admin page to the Members page", names[who])
		}
		_, members := do(who, "GET", membersPath(jam), "")
		if strings.Contains(members, fmt.Sprintf(`href="/communities/%d/admin#members"`, jam)) != settings {
			t.Errorf("%s: Change who sees it link shown: %v, want %v", names[who], !settings, settings)
		}
	}

	// The setting: manage_settings only.
	for _, who := range []int64{member, coordinator, outsider, student} {
		if code, _ := do(who, "PUT", fmt.Sprintf("/api/communities/%d/members-visibility", jam), `{"visibility":"members"}`); code != http.StatusForbidden {
			t.Errorf("%s changed who sees the list: %d", names[who], code)
		}
	}
	if code, _ := do(anonymous, "PUT", fmt.Sprintf("/api/communities/%d/members-visibility", jam), `{"visibility":"members"}`); code != http.StatusUnauthorized {
		t.Errorf("anonymous changed who sees the list: %d", code)
	}
	if code, _ := do(staff, "PUT", fmt.Sprintf("/api/communities/%d/members-visibility", jam), `{"visibility":"everyone"}`); code != http.StatusBadRequest {
		t.Errorf("an unknown setting: %d", code)
	}
	if v, _ := st.CommunityMembersVisibility(ctx, jam); v != store.MembersVisibleToAdmins {
		t.Fatalf("the setting changed: %q", v)
	}
	if code, _ := do(staff, "PUT", fmt.Sprintf("/api/communities/%d/members-visibility", jam), `{"visibility":"members"}`); code != http.StatusOK {
		t.Fatalf("staff setting it: %d", code)
	}
	if code, _ := do(coach, "PUT", fmt.Sprintf("/api/communities/%d/members-visibility", voice), `{"visibility":"members"}`); code != http.StatusOK {
		t.Fatalf("coach setting it: %d", code)
	}

	// Every member.
	check("all members", jam, map[int64]want{
		siteAdmin:   {200, true, true},
		staff:       {200, true, true},
		coordinator: {200, true, false},
		member:      {200, false, false},
		outsider:    {403, false, false},
		anonymous:   {302, false, false},
	})
	check("all members", voice, map[int64]want{
		coach:   {200, true, true},
		student: {200, false, false},
	})
	_, body := do(member, "GET", membersPath(jam), "")
	for _, s := range []string{"Sam Staff", "Mia Member", "Carla Placeholder", "trombone", "Events coordinator"} {
		if !strings.Contains(body, s) {
			t.Errorf("member's view: missing %q", s)
		}
	}
	for _, s := range []string{"@example.com", "/admin#quick-add-form", "/invite/"} {
		if strings.Contains(body, s) {
			t.Errorf("member's view: %q shown", s)
		}
	}
	// A student sees the others by name, no way into their pages; the
	// coach gets there from the list.
	studentPage := fmt.Sprintf("/communities/%d/students/%d", voice, other)
	if _, body := do(student, "GET", membersPath(voice), ""); !strings.Contains(body, "Olga Other") || strings.Contains(body, studentPage) {
		t.Errorf("student's view: other student listed %v, linked %v", strings.Contains(body, "Olga Other"), strings.Contains(body, studentPage))
	}
	if code, _ := do(student, "GET", studentPage, ""); code != http.StatusNotFound {
		t.Errorf("a student opened another's page: %d", code)
	}
	if _, body := do(coach, "GET", membersPath(voice), ""); !strings.Contains(body, studentPage) {
		t.Error("coach's view: no link to the student's page")
	}

	// Admin actions stay with manage_members, whatever the setting.
	refused := []struct{ method, path, body string }{
		{"POST", fmt.Sprintf("/api/communities/%d/members/%d/role", jam, staff), `{"role":"member"}`},
		{"DELETE", fmt.Sprintf("/api/communities/%d/members/%d", jam, staff), ""},
		{"POST", fmt.Sprintf("/api/communities/%d/members", jam), `{"email":"outsideruser@example.com","role":"member"}`},
		{"POST", fmt.Sprintf("/api/communities/%d/personal-invites", jam), `{"displayName":"Eve","role":"member"}`},
		{"POST", fmt.Sprintf("/api/communities/%d/community-link", jam), `{"role":"staff"}`},
	}
	for _, who := range []int64{member, coordinator, outsider} {
		for _, a := range refused {
			if code, _ := do(who, a.method, a.path, a.body); code != http.StatusForbidden {
				t.Errorf("%s: %s %s: %d, want 403", names[who], a.method, a.path, code)
			}
		}
	}
	if code, _ := do(student, "DELETE", fmt.Sprintf("/api/communities/%d/members/%d", voice, other), ""); code != http.StatusForbidden {
		t.Errorf("a student removed another: %d", code)
	}
	if roster, _ := st.ListCommunityRoster(ctx, jam, false); len(roster) != 4 {
		t.Errorf("the roster changed: %d members", len(roster))
	}
	// And they work for staff.
	if code, _ := do(staff, "POST", fmt.Sprintf("/api/communities/%d/members/%d/role", jam, member), `{"role":"guest"}`); code != http.StatusOK {
		t.Errorf("staff changing a role: %d", code)
	}
}
