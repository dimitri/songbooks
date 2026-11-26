package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"app/internal/store"
)

// The edit model (docs/catalogue-edit-review.md): anyone signed in fixes
// The Big Songbook's song in place, from every place it's opened from;
// an arrangement is a separate, explicit choice, offered only to who may
// make it.

// editModelFixture: the band fixture (Other Lies links the catalogue's
// "Le Vent" in its songbook and set list), with a site admin who is a
// core member of the band, a non-coaching community linking the song, a
// coaching community giving it to a student, a trusted editor, and the
// people's own songbooks.
type editModelFixture struct {
	store.BandFixture
	Admin, Trusted, Stranger int64
	Community, CommunityBook int64
	CommunityMember          int64
	Coaching, Coach, Student int64
	StudentBook, Repertoire  int64
	AdminBook, CoreBook      int64
	BandBook                 int64
	OtherBand, OtherBandSong int64
	OtherBandBook            int64
}

func newEditModelFixture(t *testing.T, st *store.Store) editModelFixture {
	t.Helper()
	ctx := context.Background()
	f := editModelFixture{BandFixture: store.NewBandFixture(t, st)}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	person := func(name string) int64 {
		t.Helper()
		id, err := st.CreatePerson(ctx, name, strings.ToLower(strings.ReplaceAll(name, " ", "."))+"@example.com")
		must(err)
		return id
	}
	f.Admin, f.Trusted, f.Stranger = person("Lucas Admin"), person("Oscar Trusted"), person("Hazel Stranger")
	must(st.SetAdmin(ctx, f.Admin, true))
	_, err := st.AddBandMember(ctx, f.Band, f.Admin, "core", f.Core)
	must(err)
	acceptedEdits(t, st, f.Trusted, f.CatalogSong, store.CatalogueTrustedEdits)
	f.BandBook, err = st.GetDefaultSongbook(ctx, "band", f.Band)
	must(err)

	// Own songbooks holding the song.
	f.AdminBook, err = st.GetDefaultSongbook(ctx, "person", f.Admin)
	must(err)
	must(st.AddToSongbook(ctx, f.AdminBook, f.CatalogSong, f.Admin))
	f.CoreBook, err = st.GetDefaultSongbook(ctx, "person", f.Core)
	must(err)
	must(st.AddToSongbook(ctx, f.CoreBook, f.CatalogSong, f.Core))

	// A community (not coaching) whose repertoire links it: its members
	// edit the repertoire.
	f.CommunityMember = person("Grace Member")
	f.Community, err = st.CreateCommunity(ctx, "Jam du Canal", f.Admin)
	must(err)
	_, err = st.AddCommunityMember(ctx, f.Community, f.CommunityMember, "member", f.Admin)
	must(err)
	f.CommunityBook, err = st.GetDefaultSongbook(ctx, "community", f.Community)
	must(err)
	must(st.AddToSongbook(ctx, f.CommunityBook, f.CatalogSong, f.Admin))

	// A coaching community giving it to a student.
	f.Coach, f.Student = person("Ines Coach"), person("Oliver Student")
	f.Coaching, err = st.CreateCommunity(ctx, "Studio Lumen", f.Coach)
	must(err)
	must(st.SetCommunityKind(ctx, f.Coaching, "coaching"))
	_, err = st.AddCommunityMember(ctx, f.Coaching, f.Student, "member", f.Coach)
	must(err)
	f.Repertoire, err = st.GetDefaultSongbook(ctx, "community", f.Coaching)
	must(err)
	must(st.AddToSongbook(ctx, f.Repertoire, f.CatalogSong, f.Coach))
	f.StudentBook, err = st.StudentSongbook(ctx, f.Coaching, f.Student, "Oliver’s songs", f.Coach)
	must(err)
	must(st.AddToSongbook(ctx, f.StudentBook, f.CatalogSong, f.Coach))

	// Another band's public song, which Other Lies links: not the
	// catalogue's, so Edit still offers the band's arrangement there.
	f.OtherBand, err = st.CreateBand(ctx, "Cobalt Avenue", f.Stranger)
	must(err)
	f.OtherBandSong, err = st.CreateVariant(ctx, store.OwnerBand, f.OtherBand, "Blue Hour", "Cobalt Avenue", f.Stranger)
	must(err)
	store.ExecForTest(t, st, `UPDATE songs SET public = true WHERE id = $1`, f.OtherBandSong)
	must(st.AddToSongbook(ctx, f.BandBook, f.OtherBandSong, f.Core))
	return f
}

// editorOpens: Edit opens the editor on the page's song (no offer in its
// place).
func editorOpens(html string) bool {
	return strings.Contains(html, `data-can-edit="true"`) && !strings.Contains(html, "data-edit-offer=")
}

