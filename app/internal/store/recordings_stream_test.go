package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app/internal/store"
)

// stubPCloud is a pCloud for the tests: getpublinkdownload for the
// folder "thecode" gives /dl/<file>, which serves file (Range included).
func stubPCloud(t *testing.T, file []byte) *int {
	t.Helper()
	asked := new(int)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/getpublinkdownload":
			*asked++
			if r.URL.Query().Get("code") != "thecode" {
				json.NewEncoder(w).Encode(map[string]any{"result": 7001, "error": "Invalid link 'code'."})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"result": 0, "hosts": []string{"c123.pcloud.com"}, "path": "/dl/" + r.URL.Query().Get("fileid") + ".mp3"})
		case strings.HasPrefix(r.URL.Path, "/dl/"):
			http.ServeContent(w, r, "take.mp3", time.Time{}, bytes.NewReader(file))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(stub.Close)
	store.StubPCloud(t, stub)
	return asked
}

// TestRecordingThroughTheApp: a band's recording plays through the app
// for who may hear it — its members; everyone once it's public — Range
// requests passed on; the band's folder link never in a page.
func TestRecordingThroughTheApp(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	file := make([]byte, 1000)
	for i := range file {
		file[i] = byte(i % 251)
	}
	asked := stubPCloud(t, file)
	rec := store.AddTestRecording(t, st, f.Band, f.BandSong, false)
	path := fmt.Sprintf("/recordings/%d/audio", rec)

	if w := as(t, st, f.Outsider).do("GET", path, nil); w.Code != http.StatusForbidden {
		t.Errorf("an outsider hearing the band's private song's recording: %d", w.Code)
	}
	if w := as(t, st, f.Former).do("GET", path, nil); w.Code != http.StatusForbidden {
		t.Errorf("a former member hearing it: %d", w.Code)
	}
	w := as(t, st, f.Guest).do("GET", path, nil)
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), file) || w.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("a member hearing it: %d, %d bytes, %q", w.Code, w.Body.Len(), w.Header().Get("Content-Type"))
	}
	if w.Header().Get("Accept-Ranges") != "bytes" || !strings.HasPrefix(w.Header().Get("Cache-Control"), "private") || w.Header().Get("ETag") == "" {
		t.Errorf("headers: %v", w.Header())
	}

	if err := st.SetSongPublic(ctx, f.Core, f.BandSong, true); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("Range", "bytes=10-19")
	w = as(t, st, f.Outsider).serve(r)
	if w.Code != http.StatusPartialContent || !bytes.Equal(w.Body.Bytes(), file[10:20]) || w.Header().Get("Content-Range") != "bytes 10-19/1000" {
		t.Fatalf("a Range of a public recording: %d %q %v", w.Code, w.Header().Get("Content-Range"), w.Body.Bytes())
	}
	if *asked != 1 {
		t.Errorf("pCloud asked %d times for the file's link (cached: once)", *asked)
	}
	r = httptest.NewRequest("GET", path, nil)
	r.Header.Set("If-None-Match", w.Header().Get("ETag"))
	if w := as(t, st, f.Outsider).serve(r); w.Code != http.StatusNotModified {
		t.Errorf("revalidating: %d", w.Code)
	}

	// The pages: the app's address, never the folder's link.
	if err := st.SetSongRecording(ctx, f.Band, f.BandSong, rec, f.Core); err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		who  int64
		path string
	}{
		{f.Outsider, fmt.Sprintf("/song/%d/recordings", f.BandSong)},
		{f.Core, fmt.Sprintf("/bands/%d/setlists/%d", f.Band, f.SetlistID)},
	} {
		html := as(t, st, p.who).page(p.path)
		if !strings.Contains(html, path) {
			t.Errorf("%s doesn't play the recording through the app", p.path)
		}
		if strings.Contains(html, "thecode") || strings.Contains(html, "eapi.pcloud.com") {
			t.Errorf("%s shows the band's folder link", p.path)
		}
	}
}

// TestDeleteKeepsPractice: deleting a song keeps the practice of it —
// on the frozen copy made for that person, else with its title — and
// deletes the notes on it.
func TestDeleteKeepsPractice(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	if err := st.SetSongPublic(ctx, f.Core, f.BandSong, true); err != nil {
		t.Fatal(err)
	}
	outBook, _ := st.GetDefaultSongbook(ctx, "person", f.Outsider)
	if err := st.AddToSongbook(ctx, outBook, f.BandSong, f.Outsider); err != nil {
		t.Fatal(err)
	}
	store.AddTestPractice(t, st, f.Outsider, f.BandSong, 600)
	fan, err := st.CreatePerson(ctx, "Fan", "fan@example.com")
	if err != nil {
		t.Fatal(err)
	}
	fanCommunity := store.AddTestPractice(t, st, fan, f.BandSong, 1200)
	if _, err := st.AnnotateVariant(ctx, f.BandSong, fan, "watch the bridge"); err != nil {
		t.Fatal(err)
	}

	if w := as(t, st, f.Core).do("DELETE", fmt.Sprintf("/api/songs/%d", f.BandSong), nil); w.Code != http.StatusOK {
		t.Fatalf("deleting: %d %s", w.Code, w.Body.String())
	}
	ids := store.SongbookSongIDs(t, st, outBook)
	if len(ids) != 1 {
		t.Fatalf("the outsider's songbook: %v", ids)
	}
	logs, _, takes := store.PracticeOf(t, st, f.Outsider)
	if len(logs) != 1 || logs[0] != ids[0] || len(takes) != 1 || takes[0] != ids[0] {
		t.Errorf("the outsider's practice: logs %v, takes %v (want the frozen copy %d)", logs, takes, ids[0])
	}
	logs, titles, takes := store.PracticeOf(t, st, fan)
	if len(logs) != 1 || logs[0] != 0 || titles[0] != "Night Drive" || len(takes) != 0 {
		t.Errorf("the fan's practice: logs %v %v, takes %v", logs, titles, takes)
	}
	l, err := st.ListPracticeLogs(ctx, fanCommunity, fan, time.Now().AddDate(0, 0, -7))
	if err != nil || len(l) != 1 || l[0].Label != "Night Drive" || l[0].Minutes() != 20 || l[0].PitchTakes != 1 {
		t.Errorf("the fan's history: %+v %v", l, err)
	}
	if n := store.AnnotationsBy(t, st, fan); n != 0 {
		t.Errorf("%d notes kept on a deleted song", n)
	}
}

// serve is r's answer.
func (c client) serve(r *http.Request) *httptest.ResponseRecorder {
	c.t.Helper()
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	return w
}
