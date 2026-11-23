package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"app/internal/httpapi"
	"app/internal/reftitle"
	"app/internal/store"
)

// A coaching community's two workflows over HTTP (docs/coaching.md,
// "The song the coach and the students share"): the coach links a
// catalogue song into the repertoire and creates one there, gives them to
// students in a key, changes the key, takes a song back, answers "Ask for
// it"; a student finds the song in their songbook in their key, makes the
// community's version of a linked song on their first edit (it takes the
// link's place in every songbook of the community, keys kept), adds a
// chord grid, a Guitar Pro file and a reference, and the coach sees it
// all. A student edits the songs they're given, not another student's,
// not the repertoire's others, not after the song is taken back.

type coachFlow struct {
	ctx                                 context.Context
	community, repertoire               int64
	coach, oliver, poppy, george, admin int64
	oliverBook, poppyBook, adminBook    int64
	catalogue, other                    int64
	c, o, p, g, a                       client
}

func newCoachFlow(t *testing.T, st *store.Store) *coachFlow {
	t.Helper()
	ctx := context.Background()
	f := &coachFlow{ctx: ctx}
	person := func(name string) int64 {
		id, err := st.CreatePerson(ctx, name, strings.ToUpper(name[:1])+name[1:], name+"@example.com")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.coach, f.oliver, f.poppy, f.george, f.admin = person("ines"), person("oliver"), person("poppy"), person("george"), person("lucas")
	if err := st.SetAdmin(ctx, f.admin, true); err != nil {
		t.Fatal(err)
	}
	var err error
	if f.community, err = st.CreateCommunity(ctx, "Studio Lumen", f.coach); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCommunityKind(ctx, f.community, "coaching"); err != nil {
		t.Fatal(err)
	}
	// Students: the built-in member role, which edits the repertoire
	// (migration 0052) — in a coaching community that alone edits none
	// of its songs.
	for _, id := range []int64{f.oliver, f.poppy, f.george, f.admin} {
		if _, err := st.AddCommunityMember(ctx, f.community, id, "member", f.coach); err != nil {
			t.Fatal(err)
		}
	}
	if f.repertoire, err = st.GetDefaultSongbook(ctx, "community", f.community); err != nil {
		t.Fatal(err)
	}
	book := func(who int64, name string) int64 {
		id, err := st.StudentSongbook(ctx, f.community, who, name+"’s songs", f.coach)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.oliverBook, f.poppyBook, f.adminBook = book(f.oliver, "Oliver"), book(f.poppy, "Poppy"), book(f.admin, "Lucas")
	if f.catalogue, err = st.CreateVariant(ctx, store.OwnerCatalog, 0, "Small Plane", "Saltwater Radio", f.coach); err != nil {
		t.Fatal(err)
	}
	if f.other, err = st.CreateVariant(ctx, store.OwnerCatalog, 0, "Faded Season", "Ivory Coastline", f.coach); err != nil {
		t.Fatal(err)
	}
	f.c, f.o, f.p, f.g, f.a = as(t, st, f.coach), as(t, st, f.oliver), as(t, st, f.poppy), as(t, st, f.george), as(t, st, f.admin)
	return f
}

// giveKey is the coach's POST /api/songs/{id}/coach-keys for a student.
func (f *coachFlow) giveKey(song, student int64, semitones int) *httptest.ResponseRecorder {
	return f.c.do("POST", fmt.Sprintf("/api/songs/%d/coach-keys", song),
		map[string]any{"communityId": f.community, "personId": student, "semitones": semitones})
}

func entryKey(t *testing.T, st *store.Store, book, song int64) (int, bool) {
	t.Helper()
	n, in, err := st.SongbookEntryKey(context.Background(), book, song)
	if err != nil {
		t.Fatal(err)
	}
	return n, in
}

// putGP is PUT /api/songs/{id}/gp-file with a small file.
func (c client) putGP(id int64, name string) *httptest.ResponseRecorder {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		c.t.Fatal(err)
	}
	fw.Write([]byte("a Guitar Pro file, for the test"))
	mw.Close()
	r := httptest.NewRequest("PUT", fmt.Sprintf("/api/songs/%d/gp-file", id), &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	return w
}

func code(w *httptest.ResponseRecorder) int { return w.Code }

func TestCoachWorkflow(t *testing.T) {
	st := store.NewTestStore(t)
	f := newCoachFlow(t, st)
	ctx := f.ctx

	// A catalogue song linked into the repertoire: a link, still the
	// catalogue's.
	if w := f.c.do("POST", fmt.Sprintf("/api/songbooks/%d/entries", f.repertoire), map[string]int64{"variantId": f.catalogue}); w.Code != http.StatusOK {
		t.Fatalf("linking the catalogue's song: %d %s", w.Code, w.Body)
	}
	if _, in := entryKey(t, st, f.repertoire, f.catalogue); !in {
		t.Fatal("the link isn't in the repertoire")
	}

	// A new song created there, with its reference: the community's,
	// private; lyrics, as the editor's Import tab saves them.
	created := f.c.variantID(f.c.do("POST", fmt.Sprintf("/api/songbooks/%d/songs", f.repertoire),
		map[string]any{"title": "Le Quai", "artist": "Studio", "referenceUrl": "https://www.youtube.com/watch?v=dQw4w9WgXcQ"}))
	if v, _ := st.GetVariant(ctx, created); v == nil || v.OwnerCommunityID == nil || *v.OwnerCommunityID != f.community || v.Public {
		t.Fatalf("the created song: %+v", v)
	}
	if text, _, _ := st.LoadVariantRaw(created); !strings.Contains(text, "youtube.com") {
		t.Errorf("the created song has no reference: %q", text)
	}
	if w := f.c.saveRaw(created, "title: Le Quai\nartist: Studio\nkey: G\nreference: https://www.youtube.com/watch?v=dQw4w9WgXcQ\n\n[Verse]\nla la\n"); w.Code != http.StatusOK {
		t.Fatalf("the coach's lyrics: %d %s", w.Code, w.Body)
	}

	// Given to Oliver in a key, then another; to Poppy too.
	if w := f.giveKey(created, f.oliver, -2); w.Code != http.StatusOK {
		t.Fatalf("giving Oliver the song: %d %s", w.Code, w.Body)
	}
	if n, in := entryKey(t, st, f.oliverBook, created); !in || n != -2 {
		t.Fatalf("Oliver's entry: %v %d", in, n)
	}
	if w := f.giveKey(created, f.oliver, 3); w.Code != http.StatusOK {
		t.Fatalf("changing Oliver's key: %d %s", w.Code, w.Body)
	}
	if n, _ := entryKey(t, st, f.oliverBook, created); n != 3 {
		t.Fatalf("Oliver's key, changed: %d", n)
	}
	if w := f.giveKey(created, f.poppy, 0); w.Code != http.StatusOK {
		t.Fatalf("giving Poppy the song: %d", w.Code)
	}
	// The catalogue's song, linked, given in a key: a link in their
	// songbooks too.
	for who, n := range map[int64]int{f.oliver: 2, f.poppy: -1} {
		if w := f.giveKey(f.catalogue, who, n); w.Code != http.StatusOK {
			t.Fatalf("giving the catalogue's song: %d %s", w.Code, w.Body)
		}
	}

	// Taken back from Poppy: she no longer edits it.
	if code(f.p.saveRaw(created, "title: Le Quai\nartist: Studio\n\n[Verse]\nPoppy\n")) != http.StatusOK {
		t.Fatal("Poppy, given the song, can't edit it")
	}
	if w := f.c.do("DELETE", fmt.Sprintf("/api/communities/%d/songbooks/%d/songs/%d", f.community, f.poppyBook, created), nil); w.Code/100 != 2 {
		t.Fatalf("taking the song back: %d %s", w.Code, w.Body)
	}
	if w := f.p.saveRaw(created, "title: Le Quai\nartist: Studio\n\n[Verse]\nPoppy again\n"); w.Code != http.StatusForbidden {
		t.Fatalf("Poppy edits the song taken back from her: %d", w.Code)
	}

	// "Ask for it": George asks for the repertoire's link, the coach
	// adds it in a key.
	w := f.g.do("POST", fmt.Sprintf("/api/communities/%d/song-requests", f.community), map[string]int64{"variantId": f.catalogue})
	if w.Code/100 != 2 {
		t.Fatalf("George asks: %d %s", w.Code, w.Body)
	}
	reqs, err := st.PendingSongRequests(ctx, f.community)
	if err != nil || len(reqs) != 1 {
		t.Fatalf("requests: %v %+v", err, reqs)
	}
	if w := f.c.do("POST", fmt.Sprintf("/api/communities/%d/song-requests/%d/add", f.community, reqs[0].ID), map[string]int{"semitones": 1}); w.Code/100 != 2 {
		t.Fatalf("adding George's request: %d %s", w.Code, w.Body)
	}
	georgeBook, _ := st.FindStudentSongbook(ctx, f.community, f.george)
	if n, in := entryKey(t, st, georgeBook, f.catalogue); !in || n != 1 {
		t.Fatalf("George's entry: %v %d", in, n)
	}

	// The coach makes the community's version of the link, from the
	// repertoire: it takes the link's place in every songbook of the
	// community, each in its key.
	path := fmt.Sprintf("/song/%d?songbook=%d", f.catalogue, f.repertoire)
	if html := f.c.page(path); !strings.Contains(html, `data-edit-offer="community"`) || !strings.Contains(html, "change together") {
		t.Fatal("the coach: no coaching \"Make the community's version\" offer")
	}
	version := f.c.variantID(f.c.do("POST", fmt.Sprintf("/api/communities/%d/catalog/fork/%d", f.community, f.catalogue), map[string]int64{"songbookId": f.repertoire}))
	for _, x := range []struct {
		book int64
		key  int
	}{{f.repertoire, 0}, {f.oliverBook, 2}, {f.poppyBook, -1}, {georgeBook, 1}} {
		if n, in := entryKey(t, st, x.book, version); !in || n != x.key {
			t.Errorf("songbook %d: the version %v, key %d (want %d)", x.book, in, n, x.key)
		}
		if _, in := entryKey(t, st, x.book, f.catalogue); in {
			t.Errorf("songbook %d still links the catalogue's song", x.book)
		}
	}
	// Linking the catalogue's song again gives the community's version.
	if got := f.c.variantID(f.c.do("POST", fmt.Sprintf("/api/songbooks/%d/entries", f.repertoire), map[string]int64{"variantId": f.catalogue})); got != version {
		t.Errorf("linking the catalogue's song again: %d (want the version %d)", got, version)
	}
	if _, in := entryKey(t, st, f.repertoire, f.catalogue); in {
		t.Error("the repertoire links the catalogue's song beside its version")
	}
	// The coach edits it in place; a new version beside it is hers.
	if w := f.c.saveRaw(version, "title: Small Plane\nartist: Saltwater Radio\nkey: D\n\n[Verse]\ngrid: | D | G | A | D |\n"); w.Code != http.StatusOK {
		t.Fatalf("the coach edits the community's version: %d", w.Code)
	}
	if html := f.c.page(fmt.Sprintf("/song/%d?songbook=%d", version, f.repertoire)); !strings.Contains(html, "new-version-open") {
		t.Error("the coach isn't offered a new version")
	}
}

func TestStudentWorkflow(t *testing.T) {
	st := store.NewTestStore(t)
	f := newCoachFlow(t, st)
	ctx := f.ctx
	for _, song := range []int64{f.catalogue, f.other} {
		if err := st.AddToSongbook(ctx, f.repertoire, song, f.coach); err != nil {
			t.Fatal(err)
		}
	}
	if code(f.giveKey(f.catalogue, f.oliver, 2)) != http.StatusOK || code(f.giveKey(f.catalogue, f.poppy, -1)) != http.StatusOK || code(f.giveKey(f.catalogue, f.admin, 4)) != http.StatusOK {
		t.Fatal("giving the song")
	}
	// What Oliver works on, before: an assignment and a request.
	if _, err := st.CreatePracticeAssignment(ctx, store.NewPracticeAssignment{CommunityID: f.community, PersonID: f.oliver, VariantID: f.catalogue, PerWeek: 3, By: f.coach}); err != nil {
		t.Fatal(err)
	}

	// Oliver's songbook, in his key: the studio's Songbooks tab, the song
	// page opened from it.
	if html := f.o.page(fmt.Sprintf("/communities/%d/songbooks", f.community)); !strings.Contains(html, "Small Plane") {
		t.Error("Oliver's songbooks don't show the song")
	}
	fromBook := fmt.Sprintf("/song/%d?songbook=%d", f.catalogue, f.oliverBook)
	html := f.o.page(fromBook)
	if !strings.Contains(html, `data-student-key="2"`) || !strings.Contains(html, `data-key-mine="1"`) {
		t.Error("the song page from Oliver's songbook isn't in his key")
	}
	if w := f.o.do("GET", fmt.Sprintf("/songbooks/%d/export-songbook.pdf?keys=songbook", f.oliverBook), nil); w.Code != http.StatusOK {
		t.Errorf("PDF in my keys: %d", w.Code)
	}
	// Edit offers the community's version, from his songbook and from
	// elsewhere; never "my version" (his alone).
	for _, p := range []string{fromBook, fmt.Sprintf("/song/%d", f.catalogue)} {
		html := f.o.page(p)
		if !strings.Contains(html, `data-edit-offer="community"`) || !strings.Contains(html, "change together") {
			t.Errorf("%s: Oliver isn't offered the community's version", p)
		}
	}
	// Not of a song he isn't given (the repertoire's other), nor from
	// Poppy's songbook (he can't open it: the context is dropped).
	if html := f.o.page(fmt.Sprintf("/song/%d?songbook=%d", f.other, f.repertoire)); strings.Contains(html, `data-edit-offer="community"`) {
		t.Error("Oliver is offered the community's version of a song he isn't given")
	}
	if w := f.o.do("POST", fmt.Sprintf("/api/communities/%d/catalog/fork/%d", f.community, f.other), map[string]int64{"songbookId": f.repertoire}); w.Code != http.StatusForbidden {
		t.Errorf("Oliver makes the community's version of a song he isn't given: %d", w.Code)
	}
	if w := f.o.saveRaw(f.catalogue, "title: x\n"); w.Code != http.StatusForbidden {
		t.Errorf("Oliver edits the catalogue's song in place: %d", w.Code)
	}

	// His first edit: the community's version, everywhere, keys kept.
	version := f.o.variantID(f.o.do("POST", fmt.Sprintf("/api/communities/%d/catalog/fork/%d", f.community, f.catalogue), map[string]int64{"songbookId": f.oliverBook}))
	for book, key := range map[int64]int{f.repertoire: 0, f.oliverBook: 2, f.poppyBook: -1, f.adminBook: 4} {
		if n, in := entryKey(t, st, book, version); !in || n != key {
			t.Errorf("songbook %d: the version %v, key %d (want %d)", book, in, n, key)
		}
	}
	if v, _ := st.GetVariant(ctx, version); v == nil || v.OwnerCommunityID == nil || *v.OwnerCommunityID != f.community || v.Public || v.ForkedFromID == nil || *v.ForkedFromID != f.catalogue {
		t.Fatalf("the community's version: %+v", v)
	}
	if as, err := st.ListStudentAssignments(ctx, f.community, f.oliver, true); err != nil || len(as) != 1 || as[0].VariantID == nil || *as[0].VariantID != version {
		t.Errorf("Oliver's assignment doesn't follow the version: %v %+v", err, as)
	}

	// A chord grid, a Guitar Pro file, a reference: in place.
	html = f.o.page(fmt.Sprintf("/song/%d?songbook=%d", version, f.oliverBook))
	if !strings.Contains(html, `data-can-edit="true"`) || strings.Contains(html, "data-edit-offer") || strings.Contains(html, "new-version-open") || strings.Contains(html, "song-publish-toggle") {
		t.Error("Oliver on the community's version: not the editor, or offered a new version or publishing")
	}
	if w := f.o.saveRaw(version, "title: Small Plane\nartist: Saltwater Radio\nkey: D\n\n[Verse]\ngrid: | D | G | A | D |\n"); w.Code != http.StatusOK {
		t.Fatalf("Oliver's chord grid: %d %s", w.Code, w.Body)
	}
	if w := f.o.putGP(version, "small-plane.gp"); w.Code != http.StatusOK {
		t.Fatalf("Oliver's Guitar Pro file: %d %s", w.Code, w.Body)
	}
	if w := f.o.do("PUT", fmt.Sprintf("/api/songs/%d/meta", version), map[string]string{"key": "reference", "value": "https://www.youtube.com/watch?v=dQw4w9WgXcQ"}); w.Code != http.StatusOK {
		t.Fatalf("Oliver's reference: %d %s", w.Code, w.Body)
	}
	if w := f.o.do("POST", fmt.Sprintf("/api/variants/%d/versions", version), map[string]any{"name": "mine", "songbookId": f.oliverBook}); w.Code != http.StatusForbidden {
		t.Errorf("Oliver makes a new version beside the community's: %d", w.Code)
	}
	// Who did it, in the history.
	revs, err := st.ListRevisions(ctx, "variant", fmt.Sprint(version))
	if err != nil || len(revs) == 0 || revs[0].ActorID == nil || *revs[0].ActorID != f.oliver {
		t.Errorf("the history doesn't say Oliver: %v %+v", err, revs)
	}

	// The coach sees it all and edits it; Poppy, given it too, edits it;
	// George, not given it, doesn't; nor an outsider to the songbooks.
	if text, _, _ := st.LoadVariantRaw(version); !strings.Contains(text, "grid:") || !strings.Contains(text, "G | A") || !strings.Contains(text, "youtube.com") {
		t.Errorf("the version's text: %q", text)
	}
	if w := f.c.do("GET", fmt.Sprintf("/song/%d/gp-file", version), nil); w.Code != http.StatusOK {
		t.Errorf("the coach gets the Guitar Pro file: %d", w.Code)
	}
	if w := f.p.do("GET", fmt.Sprintf("/song/%d/gp-file", version), nil); w.Code != http.StatusOK {
		t.Errorf("Poppy gets the Guitar Pro file: %d", w.Code)
	}
	if w := f.c.saveRaw(version, "title: Small Plane\nartist: Saltwater Radio\nkey: D\n\n[Verse]\ngrid: | D | G | A | D |\n\n[Chorus]\ngrid: | G | D |\n"); w.Code != http.StatusOK {
		t.Errorf("the coach edits after Oliver: %d", w.Code)
	}
	if w := f.p.saveRaw(version, "title: Small Plane\nartist: Saltwater Radio\nkey: D\n\n[Verse]\ngrid: | D | G | A | D |\n"); w.Code != http.StatusOK {
		t.Errorf("Poppy, given the song: %d", w.Code)
	}
	if w := f.g.saveRaw(version, "title: x\n"); w.Code != http.StatusForbidden {
		t.Errorf("George, not given the song, edits it: %d", w.Code)
	}
	if w := f.g.putGP(version, "x.gp"); w.Code != http.StatusForbidden {
		t.Errorf("George replaces its Guitar Pro file: %d", w.Code)
	}
	// Each keeps their key: the song's own key changed, not theirs.
	for book, key := range map[int64]int{f.oliverBook: 2, f.poppyBook: -1} {
		if n, _ := entryKey(t, st, book, version); n != key {
			t.Errorf("songbook %d's key after the edits: %d (want %d)", book, n, key)
		}
	}

	// Taken back from Poppy: no more edits.
	if w := f.c.do("DELETE", fmt.Sprintf("/api/communities/%d/songbooks/%d/songs/%d", f.community, f.poppyBook, version), nil); w.Code/100 != 2 {
		t.Fatalf("taking it back from Poppy: %d", w.Code)
	}
	if w := f.p.saveRaw(version, "title: x\n"); w.Code != http.StatusForbidden {
		t.Errorf("Poppy edits after it's taken back: %d", w.Code)
	}

	// A site admin who is a student there: from their songbook, the
	// community's version too (not the catalogue's song in place); from
	// elsewhere, the catalogue's song is theirs to edit in place.
	if err := st.AddToSongbook(ctx, f.adminBook, f.other, f.coach); err != nil {
		t.Fatal(err)
	}
	if html := f.a.page(fmt.Sprintf("/song/%d?songbook=%d", f.other, f.adminBook)); !strings.Contains(html, `data-edit-offer="community"`) {
		t.Error("the admin student, from their songbook: no community's version offered")
	}
	if html := f.a.page(fmt.Sprintf("/song/%d", f.other)); !strings.Contains(html, `data-can-edit="true"`) || strings.Contains(html, `data-edit-offer="community"`) {
		t.Error("the admin student, from elsewhere: not the catalogue's editor")
	}
}

// TestStudentEditRight is the rule itself (store.SongRights): a coaching
// community's song is edited by its coach and by the students their
// songbook gives it to; not by another student, not the repertoire's
// others, not after it's taken back, not by a former student.
func TestStudentEditRight(t *testing.T) {
	st := store.NewTestStore(t)
	f := newCoachFlow(t, st)
	ctx := f.ctx
	mine, err := st.CreateCommunityVariant(ctx, f.community, "Mine", "", f.coach)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := st.CreateCommunityVariant(ctx, f.community, "Theirs", "", f.coach)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddToSongbook(ctx, f.oliverBook, mine, f.coach); err != nil {
		t.Fatal(err)
	}
	if err := st.AddToSongbook(ctx, f.poppyBook, theirs, f.coach); err != nil {
		t.Fatal(err)
	}
	edit := func(who, song int64) (bool, bool) {
		r, err := st.SongRightsFor(ctx, who, song)
		if err != nil {
			t.Fatal(err)
		}
		return r.Edit, r.AssignedStudent
	}
	for _, x := range []struct {
		what       string
		who, song  int64
		edit, asgn bool
	}{
		{"Oliver, his song", f.oliver, mine, true, true},
		{"Oliver, Poppy's song", f.oliver, theirs, false, false},
		{"George, a repertoire song given to no one he knows", f.george, mine, false, false},
		{"the coach, any", f.coach, theirs, true, false},
	} {
		if e, a := edit(x.who, x.song); e != x.edit || a != x.asgn {
			t.Errorf("%s: edit %v, assigned %v (want %v, %v)", x.what, e, a, x.edit, x.asgn)
		}
	}
	if r, _ := st.SongRightsFor(ctx, f.oliver, mine); r.Publish {
		t.Error("a student may publish the community's song")
	}
	if err := st.RemoveFromSongbook(ctx, f.oliverBook, mine); err != nil {
		t.Fatal(err)
	}
	if e, _ := edit(f.oliver, mine); e {
		t.Error("Oliver edits his song after it's taken back")
	}
	// A former student: their songbook still holds the song, they edit
	// nothing.
	if err := st.AddToSongbook(ctx, f.poppyBook, mine, f.coach); err != nil {
		t.Fatal(err)
	}
	if e, _ := edit(f.poppy, mine); !e {
		t.Fatal("Poppy, given the song, doesn't edit it")
	}
	if err := st.RemoveCommunityMember(ctx, f.community, f.poppy); err != nil {
		t.Fatal(err)
	}
	if e, _ := edit(f.poppy, mine); e {
		t.Error("a former student edits the song")
	}
	// A plain community keeps its rule: who edits the repertoire edits
	// its songs.
	plain, err := st.CreateCommunity(ctx, "Jam", f.coach)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddCommunityMember(ctx, plain, f.george, "member", f.coach); err != nil {
		t.Fatal(err)
	}
	song, err := st.CreateCommunityVariant(ctx, plain, "Jam tune", "", f.coach)
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := edit(f.george, song); !e {
		t.Error("a plain community's member no longer edits its repertoire")
	}
}

// TestQuickAddForAStudent: the coach pastes a video link (or types
// "Title — Artist") in the Add dialog: the song is found — the
// catalogue's, or the community's own version once it has one — and
// linked in one click, or created with its video as its reference and
// its lyrics fetched; one more click gives it to a student in a key
// (quick_add.go). As simple as finding the video and the lyrics without
// the app.
func TestQuickAddForAStudent(t *testing.T) {
	st := store.NewTestStore(t)
	f := newCoachFlow(t, st)
	ctx := f.ctx
	if err := st.SetCatalogueReference(ctx, f.catalogue, "https://www.youtube.com/watch?v=plane111111"); err != nil {
		t.Fatal(err)
	}
	videos := map[string][2]string{
		"plane111111": {"Saltwater Radio - Small Plane (Official Video)", "SaltwaterRadioVEVO"},
		"other222222": {"Small Plane [Lyrics]", "Saltwater Radio - Topic"},
		"newsong3333": {"Lantern Walk (Clip officiel)", "Studio Lumen - Topic"},
	}
	oembed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := store.YouTubeID(r.URL.Query().Get("url"))
		x, ok := videos[v]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"title":%q,"author_name":%q}`, x[0], x[1])
	}))
	defer oembed.Close()
	httpapi.RefTitles = &reftitle.Fetcher{Client: oembed.Client(), Providers: map[string]string{
		"youtube.com": oembed.URL + "/oembed", "youtu.be": oembed.URL + "/oembed"}}
	httpapi.QuickDraft = func(ctx context.Context, _ *store.Store, title, artist string) (string, error) {
		return "title: " + title + "\nartist: " + artist + "\n\n[Verse]\nla la la\n\n[Chorus]\noh oh\n", nil
	}
	defer func() { httpapi.RefTitles, httpapi.QuickDraft = nil, nil }()

	type lookup struct {
		Title, Artist, Reference string
		Match                    *store.SongMatch
		InSongbook, Lyrics       bool
	}
	look := func(c client, q string) lookup {
		t.Helper()
		w := c.do("GET", fmt.Sprintf("/api/songbooks/%d/quick-add?q=%s", f.repertoire, url.QueryEscape(q)), nil)
		if w.Code != http.StatusOK {
			t.Fatalf("quick add %q: %d %s", q, w.Code, w.Body)
		}
		var l lookup
		if err := json.Unmarshal(w.Body.Bytes(), &l); err != nil {
			t.Fatal(err)
		}
		return l
	}

	// The catalogue's song, by its video: found, linked (1 click), given
	// to Oliver in his key (1 click).
	l := look(f.c, "https://youtu.be/plane111111")
	if l.Match == nil || l.Match.ID != f.catalogue || !l.Match.ByVideo || l.InSongbook {
		t.Fatalf("the catalogue's song by its video: %+v", l)
	}
	if l.Title != "Small Plane" || l.Artist != "Saltwater Radio" {
		t.Errorf("the video's title read as %q — %q", l.Title, l.Artist)
	}
	id := f.c.variantID(f.c.do("POST", fmt.Sprintf("/api/songbooks/%d/quick-add", f.repertoire), map[string]any{"songId": l.Match.ID}))
	if _, in := entryKey(t, st, f.repertoire, id); id != f.catalogue || !in {
		t.Fatalf("linked: %d %v", id, in)
	}
	if w := f.giveKey(id, f.oliver, 2); w.Code != http.StatusOK {
		t.Fatalf("given to Oliver: %d", w.Code)
	}
	// Another video of it, on the artist's channel: found by its title
	// and artist, in the songbook already.
	if l := look(f.c, "https://www.youtube.com/watch?v=other222222"); l.Match == nil || l.Match.ID != f.catalogue || l.Match.ByVideo || !l.InSongbook {
		t.Errorf("another video of the song: %+v", l)
	}
	// Typed, either way round.
	for _, q := range []string{"Faded Season — Ivory Coastline", "Ivory Coastline - Faded Season", "the faded season - ivory coastline"} {
		if l := look(f.c, q); l.Match == nil || l.Match.ID != f.other {
			t.Errorf("typed %q: %+v", q, l)
		}
	}

	// A song nowhere yet: created with its reference and its lyrics, in
	// the repertoire (1 click), then given to Poppy (1 click).
	l = look(f.c, "https://www.youtube.com/watch?v=newsong3333")
	if l.Match != nil || l.Title != "Lantern Walk" || l.Artist != "Studio Lumen" || !l.Lyrics || !strings.Contains(l.Reference, "newsong3333") {
		t.Fatalf("a new song's lookup: %+v", l)
	}
	w := f.c.do("POST", fmt.Sprintf("/api/songbooks/%d/quick-add", f.repertoire), map[string]any{"title": l.Title, "artist": l.Artist, "reference": l.Reference})
	created := f.c.variantID(w)
	if !strings.Contains(w.Body.String(), `"lyrics":true`) {
		t.Errorf("the new song's lyrics: %s", w.Body)
	}
	if v, _ := st.GetVariant(ctx, created); v == nil || v.OwnerCommunityID == nil || *v.OwnerCommunityID != f.community || v.Public {
		t.Fatalf("the new song: %+v", v)
	}
	if text, _, _ := st.LoadVariantRaw(created); !strings.Contains(text, "[Chorus]") || !strings.Contains(text, "newsong3333") {
		t.Errorf("the new song's text: %q", text)
	}
	if _, in := entryKey(t, st, f.repertoire, created); !in {
		t.Error("the new song isn't in the repertoire")
	}
	if w := f.giveKey(created, f.poppy, -1); w.Code != http.StatusOK {
		t.Fatalf("given to Poppy: %d", w.Code)
	}
	// Pasted again: the song just made.
	if l := look(f.c, "https://www.youtube.com/watch?v=newsong3333"); l.Match == nil || l.Match.ID != created || !l.Match.Own || !l.InSongbook {
		t.Errorf("the new song, pasted again: %+v", l)
	}

	// Once the community has its version of the catalogue's song, the
	// video finds that version.
	version := f.c.variantID(f.c.do("POST", fmt.Sprintf("/api/communities/%d/catalog/fork/%d", f.community, f.catalogue), map[string]int64{"songbookId": f.repertoire}))
	if l := look(f.c, "https://youtu.be/plane111111"); l.Match == nil || l.Match.ID != version || !l.Match.Own {
		t.Errorf("the video, after the community's version: %+v", l)
	}
	// Someone who can't change the songbook: refused.
	outsider, err := st.CreatePerson(ctx, "outsider", "Outsider", "outsider@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if w := as(t, st, outsider).do("GET", fmt.Sprintf("/api/songbooks/%d/quick-add?q=x+-+y", f.repertoire), nil); w.Code != http.StatusForbidden {
		t.Errorf("an outsider's quick add: %d", w.Code)
	}
}
