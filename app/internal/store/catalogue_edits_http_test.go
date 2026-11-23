package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"app/internal/httpapi"
	"app/internal/store"
)

// The catalogue: open edits (docs/ownership-model.md, "The catalogue:
// open edits"; store/catalogue_edits.go, httpapi/catalogue_edits.go):
// anyone signed in edits The Big Songbook's songs; each edit is weighed
// against the song's reference recording — applied at once, or held for
// review — and kept as a revision with its summary.

// catalogueFixture: the band fixture (its catalogue song "Le Vent"), an
// admin (the catalogue's staff), and a regular editor past the newcomer's
// edits.
type catalogueFixture struct {
	store.BandFixture
	Admin, Editor int64
}

func newCatalogueFixture(t *testing.T, st *store.Store) catalogueFixture {
	t.Helper()
	ctx := context.Background()
	f := catalogueFixture{BandFixture: store.NewBandFixture(t, st)}
	var err error
	if f.Admin, err = st.CreatePerson(ctx, "admin", "Admin", "admin@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAdmin(ctx, f.Admin, true); err != nil {
		t.Fatal(err)
	}
	if f.Editor, err = st.CreatePerson(ctx, "editor", "Editor", "editor@example.com"); err != nil {
		t.Fatal(err)
	}
	acceptedEdits(t, st, f.Editor, f.CatalogSong, store.CatalogueNewcomerEdits)
	return f
}

// acceptedEdits records n accepted catalogue edits of song by person.
func acceptedEdits(t *testing.T, st *store.Store, person, song int64, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		store.ExecForTest(t, st, `
			INSERT INTO catalogue_edits (song_id, author_id, status, base_version, base_body, title, artist, body)
			VALUES ($1, $2, 'applied', 1, '', 'x', 'x', '')`, song, person)
	}
}

// setChart sets song's body, as an import would (no revision).
func setChart(t *testing.T, st *store.Store, song int64, body string) {
	t.Helper()
	store.ExecForTest(t, st, `UPDATE songs SET body = $2, title = 'Le Vent', artist = 'Someone' WHERE id = $1`, song, body)
}

// withAnalysis gives song a reference recording analysed as song 334's
// (internal/chartalign/testdata).
func withAnalysis(t *testing.T, st *store.Store, song int64) {
	t.Helper()
	rec, err := os.ReadFile("../chartalign/testdata/song334.json")
	if err != nil {
		t.Fatal(err)
	}
	store.ExecForTest(t, st, `
		WITH a AS (INSERT INTO song_audio (variant_id, byte_size, sha256, data, category) VALUES ($1::bigint, 1, 'sha'||$1::text, '\x00', 'youtube') RETURNING id)
		INSERT INTO song_audio_analysis (song_audio_id, source_sha256, analyzer, duration_s, data) SELECT id, 'sha'||$1::text, 'test', 240, $2::jsonb FROM a`,
		song, string(rec))
}

func chart334(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../chartalign/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return "title: Le Vent\nartist: Someone\n" + string(b)
}

// saveRawSaying is PUT /api/songs/{id}/raw at the song's version, with
// the edit's summary.
func (c client) saveRawSaying(id int64, text, summary string) *httptest.ResponseRecorder {
	c.t.Helper()
	_, version, err := c.st.LoadVariantRaw(id)
	if err != nil {
		c.t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"version": version, "text": text})
	r := httptest.NewRequest("PUT", fmt.Sprintf("/api/songs/%d/raw", id), strings.NewReader(string(raw)))
	r.Header.Set("Content-Type", "application/json")
	if summary != "" {
		r.Header.Set("X-Edit-Summary", url.QueryEscape(summary))
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	return w
}

