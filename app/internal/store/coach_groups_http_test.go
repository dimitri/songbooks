package store_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/internal/store"
)

// A coaching group works on one song (docs/coaching.md, "A group works on
// one song"): the community's, never a copy per student or per group.
// Every member of the group edits it in place, the coach and the others
// see each edit (a revision with its author, revertable by the coach);
// only the key is each place's (the group's entry, a student's own
// entry). Leaving the group ends it. A linked catalogue song stays the
// catalogue's: a member's fix goes to it under its rules, a change their
// way makes the community's arrangement.

// newGroup makes a group of the community with members, and returns it
// and its songbook.
func (f *coachFlow) newGroup(t *testing.T, st *store.Store, name string, members ...int64) (int64, int64) {
	t.Helper()
	id, err := st.CreateCoachingGroup(f.ctx, f.community, name, "year", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetGroupMembers(f.ctx, id, members, f.coach); err != nil {
		t.Fatal(err)
	}
	g, err := st.GetCoachingGroup(f.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return id, g.SongbookID
}

// giveGroupKey is the coach's POST /api/songs/{id}/coach-keys for a group.
func (f *coachFlow) giveGroupKey(song, group int64, semitones int) *httptest.ResponseRecorder {
	return f.c.do("POST", fmt.Sprintf("/api/songs/%d/coach-keys", song),
		map[string]any{"communityId": f.community, "groupId": group, "semitones": semitones})
}

func songCount(t *testing.T, st *store.Store) int {
	return store.CountForTest(t, st, `SELECT count(*) FROM songs`)
}

func TestGroupSharesOneSong(t *testing.T) {
	st := store.NewTestStore(t)
	f := newCoachFlow(t, st)
	ctx := f.ctx
	group, groupBook := f.newGroup(t, st, "Mardi soir", f.oliver, f.poppy)

	// The community's song, in the repertoire, given to the group in its
	// key and to Oliver in his: links in both songbooks, nothing copied.
	song, err := st.CreateCommunityVariant(ctx, f.community, "Le Quai", "Studio", f.coach)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddToSongbook(ctx, f.repertoire, song, f.coach); err != nil {
		t.Fatal(err)
	}
	if w := f.c.saveRaw(song, "title: Le Quai\nartist: Studio\nkey: G\n\n[Verse]\nthe coach's line\n"); w.Code != http.StatusOK {
		t.Fatalf("the coach's chart: %d %s", w.Code, w.Body)
	}
	songs := songCount(t, st)
	if w := f.giveGroupKey(song, group, -2); w.Code != http.StatusOK {
		t.Fatalf("giving the group the song: %d %s", w.Code, w.Body)
	}
	if w := f.giveKey(song, f.oliver, 3); w.Code != http.StatusOK {
		t.Fatalf("giving Oliver the song: %d %s", w.Code, w.Body)
	}
	if n := songCount(t, st); n != songs {
		t.Errorf("assigning made %d songs", n-songs)
	}
	if n, in := entryKey(t, st, groupBook, song); !in || n != -2 {
		t.Errorf("the group's entry: %v %d", in, n)
	}
	if text, _, _ := st.LoadVariantRaw(song); !strings.Contains(text, "key: G") {
		t.Errorf("picking keys changed the song's own key: %q", text)
	}

	// Both members and the coach on the one song: the editor, in place,
	// from the group's songbook; no offer of a copy of their own.
	for name, c := range map[string]client{"Oliver": f.o, "Poppy": f.p, "the coach": f.c} {
		html := c.page(fmt.Sprintf("/song/%d?songbook=%d", song, groupBook))
		if !strings.Contains(html, `data-can-edit="true"`) || strings.Contains(html, "data-edit-offer") {
			t.Errorf("%s from the group's songbook: not the editor in place", name)
		}
	}
	if r, err := st.SongRightsFor(ctx, f.poppy, song); err != nil || !r.Edit || !r.GroupMember || r.AssignedStudent || r.Publish {
		t.Errorf("Poppy's rights: %v %+v", err, r)
	}

	// Oliver edits, Poppy sees it and edits after him, the coach sees
	// both: one song, two revisions with their authors.
	if w := f.o.saveRaw(song, "title: Le Quai\nartist: Studio\nkey: G\n\n[Verse]\nthe coach's line\n\n[Chorus]\nOliver's chorus\n"); w.Code != http.StatusOK {
		t.Fatalf("Oliver's edit: %d %s", w.Code, w.Body)
	}
	if html := f.p.page(fmt.Sprintf("/song/%d?songbook=%d", song, groupBook)); !strings.Contains(html, "s chorus") {
		t.Error("Poppy doesn't see Oliver's chorus")
	}
	if w := f.p.saveRaw(song, "title: Le Quai\nartist: Studio\nkey: G\n\n[Verse]\nthe coach's line\n\n[Chorus]\nOliver's chorus\n\n[Bridge]\nPoppy's bridge\n"); w.Code != http.StatusOK {
		t.Fatalf("Poppy's edit: %d %s", w.Code, w.Body)
	}
	if w := f.p.putGP(song, "le-quai.gp"); w.Code != http.StatusOK {
		t.Errorf("Poppy's Guitar Pro file: %d %s", w.Code, w.Body)
	}
	if w := f.o.do("GET", fmt.Sprintf("/song/%d/gp-file", song), nil); w.Code != http.StatusOK {
		t.Errorf("Oliver gets Poppy's Guitar Pro file: %d", w.Code)
	}
	if text, _, _ := st.LoadVariantRaw(song); !strings.Contains(text, "Oliver's chorus") || !strings.Contains(text, "Poppy's bridge") {
		t.Errorf("the song the coach sees: %q", text)
	}
	if n := songCount(t, st); n != songs {
		t.Errorf("editing made %d songs", n-songs)
	}
	revs, err := st.ListRevisions(ctx, "variant", fmt.Sprint(song))
	if err != nil || len(revs) < 2 || revs[0].ActorID == nil || *revs[0].ActorID != f.poppy || revs[1].ActorID == nil || *revs[1].ActorID != f.oliver {
		t.Fatalf("the history: %v %+v", err, revs)
	}
	// The coach takes Poppy's edit back.
	if w := f.c.do("POST", fmt.Sprintf("/api/revisions/%d/revert", revs[0].ID), nil); w.Code/100 != 2 {
		t.Fatalf("the coach reverts Poppy's edit: %d %s", w.Code, w.Body)
	}
	if text, _, _ := st.LoadVariantRaw(song); !strings.Contains(text, "Oliver's chorus") || strings.Contains(text, "Poppy's bridge") {
		t.Errorf("after the revert: %q", text)
	}

	// The keys stayed where they were put: the group's, Oliver's own.
	for book, key := range map[int64]int{groupBook: -2, f.oliverBook: 3, f.repertoire: 0} {
		if n, _ := entryKey(t, st, book, song); n != key {
			t.Errorf("songbook %d's key after the edits: %d (want %d)", book, n, key)
		}
	}

	// George, not in the group, edits nothing of it; nor Poppy once she
	// leaves the group (the coach's members list).
	if w := f.g.saveRaw(song, "title: x\n"); w.Code != http.StatusForbidden {
		t.Errorf("George, not in the group: %d", w.Code)
	}
	if w := f.c.do("PUT", fmt.Sprintf("/api/communities/%d/groups/%d/members", f.community, group), map[string][]int64{"members": {f.oliver}}); w.Code/100 != 2 {
		t.Fatalf("Poppy leaves the group: %d %s", w.Code, w.Body)
	}
	if w := f.p.saveRaw(song, "title: x\n"); w.Code != http.StatusForbidden {
		t.Errorf("Poppy edits after leaving the group: %d", w.Code)
	}
	if w := f.p.putGP(song, "x.gp"); w.Code != http.StatusForbidden {
		t.Errorf("Poppy replaces its Guitar Pro file after leaving: %d", w.Code)
	}
	if w := f.o.saveRaw(song, "title: Le Quai\nartist: Studio\nkey: G\n\n[Verse]\nstill Oliver\n"); w.Code != http.StatusOK {
		t.Errorf("Oliver, still in the group: %d", w.Code)
	}
	// Taken out of the group's songbook (and given to Oliver alone
	// before): Oliver keeps it through his own songbook only.
	if err := st.RemoveFromSongbook(ctx, groupBook, song); err != nil {
		t.Fatal(err)
	}
	if r, _ := st.SongRightsFor(ctx, f.oliver, song); !r.Edit || r.GroupMember || !r.AssignedStudent {
		t.Errorf("Oliver, the song out of the group's songbook: %+v", r)
	}
}

// TestGroupLinkedCatalogueSong: a catalogue song the group's songbook
// links stays The Big Songbook's: a member's fix of it goes to the
// catalogue (an open edit, a newcomer's waiting for review), never in
// place on the community's account; Edit offers both, the fix and the
// community's arrangement, which takes the link's place everywhere in
// the community, keys kept, and every member edits in place.
func TestGroupLinkedCatalogueSong(t *testing.T) {
	st := store.NewTestStore(t)
	f := newCoachFlow(t, st)
	ctx := f.ctx
	group, groupBook := f.newGroup(t, st, "Jeudi midi", f.oliver, f.poppy)
	if err := st.AddToSongbook(ctx, f.repertoire, f.catalogue, f.coach); err != nil {
		t.Fatal(err)
	}
	songs := songCount(t, st)
	if w := f.giveGroupKey(f.catalogue, group, -1); w.Code != http.StatusOK {
		t.Fatalf("giving the group the catalogue's song: %d %s", w.Code, w.Body)
	}
	if n := songCount(t, st); n != songs {
		t.Errorf("assigning the catalogue's song made %d songs", n-songs)
	}
	if r, _ := st.SongRightsFor(ctx, f.oliver, f.catalogue); r.Edit || !r.Contribute {
		t.Errorf("Oliver on the linked catalogue song: %+v (the catalogue's rules: contribute, not edit)", r)
	}

	// Edit, from the group's songbook and from elsewhere: the fix or the
	// community's arrangement, never his own.
	for _, p := range []string{fmt.Sprintf("/song/%d?songbook=%d", f.catalogue, groupBook), fmt.Sprintf("/song/%d", f.catalogue)} {
		html := f.o.page(p)
		if !strings.Contains(html, `data-edit-offer="community"`) || !strings.Contains(html, "data-edit-offer-fix") || !strings.Contains(html, `data-can-edit="true"`) {
			t.Errorf("%s: Oliver isn't offered the fix and the community's arrangement", p)
		}
		if strings.Contains(html, `data-action="copy"`) {
			t.Errorf("%s: Oliver is offered his own arrangement", p)
		}
	}
	// George, in no group of it and not given it: no community's
	// arrangement; the catalogue's song is open to him as to anyone.
	if html := f.g.page(fmt.Sprintf("/song/%d?songbook=%d", f.catalogue, f.repertoire)); strings.Contains(html, `data-edit-offer="community"`) {
		t.Error("George is offered the community's arrangement")
	}
	if w := f.g.do("POST", fmt.Sprintf("/api/communities/%d/catalog/fork/%d", f.community, f.catalogue), map[string]int64{"songbookId": f.repertoire}); w.Code != http.StatusForbidden {
		t.Errorf("George makes the community's arrangement: %d", w.Code)
	}

	// The fix: the catalogue's song, under its rules (his, a newcomer's,
	// waits for review); the song stays as it is meanwhile.
	text, _, err := st.LoadVariantRaw(f.catalogue)
	if err != nil {
		t.Fatal(err)
	}
	before := version(t, st, f.catalogue)
	if w := f.o.saveRaw(f.catalogue, text+"\n[Outro]\nla la\n"); w.Code != http.StatusAccepted || version(t, st, f.catalogue) != before {
		t.Errorf("Oliver's fix of the catalogue's song: %d (want it held for review)", w.Code)
	}

	// The community's arrangement, made by a member from the group's
	// songbook: the link's place everywhere, the group's key kept; Poppy
	// edits it in place.
	arr := f.o.variantID(f.o.do("POST", fmt.Sprintf("/api/communities/%d/catalog/fork/%d", f.community, f.catalogue), map[string]int64{"songbookId": groupBook}))
	for book, key := range map[int64]int{f.repertoire: 0, groupBook: -1} {
		if n, in := entryKey(t, st, book, arr); !in || n != key {
			t.Errorf("songbook %d: the arrangement %v, key %d (want %d)", book, in, n, key)
		}
		if _, in := entryKey(t, st, book, f.catalogue); in {
			t.Errorf("songbook %d still links the catalogue's song", book)
		}
	}
	if v, _ := st.GetVariant(ctx, arr); v == nil || v.OwnerCommunityID == nil || *v.OwnerCommunityID != f.community || v.Public {
		t.Fatalf("the arrangement: %+v", v)
	}
	if n := songCount(t, st); n != songs+1 {
		t.Errorf("one arrangement for the community: %d songs made", n-songs)
	}
	if w := f.p.saveRaw(arr, "title: Small Plane\nartist: Saltwater Radio\n\n[Verse]\ngrid: | D | G |\n"); w.Code != http.StatusOK {
		t.Errorf("Poppy edits the community's arrangement: %d %s", w.Code, w.Body)
	}
	// The catalogue's song given to the group again: the community's
	// arrangement, nothing new.
	if err := st.RemoveFromSongbook(ctx, groupBook, arr); err != nil {
		t.Fatal(err)
	}
	if w := f.c.do("POST", fmt.Sprintf("/api/communities/%d/songbooks/%d/songs", f.community, groupBook), map[string]any{"variantId": f.catalogue, "semitones": -1}); w.Code/100 != 2 {
		t.Fatalf("giving the catalogue's song again: %d %s", w.Code, w.Body)
	}
	if _, in := entryKey(t, st, groupBook, arr); !in {
		t.Error("the group's songbook got the catalogue's song again, not the community's arrangement")
	}
	if n := songCount(t, st); n != songs+1 {
		t.Errorf("giving it again made %d songs", n-songs-1)
	}
	if w := f.g.saveRaw(arr, "title: x\n"); w.Code != http.StatusForbidden {
		t.Errorf("George edits the group's arrangement: %d", w.Code)
	}
}
