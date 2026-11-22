package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"app/internal/store"
)

// TestNavNotifications: the sidebar's Notifications entry sits under
// Accueil on every page, with the unread count rendered by the server,
// unread only; below 901px the top bar's bell has the count and the
// Accueil tab a dot; the song page's toolbar has its bell with a dot.
func TestNavNotifications(t *testing.T) {
	st, h := newTestServer(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	me, err := st.CreatePerson(ctx, "me", "Mia Me", "me@example.com")
	must(err)
	other, err := st.CreatePerson(ctx, "other", "Otto Other", "other@example.com")
	must(err)
	var ids []int64
	for _, body := range []string{"one", "two", "three"} {
		id, err := st.CreateNotification(ctx, me, "test", body, "/home")
		must(err)
		ids = append(ids, id)
	}
	_, err = st.CreateNotification(ctx, other, "test", "not mine", "/home")
	must(err)
	must(st.MarkRead(ctx, ids[0], me))
	song, err := st.CreateVariant(ctx, store.OwnerPerson, me, "A song", "", me)
	must(err)

	tok, err := st.CreateSession(ctx, me)
	must(err)
	get := func(path string) string {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: tok})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, w.Code)
		}
		return w.Body.String()
	}
	// The sidebar's entry (with its count slot), right under Accueil;
	// the count is server-rendered.
	entry := regexp.MustCompile(`<a href="/notifications"[^>]*>`)
	const two = `data-nav-badge="notifications" data-unread-count>2</span>`
	songPath := fmt.Sprintf("/song/%d", song)
	for _, path := range []string{"/home", "/calendar", "/catalog", "/notifications", songPath} {
		body := get(path)
		sidebar := body[strings.Index(body, `class="sidebar-primary"`):]
		sidebar = sidebar[:strings.Index(sidebar, "</div>")]
		loc := entry.FindStringIndex(sidebar)
		if loc == nil {
			t.Errorf("%s: no Notifications entry in the sidebar", path)
			continue
		}
		// Right under Accueil: the entry before it is Accueil's.
		if home := strings.LastIndex(sidebar[:loc[0]], `<a href="`); home < 0 || !strings.HasPrefix(sidebar[home:], `<a href="/home"`) {
			t.Errorf("%s: the Notifications entry isn't right under Accueil", path)
		}
		if !strings.Contains(sidebar[loc[0]:], two) {
			t.Errorf("%s: no unread count of 2 (one of three is read, one is someone else's)", path)
		}
		if path == songPath {
			continue
		}
		// Below 901px: the bell in the top bar's slot, with the count; the
		// Accueil tab's dot; no bell left in the page's own header.
		if !strings.Contains(body, `data-topbar-slot="notifications"><a href="/notifications" class="notif-bell"`) || !strings.Contains(body, `class="notif-badge" data-unread-count>2</span>`) {
			t.Errorf("%s: the top bar's slot has no bell with the count", path)
		}
		bar := body[strings.Index(body, `id="global-tabbar"`):]
		bar = bar[:strings.Index(bar, "</nav>")]
		if !regexp.MustCompile(`<a href="/home"[^>]*>[^<]*<svg.*?</svg><span class="nav-unread-dot" data-unread-dot aria-hidden="true"></span></a>`).MatchString(bar) {
			t.Errorf("%s: the Accueil tab has no unread dot", path)
		}
		if n := strings.Count(body, `class="notif-bell"`); n != 1 {
			t.Errorf("%s: %d bells, want the top bar's only", path, n)
		}
	}
	// The song page has no bell of its own (the main navigation's stays).
	if body := get(songPath); strings.Contains(body, `id="song-toolbar-notifications"`) || strings.Contains(body, `class="nav-notifications-dot"`) {
		t.Error("the song page still has its own notifications bell")
	}

	// All read: the count goes.
	must(st.MarkAllRead(ctx, me))
	body := get("/home")
	if !strings.Contains(body, `class="nav-count" data-nav-badge="notifications" data-unread-count hidden></span>`) {
		t.Error("all read: the count should be hidden and empty")
	}
	if !strings.Contains(body, `class="nav-unread-dot" data-unread-dot aria-hidden="true" hidden>`) {
		t.Error("all read: the Accueil tab's dot should be hidden")
	}
}
