package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app/internal/reftitle"
	"app/internal/store"
)

// A song's reference gets its video's title: at once when the link is
// saved (the answer carries it, the page shows it in the header and the
// Reference view), in the background for a link saved before — the page
// that asks shows the site and doesn't wait, the next one the title.
func TestReferenceTitle(t *testing.T) {
	st, _ := newTestServer(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	oembed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		titles := map[string]string{"jNQXAC9IVRw": "Me at the zoo", "dQw4w9WgXcQ": "A video saved before"}
		for id, title := range titles {
			if strings.Contains(r.URL.Query().Get("url"), id) {
				json.NewEncoder(w).Encode(map[string]string{"title": title})
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer oembed.Close()
	fetcher := &reftitle.Fetcher{Client: oembed.Client(), Providers: map[string]string{"youtube.com": oembed.URL + "/oembed?format=json"}}
	t.Cleanup(func() { RefTitles = nil })

	me, err := st.CreatePerson(ctx, "reftitler", "Rita Ref", "rita@example.com")
	must(err)
	p, err := st.GetPersonByID(ctx, me)
	must(err)
	h := NewServer(st, nil, p, "", "")
	song, err := st.CreateVariant(ctx, store.OwnerPerson, me, "Valerie", "Amy Winehouse", me)
	must(err)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	put := func(id int64, link string) *httptest.ResponseRecorder {
		return do("PUT", "/api/songs/"+itoa(id)+"/meta", `{"key":"reference","value":"`+link+`"}`)
	}

	// Saved: the title comes back at once and is kept.
	RefTitles = fetcher
	w := put(song, "https://www.youtube.com/watch?v=jNQXAC9IVRw")
	var out map[string]string
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil || out["referenceTitle"] != "Me at the zoo" {
		t.Fatalf("PUT reference: %d %s", w.Code, w.Body)
	}
	page := do("GET", "/song/"+itoa(song), "").Body.String()
	for _, want := range []string{`id="topbar-ref-name" data-title="Me at the zoo" title="Me at the zoo">Me at the zoo<`, `data-default-label="reference">Me at the zoo &#8599;</a>`} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	// A link without a title: none, the site instead; cleared: nothing.
	if w := put(song, "https://www.youtube.com/watch?v=unknown1234"); !strings.Contains(w.Body.String(), `"referenceTitle":""`) {
		t.Errorf("no title: %s", w.Body)
	}
	if page := do("GET", "/song/"+itoa(song), "").Body.String(); !strings.Contains(page, `id="topbar-ref-name" data-title="">YouTube<`) || strings.Contains(page, "Me at the zoo") {
		t.Errorf("a link without a title shows the site")
	}
	if w := put(song, ""); strings.Contains(w.Body.String(), "referenceTitle") {
		t.Errorf("cleared: %s", w.Body)
	}

	// Saved before titles were fetched: the page doesn't wait, then the
	// title is there.
	RefTitles = nil
	old, err := st.CreateVariant(ctx, store.OwnerPerson, me, "Rehab", "Amy Winehouse", me)
	must(err)
	if w := put(old, "https://youtu.be/dQw4w9WgXcQ"); w.Code != http.StatusOK || strings.Contains(w.Body.String(), "referenceTitle\":\"A") {
		t.Fatalf("PUT without a fetcher: %d %s", w.Code, w.Body)
	}
	fetcher.Providers["youtu.be"] = oembed.URL + "/oembed?format=json"
	RefTitles = fetcher
	if page := do("GET", "/song/"+itoa(old), "").Body.String(); !strings.Contains(page, `data-title="">YouTube<`) {
		t.Errorf("first visit: the site")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		rt, _ := st.ReferenceTitle(ctx, old)
		if rt.For("https://youtu.be/dQw4w9WgXcQ") == "A video saved before" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the background fetch: %+v", rt)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if page := do("GET", "/song/"+itoa(old), "").Body.String(); !strings.Contains(page, `>A video saved before<`) {
		t.Errorf("next visit: the title")
	}
}