func body(t *testing.T, st *store.Store, id int64) string {
	t.Helper()
	b, _, err := st.LoadVariantRaw(id)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func pendingOf(t *testing.T, st *store.Store, person, song int64) *store.CatalogueEdit {
	t.Helper()
	e, err := st.PendingCatalogueEditOf(context.Background(), person, song)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func unreadOf(t *testing.T, st *store.Store, person int64, kind string) []store.Notification {
	t.Helper()
	ns, err := st.ListNotifications(context.Background(), person, true)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Notification
	for _, n := range ns {
		if n.Kind == kind {
			out = append(out, n)
		}
	}
	return out
}

func hasReason(e *store.CatalogueEdit, why string) bool {
	for _, r := range e.Reasons {
		if r == why {
			return true
		}
	}
	return false
}

// TestCatalogueEditFollowsTheRecording: on a song whose recording has an
// analysis, an edit that keeps it following the recording (a chord) is
// applied at once, its summary on its revision; one that makes it differ
// waits for review — the song stays as it was, its author sees it
// pending, the reviewers are told once — and becomes current when
// approved, its author told.
func TestCatalogueEditFollowsTheRecording(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newCatalogueFixture(t, st)
	follows := chart334(t, "song334-new.chart")
	setChart(t, st, f.CatalogSong, follows)
	withAnalysis(t, st, f.CatalogSong)
	ed := as(t, st, f.Editor)

	// A chord: applied.
	chord := strings.Replace(follows, "grid: |: D | Am | C . D7sus4 D | G :|", "grid: |: D | Am7 | C . D7sus4 D | G :|", 1)
	v := version(t, st, f.CatalogSong)
	if w := ed.saveRawSaying(f.CatalogSong, chord, "Outro: Am7, as played"); w.Code != http.StatusOK {
		t.Fatalf("a chord fixed: %d %s", w.Code, w.Body.String())
	}
	if version(t, st, f.CatalogSong) != v+1 || body(t, st, f.CatalogSong) != chord {
		t.Fatal("the chord fix wasn't applied")
	}
	revs, err := st.ListRevisions(ctx, "variant", strconv.FormatInt(f.CatalogSong, 10))
	if err != nil || len(revs) == 0 || revs[0].Summary != "Outro: Am7, as played" || revs[0].ActorID == nil || *revs[0].ActorID != f.Editor {
		t.Fatalf("the edit's revision: %+v, %v", revs, err)
	}
	if s, _ := st.CatalogueStandingOf(ctx, f.Editor); s.Accepted != store.CatalogueNewcomerEdits+1 {
		t.Errorf("accepted edits: %d", s.Accepted)
	}
	if html := as(t, st, f.Outsider).page(fmt.Sprintf("/variants/%d/history", f.CatalogSong)); !strings.Contains(html, "Outro: Am7, as played") {
		t.Error("the history doesn't show the edit's summary")
	}

	// The old chart (an intro missing, a verse too long): waits.
	worse := chart334(t, "song334-old.chart")
	v = version(t, st, f.CatalogSong)
	w := ed.saveRawSaying(f.CatalogSong, worse, "regrouped the bars")
	if w.Code != http.StatusAccepted {
		t.Fatalf("an edit that differs from the recording: %d %s", w.Code, w.Body.String())
	}
	if version(t, st, f.CatalogSong) != v || body(t, st, f.CatalogSong) != chord {
		t.Fatal("the song changed while its edit waits for review")
	}
	e := pendingOf(t, st, f.Editor, f.CatalogSong)
	if e == nil || !hasReason(e, "fit") || e.FitBefore != "follows" || e.FitAfter != "differs" || e.Body != worse {
		t.Fatalf("the pending edit: %+v", e)
	}
	if html := ed.page(fmt.Sprintf("/song/%d", f.CatalogSong)); !strings.Contains(html, "Your edit of this song waits for review") ||
		!strings.Contains(html, fmt.Sprintf("/big-songbook/edits/%d", e.ID)) {
		t.Error("the author doesn't see their edit pending")
	}
	if html := ed.page(fmt.Sprintf("/big-songbook/edits/%d", e.ID)); !strings.Contains(html, "regrouped the bars") || !strings.Contains(html, "follow its recording less well") {
		t.Error("the edit's page: no summary or reason")
	}
	if w := as(t, st, f.Outsider).do("GET", fmt.Sprintf("/big-songbook/edits/%d", e.ID), nil); w.Code != http.StatusForbidden {
		t.Errorf("someone else opens the edit: %d", w.Code)
	}
	// The reviewers are told once, however many edits wait.
	if n := unreadOf(t, st, f.Admin, store.NotificationCatalogueReview); len(n) != 1 {
		t.Errorf("the admin's review notifications: %d, want 1", len(n))
	}
	if w := as(t, st, f.Core).saveRaw(f.CatalogSong, chord+"\n# a comment\n"); w.Code != http.StatusAccepted {
		t.Errorf("a newcomer's edit: %d, want 202", w.Code)
	}
	if n := unreadOf(t, st, f.Admin, store.NotificationCatalogueReview); len(n) != 1 {
		t.Errorf("after a second edit, the admin's review notifications: %d, want still 1", len(n))
	}
	if html := as(t, st, f.Admin).page("/big-songbook/review"); !strings.Contains(html, "regrouped the bars") || !strings.Contains(html, "2 edits waiting") {
		t.Error("the queue doesn't list the edits")
	}
	if n := unreadOf(t, st, f.Admin, store.NotificationCatalogueReview); len(n) != 0 {
		t.Error("the queue opened: the notification stays unread")
	}

	// Approved: current, as its author's edit; the author told.
	a := as(t, st, f.Admin)
	if w := a.do("POST", fmt.Sprintf("/api/big-songbook/edits/%d/approve", e.ID), map[string]string{"note": "thanks"}); w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}
	if body(t, st, f.CatalogSong) != worse {
		t.Error("the approved edit isn't the song's")
	}
	revs, _ = st.ListRevisions(ctx, "variant", strconv.FormatInt(f.CatalogSong, 10))
	if len(revs) == 0 || revs[0].Summary != "regrouped the bars" || *revs[0].ActorID != f.Editor {
		t.Errorf("the approved edit's revision: %+v", revs[0])
	}
	if n := unreadOf(t, st, f.Editor, store.NotificationCatalogueEditDecided); len(n) != 1 || !strings.Contains(n[0].Body, "approved") {
		t.Errorf("the author's notification: %+v", n)
	}
	if w := a.do("POST", fmt.Sprintf("/api/big-songbook/edits/%d/approve", e.ID), nil); w.Code != http.StatusConflict {
		t.Errorf("approving it again: %d", w.Code)
	}
}

// TestCatalogueEditWithoutAnalysis: without an analysis of the recording,
// a change of the sections or their bars waits for review; lyrics apply.
// A reject leaves the song as it is and tells its author, with the note.
func TestCatalogueEditWithoutAnalysis(t *testing.T) {
	st := store.NewTestStore(t)
	f := newCatalogueFixture(t, st)
	start := "title: Le Vent\nartist: Someone\n\n[Verse]\ngrid: C | G | Am | F\n[C]Le vent se [G]lève\n"
	setChart(t, st, f.CatalogSong, start)
	ed := as(t, st, f.Editor)

	lyrics := strings.Replace(start, "se [G]lève", "se [G]lève, il faut tenter de vivre", 1)
	if w := ed.saveRaw(f.CatalogSong, lyrics); w.Code != http.StatusOK {
		t.Fatalf("lyrics: %d %s", w.Code, w.Body.String())
	}
	bars := strings.Replace(lyrics, "grid: C | G | Am | F", "grid: C | G | Am | F | C", 1)
	if w := ed.saveRaw(f.CatalogSong, bars); w.Code != http.StatusAccepted {
		t.Fatalf("a bar added: %d %s", w.Code, w.Body.String())
	}
	e := pendingOf(t, st, f.Editor, f.CatalogSong)
	if e == nil || !hasReason(e, "structure") || e.FitBefore != "" {
		t.Fatalf("the pending edit: %+v", e)
	}
	for _, c := range []struct{ what, text string }{
		{"lead", "lead: 2\n" + lyrics},
		{"gp", strings.Replace(lyrics, "[Verse]\n", "[Verse]\ngp[Lead]: 1-4\n", 1)},
		{"key", strings.Replace(lyrics, "artist: Someone\n", "artist: Someone\nkey: Am\n", 1)},
		{"title", strings.Replace(lyrics, "artist: Someone", "artist: Somebody Else", 1)},
		{"struct", strings.Replace(lyrics, "[Verse]", "[Chorus]", 1)},
	} {
		if w := ed.saveRaw(f.CatalogSong, c.text); w.Code != http.StatusAccepted {
			t.Errorf("%s: %d, want 202", c.what, w.Code)
		}
	}
	if body(t, st, f.CatalogSong) != lyrics {
		t.Error("the song changed while its edits wait")
	}
	// One edit waiting per author and song: the latest.
	e = pendingOf(t, st, f.Editor, f.CatalogSong)
	if !hasReason(e, "structure") || !strings.Contains(e.Body, "[Chorus]") {
		t.Errorf("the latest edit doesn't take the earlier one's place: %+v", e.Reasons)
	}

	if w := as(t, st, f.Admin).do("POST", fmt.Sprintf("/api/big-songbook/edits/%d/reject", e.ID), map[string]string{"note": "the recording says Verse"}); w.Code != http.StatusOK {
		t.Fatalf("reject: %d %s", w.Code, w.Body.String())
	}
	if body(t, st, f.CatalogSong) != lyrics {
		t.Error("a rejected edit changed the song")
	}
	if got, _ := st.CatalogueEditByID(context.Background(), e.ID); got.Status != store.EditRejected || got.Note != "the recording says Verse" {
		t.Errorf("the rejected edit: %s %q", got.Status, got.Note)
	}
	if n := unreadOf(t, st, f.Editor, store.NotificationCatalogueEditDecided); len(n) != 1 || !strings.Contains(n[0].Body, "the recording says Verse") {
		t.Errorf("the author's notification: %+v", n)
	}
}

// TestCatalogueNewcomerAndTrustedEditors: a newcomer's first edits wait
// whatever they change; enough accepted edits make a trusted editor, who
// sees the queue and decides others' edits, never their own.
func TestCatalogueNewcomerAndTrustedEditors(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newCatalogueFixture(t, st)
	setChart(t, st, f.CatalogSong, "title: Le Vent\nartist: Someone\n\n[Verse]\nLe vent\n")
	o := as(t, st, f.Outsider)
	if w := o.saveRaw(f.CatalogSong, "title: Le Vent\nartist: Someone\n\n[Verse]\nLe vent se lève\n"); w.Code != http.StatusAccepted {
		t.Fatalf("a newcomer's typo fix: %d", w.Code)
	}
	newcomer := pendingOf(t, st, f.Outsider, f.CatalogSong)
	if newcomer == nil || !hasReason(newcomer, "newcomer") {
		t.Fatalf("the newcomer's edit: %+v", newcomer)
	}

	// Not trusted yet: no queue, no decisions.
	ed := as(t, st, f.Editor)
	if w := ed.do("GET", "/big-songbook/review", nil); w.Code != http.StatusForbidden {
		t.Errorf("an editor, not trusted: the queue %d", w.Code)
	}
	if w := ed.do("POST", fmt.Sprintf("/api/big-songbook/edits/%d/approve", newcomer.ID), nil); w.Code != http.StatusForbidden {
		t.Errorf("an editor, not trusted, approves: %d", w.Code)
	}
	// Trusted.
	acceptedEdits(t, st, f.Editor, f.CatalogSong, store.CatalogueTrustedEdits-store.CatalogueNewcomerEdits)
	if s, _ := st.CatalogueStandingOf(ctx, f.Editor); !s.Trusted || !s.Reviewer() {
		t.Fatalf("the editor's standing: %+v", s)
	}
	if html := ed.page("/big-songbook"); !strings.Contains(html, "/big-songbook/review") {
		t.Error("The Big Songbook doesn't link the queue for a trusted editor")
	}
	if html := ed.page("/big-songbook/review"); !strings.Contains(html, "Le Vent") {
		t.Error("the trusted editor's queue: no edit")
	}
	// Their own edit waits for someone else.
	if w := ed.saveRaw(f.CatalogSong, "title: Le Vent\nartist: Someone\nkey: Dm\n\n[Verse]\nLe vent\n"); w.Code != http.StatusAccepted {
		t.Fatalf("a trusted editor's key change: %d", w.Code)
	}
	own := pendingOf(t, st, f.Editor, f.CatalogSong)
	if w := ed.do("POST", fmt.Sprintf("/api/big-songbook/edits/%d/approve", own.ID), nil); w.Code != http.StatusForbidden {
		t.Errorf("a trusted editor approves their own edit: %d", w.Code)
	}
	if w := ed.do("POST", fmt.Sprintf("/api/big-songbook/edits/%d/approve", newcomer.ID), nil); w.Code != http.StatusOK {
		t.Fatalf("a trusted editor approves the newcomer's: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(body(t, st, f.CatalogSong), "Le vent se lève") {
		t.Error("the approved edit isn't the song's")
	}
	// The trusted editor's key change was made on the song as it was: the
	// newcomer's line changed since in the same section.
	if w := as(t, st, f.Admin).do("POST", fmt.Sprintf("/api/big-songbook/edits/%d/approve", own.ID), nil); w.Code != http.StatusOK {
		t.Fatalf("approving an edit made on an older text, in another part: %d %s", w.Code, w.Body.String())
	}
	if b := body(t, st, f.CatalogSong); !strings.Contains(b, "key: Dm") || !strings.Contains(b, "Le vent se lève") {
		t.Errorf("the two edits merged: %q", b)
	}
}

// TestCatalogueRevert: an edit's author reverts it while it's the
// latest; someone else (not trusted) can't; a trusted editor reverts any.
func TestCatalogueRevert(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newCatalogueFixture(t, st)
	start := "title: Le Vent\nartist: Someone\n\n[Verse]\nLe vent\n"
	setChart(t, st, f.CatalogSong, start)
	other, err := st.CreatePerson(ctx, "other", "Other", "other@example.com")
	if err != nil {
		t.Fatal(err)
	}
	acceptedEdits(t, st, other, f.CatalogSong, store.CatalogueNewcomerEdits)
	ed, ot := as(t, st, f.Editor), as(t, st, other)
	latest := func() int64 {
		revs, err := st.ListRevisions(ctx, "variant", strconv.FormatInt(f.CatalogSong, 10))
		if err != nil || len(revs) == 0 {
			t.Fatal(err)
		}
		return revs[0].ID
	}
	if w := ed.saveRaw(f.CatalogSong, start+"se lève\n"); w.Code != http.StatusOK {
		t.Fatalf("an edit: %d", w.Code)
	}
	mine := latest()
	if html := ed.page(fmt.Sprintf("/variants/%d/history", f.CatalogSong)); !strings.Contains(html, fmt.Sprintf(`class="revert-btn" data-revision-id="%d"`, mine)) {
		t.Error("the author isn't offered to revert their edit")
	}
	if w := ot.do("POST", fmt.Sprintf("/api/revisions/%d/revert", mine), nil); w.Code != http.StatusForbidden {
		t.Errorf("someone else reverts it: %d", w.Code)
	}
	if w := ed.do("POST", fmt.Sprintf("/api/revisions/%d/revert", mine), nil); w.Code != http.StatusOK {
		t.Fatalf("its author reverts it: %d %s", w.Code, w.Body.String())
	}
	if body(t, st, f.CatalogSong) != start {
		t.Error("the revert didn't restore the song")
	}

	if w := ed.saveRaw(f.CatalogSong, start+"il faut tenter de vivre\n"); w.Code != http.StatusOK {
		t.Fatalf("an edit: %d", w.Code)
	}
	mine = latest()
	if w := ot.saveRaw(f.CatalogSong, start+"il faut tenter de vivre !\n"); w.Code != http.StatusOK {
		t.Fatalf("another's edit: %d", w.Code)
	}
	if w := ed.do("POST", fmt.Sprintf("/api/revisions/%d/revert", mine), nil); w.Code != http.StatusForbidden {
		t.Errorf("the author reverts an edit that isn't the latest: %d", w.Code)
	}
	acceptedEdits(t, st, f.Editor, f.CatalogSong, store.CatalogueTrustedEdits)
	if w := ed.do("POST", fmt.Sprintf("/api/revisions/%d/revert", mine), nil); w.Code != http.StatusOK {
		t.Errorf("a trusted editor reverts it: %d", w.Code)
	}
}

// TestCatalogueReport: anyone reports a catalogue song (a reason); the
// report is in the queue until a reviewer marks it done.
func TestCatalogueReport(t *testing.T) {
	st := store.NewTestStore(t)
	f := newCatalogueFixture(t, st)
	o := as(t, st, f.Outsider)
	if html := o.page(fmt.Sprintf("/song/%d", f.CatalogSong)); !strings.Contains(html, "data-catalogue-report") || !strings.Contains(html, "catalogue-report-dialog") {
		t.Error("no Report on the catalogue's song")
	}
	if html := as(t, st, f.Core).page(fmt.Sprintf("/song/%d", f.BandSong)); strings.Contains(html, "data-catalogue-report") {
		t.Error("Report on a band's song")
	}
	if w := o.do("POST", fmt.Sprintf("/api/big-songbook/songs/%d/report", f.CatalogSong), map[string]string{"reason": " "}); w.Code != http.StatusBadRequest {
		t.Errorf("a report without a reason: %d", w.Code)
	}
	if w := o.do("POST", fmt.Sprintf("/api/big-songbook/songs/%d/report", f.BandSong), map[string]string{"reason": "spam"}); w.Code != http.StatusForbidden {
		t.Errorf("a report on a band's song: %d", w.Code)
	}
	if w := o.do("POST", fmt.Sprintf("/api/big-songbook/songs/%d/report", f.CatalogSong), map[string]string{"reason": "The chorus is another song's"}); w.Code != http.StatusOK {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}
	if n := unreadOf(t, st, f.Admin, store.NotificationCatalogueReview); len(n) != 1 {
		t.Errorf("the admin isn't told of the report: %d", len(n))
	}
	a := as(t, st, f.Admin)
	if html := a.page("/big-songbook/review"); !strings.Contains(html, "The chorus is another song&#39;s") || !strings.Contains(html, "1 song reported") {
		t.Error("the report isn't in the queue")
	}
	reports, _ := st.OpenCatalogueReports(context.Background())
	if len(reports) != 1 {
		t.Fatalf("open reports: %d", len(reports))
	}
	if w := as(t, st, f.Editor).do("POST", fmt.Sprintf("/api/big-songbook/reports/%d/handled", reports[0].ID), nil); w.Code != http.StatusForbidden {
		t.Errorf("a non-reviewer handles a report: %d", w.Code)
	}
	if w := a.do("POST", fmt.Sprintf("/api/big-songbook/reports/%d/handled", reports[0].ID), nil); w.Code != http.StatusOK {
		t.Errorf("handled: %d", w.Code)
	}
	if html := a.page("/big-songbook/review"); !strings.Contains(html, "No songs reported.") {
		t.Error("a handled report stays in the queue")
	}
}

// TestCatalogueReferenceMakesANewVersion: another recording as the
// catalogue song's reference makes a new catalogue song for it, with the
// edited chart (its artist the performer's); the song edited stays as it
// was; the duplicates review doesn't take them for duplicates.
func TestCatalogueReferenceMakesANewVersion(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newCatalogueFixture(t, st)
	start := "title: Le Vent\nartist: Someone\nreference: https://www.youtube.com/watch?v=AAAAAAAAAAA\n\n[Verse]\nLe vent\n"
	setChart(t, st, f.CatalogSong, start)
	store.ExecForTest(t, st, `UPDATE songs SET reference_url = 'https://www.youtube.com/watch?v=AAAAAAAAAAA' WHERE id = $1`, f.CatalogSong)
	v := version(t, st, f.CatalogSong)

	// The same recording, another link to it: an edit like any other.
	o := as(t, st, f.Outsider)
	live := strings.Replace(start, "watch?v=AAAAAAAAAAA", "watch?v=BBBBBBBBBBB", 1)
	live = strings.Replace(live, "Le vent\n", "Le vent (live)\n", 1)
	w := o.saveRaw(f.CatalogSong, live)
	if w.Code != http.StatusCreated {
		t.Fatalf("another recording: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		NewSongID int64 `json:"newSongId"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.NewSongID == 0 || out.NewSongID == f.CatalogSong {
		t.Fatalf("the new version: %d", out.NewSongID)
	}
	if version(t, st, f.CatalogSong) != v || body(t, st, f.CatalogSong) != start {
		t.Error("the song edited changed")
	}
	orig, _ := st.GetVariant(ctx, f.CatalogSong)
	nv, err := st.GetVariant(ctx, out.NewSongID)
	if err != nil {
		t.Fatal(err)
	}
	if nv.Visibility != "global" || !nv.Public || nv.WorkID != orig.WorkID || nv.ForkedFromID == nil || *nv.ForkedFromID != f.CatalogSong {
		t.Errorf("the new version: %+v", nv)
	}
	if body(t, st, out.NewSongID) != live {
		t.Error("the new version's chart isn't the edited one")
	}
	if r, _ := st.SongRightsFor(ctx, f.Outsider, out.NewSongID); !r.Catalogue || !r.Contribute {
		t.Errorf("the new version: catalogue %v, open to edits %v", r.Catalogue, r.Contribute)
	}
	if groups, _ := st.FindDuplicates(ctx); len(groups) != 0 {
		t.Errorf("the duplicates review: %+v, want none (two recordings)", groups)
	}
	// Same title and artist, the same video: duplicates still.
	store.ExecForTest(t, st, `UPDATE songs SET reference_url = 'https://youtu.be/AAAAAAAAAAA' WHERE id = $1`, out.NewSongID)
	if groups, _ := st.FindDuplicates(ctx); len(groups) != 1 {
		t.Errorf("the same video: %d groups, want 1", len(groups))
	}

	// Its staff too: another recording is another song.
	if w := as(t, st, f.Admin).saveRaw(f.CatalogSong, strings.Replace(start, "AAAAAAAAAAA", "CCCCCCCCCCC", 1)); w.Code != http.StatusCreated {
		t.Errorf("the staff change the recording: %d", w.Code)
	}
	// Removing the recording waits for review.
	if w := as(t, st, f.Editor).saveRaw(f.CatalogSong, strings.Replace(start, "reference: https://www.youtube.com/watch?v=AAAAAAAAAAA\n", "", 1)); w.Code != http.StatusAccepted {
		t.Errorf("the recording removed: %d", w.Code)
	} else if e := pendingOf(t, st, f.Editor, f.CatalogSong); !hasReason(e, "reference") {
		t.Errorf("the recording removed: reasons %v", e.Reasons)
	}
	// The reference editor's own save (PUT meta) does the same.
	w = o.do("PUT", fmt.Sprintf("/api/songs/%d/meta", f.CatalogSong), map[string]string{"key": "reference", "value": "https://www.youtube.com/watch?v=DDDDDDDDDDD"})
	if w.Code != http.StatusCreated {
		t.Errorf("the reference editor's save: %d %s", w.Code, w.Body.String())
	}
}

// TestCatalogueKeyIsTheRecordings: a catalogue song isn't transposed in
// place (422, staff included); the editor's Transpose is off on it, on
// for one's own song.
func TestCatalogueKeyIsTheRecordings(t *testing.T) {
	st := store.NewTestStore(t)
	f := newCatalogueFixture(t, st)
	start := "title: Le Vent\nartist: Someone\nkey: C\n\n[Verse]\ngrid: C | G | Am | F\n"
	setChart(t, st, f.CatalogSong, start)
	up := "title: Le Vent\nartist: Someone\nkey: D\n\n[Verse]\ngrid: D | A | Bm | G\n"
	for _, who := range []int64{f.Editor, f.Admin} {
		if w := as(t, st, who).saveRaw(f.CatalogSong, up); w.Code != http.StatusUnprocessableEntity {
			t.Errorf("person %d transposes the catalogue's song: %d", who, w.Code)
		}
	}
	if body(t, st, f.CatalogSong) != start {
		t.Error("the song was transposed")
	}
	if html := as(t, st, f.Editor).page(fmt.Sprintf("/song/%d", f.CatalogSong)); !strings.Contains(html, `aria-controls="transpose-panel" disabled`) {
		t.Error("Transpose isn't off on the catalogue's song")
	}
	if html := as(t, st, f.Core).page(fmt.Sprintf("/song/%d", f.BandSong)); strings.Contains(html, `aria-controls="transpose-panel" disabled`) {
		t.Error("Transpose is off on the band's song")
	}
}

// TestCatalogueEditAccess: no one signed in can't edit a catalogue song;
// someone its staff kept from editing can't either (their waiting edits
// rejected with it), until let again; the editor opens for anyone else,
// without the recordings' and the Guitar Pro file's tools.
func TestCatalogueEditAccess(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newCatalogueFixture(t, st)
	text := "title: Le Vent\nartist: Someone\n\n[Verse]\nLe vent\n"
	anon := client{t: t, st: st, h: httpapi.NewServer(st, nil, nil, "", "")}
	_, v, _ := st.LoadVariantRaw(f.CatalogSong)
	if w := anon.do("PUT", fmt.Sprintf("/api/songs/%d/raw", f.CatalogSong), map[string]string{"version": v, "text": text}); w.Code == http.StatusOK || w.Code == http.StatusAccepted {
		t.Errorf("no one signed in: %d", w.Code)
	}

	o := as(t, st, f.Outsider)
	html := o.page(fmt.Sprintf("/song/%d", f.CatalogSong))
	if !strings.Contains(html, `id="catalogue-edit-summary"`) || strings.Contains(html, `data-tab="gpimport"`) || strings.Contains(html, `id="redo-song-btn"`) {
		t.Error("the editor on the catalogue's song: no summary, or the files' tools")
	}
	if w := o.saveRaw(f.CatalogSong, text); w.Code != http.StatusAccepted {
		t.Fatalf("a newcomer's edit: %d", w.Code)
	}
	if w := o.do("DELETE", fmt.Sprintf("/api/songs/%d", f.CatalogSong), nil); w.Code != http.StatusForbidden {
		t.Errorf("anyone deletes the catalogue's song: %d", w.Code)
	}
	if w := o.do("PUT", fmt.Sprintf("/api/songs/%d/redo", f.CatalogSong), map[string]string{"title": "x"}); w.Code != http.StatusForbidden {
		t.Errorf("anyone erases the catalogue's song: %d", w.Code)
	}

	e := pendingOf(t, st, f.Outsider, f.CatalogSong)
	if w := as(t, st, f.Editor).do("POST", fmt.Sprintf("/api/big-songbook/edits/%d/reject", e.ID), map[string]any{"ban": true}); w.Code != http.StatusForbidden {
		t.Errorf("a non-reviewer bans: %d", w.Code)
	}
	if w := as(t, st, f.Admin).do("POST", fmt.Sprintf("/api/big-songbook/edits/%d/reject", e.ID), map[string]any{"note": "vandalism", "ban": true}); w.Code != http.StatusOK {
		t.Fatalf("reject and ban: %d %s", w.Code, w.Body.String())
	}
	if r, _ := st.SongRightsFor(ctx, f.Outsider, f.CatalogSong); r.Contribute || !r.See {
		t.Errorf("a banned person: contribute %v, see %v", r.Contribute, r.See)
	}
	if w := o.saveRaw(f.CatalogSong, text+"x\n"); w.Code != http.StatusForbidden {
		t.Errorf("a banned person edits: %d", w.Code)
	}
	if html := o.page(fmt.Sprintf("/song/%d", f.CatalogSong)); strings.Contains(html, `id="catalogue-edit-summary"`) && strings.Contains(html, `data-can-edit="true"`) {
		t.Error("a banned person gets the editor")
	}
	if html := as(t, st, f.Admin).page("/big-songbook/review"); !strings.Contains(html, "Outsider") || !strings.Contains(html, "Let them edit again") {
		t.Error("the staff don't see who is kept from editing")
	}
	if w := as(t, st, f.Admin).do("POST", fmt.Sprintf("/api/big-songbook/people/%d/ban", f.Outsider), map[string]bool{"banned": false}); w.Code != http.StatusOK {
		t.Fatalf("unban: %d", w.Code)
	}
	if w := o.saveRaw(f.CatalogSong, text+"x\n"); w.Code != http.StatusAccepted {
		t.Errorf("let edit again: %d", w.Code)
	}
}
