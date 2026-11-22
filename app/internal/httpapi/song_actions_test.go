package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/internal/store"
)

// The song page's actions: no stage mode and no bell of its own; the
// tuner and tap tempo in the ⋯ menu and the phone's Morceau sheet; tap
// tempo's "Use this tempo" for whoever can edit the song, saved to its
// "tempo:" header with a revision, refused to anyone else.
func TestSongActions(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	person := func(name string) *store.Person {
		t.Helper()
		id, err := st.CreatePerson(ctx, name, name, name+"@example.com")
		if err != nil {
			t.Fatal(err)
		}
		p, err := st.GetPersonByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	staff, guest := person("songstaff"), person("songguest")
	cid, err := st.CreateCommunity(ctx, "Studio Tempo", staff.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddCommunityMember(ctx, cid, guest.ID, "guest", staff.ID); err != nil {
		t.Fatal(err)
	}
	song, err := st.CreateCommunityVariant(ctx, cid, "Valerie", "Amy Winehouse", staff.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := st.CanViewVariant(ctx, guest.ID, song); err != nil || !ok {
		t.Fatalf("the guest should see the community's song: %v %v", ok, err)
	}
	if ok, err := st.CanEditVariant(ctx, guest.ID, song); err != nil || ok {
		t.Fatalf("a guest can't edit the community's song: %v %v", ok, err)
	}
	as := func(p *store.Person) func(method, path, body string) *httptest.ResponseRecorder {
		h := NewServer(st, nil, p, "", "")
		return func(method, path, body string) *httptest.ResponseRecorder {
			t.Helper()
			r := httptest.NewRequest(method, path, strings.NewReader(body))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			return w
		}
	}
	editor, reader := as(staff), as(guest)
	songPath := "/song/" + itoa(song)
	putTempo := func(do func(string, string, string) *httptest.ResponseRecorder, v string) *httptest.ResponseRecorder {
		return do("PUT", "/api/songs/"+itoa(song)+"/meta", `{"key":"tempo","value":"`+v+`"}`)
	}
	if w := putTempo(editor, "96"); w.Code != http.StatusOK {
		t.Fatalf("PUT tempo as the editor: %d %s", w.Code, w.Body)
	}

	// between returns the part of s from the element with id up to end.
	between := func(t *testing.T, s, id, end string) string {
		t.Helper()
		i := strings.Index(s, `id="`+id+`"`)
		if i < 0 {
			t.Fatalf("no #%s on the page", id)
		}
		j := strings.Index(s[i:], end)
		if j < 0 {
			t.Fatalf("#%s has no %q after it", id, end)
		}
		return s[i : i+j]
	}

	for _, c := range []struct {
		name    string
		do      func(string, string, string) *httptest.ResponseRecorder
		canEdit bool
	}{{"editor", editor, true}, {"guest", reader, false}} {
		w := c.do("GET", songPath, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: GET %s: %d", c.name, songPath, w.Code)
		}
		page := w.Body.String()
		for _, gone := range []string{"stage-mode-toggle", "stage.js", "Stage mode", "song-toolbar-notifications", "nav-notifications-dot"} {
			if strings.Contains(page, gone) {
				t.Errorf("%s: the song page still has %q", c.name, gone)
			}
		}
		// The toolbar's actions: Edit, Notes and ⋯; the tuner isn't one.
		toolbar := page[strings.Index(page, `class="song-actions"`):strings.Index(page, `id="song-more-menu"`)]
		if strings.Contains(toolbar, "data-tuner-open") || strings.Contains(toolbar, "data-tap-tempo-open") {
			t.Errorf("%s: the tuner or tap tempo is still a toolbar button", c.name)
		}
		menu := between(t, page, "song-more-menu", "</div>")
		sheet := between(t, page, "song-stack", "</div>")
		for where, html := range map[string]string{"the ⋯ menu": menu, "the Morceau sheet": sheet} {
			if !strings.Contains(html, `data-tuner-open`) || !strings.Contains(html, `href="/tuner?song=`+itoa(song)) {
				t.Errorf("%s: %s has no Tuner", c.name, where)
			}
			if !strings.Contains(html, `data-tap-tempo-open`) {
				t.Errorf("%s: %s has no Tap tempo", c.name, where)
			}
		}
		if tools := between(t, page, "tools-stack", "</div>"); !strings.Contains(tools, "data-tuner-open") {
			t.Errorf("%s: the Outils sheet has no tuner", c.name)
		}
		dialog := between(t, page, "tap-tempo-dialog", "</dialog>")
		if !strings.Contains(page, `id="tap-tempo-dialog" aria-labelledby="tap-tempo-title" data-variant-id="`+itoa(song)+`" data-song-tempo="96"`) {
			t.Errorf("%s: the tap tempo dialog lacks the song and its tempo", c.name)
		}
		if got := strings.Contains(dialog, `id="tap-tempo-use"`); got != c.canEdit {
			t.Errorf("%s: Use this tempo shown = %v, want %v", c.name, got, c.canEdit)
		}
		if !strings.Contains(page, `src="/static/js/tap-tempo.js`) {
			t.Errorf("%s: no tap-tempo.js", c.name)
		}
	}

	// A song with no tempo of its own compares with nothing, not 120.
	bare, err := st.CreateCommunityVariant(ctx, cid, "No Tempo", "", staff.ID)
	if err != nil {
		t.Fatal(err)
	}
	if page := editor("GET", "/song/"+itoa(bare), "").Body.String(); !strings.Contains(page, `data-song-tempo=""`) {
		t.Error("a song without a tempo: data-song-tempo should be empty")
	}

	// Saving the tapped tempo: only for whoever can edit the song.
	revisions := func() int {
		t.Helper()
		revs, err := st.ListRevisions(ctx, "variant", itoa(song))
		if err != nil {
			t.Fatal(err)
		}
		return len(revs)
	}
	before := revisions()
	if w := putTempo(reader, "132"); w.Code != http.StatusForbidden {
		t.Errorf("PUT tempo as a guest: %d, want 403", w.Code)
	}
	if body, _, err := st.LoadVariantRaw(song); err != nil || !strings.Contains(body, "tempo: 96") || strings.Contains(body, "132") {
		t.Errorf("a guest's tempo was saved: %v\n%s", err, body)
	}
	if revisions() != before {
		t.Error("a refused save recorded a revision")
	}
	if w := putTempo(editor, "132"); w.Code != http.StatusOK {
		t.Fatalf("PUT tempo as the editor: %d %s", w.Code, w.Body)
	}
	body, _, err := st.LoadVariantRaw(song)
	if err != nil || !strings.Contains(body, "tempo: 132") || strings.Contains(body, "tempo: 96") {
		t.Errorf("the editor's tempo wasn't saved: %v\n%s", err, body)
	}
	if revisions() != before+1 {
		t.Errorf("revisions: %d, want %d", revisions(), before+1)
	}
	if page := reader("GET", songPath, "").Body.String(); !strings.Contains(page, `data-song-tempo="132"`) {
		t.Error("the page doesn't show the saved tempo")
	}
}