// TestEditOpensTheSharedSongFromEveryPlace: on The Big Songbook's song,
// Edit opens the editor for everyone signed in — the site admin who plays
// it with a band, a band's core and guest members, a community's
// repertoire editor, a coach, a student, a newcomer, a stranger, a trusted
// editor — from every place: the catalogue, one's own songbook, the
// band's songbook and set list, a repertoire, a student's songbook. The
// arrangements are offered beside it, only to who may make them: the
// band's to its members who change its songbook (not a guest), the
// community's to who edits its repertoire or, in coaching, to the coach
// and the student it's given to; one's own to anyone but a coaching
// community's people on its song.
func TestEditOpensTheSharedSongFromEveryPlace(t *testing.T) {
	st := store.NewTestStore(t)
	f := newEditModelFixture(t, st)
	song := f.CatalogSong
	at := func(q string) string {
		if q == "" {
			return fmt.Sprintf("/song/%d", song)
		}
		return fmt.Sprintf("/song/%d?%s", song, q)
	}
	band := fmt.Sprintf(`data-arrangement-go data-action="band" data-band-id="%d"`, f.Band)
	community := fmt.Sprintf(`data-action="community" data-community-id="%d"`, f.Community)
	coaching := fmt.Sprintf(`data-action="community" data-community-id="%d"`, f.Coaching)
	own := `data-arrangement-go data-action="copy"`
	for _, c := range []struct {
		who    string
		person int64
		places []string
		offers []string // offered on every place
		not    []string // never offered
	}{
		{"site admin, band core", f.Admin, []string{"", fmt.Sprintf("setlist=%d", f.SetlistID), fmt.Sprintf("band=%d", f.Band),
			fmt.Sprintf("songbook=%d", f.BandBook), fmt.Sprintf("songbook=%d", f.AdminBook)}, []string{band, own}, nil},
		{"band core", f.Core, []string{"", fmt.Sprintf("setlist=%d", f.SetlistID), fmt.Sprintf("songbook=%d", f.CoreBook)}, []string{band, own}, nil},
		{"band guest", f.Guest, []string{"", fmt.Sprintf("setlist=%d", f.SetlistID), fmt.Sprintf("band=%d", f.Band)}, []string{own}, []string{band}},
		{"community editor", f.CommunityMember, []string{"", fmt.Sprintf("songbook=%d", f.CommunityBook)}, []string{own}, []string{band}},
		{"coach", f.Coach, []string{"", fmt.Sprintf("songbook=%d", f.Repertoire)}, []string{coaching}, []string{own, band}},
		{"student", f.Student, []string{"", fmt.Sprintf("songbook=%d", f.StudentBook)}, []string{coaching}, []string{own, band}},
		{"newcomer", f.Outsider, []string{""}, []string{own}, []string{band, community}},
		{"stranger", f.Stranger, []string{""}, []string{own}, []string{band, community}},
		{"trusted editor", f.Trusted, []string{""}, []string{own}, []string{band, community}},
	} {
		cl := as(t, st, c.person)
		for _, q := range c.places {
			html := cl.page(at(q))
			if !editorOpens(html) {
				t.Errorf("%s from %q: Edit doesn't open the editor on the shared song", c.who, q)
			}
			if !strings.Contains(html, `id="catalogue-edit-note"`) {
				t.Errorf("%s from %q: no editor note", c.who, q)
			}
			for _, o := range c.offers {
				if !strings.Contains(html, o) {
					t.Errorf("%s from %q: not offered %s", c.who, q, o)
				}
			}
			for _, o := range c.not {
				if strings.Contains(html, o) {
					t.Errorf("%s from %q: offered %s", c.who, q, o)
				}
			}
			if strings.Contains(html, "Changing it here would change it for everyone") {
				t.Errorf("%s from %q: the old warning", c.who, q)
			}
		}
	}
	// The community's arrangement, from its repertoire, for who edits it.
	if html := as(t, st, f.CommunityMember).page(at(fmt.Sprintf("songbook=%d", f.CommunityBook))); !strings.Contains(html, community) || !strings.Contains(html, "Make Jam du Canal’s arrangement…") {
		t.Error("the community's arrangement isn't offered from its repertoire")
	}
	// From the band's set list, the band's arrangement comes first: the
	// editor's note offers it.
	if html := as(t, st, f.Core).page(at(fmt.Sprintf("setlist=%d", f.SetlistID))); !strings.Contains(html, "Make Other Lies’s arrangement instead") {
		t.Error("the editor's note doesn't offer the band's arrangement from its set list")
	}
	// A guest makes no band arrangement, even asked directly.
	if w := as(t, st, f.Guest).do("POST", fmt.Sprintf("/api/bands/%d/catalog/fork/%d", f.Band, song), nil); w.Code != http.StatusForbidden {
		t.Errorf("a guest makes the band's arrangement: %d", w.Code)
	}
	// The editor saves on the shared song for a band member opened from
	// the set list: no copy made.
	n := songCount(t, st)
	text := body(t, st, song)
	if w := as(t, st, f.Admin).saveRawAs(song, text+"\n[Outro]\nla la\n", map[string]string{"X-Structure-Confirmed": "1"}); w.Code != http.StatusOK {
		t.Fatalf("the admin's fix: %d %s", w.Code, w.Body.String())
	}
	if songCount(t, st) != n || !strings.Contains(body(t, st, song), "[Outro]") {
		t.Error("the admin's fix didn't change the shared song in place")
	}
}

