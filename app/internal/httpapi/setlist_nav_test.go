package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSetlistPages: a band's set list has one address, inside the band
// (the old one redirects there, with its click track); its views and its
// actions are apart; a new one is made in the band's Set lists section.
func TestSetlistPages(t *testing.T) {
	st, h := newTestServer(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	player, err := st.CreatePerson(ctx, "Pia Player", "pia@example.com")
	must(err)
	band, err := st.CreateBand(ctx, "Night Owls", player)
	must(err)
	sl, err := st.CreateSetlist(ctx, "band", "Saturday", "", nil, &band, nil, player)
	must(err)
	tok, err := st.CreateSession(ctx, player)
	must(err)
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: tok})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	canonical := fmt.Sprintf("/bands/%d/setlists/%d", band, sl)
	for from, to := range map[string]string{
		fmt.Sprintf("/setlists/%d", sl):            canonical,
		fmt.Sprintf("/setlists/%d?x=1", sl):        canonical + "?x=1",
		fmt.Sprintf("/setlists/%d/clicktrack", sl): canonical + "/clicktrack",
	} {
		w := get(from)
		if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != to {
			t.Errorf("GET %s: %d to %q, want 301 to %q", from, w.Code, w.Header().Get("Location"), to)
		}
	}
	w := get(canonical)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: %d", canonical, w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`class="setlist-views"`,
		`href="` + canonical + `" class="active" aria-current="page"`,
		`href="` + canonical + `/clicktrack"`,
		`id="reorder-toggle"`,
		fmt.Sprintf(`href="/setlists/%d/export-setlist-page.pdf"`, sl),
		fmt.Sprintf(`href="/setlists/%d/export.pdf"`, sl),
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the set list's page lacks %q", want)
		}
	}
	// Its exports are the toolbar's, not a sidebar group's.
	if strings.Contains(body, "sidebar-song-group") {
		t.Errorf("the set list's page still has a sidebar group of its own")
	}
	// Reorder is an action, not one of the views.
	views := body[strings.Index(body, `class="setlist-views"`):]
	views = views[:strings.Index(views, "</nav>")]
	if strings.Contains(views, "reorder") || strings.Contains(views, ".pdf") {
		t.Errorf("an action among the set list's views: %s", views)
	}

	w = get(fmt.Sprintf("/bands/%d/setlists", band))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `id="new-band-setlist-form"`) {
		t.Errorf("the band's Set lists section has no New set list (%d)", w.Code)
	}
}
