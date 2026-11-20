package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/internal/store"
)

// TestNavNotifications: the sidebar's Notifications entry sits under
// Accueil on every page, with the unread count rendered by the server,
// unread only; the song page has its bell with a dot.
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
	const entry = `<a class="nav-notifications" href="/notifications"`
	const two = `data-unread-count>2</span>`
	for _, path := range []string{"/home", "/calendar", "/catalog", "/notifications", fmt.Sprintf("/song/%d", song)} {
		body := get(path)
		i := strings.Index(body, entry)
		if i < 0 {
			t.Errorf("%s: no Notifications entry in the sidebar", path)
			continue
		}
		// Right under Accueil.
		if home := strings.LastIndex(body[:i], `href="/home"`); home < 0 || strings.Contains(body[home:i], `href="/calendar"`) {
			t.Errorf("%s: the Notifications entry isn't right under Accueil", path)
		}
		if !strings.Contains(body[i:], two) {
			t.Errorf("%s: no unread count of 2 (one of three is read, one is someone else's)", path)
		}
	}
	if body := get(fmt.Sprintf("/song/%d", song)); !strings.Contains(body, `class="nav-notifications-dot"`) {
		t.Error("the song page has no notifications bell")
	}

	// All read: the count goes.
	must(st.MarkAllRead(ctx, me))
	if body := get("/home"); !strings.Contains(body, `class="nav-count" data-unread-count hidden></span>`) {
		t.Error("all read: the count should be hidden and empty")
	}
}