// TestEditOnAnotherOwnersSong: a song that isn't the catalogue's (another
// band's public song Other Lies links) isn't a member's to change: Edit
// offers the band's arrangement from the band, one's own from elsewhere —
// and to a guest who may make no band arrangement, their own.
func TestEditOnAnotherOwnersSong(t *testing.T) {
	st := store.NewTestStore(t)
	f := newEditModelFixture(t, st)
	core, guest := as(t, st, f.Core), as(t, st, f.Guest)
	if html := core.page(fmt.Sprintf("/song/%d?songbook=%d", f.OtherBandSong, f.BandBook)); !strings.Contains(html, `data-edit-offer="band"`) || !strings.Contains(html, `data-can-edit="false"`) {
		t.Error("a core member, from the band: no band's arrangement offered")
	}
	if html := core.page(fmt.Sprintf("/song/%d", f.OtherBandSong)); !strings.Contains(html, `data-edit-offer="copy"`) {
		t.Error("a core member, from no band: not their own arrangement offered")
	}
	if html := guest.page(fmt.Sprintf("/song/%d?songbook=%d", f.OtherBandSong, f.BandBook)); strings.Contains(html, `data-edit-offer="band"`) || !strings.Contains(html, `data-edit-offer="copy"`) {
		t.Error("a guest is offered the band's arrangement they can't make")
	}
}

// TestSharedSongPointsToThePlacesArrangement: opened from a band that has
// its own arrangement of the song, the shared song's page says so, with a
// link to it; the editor still opens on the shared song.
func TestSharedSongPointsToThePlacesArrangement(t *testing.T) {
	st := store.NewTestStore(t)
	f := newEditModelFixture(t, st)
	core := as(t, st, f.Core)
	arr := core.variantID(core.do("POST", fmt.Sprintf("/api/bands/%d/catalog/fork/%d", f.Band, f.CatalogSong), nil))
	html := core.page(fmt.Sprintf("/song/%d?band=%d", f.CatalogSong, f.Band))
	if !editorOpens(html) || !strings.Contains(html, fmt.Sprintf(`href="/song/%d?band=%d"`, arr, f.Band)) || !strings.Contains(html, "Other Lies has its own arrangement of this song") {
		t.Error("the shared song, opened from the band: no pointer to the band's arrangement, or no editor")
	}
	if html := core.page(fmt.Sprintf("/song/%d", f.CatalogSong)); strings.Contains(html, "Other Lies has its own arrangement of this song") {
		t.Error("the pointer shows outside the band")
	}
}

