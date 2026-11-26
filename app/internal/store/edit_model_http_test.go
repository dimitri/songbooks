package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	if html := page(f.Trusted, ""); !strings.Contains(html, "Your edit goes live at once when it keeps the chart following its recording") {
		t.Error("an editor's note")
	}
	if html := page(f.Stranger, ""); !strings.Contains(html, "Your first edits wait for a reviewer") || strings.Contains(html, "Your edit goes live at once") {
		t.Error("a newcomer's note promises what won't happen")
	}
	if html := page(f.Core, fmt.Sprintf("?songbook=%d", f.BandBook)); !strings.Contains(html, "keeps its key: D.") {
		t.Error("the band's songbook: the note doesn't say the band keeps its key")
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
