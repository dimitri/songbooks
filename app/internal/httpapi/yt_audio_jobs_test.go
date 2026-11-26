package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/internal/store"
	"app/internal/ytaudio"
)

// The YouTube reference's audio, fetched in the background
// (yt_audio_jobs.go), as the song page and its API show it: queued when
// the reference is set (PUT meta), the worker's progress, ready with its
// length, removed, failed with why; who may see the song sees where it
// is and opening the song queues it, only an editor asks again, removes
// it; no new try on opening within a day of a failure. YouTube is never
// reached: the fetch is the test's.
func TestYouTubeAudioAutomatic(t *testing.T) {
	st, h, sql := newTestServerSQL(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	owner, err := st.CreatePerson(ctx, "Yta Owner", "yta@example.com")
	must(err)
	reader, err := st.CreatePerson(ctx, "Yta Reader", "ytr@example.com")
	must(err)
	stranger, err := st.CreatePerson(ctx, "Yta Stranger", "yts@example.com")
	must(err)
	song, err := st.CreateVariant(ctx, store.OwnerPerson, owner, "Tune", "", owner)
	must(err)
	_, err = st.SaveVariantByID(ctx, song, "Tune", "", "", "title: Tune\nkey: G\n\n[Verse]\ngrid: | G | C |\n", "", owner)
	must(err)
	tokens := map[int64]string{}
	for _, p := range []int64{owner, reader, stranger} {
		tok, err := st.CreateSession(ctx, p)
		must(err)
		tokens[p] = tok
	}
	call := func(who int64, method, path string, body any) (int, []byte) {
		t.Helper()
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		r := httptest.NewRequest(method, path, rd)
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: tokens[who]})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	statusPath := fmt.Sprintf("/api/songs/%d/audio/youtube/status", song)
	status := func(who int64) ytAudioStatus {
		t.Helper()
		code, out := call(who, "GET", statusPath, nil)
		if code != http.StatusOK {
			t.Fatalf("status: %d %s", code, out)
		}
		var d ytAudioStatus
		must(json.Unmarshal(out, &d))
		return d
	}
	fetches := 0
	fetch := func(f func(ctx context.Context, url string, on func(ytaudio.Progress)) (*ytaudio.Result, error)) {
		YouTubeAudioFetch = func(ctx context.Context, url string, on func(ytaudio.Progress)) (*ytaudio.Result, error) {
			fetches++
			return f(ctx, url, on)
		}
	}
	t.Cleanup(func() { YouTubeAudioFetch = ytaudio.Fetch })
	work := func() {
		t.Helper()
		did, err := RunYouTubeAudioJob(ctx, st)
		if err != nil || !did {
			t.Fatalf("the worker: %v %v", did, err)
		}
	}

	// No reference: none; a stranger can't see a private song's.
	if d := status(owner); d.State != "none" || !d.CanEdit {
		t.Fatalf("no reference: %+v", d)
	}
	if code, _ := call(stranger, "GET", statusPath, nil); code != http.StatusForbidden {
		t.Errorf("a stranger reads the status: %d", code)
	}

	// The reference set (the picker's save, PUT meta): queued at once.
	const link = "https://www.youtube.com/watch?v=AAAAAAAAAAA"
	if code, out := call(owner, "PUT", fmt.Sprintf("/api/songs/%d/meta", song), map[string]string{"key": "reference", "value": link}); code != http.StatusOK {
		t.Fatalf("saving the reference: %d %s", code, out)
	}
	if d := status(owner); d.State != "queued" || d.Poll == 0 {
		t.Fatalf("set: %+v", d)
	}
	must(st.SetSongPublic(ctx, owner, song, true))
	if d := status(reader); d.State != "queued" || d.CanEdit {
		t.Fatalf("a reader: %+v", d)
	}
	// The melody waits for it.
	if code, out := call(reader, "GET", fmt.Sprintf("/api/songs/%d/melody/status", song), nil); code != http.StatusOK || !bytes.Contains(out, []byte(`"state":"audio"`)) {
		t.Errorf("the melody while the audio is fetched: %d %s", code, out)
	}

	// Fetched: the progress as the worker reports it, then ready with
	// its length; the split queued after it.
	fetch(func(_ context.Context, url string, on func(ytaudio.Progress)) (*ytaudio.Result, error) {
		if url != link {
			t.Errorf("fetched %q", url)
		}
		on(ytaudio.Progress{Fraction: 0.5, ETA: 7, Phase: "download"})
		if d := status(reader); d.State != "running" || d.Progress == nil || *d.Progress != 0.5 || d.ETA == nil || *d.ETA != 7 || d.Poll == 0 {
			t.Errorf("running: %+v", d)
		}
		return &ytaudio.Result{Data: []byte("ID3 the audio"), Duration: 222}, nil
	})
	work()
	d := status(reader)
	if d.State != "ready" || d.Duration == nil || *d.Duration != 222 || d.Poll != 0 {
		t.Fatalf("ready: %+v", d)
	}
	meta, err := st.GetSongAudioMeta(ctx, song, store.AudioYouTube)
	must(err)
	if meta.SourceURL != link {
		t.Errorf("stored from %q", meta.SourceURL)
	}
	if ss, err := st.GetStemStatus(ctx, meta.ID); err != nil || ss.Status != "queued" {
		t.Errorf("its split: %+v %v", ss, err)
	}

	// The page, under the video: the state, and an editor's ways.
	code, page := call(owner, "GET", fmt.Sprintf("/song/%d", song), nil)
	if code != http.StatusOK || !bytes.Contains(page, []byte(`data-yt-audio`)) || !bytes.Contains(page, []byte(`&#34;state&#34;:&#34;ready&#34;`)) || !bytes.Contains(page, []byte(`data-yt-remove`)) {
		t.Fatalf("the owner's page: %d", code)
	}
	if bytes.Contains(page, []byte(`audio-youtube-download-btn`)) {
		t.Error("the old download button is still there")
	}
	code, page = call(reader, "GET", fmt.Sprintf("/song/%d", song), nil)
	if code != http.StatusOK || !bytes.Contains(page, []byte(`data-yt-audio`)) || bytes.Contains(page, []byte(`data-yt-remove`)) || bytes.Contains(page, []byte(`data-yt-retry`)) {
		t.Fatalf("a reader's page: %d, an editor's buttons", code)
	}

	// Only an editor asks again or removes it.
	if code, _ := call(reader, "POST", fmt.Sprintf("/api/songs/%d/audio/youtube", song), map[string]string{}); code != http.StatusForbidden {
		t.Errorf("a reader asks again: %d", code)
	}
	if code, _ := call(reader, "DELETE", fmt.Sprintf("/api/songs/%d/audio/youtube", song), nil); code != http.StatusForbidden {
		t.Errorf("a reader removes it: %d", code)
	}
	// Removed by the owner: not fetched again when opened.
	if code, _ := call(owner, "DELETE", fmt.Sprintf("/api/songs/%d/audio/youtube", song), nil); code != http.StatusNoContent {
		t.Fatalf("removing: %d", code)
	}
	if d := status(owner); d.State != "removed" {
		t.Fatalf("removed: %+v", d)
	}
	call(reader, "GET", fmt.Sprintf("/song/%d", song), nil)
	if d := status(owner); d.State != "removed" {
		t.Fatalf("opened after removal: %+v", d)
	}

	// The owner asks for it again; YouTube says it's private now: failed,
	// with why, at once (not a failure that passes).
	code, out := call(owner, "POST", fmt.Sprintf("/api/songs/%d/audio/youtube", song), map[string]string{})
	if code != http.StatusAccepted || !bytes.Contains(out, []byte(`"state":"queued"`)) {
		t.Fatalf("asked again: %d %s", code, out)
	}
	fetch(func(context.Context, string, func(ytaudio.Progress)) (*ytaudio.Result, error) {
		return nil, &ytaudio.Error{Kind: ytaudio.KindUnavailable, Detail: "ERROR: Private video"}
	})
	work()
	if d := status(reader); d.State != "failed" || d.Reason != "unavailable" || d.CanEdit {
		t.Fatalf("failed, to a reader: %+v", d)
	}
	if d := status(owner); d.State != "failed" || !d.CanEdit {
		t.Fatalf("failed, to the owner: %+v", d)
	}

	// Opened within a day of the failure: not tried again; a day later,
	// by anyone who may open the song: tried again.
	n := fetches
	call(reader, "GET", fmt.Sprintf("/song/%d", song), nil)
	if did, err := RunYouTubeAudioJob(ctx, st); err != nil || did || fetches != n {
		t.Fatalf("tried again within a day: %v %v", did, err)
	}
	_, err = sql.Exec(ctx, `UPDATE song_youtube_audio_jobs SET finished_at = now() - interval '25 hours' WHERE variant_id = $1`, song)
	must(err)
	call(reader, "GET", fmt.Sprintf("/song/%d", song), nil)
	if d := status(reader); d.State != "queued" {
		t.Fatalf("opened a day later: %+v", d)
	}

	// Rate-limited: tried again later, the page saying when.
	fetch(func(context.Context, string, func(ytaudio.Progress)) (*ytaudio.Result, error) {
		return nil, &ytaudio.Error{Kind: ytaudio.KindBlocked, Transient: true, Detail: "HTTP Error 429"}
	})
	work()
	if d := status(owner); d.State != "queued" || d.Reason != "blocked" || d.RetryIn == nil || *d.RetryIn < 30 {
		t.Fatalf("rate-limited: %+v", d)
	}

	// An editor's request names a link that isn't a video: refused; a
	// song whose reference isn't one: nothing to fetch.
	if code, _ := call(owner, "POST", fmt.Sprintf("/api/songs/%d/audio/youtube", song), map[string]string{"url": "https://example.com/x"}); code != http.StatusBadRequest {
		t.Errorf("a page link: %d", code)
	}
	other, err := st.CreateVariant(ctx, store.OwnerPerson, owner, "Other", "", owner)
	must(err)
	if code, _ := call(owner, "POST", fmt.Sprintf("/api/songs/%d/audio/youtube", other), map[string]string{}); code != http.StatusConflict {
		t.Errorf("no video: %d", code)
	}
	// The import wizard's: a video named before the chart is saved.
	code, out = call(owner, "POST", fmt.Sprintf("/api/songs/%d/audio/youtube", other), map[string]string{"url": "https://youtu.be/BBBBBBBBBBB"})
	if code != http.StatusAccepted || !strings.Contains(string(out), `"queued"`) {
		t.Errorf("the wizard's video: %d %s", code, out)
	}
}