// TestEditNoteSaysWhatASaveDoes: the editor's note says one chart is for
// everyone who plays it, the place keeps its key, and what a save of the
// viewer's does — never "at once" to someone whose edit will wait. A save
// applied answers its revision, which its author undoes.
func TestEditNoteSaysWhatASaveDoes(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newEditModelFixture(t, st)
	setChart(t, st, f.CatalogSong, "title: Le Vent\nartist: Someone\nkey: C\n\n[Verse]\ngrid: C | G | Am | F\n")
	if err := st.SetEntrySemitones(ctx, f.BandBook, f.CatalogSong, 2); err != nil {
		t.Fatal(err)
	}
	page := func(who int64, q string) string {
		return as(t, st, who).page(fmt.Sprintf("/song/%d%s", f.CatalogSong, q))
	}
	if html := page(f.Admin, ""); !strings.Contains(html, "One chart for everyone who plays this song.") || !strings.Contains(html, "Your edit goes live at once.") {
		t.Error("the admin's note")
	}
	if html := page(f.Trusted, ""); !strings.Contains(html, "Your edit goes live at once.") {
		t.Error("a trusted editor's note")
	}
	if html := page(f.Core, ""); !strings.Contains(html, "Your edit goes live at once, and a reviewer looks at it afterwards") {
		t.Error("a band member's note")
	}
	if html := page(f.Stranger, ""); !strings.Contains(html, "Your first edit of a song none of your songbooks holds waits for a reviewer") || strings.Contains(html, "Your edit goes live at once") {
		t.Error("a passer-by's note promises what won't happen")
	}
	html := page(f.Core, fmt.Sprintf("?songbook=%d", f.BandBook))
	if !strings.Contains(html, "keeps its key: D.") {
		t.Error("the band's songbook: the note doesn't say the band keeps its key")
	}
	// Who plays this chart: counts, and the viewer's places with their keys.
	if !strings.Contains(html, "Who plays this chart: 6 songbooks, 1 set list") || !strings.Contains(html, "Other Lies · ") || !strings.Contains(html, "in D") {
		t.Error("the note doesn't say who plays the chart")
	}
	if strings.Contains(html, "Studio Lumen ·") {
		t.Error("the note names someone else's place")
	}

	// Applied: its revision, which its author undoes.
	tr := as(t, st, f.Trusted)
	before := body(t, st, f.CatalogSong)
	w := tr.saveRaw(f.CatalogSong, strings.Replace(before, "Am | F", "Am7 | F", 1))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"revisionId":`) || !strings.Contains(w.Body.String(), "Saved for everyone who plays it.") {
		t.Fatalf("an applied fix: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		RevisionID int64 `json:"revisionId"`
	}
	decode(t, w, &out)
	if w := tr.do("POST", fmt.Sprintf("/api/revisions/%d/revert", out.RevisionID), nil); w.Code != http.StatusOK || body(t, st, f.CatalogSong) != before {
		t.Errorf("Undo: %d", w.Code)
	}
}

// TestBandMemberStructureHint: a member of a band that plays the song,
// changing its sections, is told once that it changes the song for
// everyone, the band included (428): a hint, never a gate. Someone in no
// such band isn't asked.
func TestBandMemberStructureHint(t *testing.T) {
	st := store.NewTestStore(t)
	f := newEditModelFixture(t, st)
	start := "title: Le Vent\nartist: Someone\n\n[Verse]\ngrid: C | G | Am | F\n"
	setChart(t, st, f.CatalogSong, start)
	more := start + "\n[Verse]\ngrid: C | G | Am | F\n"
	a := as(t, st, f.Admin)
	w := a.saveRaw(f.CatalogSong, more)
	if w.Code != http.StatusPreconditionRequired || !strings.Contains(w.Body.String(), `"structureHint":true`) || !strings.Contains(w.Body.String(), "Other Lies") {
		t.Fatalf("a band member's structure change: %d %s", w.Code, w.Body.String())
	}
	if body(t, st, f.CatalogSong) != start {
		t.Fatal("saved before the hint was answered")
	}
	if w := a.saveRawAs(f.CatalogSong, more, map[string]string{"X-Structure-Confirmed": "1"}); w.Code != http.StatusOK || body(t, st, f.CatalogSong) != more {
		t.Errorf("saved for everyone after the hint: %d", w.Code)
	}
	// A chord isn't a structure change: no hint.
	if w := a.saveRaw(f.CatalogSong, strings.Replace(more, "Am | F", "Am7 | F", 1)); w.Code != http.StatusOK {
		t.Errorf("a band member's chord fix: %d", w.Code)
	}
	// Not a band's: not asked.
	if w := as(t, st, f.Trusted).saveRaw(f.CatalogSong, start); w.Code == http.StatusPreconditionRequired {
		t.Error("someone in no band that plays it gets the band hint")
	}
}

// decode is a JSON answer's body into v.
func decode(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("%d %s: %v", w.Code, w.Body.String(), err)
	}
}

// TestPatrolInsteadOfHolding: a newcomer who plays the song (a band
// member) has their fix applied at once, flagged for the reviewers; a
// passer-by's very first edit waits; past the daily limit, edits wait
// until trusted; a trusted editor's fix is seen as made. A reviewer's
// Revert of an edit later edits came after takes only its changes out, or
// says to use History when a later edit changed the same place.
func TestPatrolInsteadOfHolding(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newEditModelFixture(t, st)
	start := "title: Le Vent\nartist: Someone\n\n[Verse]\ngrid: C | G | Am | F\n\n[Chorus]\ngrid: F | G | C | C\n"
	setChart(t, st, f.CatalogSong, start)
	toCheck := func() []store.CatalogueEdit {
		t.Helper()
		es, err := st.CatalogueEditsToCheck(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return es
	}

	// A newcomer who plays it: applied, flagged.
	core := as(t, st, f.Core)
	verse := strings.Replace(start, "Am | F\n", "Am7 | F\n", 1)
	if w := core.saveRaw(f.CatalogSong, verse); w.Code != http.StatusOK || body(t, st, f.CatalogSong) != verse {
		t.Fatalf("a band member's first fix: %d %s", w.Code, w.Body.String())
	}
	if es := toCheck(); len(es) != 1 || es[0].AuthorID != f.Core || !hasReason(&es[0], "newcomer") {
		t.Fatalf("to check: %+v", es)
	}
	// A passer-by's first edit: waits.
	if w := as(t, st, f.Stranger).saveRaw(f.CatalogSong, strings.Replace(verse, "G | C | C", "G | C | C7", 1)); w.Code != http.StatusAccepted {
		t.Errorf("a passer-by's first edit: %d", w.Code)
	} else if e := pendingOf(t, st, f.Stranger, f.CatalogSong); e == nil || !hasReason(e, "passerby") {
		t.Errorf("a passer-by's first edit waits for: %+v", e)
	}
	// A trusted editor's: applied, seen.
	tr := as(t, st, f.Trusted)
	chorus := strings.Replace(verse, "G | C | C\n", "G | C | Csus4\n", 1)
	if w := tr.saveRaw(f.CatalogSong, chorus); w.Code != http.StatusOK {
		t.Fatalf("a trusted editor's fix: %d", w.Code)
	}
	if es := toCheck(); len(es) != 1 {
		t.Errorf("a trusted editor's fix is listed to check: %d", len(es))
	}

	// Revert the band member's (verse) edit, a later one (chorus) kept.
	a := as(t, st, f.Admin)
	es := toCheck()
	if w := a.do("POST", fmt.Sprintf("/api/big-songbook/edits/%d/revert", es[0].ID), map[string]string{"note": "Am, as on the record"}); w.Code != http.StatusOK {
		t.Fatalf("revert under a later edit: %d %s", w.Code, w.Body.String())
	}
	if got := body(t, st, f.CatalogSong); !strings.Contains(got, "Am | F") || !strings.Contains(got, "Csus4") {
		t.Errorf("only the reverted edit's changes go: %q", got)
	}
	// A later edit of the same place: History, not patrol.
	if w := core.saveRaw(f.CatalogSong, strings.Replace(body(t, st, f.CatalogSong), "Am | F\n", "Am9 | F\n", 1)); w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	later := strings.Replace(body(t, st, f.CatalogSong), "Am9 | F\n", "Am11 | F\n", 1)
	if w := tr.saveRaw(f.CatalogSong, later); w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	es = toCheck()
	if len(es) != 1 {
		t.Fatalf("to check: %d", len(es))
	}
	if w := a.do("POST", fmt.Sprintf("/api/big-songbook/edits/%d/revert", es[0].ID), nil); w.Code != http.StatusConflict {
		t.Errorf("revert under a later edit of the same place: %d", w.Code)
	}
	if body(t, st, f.CatalogSong) != later {
		t.Error("a refused revert changed the song")
	}
	// Reverted in History instead: the edit is reverted, no longer to
	// check, and doesn't count towards its author's trust.
	if err := st.RevertRevision(ctx, *es[0].RevisionID, f.Admin); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.CatalogueEditByID(ctx, es[0].ID); got.RevertedAt == nil {
		t.Error("reverted in History, the edit isn't marked reverted")
	}
	if left := toCheck(); len(left) != 0 {
		t.Errorf("left to check: %d", len(left))
	}
	if s, _ := st.CatalogueStandingOf(ctx, f.Core); s.Accepted != 0 {
		t.Errorf("the band member's accepted edits, both reverted: %d", s.Accepted)
	}

	// The daily limit: past it, edits wait until trusted (these, reverted,
	// don't make them trusted).
	for i := 0; i < store.CatalogueDailyLimit; i++ {
		store.ExecForTest(t, st, `
			INSERT INTO catalogue_edits (song_id, author_id, status, base_version, base_body, title, artist, body, patrolled_at, reverted_at)
			VALUES ($1, $2, 'applied', 1, '', 'x', 'x', '', now(), now())`, f.CatalogSong, f.Core)
	}
	if w := core.saveRaw(f.CatalogSong, strings.Replace(body(t, st, f.CatalogSong), "G | Am", "G7 | Am", 1)); w.Code != http.StatusAccepted {
		t.Errorf("past the daily limit: %d", w.Code)
	} else if e := pendingOf(t, st, f.Core, f.CatalogSong); e == nil || !hasReason(e, "rate") {
		t.Errorf("past the daily limit, waits for: %+v", e)
	}
}

// TestSuggestions: what identifies the song or lines it up with its
// recording is a suggestion. In a mixed save the chords go live and the
// key's name is suggested; its author sees it in place on the song (the
// song as it is one click away), others don't; a reviewer sees it on the
// song and accepts it there. A title fixed is a suggestion too.
func TestSuggestions(t *testing.T) {
	st := store.NewTestStore(t)
	f := newEditModelFixture(t, st)
	start := "title: Le Vent\nartist: Someone\nkey: C\n\n[Verse]\ngrid: C | G | Am | F\n"
	setChart(t, st, f.CatalogSong, start)
	core := as(t, st, f.Core)
	mixed := strings.Replace(strings.Replace(start, "key: C", "key: Am", 1), "Am | F", "Am7 | F", 1)
	w := core.saveRaw(f.CatalogSong, mixed)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"suggested":true`) {
		t.Fatalf("a mixed save: %d %s", w.Code, w.Body.String())
	}
	live := strings.Replace(start, "Am | F", "Am7 | F", 1)
	if got := body(t, st, f.CatalogSong); got != live {
		t.Fatalf("the chords didn't go live alone: %q", got)
	}
	e := pendingOf(t, st, f.Core, f.CatalogSong)
	if e == nil || e.Body != mixed || e.BaseBody != live || !hasReason(e, "key") {
		t.Fatalf("the suggestion: %+v", e)
	}
	page := fmt.Sprintf("/song/%d", f.CatalogSong)
	if html := core.page(page); !strings.Contains(html, `id="catalogue-suggestion"`) || !strings.Contains(html, `data-key="Am"`) || !strings.Contains(html, "?suggestion=off") {
		t.Error("its author doesn't see the suggestion in place")
	}
	if html := core.page(page + "?suggestion=off"); !strings.Contains(html, `data-key="C"`) {
		t.Error("its author can't see the song as it is")
	}
	if html := as(t, st, f.Guest).page(page); strings.Contains(html, "catalogue-suggestion") || !strings.Contains(html, `data-key="C"`) {
		t.Error("someone else sees another's suggestion")
	}
	tr := as(t, st, f.Trusted)
	html := tr.page(page)
	if !strings.Contains(html, "1 suggestion waits for this song") || !strings.Contains(html, fmt.Sprintf(`class="catalogue-edit-decide form-stack" data-edit-id="%d"`, e.ID)) {
		t.Fatal("a reviewer doesn't see the suggestion on the song")
	}
	if w := tr.do("POST", fmt.Sprintf("/api/big-songbook/edits/%d/approve", e.ID), nil); w.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", w.Code, w.Body.String())
	}
	if got := body(t, st, f.CatalogSong); got != mixed {
		t.Errorf("accepted: %q", got)
	}

	// A title fixed: suggested, the song's title kept meanwhile.
	renamed := strings.Replace(mixed, "title: Le Vent", "title: Le Vent se lève", 1)
	if w := core.saveRawAs(f.CatalogSong, renamed, map[string]string{"X-Edit-Kind": "fix"}); w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"suggested":true`) {
		t.Fatalf("a title fixed: %d %s", w.Code, w.Body.String())
	}
	if v, _ := st.GetVariant(context.Background(), f.CatalogSong); v.Title != "Le Vent" {
		t.Errorf("the title changed before review: %q", v.Title)
	}
	if html := core.page(page); !strings.Contains(html, "Le Vent se lève") {
		t.Error("its author doesn't see the suggested title")
	}
}

// saveRawAt is PUT /api/songs/{id}/raw at version, with headers
// (URL-encoded).
func (c client) saveRawAt(id int64, text string, version int, headers map[string]string) *httptest.ResponseRecorder {
	c.t.Helper()
	raw, _ := json.Marshal(map[string]string{"version": strconv.Itoa(version), "text": text})
	r := httptest.NewRequest("PUT", fmt.Sprintf("/api/songs/%d/raw", id), strings.NewReader(string(raw)))
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		r.Header.Set(k, url.QueryEscape(v))
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	return w
}

// TestMergeAtSave: two people editing one song, the second saving over
// the version they opened. Different sections merge silently (who saved
// meanwhile said); the same section is asked once (409, theirs and
// yours), then saved with the choice. A band's own song merges the same;
// a version that kept no revision is refused as before.
func TestMergeAtSave(t *testing.T) {
	st := store.NewTestStore(t)
	f := newEditModelFixture(t, st)
	start := "title: Le Vent\nartist: Someone\n\n[Verse]\ngrid: C | G | Am | F\n\n[Chorus]\ngrid: F | G | C | C\n"
	setChart(t, st, f.CatalogSong, start)
	opened := version(t, st, f.CatalogSong)
	tr, a := as(t, st, f.Trusted), as(t, st, f.Admin)
	// Oscar fixes the chorus.
	if w := tr.saveRaw(f.CatalogSong, strings.Replace(start, "C | C\n", "C | C7\n", 1)); w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	// Lucas, from the version he opened, fixes the verse: both kept.
	mine := strings.Replace(start, "Am | F\n", "Am7 | F\n", 1)
	w := a.saveRawAt(f.CatalogSong, mine, opened, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("X-Merged-With"), "Oscar") {
		t.Fatalf("different sections: %d %s (merged with %q)", w.Code, w.Body.String(), w.Header().Get("X-Merged-With"))
	}
	if got := body(t, st, f.CatalogSong); !strings.Contains(got, "Am7 | F") || !strings.Contains(got, "C | C7") {
		t.Fatalf("both edits kept? %q", got)
	}

	// The same section: asked.
	opened = version(t, st, f.CatalogSong)
	now := body(t, st, f.CatalogSong)
	if w := tr.saveRaw(f.CatalogSong, strings.Replace(now, "Am7 | F", "Am9 | F", 1)); w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	mine = strings.Replace(now, "Am7 | F", "A7 | F", 1)
	w = a.saveRawAt(f.CatalogSong, mine, opened, nil)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"key":"s:verse#1"`) || !strings.Contains(w.Body.String(), "Am9") || !strings.Contains(w.Body.String(), "A7") {
		t.Fatalf("the same section: %d %s", w.Code, w.Body.String())
	}
	if w := a.saveRawAt(f.CatalogSong, mine, opened, map[string]string{"X-Merge-Resolve": "s:verse#1=mine"}); w.Code != http.StatusOK {
		t.Fatalf("mine chosen: %d %s", w.Code, w.Body.String())
	}
	if got := body(t, st, f.CatalogSong); !strings.Contains(got, "A7 | F") || !strings.Contains(got, "C | C7") {
		t.Errorf("mine chosen: %q", got)
	}

	// A band's own song: the same.
	core, guest := as(t, st, f.Core), as(t, st, f.Guest)
	f.GuestsEditTheSongbook(t, st)
	if w := core.saveRaw(f.BandSong, start); w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	opened = version(t, st, f.BandSong)
	if w := core.saveRaw(f.BandSong, strings.Replace(start, "C | C\n", "C | G\n", 1)); w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	if w := guest.saveRawAt(f.BandSong, strings.Replace(start, "G | Am", "E | Am", 1), opened, nil); w.Code != http.StatusOK {
		t.Fatalf("a band's song, merged: %d %s", w.Code, w.Body.String())
	}
	if got := body(t, st, f.BandSong); !strings.Contains(got, "C | E | Am") || !strings.Contains(got, "C | G\n") {
		t.Errorf("the band's song merged: %q", got)
	}

	// A version that kept no revision (from before merge at save):
	// refused as before.
	opened = version(t, st, f.BandSong)
	if w := core.saveRaw(f.BandSong, start); w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	store.ExecForTest(t, st, `UPDATE revisions SET song_version = NULL WHERE entity_type = 'variant' AND entity_id = $1::bigint::text`, f.BandSong)
	if w := guest.saveRawAt(f.BandSong, start, opened, nil); w.Code != http.StatusConflict || strings.Contains(w.Body.String(), "conflicts") {
		t.Errorf("no revision to merge against: %d %s", w.Code, w.Body.String())
	}
}

