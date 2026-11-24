package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/audioanalysis"
	"app/internal/mailer"
	"app/internal/store"
)

// newTestServerSQL is newTestServer with a connection of its own to the
// scratch database, to set a job's clock as a worker would have.
func newTestServerSQL(t *testing.T) (*store.Store, http.Handler, *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	connString := os.Getenv("SONGBOOKS_TEST_DATABASE_URL")
	if connString == "" {
		connString = "postgres://localhost/songbooks_test?sslmode=disable"
	}
	admin, err := pgx.Connect(ctx, connString)
	if err != nil {
		t.Skipf("no test database available at %s: %v", connString, err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	b := make([]byte, 6)
	rand.Read(b)
	name := "songbooks_httpapi_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Skipf("can't make a scratch database: %v", err)
	}
	t.Cleanup(func() { admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)") })
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
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	return st, NewServer(st, mailer.NewConsoleMailer(st), nil, "", ""), conn
}

// TestMelodyStatus: the Melody view's state until it can be drawn, as the
// API says it (GET /api/songs/{id}/melody/status), to who may see the
// song: no recording; no stems worker on the server; queued behind
// another recording; running, with the worker's progress; failed, with a
// retry for an editor only; the notes being found; no bars; ready. The
// song page always has the view, its tab dimmed until then.
func TestMelodyStatus(t *testing.T) {
	st, h, sql := newTestServerSQL(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	owner, err := st.CreatePerson(ctx, "statusowner", "Sta Owner", "sta@example.com")
	must(err)
	reader, err := st.CreatePerson(ctx, "statusreader", "Rea Der", "rea@example.com")
	must(err)
	stranger, err := st.CreatePerson(ctx, "statusstranger", "Str Anger", "str@example.com")
	must(err)
	song, err := st.CreateVariant(ctx, store.OwnerPerson, owner, "Tune", "", owner)
	must(err)
	body := "title: Tune\nkey: G\ntempo: 120\ntime: 4/4\n\n[Verse]\ngrid: | G | C | D | G |\nwalking in the rain\nunder the sky\n"
	_, err = st.SaveVariantByID(ctx, song, "Tune", "", "", body, "", owner)
	must(err)
	tokens := map[int64]string{}
	for _, p := range []int64{owner, reader, stranger} {
		tok, err := st.CreateSession(ctx, p)
		must(err)
		tokens[p] = tok
	}
	call := func(who int64, method, path string) (int, []byte) {
		t.Helper()
		r := httptest.NewRequest(method, path, nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: tokens[who]})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	status := func(who int64) melodyStatusResponse {
		t.Helper()
		code, out := call(who, "GET", fmt.Sprintf("/api/songs/%d/melody/status", song))
		if code != http.StatusOK {
			t.Fatalf("status: %d %s", code, out)
		}
		var d melodyStatusResponse
		must(json.Unmarshal(out, &d))
		if d.Message == "" && d.State != "ready" {
			t.Errorf("%s: no message", d.State)
		}
		return d
	}
	// The same rights as the song: a stranger can't see a private song's.
	if code, _ := call(stranger, "GET", fmt.Sprintf("/api/songs/%d/melody/status", song)); code != http.StatusForbidden {
		t.Errorf("a stranger reads a private song's melody status: %d", code)
	}
	if d := status(owner); d.State != "none" || d.Poll != 0 {
		t.Fatalf("no recording: %+v", d)
	}
	// The song page: the view is there anyway, its tab dimmed.
	code, page := call(owner, "GET", fmt.Sprintf("/song/%d", song))
	if code != http.StatusOK || !bytes.Contains(page, []byte(`id="melody-tab"`)) || !bytes.Contains(page, []byte(`class="is-unready"`)) || !bytes.Contains(page, []byte(`data-status="{&#34;state&#34;:&#34;none&#34;`)) {
		t.Errorf("song page without a recording: %d, the Melody tab missing or not dimmed", code)
	}

	// A recording, queued by the database; no worker ever seen here.
	audio, err := st.SetSongAudio(ctx, song, store.AudioYouTube, []byte(strings.Repeat("m", 2e6)), "audio/mpeg", "sha1", "", owner)
	must(err)
	if d := status(owner); d.State != "unavailable" || d.Poll != 0 {
		t.Fatalf("no worker: %+v", d)
	}
	// A worker, and another song's recording queued before it.
	must(st.NoteStemWorker(ctx, "htdemucs"))
	other, err := st.CreateVariant(ctx, store.OwnerPerson, owner, "Other", "", owner)
	must(err)
	otherAudio, err := st.SetSongAudio(ctx, other, store.AudioYouTube, []byte(strings.Repeat("o", 3e6)), "audio/mpeg", "sha2", "", owner)
	must(err)
	_, err = sql.Exec(ctx, `UPDATE song_audio_stem_jobs SET queued_at = now() - interval '1 minute' WHERE song_audio_id = $1`, otherAudio)
	must(err)
	if d := status(owner); d.State != "queued" || d.Position != 1 || d.ETA != nil || d.Poll == 0 {
		t.Fatalf("queued behind one, no speed known: %+v", d)
	}
	// The worker's speed on its recent splits: 50 s a MB.
	prev, err := st.CreateVariant(ctx, store.OwnerPerson, owner, "Done", "", owner)
	must(err)
	prevAudio, err := st.SetSongAudio(ctx, prev, store.AudioYouTube, []byte(strings.Repeat("d", 4e6)), "audio/mpeg", "sha3", "", owner)
	must(err)
	_, err = sql.Exec(ctx, `UPDATE song_audio_stem_jobs SET status = 'done', started_at = now() - interval '300 seconds', finished_at = now() - interval '100 seconds' WHERE song_audio_id = $1`, prevAudio)
	must(err)
	d := status(owner)
	if d.State != "queued" || d.Position != 1 || d.ETA == nil || d.ETASource != "estimate" || *d.ETA < 240 || *d.ETA > 270 {
		t.Fatalf("queued, estimated (3 MB + 2 MB at 50 s a MB, and the notes): %+v", d)
	}
	// The other one taken, then ours: running.
	j1, err := st.ClaimStemJob(ctx, "htdemucs")
	must(err)
	if j1.SongAudioID != otherAudio {
		t.Fatalf("claimed %d first, want %d", j1.SongAudioID, otherAudio)
	}
	if d := status(owner); d.State != "queued" || d.Position != 1 {
		t.Fatalf("queued behind a running one: %+v", d)
	}
	_, err = st.SaveStems(ctx, *j1, "audio/mpeg", map[string][]byte{"vocals": []byte("ov")})
	must(err)
	if d := status(owner); d.State != "queued" || d.Position != 0 || !strings.Contains(d.Message, "next in line") {
		t.Fatalf("next: %+v", d)
	}
	job, err := st.ClaimStemJob(ctx, "htdemucs")
	must(err)
	if d := status(owner); d.State != "running" || d.ETASource != "estimate" || d.Progress == nil || d.Poll == 0 {
		t.Fatalf("running, no report yet: %+v", d)
	}
	// The worker reports Demucs's progress: 50 % after 60 s.
	must(st.SetStemJobProgress(ctx, *job, 0.5))
	_, err = sql.Exec(ctx, `UPDATE song_audio_stem_jobs SET started_at = now() - interval '60 seconds' WHERE song_audio_id = $1`, audio)
	must(err)
	d = status(owner)
	if d.State != "running" || d.Progress == nil || *d.Progress != 0.5 || d.ETASource != "worker" || d.ETA == nil || *d.ETA < 60 || *d.ETA > 90 {
		t.Fatalf("running at 50%%: %+v", d)
	}
	if !strings.Contains(d.Message, "50%") {
		t.Errorf("message %q", d.Message)
	}
	// Failed three times: for good, retried by an editor only.
	must(st.FailStemJob(ctx, *job, "demucs: killed"))
	for range 2 {
		j, err := st.ClaimStemJob(ctx, "htdemucs")
		must(err)
		must(st.FailStemJob(ctx, *j, "demucs: killed"))
	}
	if d := status(owner); d.State != "failed" || !d.CanRetry || d.Poll != 0 {
		t.Fatalf("failed: %+v", d)
	}
	must(st.SetSongPublic(ctx, owner, song, true))
	if d := status(reader); d.State != "failed" || d.CanRetry {
		t.Fatalf("failed, to a reader: %+v", d)
	}
	if code, _ := call(reader, "POST", fmt.Sprintf("/api/songs/%d/audio/youtube/stems", song)); code != http.StatusForbidden {
		t.Errorf("a reader queues the split again: %d", code)
	}
	if code, _ := call(owner, "POST", fmt.Sprintf("/api/songs/%d/audio/youtube/stems", song)); code != http.StatusOK {
		t.Errorf("the owner queues the split again: %d", code)
	}
	if d := status(owner); d.State != "queued" {
		t.Fatalf("queued again: %+v", d)
	}
	// Split: the notes are being found; then there are notes but no bars.
	job, err = st.ClaimStemJob(ctx, "htdemucs")
	must(err)
	_, err = st.SaveStems(ctx, *job, "audio/mpeg", map[string][]byte{"vocals": []byte("v1")})
	must(err)
	if d := status(owner); d.State != "notes" || d.Poll == 0 {
		t.Fatalf("notes: %+v", d)
	}
	var notes []audioanalysis.SungNote
	for i, p := range []float64{67, 69, 71, 72, 74, 72, 71, 69} {
		notes = append(notes, audioanalysis.SungNote{Start: 10 + 0.5*float64(i), End: 10.45 + 0.5*float64(i), Pitch: p, Conf: 0.9})
	}
	_, err = findMelodyNotes(ctx, st, func(context.Context, []byte) (audioanalysis.MelodyNotes, bool, error) {
		return audioanalysis.MelodyNotes{Notes: notes, Voiced: 4}, true, nil
	})
	must(err)
	// No sync, the recording's analysis queued and a worker there: it'll
	// come; no worker: save a sync.
	if d := status(owner); d.State != "analysis" || d.Poll == 0 {
		t.Fatalf("no bars, the analysis to come: %+v", d)
	}
	_, err = sql.Exec(ctx, `UPDATE stem_workers SET seen_at = now() - interval '1 hour'`)
	must(err)
	if d := status(owner); d.State != "sync" || d.Poll != 0 {
		t.Fatalf("no bars: %+v", d)
	}
	must(st.ReplaceAudioMarkers(ctx, audio, []store.AudioMarker{{SectionIndex: 0, TimeSeconds: 10}}, owner))
	if d := status(owner); d.State != "ready" || d.Poll != 0 {
		t.Fatalf("ready: %+v", d)
	}
	code, page = call(owner, "GET", fmt.Sprintf("/song/%d", song))
	if code != http.StatusOK || bytes.Contains(page, []byte(`class="is-unready"`)) || !bytes.Contains(page, []byte(`id="melody-tab"`)) {
		t.Errorf("song page, ready: %d, the tab dimmed or missing", code)
	}
}

// TestMelodyProgress: the waits as melodyProgress works them out, on a
// fixed clock.
func TestMelodyProgress(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	half := 0.5
	base := store.MelodyStatus{Category: store.AudioYouTube, ByteSize: 4e6, SecondsPerMB: 50, NotesSeconds: 2, WorkerSeen: now.Add(-time.Minute), Now: now}
	cases := []struct {
		name  string
		edit  func(*store.MelodyStatus)
		state string
		eta   int
		src   string
	}{
		{"queued, a 6 MB one ahead and one running at half after 100 s", func(m *store.MelodyStatus) {
			m.Job, m.Ahead, m.AheadBytes = "queued", 2, 6e6
			m.AheadRunning = []store.RunningSplit{{Started: now.Add(-100 * time.Second), ByteSize: 4e6, Progress: &half}}
		}, "queued", 300 + 200 + 2 + 100 + 16, "estimate"},
		{"running, no report: 200 s estimated, 50 s in", func(m *store.MelodyStatus) {
			m.Job, m.Started = "running", now.Add(-50*time.Second)
		}, "running", 150 + 2, "estimate"},
		{"running at half after 100 s", func(m *store.MelodyStatus) {
			m.Job, m.Started, m.Progress = "running", now.Add(-100*time.Second), &half
		}, "running", 100 + 16 + 2, "worker"},
		{"no worker seen for an hour", func(m *store.MelodyStatus) {
			m.Job, m.WorkerSeen = "queued", now.Add(-time.Hour)
		}, "unavailable", 0, ""},
		{"a run of the last half hour keeps it alive", func(m *store.MelodyStatus) {
			m.Job, m.WorkerSeen, m.Started = "running", time.Time{}, now.Add(-20*time.Minute)
		}, "running", 15 + 2, "estimate"},
		{"failed, even with no worker", func(m *store.MelodyStatus) {
			m.Job, m.WorkerSeen = "failed", time.Time{}
		}, "failed", 0, ""},
		{"split, 2 found before it", func(m *store.MelodyStatus) {
			m.Split, m.NotesAhead = true, 2
		}, "notes", 6, "estimate"},
		{"found, too little singing", func(m *store.MelodyStatus) { m.Split, m.Found = true, true }, "unsung", 0, ""},
		{"found", func(m *store.MelodyStatus) { m.Split, m.Found, m.OK = true, true, true }, "ready", 0, ""},
	}
	for _, c := range cases {
		m := base
		c.edit(&m)
		r := melodyProgress(&m)
		eta := 0
		if r.ETA != nil {
			eta = *r.ETA
		}
		if r.State != c.state || eta != c.eta || r.ETASource != c.src {
			t.Errorf("%s: %s, eta %d (%s); want %s, %d (%s)", c.name, r.State, eta, r.ETASource, c.state, c.eta, c.src)
		}
		if melodyStateDone[r.State] != (r.Poll == 0) {
			t.Errorf("%s: %s polled every %d s", c.name, r.State, r.Poll)
		}
	}
	if r := melodyProgress(nil); r.State != "none" {
		t.Errorf("no recording: %s", r.State)
	}
	// Said in French too.
	m := base
	m.Job, m.Started, m.Progress = "running", now.Add(-100*time.Second), &half
	if msg := melodyMessage("fr", melodyProgress(&m)); !strings.Contains(msg, "séparation de la voix") || !strings.Contains(msg, "50") || !strings.Contains(msg, "Encore environ 2") {
		t.Errorf("French: %q", msg)
	}
}

// TestMelodyKeySignature: the sheet's key signature is the chart's key:
// at the page's key; without one, the key the melody and the chords
// point to (said so); an editor, and no one else, is told when the
// chart's key and the melody disagree.
func TestMelodyKeySignature(t *testing.T) {
	st, h := newTestServer(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	owner, err := st.CreatePerson(ctx, "keyowner", "Key Owner", "key@example.com")
	must(err)
	reader, err := st.CreatePerson(ctx, "keyreader", "Key Reader", "keyr@example.com")
	must(err)
	song, err := st.CreateVariant(ctx, store.OwnerPerson, owner, "Tune", "", owner)
	must(err)
	audio, err := st.SetSongAudio(ctx, song, store.AudioYouTube, []byte("mp3"), "audio/mpeg", "sha1", "", owner)
	must(err)
	job, err := st.ClaimStemJob(ctx, "test")
	must(err)
	_, err = st.SaveStems(ctx, *job, "audio/mpeg", map[string][]byte{"vocals": []byte("v")})
	must(err)
	// E minor: the melody around E, G and B, the chords Em, C, D, Em.
	var notes []audioanalysis.SungNote
	for i, p := range []float64{64, 67, 71, 69, 67, 66, 64, 62, 64, 67, 66, 64, 71, 69, 67, 64} {
		notes = append(notes, audioanalysis.SungNote{Start: 10 + 0.5*float64(i), End: 10.45 + 0.5*float64(i), Pitch: p, Conf: 0.9})
	}
	_, err = findMelodyNotes(ctx, st, func(context.Context, []byte) (audioanalysis.MelodyNotes, bool, error) {
		return audioanalysis.MelodyNotes{Notes: notes, Voiced: 8}, true, nil
	})
	must(err)
	must(st.ReplaceAudioMarkers(ctx, audio, []store.AudioMarker{{SectionIndex: 0, TimeSeconds: 10}}, owner))
	must(st.SetSongPublic(ctx, owner, song, true))
	tokens := map[int64]string{}
	for _, p := range []int64{owner, reader} {
		tok, err := st.CreateSession(ctx, p)
		must(err)
		tokens[p] = tok
	}
	get := func(who int64, transpose int) melodyResponse {
		t.Helper()
		r := httptest.NewRequest("GET", fmt.Sprintf("/api/songs/%d/melody?transpose=%d", song, transpose), nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: tokens[who]})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var d melodyResponse
		must(json.Unmarshal(w.Body.Bytes(), &d))
		if d.State != "ready" {
			t.Fatalf("melody: %d %s", w.Code, w.Body.String())
		}
		return d
	}
	save := func(key string) {
		t.Helper()
		head := "title: Tune\n"
		if key != "" {
			head += "key: " + key + "\n"
		}
		_, err := st.SaveVariantByID(ctx, song, "Tune", "", "", head+"tempo: 120\ntime: 4/4\n\n[Verse]\ngrid: | Em | C | D | Em |\nwalking in the rain\n", "", owner)
		must(err)
	}
	// The chart's key, moved with the page.
	save("Em")
	if d := get(owner, 0); d.Key != "Em" || d.KeySource != "chart" || d.KeyCheck != nil || !strings.Contains(d.AlphaTex, `\ks (eminor)`) {
		t.Errorf("the chart's Em: %s %s %+v", d.Key, d.KeySource, d.KeyCheck)
	}
	if d := get(owner, 2); d.Key != "F#m" || !strings.Contains(d.AlphaTex, `\ks (f#minor)`) {
		t.Errorf("Em up a tone: %s", d.Key)
	}
	// No key: the melody's and the chords', said to be estimated.
	save("")
	if d := get(reader, 0); d.Key != "Em" || d.KeySource != "melody" || !strings.Contains(d.AlphaTex, `\ks (eminor)`) {
		t.Errorf("no key: %s %s", d.Key, d.KeySource)
	}
	if d := get(reader, -2); d.Key != "Dm" || d.KeySource != "melody" {
		t.Errorf("no key, down a tone: %s %s", d.Key, d.KeySource)
	}
	// The relative major writes the same signature: nothing to say.
	save("G")
	if d := get(owner, 0); d.Key != "G" || d.KeyCheck != nil {
		t.Errorf("G, the relative major: %s %+v", d.Key, d.KeyCheck)
	}
	// A key with another signature: the editor is told, no one else.
	save("Bb")
	if d := get(owner, 0); d.Key != "Bb" || d.KeyCheck == nil || d.KeyCheck.Found != "Em" || d.KeyCheck.Chart != "Bb" {
		t.Errorf("Bb against an E minor melody, to the owner: %s %+v", d.Key, d.KeyCheck)
	}
	if d := get(owner, 2); d.KeyCheck == nil || d.KeyCheck.Found != "F#m" || d.KeyCheck.Chart != "C" {
		t.Errorf("the check moves with the page: %+v", d.KeyCheck)
	}
	if d := get(reader, 0); d.Key != "Bb" || d.KeyCheck != nil {
		t.Errorf("Bb, to a reader: %s %+v", d.Key, d.KeyCheck)
	}
}