// ytAudioStatusOf, the states read from the job and the audio.
func TestYTAudioStatusOf(t *testing.T) {
	const a, b = "https://youtu.be/AAAAAAAAAAA", "https://youtu.be/BBBBBBBBBBB"
	p := 0.3
	cases := []struct {
		name  string
		s     store.YouTubeAudioState
		state string
	}{
		{"no reference", store.YouTubeAudioState{}, "none"},
		{"a video, nothing yet", store.YouTubeAudioState{Reference: a, IsVideo: true}, "missing"},
		{"running", store.YouTubeAudioState{Reference: a, IsVideo: true, Job: &store.YouTubeAudioJob{URL: a, Status: "running", Progress: &p}}, "running"},
		{"running over an older link's audio", store.YouTubeAudioState{Reference: b, IsVideo: true, HasAudio: true, AudioURL: a, Job: &store.YouTubeAudioJob{URL: b, Status: "running"}}, "running"},
		{"ready", store.YouTubeAudioState{Reference: a, IsVideo: true, HasAudio: true, AudioURL: a}, "ready"},
		{"fetched again, failed: the audio stays", store.YouTubeAudioState{Reference: a, IsVideo: true, HasAudio: true, AudioURL: a, Job: &store.YouTubeAudioJob{URL: a, Status: "failed", Reason: "blocked"}}, "ready"},
		{"a new link failed, the old one's audio", store.YouTubeAudioState{Reference: b, IsVideo: true, HasAudio: true, AudioURL: a, Job: &store.YouTubeAudioJob{URL: b, Status: "failed", Reason: "unavailable"}}, "failed"},
		{"removed", store.YouTubeAudioState{Reference: a, IsVideo: true, Job: &store.YouTubeAudioJob{URL: a, Status: "removed"}}, "removed"},
		{"an uploaded MP3 in its place", store.YouTubeAudioState{Reference: a, IsVideo: true, HasAudio: true}, "ready"},
	}
	for _, c := range cases {
		if got := ytAudioStatusOf(&c.s); got.State != c.state {
			t.Errorf("%s: %+v, want %s", c.name, got, c.state)
		}
	}
}