// TestFoldBack: an arrangement of the catalogue's song identical to it is
// offered, to its editors only, to go back to the shared chart: its
// songbook entries (key kept) and set list items move to the original,
// it goes (its old link opens the original). One that differs by a chord
// sends the fix first (an edit of the shared song, weighed like any), then
// goes back. A named arrangement, one kept by its owner, one with a
// recording of its own aren't offered.
func TestFoldBack(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newEditModelFixture(t, st)
	start := "title: Le Vent\nartist: Someone\n\n[Verse]\ngrid: C | G | Am | F\n"
	setChart(t, st, f.CatalogSong, start)
	if err := st.SetEntrySemitones(ctx, f.BandBook, f.CatalogSong, -2); err != nil {
		t.Fatal(err)
	}
	core := as(t, st, f.Core)
	arr := core.variantID(core.do("POST", fmt.Sprintf("/api/bands/%d/catalog/fork/%d", f.Band, f.CatalogSong), nil))
	page := fmt.Sprintf("/song/%d", arr)
	if html := core.page(page); !strings.Contains(html, "This arrangement is the same as the shared chart.") || !strings.Contains(html, fmt.Sprintf(`data-fold-id="%d"`, arr)) {
		t.Fatal("an identical arrangement isn't offered to go back")
	}
	if html := core.page("/home"); !strings.Contains(html, "Arrangements that are only fixes") {
		t.Error("the home page doesn't offer it")
	}
	if html := as(t, st, f.Guest).page(page); strings.Contains(html, "data-fold-id") {
		t.Error("a guest who can't change the band's song is offered to fold it")
	}
	if w := as(t, st, f.Guest).do("POST", fmt.Sprintf("/api/variants/%d/fold", arr), nil); w.Code != http.StatusForbidden {
		t.Errorf("a guest folds it: %d", w.Code)
	}
	if w := core.do("POST", fmt.Sprintf("/api/variants/%d/fold", arr), nil); w.Code != http.StatusOK {
		t.Fatalf("fold: %d %s", w.Code, w.Body.String())
	}
	if n, in, _ := st.SongbookEntryKey(ctx, f.BandBook, f.CatalogSong); !in || n != -2 {
		t.Errorf("the band's songbook: the shared song %v, key %d (want -2)", in, n)
	}
	if got := setlistSongs(t, st, f.SetlistID); len(got) != 2 || got[1] != f.CatalogSong {
		t.Errorf("the set list: %v", got)
	}
	if v, _ := st.GetVariant(ctx, arr); v != nil {
		t.Error("the arrangement is still there")
	}
	if w := core.do("GET", page, nil); w.Code != http.StatusMovedPermanently || !strings.Contains(w.Header().Get("Location"), fmt.Sprintf("/song/%d", f.CatalogSong)) {
		t.Errorf("the arrangement's old link: %d %s", w.Code, w.Header().Get("Location"))
	}

	// A fix: sent, then back.
	arr = core.variantID(core.do("POST", fmt.Sprintf("/api/bands/%d/catalog/fork/%d", f.Band, f.CatalogSong), nil))
	fixed := strings.Replace(body(t, st, arr), "Am | F", "Am7 | F", 1)
	if w := core.saveRaw(arr, fixed); w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	if html := core.page(fmt.Sprintf("/song/%d", arr)); !strings.Contains(html, "This arrangement differs from the shared chart by one change:") || !strings.Contains(html, "data-fold-send") {
		t.Fatal("a fix-only arrangement isn't offered to send its fix")
	}
	if w := core.do("POST", fmt.Sprintf("/api/variants/%d/fold", arr), nil); w.Code != http.StatusBadRequest {
		t.Errorf("folding a fix without sending it: %d", w.Code)
	}
	if w := core.do("POST", fmt.Sprintf("/api/variants/%d/fold", arr), map[string]bool{"send": true}); w.Code != http.StatusOK {
		t.Fatalf("send the fix: %d %s", w.Code, w.Body.String())
	}
	if got := body(t, st, f.CatalogSong); !strings.Contains(got, "Am7 | F") {
		t.Errorf("the shared song didn't get the fix: %q", got)
	}
	if v, _ := st.GetVariant(ctx, arr); v != nil {
		t.Error("the fix's arrangement is still there")
	}

	// Kept, named, with a recording of its own: not offered.
	arr = core.variantID(core.do("POST", fmt.Sprintf("/api/bands/%d/catalog/fork/%d", f.Band, f.CatalogSong), nil))
	if w := core.do("POST", fmt.Sprintf("/api/variants/%d/fold-keep", arr), nil); w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	if html := core.page(fmt.Sprintf("/song/%d", arr)); strings.Contains(html, "data-fold-id") {
		t.Error("a kept arrangement is offered again")
	}
	named := core.variantID(core.do("POST", fmt.Sprintf("/api/variants/%d/versions", arr), map[string]any{"name": "psychedelic"}))
	store.ExecForTest(t, st, `UPDATE songs SET forked_from_id = $2 WHERE id = $1`, named, f.CatalogSong)
	media := core.variantID(core.do("POST", fmt.Sprintf("/api/variants/%d/fork", f.CatalogSong), nil))
	store.ExecForTest(t, st, `INSERT INTO song_audio (variant_id, byte_size, sha256, data, category) VALUES ($1, 1, 'own', '\x00', 'band')`, media)
	cs, err := st.FoldCandidates(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.Foldable() {
			t.Errorf("offered: %+v", c)
		}
	}
	if _, err := st.FoldBack(ctx, 0, media, false, store.FoldIdentical); err == nil {
		t.Error("an arrangement with a recording of its own folded")
	}
}
