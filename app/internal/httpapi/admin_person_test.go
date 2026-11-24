package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"app/internal/store"
)

// TestAdminPersonPage: the site admin's page for one person — their
// state, their personal invite link while they haven't joined (a
// placeholder, or an account nobody ever got into), New link and Revoke,
// the page turning "active" once the link is claimed, the band's own
// roles in the role select, and nothing of it for anyone but a site admin.
func TestAdminPersonPage(t *testing.T) {
	st, h, conn := newTestServerSQL(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	admin := newTestPerson(t, st, true)
	member := newTestPerson(t, st, false)
	page := func(as testPerson, id int64) (int, string) {
		t.Helper()
		rec := do(t, h, as, "GET", fmt.Sprintf("/admin/people/%d", id), "")
		return rec.Code, rec.Body.String()
	}
	newLink := func(as testPerson, id int64) (int, string) {
		t.Helper()
		rec := do(t, h, as, "POST", fmt.Sprintf("/api/admin/people/%d/invite", id), "")
		var got struct{ URL string }
		json.Unmarshal(rec.Body.Bytes(), &got)
		_, token, _ := strings.Cut(got.URL, "/invite/")
		return rec.Code, token
	}
	revoke := func(as testPerson, id int64) int {
		t.Helper()
		return do(t, h, as, "DELETE", fmt.Sprintf("/api/admin/people/%d/invite", id), "").Code
	}

	shellID, err := st.CreateShellPerson(ctx, "Chloé Placeholder")
	must(err)
	neverID, err := st.CreatePerson(ctx, "Never Joined", "") // an import's name: no email, no password, never in
	must(err)

	t.Run("non-admins get 403", func(t *testing.T) {
		for _, c := range []struct{ method, path, body string }{
			{"GET", fmt.Sprintf("/admin/people/%d", shellID), ""},
			{"POST", fmt.Sprintf("/api/admin/people/%d/invite", shellID), ""},
			{"DELETE", fmt.Sprintf("/api/admin/people/%d/invite", shellID), ""},
			{"POST", fmt.Sprintf("/admin/people/%d/memberships", shellID), `{"kind":"band","targetID":1}`},
		} {
			if rec := do(t, h, member, c.method, c.path, c.body); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s as a member: %d, want 403", c.method, c.path, rec.Code)
			}
		}
	})

	t.Run("a placeholder gets a link, shown on the page", func(t *testing.T) {
		code, body := page(admin, shellID)
		if code != http.StatusOK || !strings.Contains(body, "hasn’t joined yet") || !strings.Contains(body, "No link yet.") {
			t.Fatalf("page before: %d, chip or 'No link yet.' missing", code)
		}
		code, token := newLink(admin, shellID)
		if code != http.StatusOK || token == "" {
			t.Fatalf("new link: %d", code)
		}
		_, body = page(admin, shellID)
		if !strings.Contains(body, "/invite/"+token) || !strings.Contains(body, "<svg") || !strings.Contains(body, "by "+admin.Name) {
			t.Errorf("page after: the link, its QR code or who made it is missing")
		}
	})

	t.Run("an account nobody ever got into gets a link and becomes a placeholder", func(t *testing.T) {
		_, body := page(admin, neverID)
		if !strings.Contains(body, "hasn’t joined yet") {
			t.Errorf("never-joined account isn't shown as not joined yet")
		}
		code, token := newLink(admin, neverID)
		if code != http.StatusOK || token == "" {
			t.Fatalf("new link: %d", code)
		}
		if shell, err := st.IsShellPerson(ctx, neverID); err != nil || !shell {
			t.Errorf("is_shell = %v (%v), want true", shell, err)
		}
		if _, err := st.GetPersonalInvite(ctx, token); err != nil {
			t.Errorf("the link doesn't resolve: %v", err)
		}
	})

	t.Run("someone with an account gets no invite link", func(t *testing.T) {
		if code, _ := newLink(admin, member.ID); code != http.StatusConflict {
			t.Errorf("new link for an account: %d, want 409", code)
		}
		// Ever logged in (a session), even with no email nor password: not "never joined".
		loggedID, err := st.CreatePerson(ctx, "Once In", "")
		must(err)
		_, err = st.CreateSession(ctx, loggedID)
		must(err)
		if code, _ := newLink(admin, loggedID); code != http.StatusConflict {
			t.Errorf("new link for an account that had a session: %d, want 409", code)
		}
		if _, err := st.CreatePersonalInvite(ctx, loggedID, admin.ID, time.Hour); !errors.Is(err, store.ErrNotShell) {
			t.Errorf("store: %v, want ErrNotShell", err)
		}
		_, body := page(admin, member.ID)
		if !strings.Contains(body, "login-link-btn") || strings.Contains(body, `id="person-invite-new"`) {
			t.Errorf("an account's page should offer a login link, not an invite link")
		}
	})

	t.Run("New link revokes the old one; Revoke ends it", func(t *testing.T) {
		_, first := newLink(admin, shellID)
		_, second := newLink(admin, shellID)
		if first == second {
			t.Fatal("same token twice")
		}
		if _, err := st.GetPersonalInvite(ctx, first); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("the old link still works: %v", err)
		}
		if _, err := st.GetPersonalInvite(ctx, second); err != nil {
			t.Errorf("the new link doesn't: %v", err)
		}
		if code := revoke(admin, shellID); code != http.StatusOK {
			t.Fatalf("revoke: %d", code)
		}
		if _, err := st.GetPersonalInvite(ctx, second); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("the revoked link still works: %v", err)
		}
		if code := revoke(admin, shellID); code != http.StatusNotFound {
			t.Errorf("revoking nothing: %d, want 404", code)
		}
		if _, body := page(admin, shellID); !strings.Contains(body, "No link yet.") {
			t.Errorf("page after revoke should say there's no link")
		}
	})

	t.Run("a claimed link turns the page to active", func(t *testing.T) {
		_, token := newLink(admin, shellID)
		if _, err := st.ClaimPersonalInvite(ctx, token, fmt.Sprintf("claimed-%d@example.test", shellID), "$2a$10$notarealhashbutnotempty"); err != nil {
			t.Fatal(err)
		}
		_, body := page(admin, shellID)
		if !strings.Contains(body, `person-chip is-active`) || strings.Contains(body, "hasn’t joined yet") {
			t.Errorf("claimed account isn't shown active")
		}
		if code, _ := newLink(admin, shellID); code != http.StatusConflict {
			t.Errorf("new invite link after claiming: %d, want 409", code)
		}
	})

	t.Run("the role select lists the band's own roles", func(t *testing.T) {
		bandID, err := st.CreateBand(ctx, "Admin Page Band", admin.ID)
		must(err)
		roleID, err := st.GroupRoles("band").Create(ctx, bandID, "Sound engineer", []string{store.BandPrivManageSetlists})
		must(err)
		// Added with the band's own role, from the page's add row.
		rec := do(t, h, admin, "POST", fmt.Sprintf("/admin/people/%d/memberships", member.ID),
			fmt.Sprintf(`{"kind":"band","targetID":%d,"roleId":%d}`, bandID, roleID))
		if rec.Code != http.StatusOK {
			t.Fatalf("add: %d %s", rec.Code, rec.Body)
		}
		_, body := page(admin, member.ID)
		if !strings.Contains(body, fmt.Sprintf(`<option value="%d" selected>Sound engineer</option>`, roleID)) {
			t.Errorf("the role select doesn't list the band's own role, selected")
		}
		// Back to core by id, then to the custom role again.
		roles, err := st.ListBandRoles(ctx, bandID)
		must(err)
		var coreID int64
		for _, r := range roles {
			if r.BuiltinKey == "core" {
				coreID = r.ID
			}
		}
		rec = do(t, h, admin, "POST", fmt.Sprintf("/admin/people/%d/memberships/band/%d/role", member.ID, bandID), fmt.Sprintf(`{"roleId":%d}`, coreID))
		if rec.Code != http.StatusOK {
			t.Fatalf("set role: %d %s", rec.Code, rec.Body)
		}
		ms, err := st.GetPersonMemberships(ctx, member.ID)
		must(err)
		for _, m := range ms {
			if m.Kind == "band" && m.ID == bandID && m.RoleID != coreID {
				t.Errorf("role after change: %d, want core %d", m.RoleID, coreID)
			}
		}
	})

	t.Run("the admin's invites list: personal rows link to the person, no nameless band rows", func(t *testing.T) {
		bandID, err := st.CreateBand(ctx, "Old Links Band", admin.ID)
		must(err)
		_, err = conn.Exec(ctx, `INSERT INTO invites (token, band_id, created_by_person_id, expires_at) VALUES ('nameless-leftover', $1, $2, now() + interval '3 days')`, bandID, admin.ID)
		must(err)
		rows, err := st.ListPlainInvites(ctx)
		must(err)
		for _, r := range rows {
			if r.Token == "nameless-leftover" {
				t.Errorf("a band's nameless one-off link is listed")
			}
		}
		pid, err := st.CreateShellPerson(ctx, "Listed Placeholder")
		must(err)
		_, err = st.CreatePersonalInvite(ctx, pid, admin.ID, time.Hour)
		must(err)
		rec := do(t, h, admin, "GET", "/admin", "")
		if !strings.Contains(rec.Body.String(), fmt.Sprintf(`personal: <a href="/admin/people/%d">Listed Placeholder</a>`, pid)) {
			t.Errorf("the personal invite's row doesn't link to the person page")
		}
	})
}
