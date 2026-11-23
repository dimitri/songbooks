package store

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

// TestKeepEntryKeys: a save transposing a song (its chords moved, all by
// one interval) moves the keys songbooks keep it in back by as much, in
// the save's transaction, so each sounds where it did: a student's and a
// group's (even "original"), any chosen key in a person's or a band's
// songbook, an event program's; an entry in the song's own key elsewhere
// follows the song. A revert moves them back again; an edit that doesn't
// move the chords moves nothing — the key: header put right alone is a
// label fix: every key kept stays (the same sound, a new name).
func TestKeepEntryKeys(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	coach, err := st.CreatePerson(ctx, "coach", "Coach", "coach@example.com")
	must(err)
	zoe, err := st.CreatePerson(ctx, "zoe", "Zoé", "zoe@example.com")
	must(err)
	max, err := st.CreatePerson(ctx, "max", "Max", "max@example.com")
	must(err)
	c, err := st.CreateCommunity(ctx, "Voice", coach)
	must(err)
	must(st.SetCommunityKind(ctx, c, "coaching"))
	for _, p := range []int64{zoe, max} {
		_, err := st.AddCommunityMember(ctx, c, p, "member", coach)
		must(err)
	}
	song, err := st.CreateVariant(ctx, OwnerCommunity, c, "Song", "", coach)
	must(err)
	_, err = st.SaveVariantRaw(song, "title: Song\nkey: G\n\n[V]\n[G]la [C]la [D]la\n", "", coach)
	must(err)
	rep, err := st.GetDefaultSongbook(ctx, "community", c)
	must(err)
	zoeBook, err := st.StudentSongbook(ctx, c, zoe, "Zoé’s songs", coach)
	must(err)
	maxBook, err := st.StudentSongbook(ctx, c, max, "Max’s songs", coach)
	must(err)
	g, err := st.CreateCoachingGroup(ctx, c, "Tuesday", "year", nil, nil, nil)
	must(err)
	var groupBook int64
	must(st.db.QueryRow(ctx, `SELECT songbook_id FROM coaching_groups WHERE id = $1`, g).Scan(&groupBook))
	mine, err := st.GetDefaultSongbook(ctx, "person", max)
	must(err)
	other, err := st.CreateSongbook(ctx, "person", zoe, "Gigs", false)
	must(err)
	for _, sb := range []int64{rep, zoeBook, maxBook, groupBook, mine, other} {
		must(st.AddToSongbook(ctx, sb, song, coach))
	}
	must(st.SetEntrySemitones(ctx, zoeBook, song, -2))  // F
	must(st.SetEntrySemitones(ctx, groupBook, song, 3)) // Bb
	must(st.SetEntrySemitones(ctx, mine, song, 12))     // G, an octave up
	// maxBook: "original" (G), the coach's choice; other: G, following.
	ev, err := st.CreateEvent(ctx, "community", c, "concert", "Concert", "", "", time.Now().Add(24*time.Hour), nil, "members", coach)
	must(err)
	must(st.AddProgramSongs(ctx, ev, []int64{song}))
	prog, err := st.ListEventProgram(ctx, ev)
	must(err)
	must(st.SetProgramKey(ctx, ev, prog[0].ID, 1)) // Ab
	keys := func() map[int64]int {
		t.Helper()
		out := map[int64]int{}
		for _, sb := range []int64{rep, zoeBook, maxBook, groupBook, mine, other} {
			n, err := st.EntrySemitones(ctx, sb, song)
			must(err)
			out[sb] = n
		}
		n, _, err := st.ProgramKeyOf(ctx, ev, song)
		must(err)
		out[0] = n
		return out
	}
	check := func(when string, want map[int64]int) {
		t.Helper()
		got := keys()
		for sb, n := range want {
			if got[sb] != n {
				t.Errorf("%s: songbook %d at %d, want %d (all: %v)", when, sb, got[sb], n, got)
			}
		}
	}

	// G → A: each key back by 2; the repertoire's and Zoé's other
	// songbook's "own key" follow the song; Max's octave wraps.
	_, err = st.SaveVariantRaw(song, "title: Song\nkey: A\n\n[V]\n[A]la [D]la [E]la\n", "", coach)
	must(err)
	check("G → A", map[int64]int{rep: 0, zoeBook: -4, maxBook: -2, groupBook: 1, mine: 10, other: 0, 0: -1})

	// A lyric edit: nothing moves.
	_, err = st.SaveVariantRaw(song, "title: Song\nkey: A\n\n[V]\n[A]lo [D]lo [E]lo\n", "", coach)
	must(err)
	check("a lyric edit", map[int64]int{zoeBook: -4, maxBook: -2, groupBook: 1, mine: 10, 0: -1})

	// Reverted to G (the revision before the move): back where they were.
	revs, err := st.ListRevisions(ctx, "variant", strconv.FormatInt(song, 10))
	must(err)
	if len(revs) < 2 {
		t.Fatalf("revisions: %d", len(revs))
	}
	must(st.RevertRevision(ctx, revs[1].ID, coach))
	check("reverted to G", map[int64]int{rep: 0, zoeBook: -2, maxBook: 0, groupBook: 3, mine: 12, other: 0, 0: 1})

	// Down a fourth with no header (the chart transposed in place): each up
	// 5, an octave off once past +12.
	_, err = st.SaveVariantRaw(song, "title: Song\n\n[V]\n[G]la [C]la [D]la\n", "", coach)
	must(err)
	_, err = st.SaveVariantRaw(song, "title: Song\n\n[V]\n[D]la [G]la [A]la\n", "", coach)
	must(err)
	check("G → D in place", map[int64]int{zoeBook: 3, maxBook: 5, groupBook: 8, mine: 5, 0: 6})

	// The header alone put right (the chart was in D, its header said
	// nothing; then a wrong "E"; then "D"): a label fix each time, every
	// key stays.
	_, err = st.SaveVariantRaw(song, "title: Song\nkey: E\n\n[V]\n[D]la [G]la [A]la\n", "", coach)
	must(err)
	_, err = st.SaveVariantRaw(song, "title: Song\nkey: D\n\n[V]\n[D]la [G]la [A]la\n", "", coach)
	must(err)
	check("the header put right", map[int64]int{rep: 0, zoeBook: 3, maxBook: 5, groupBook: 8, mine: 5, other: 0, 0: 6})
	// Reverting a label fix moves nothing either.
	revs, err = st.ListRevisions(ctx, "variant", strconv.FormatInt(song, 10))
	must(err)
	must(st.RevertRevision(ctx, revs[0].ID, coach))
	check("a label fix reverted", map[int64]int{zoeBook: 3, maxBook: 5, groupBook: 8, mine: 5, 0: 6})
}

// TestSongProposals: a student proposes their own song or a public one,
// never a band's private song; the coach sees it while it waits (open,
// play: no link, no arrangement); accepted, a public song is linked into
// the repertoire and the student's songbook in a key, a private one
// becomes the community's arrangement made from it (theirs untouched),
// found again by a later proposal; declined, nothing moves.
func TestSongProposals(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	coach, err := st.CreatePerson(ctx, "coach", "Coach", "coach@example.com")
	must(err)
	zoe, err := st.CreatePerson(ctx, "zoe", "Zoé Student", "zoe@example.com")
	must(err)
	c, err := st.CreateCommunity(ctx, "Voice", coach)
	must(err)
	must(st.SetCommunityKind(ctx, c, "coaching"))
	_, err = st.AddCommunityMember(ctx, c, zoe, "member", coach)
	must(err)
	if cs, err := st.StudentCoachingCommunities(ctx, zoe); err != nil || len(cs) != 1 || cs[0].ID != c {
		t.Fatalf("Zoé's coaching communities: %+v %v", cs, err)
	}
	if cs, _ := st.StudentCoachingCommunities(ctx, coach); len(cs) != 0 {
		t.Fatalf("the coach is nobody's student: %+v", cs)
	}

	own, err := st.CreateVariant(ctx, OwnerPerson, zoe, "Own", "Zoé", zoe)
	must(err)
	_, err = st.SaveVariantRaw(own, "title: Own\nkey: D\n\n[V]\n[D]la\n", "", zoe)
	must(err)
	pub, err := st.CreateVariant(ctx, OwnerCatalog, 0, "Public", "Someone", coach)
	must(err)
	band, err := st.CreateBand(ctx, "Band", zoe)
	must(err)
	bandSong, err := st.CreateVariant(ctx, OwnerBand, band, "Band song", "", zoe)
	must(err)

	for _, x := range []struct {
		song int64
		ok   bool
	}{{own, true}, {pub, true}, {bandSong, false}} {
		if err := st.ProposalCheck(ctx, zoe, c, x.song); (err == nil) != x.ok {
			t.Errorf("ProposalCheck(%d): %v", x.song, err)
		}
	}

	// The coach doesn't see Zoé's song, until she proposes it.
	if r, _ := st.SongRightsFor(ctx, coach, own); r.See {
		t.Fatal("the coach sees Zoé's private song before it's proposed")
	}
	wished := 2
	id, created, err := st.ProposeSong(ctx, c, zoe, own, "  I love it  ", &wished)
	must(err)
	if !created {
		t.Fatal("a first proposal wasn't created")
	}
	again, created, err := st.ProposeSong(ctx, c, zoe, own, "I love it, really", nil)
	must(err)
	if created || again != id {
		t.Fatalf("proposing again while it waits: %d %v", again, created)
	}
	q, err := st.GetSongRequest(ctx, id)
	must(err)
	if !q.IsProposal() || q.Note != "I love it, really" || q.WishedSemitones != nil || q.Status != SongRequestPending {
		t.Fatalf("the proposal: %+v", q)
	}
	r, err := st.SongRightsFor(ctx, coach, own)
	must(err)
	if !r.See || !r.ViaProposal || r.Edit || r.CanLinkFrom(PersonOwner(coach)) || r.CanLinkFrom(CommunityOwner(c)) || r.CanVersionFor(CommunityOwner(c)) {
		t.Fatalf("the coach's rights on a proposed song: %+v", r)
	}
	if err := st.LinkCheckFor(ctx, coach, own, PersonOwner(coach)); err == nil {
		t.Fatal("the coach may link the proposed song into their own songbook")
	}

	// Accepted: the community's arrangement of it, in the repertoire and
	// in Zoé's songbook at +2; hers untouched; the coach's sight of hers
	// gone with the proposal.
	zoeBook, err := st.StudentSongbook(ctx, c, zoe, "Zoé’s songs", coach)
	must(err)
	got, err := st.AcceptSongProposal(ctx, id, coach, zoeBook, 2, "")
	must(err)
	if !got.Copied || got.SongID == own {
		t.Fatalf("accepting a private song: %+v", got)
	}
	var owner int64
	var forked *int64
	var public bool
	must(st.db.QueryRow(ctx, `SELECT coalesce(owner_community_id, 0), forked_from_id, public FROM songs WHERE id = $1`, got.SongID).Scan(&owner, &forked, &public))
	if owner != c || forked == nil || *forked != own || public {
		t.Fatalf("the community's arrangement: owner %d forked %v public %v", owner, forked, public)
	}
	if ok, _ := st.RepertoireHas(ctx, c, got.SongID); !ok {
		t.Error("not in the repertoire")
	}
	if n, _ := st.EntrySemitones(ctx, zoeBook, got.SongID); n != 2 {
		t.Errorf("Zoé's key: %d", n)
	}
	if r, _ := st.SongRightsFor(ctx, zoe, got.SongID); !r.See || !r.Edit {
		t.Errorf("Zoé on the community's arrangement: %+v", r)
	}
	if r, _ := st.SongRightsFor(ctx, coach, got.SongID); !r.Edit {
		t.Errorf("the coach on the community's arrangement: %+v", r)
	}
	if r, _ := st.SongRightsFor(ctx, coach, own); r.See {
		t.Error("the coach still sees Zoé's own song")
	}
	if r, _ := st.SongRightsFor(ctx, zoe, own); !r.Edit {
		t.Error("Zoé lost her own song")
	}
	q, _ = st.GetSongRequest(ctx, id)
	if q.Status != SongRequestAdded || q.ResultVariantID != got.SongID {
		t.Errorf("the answered proposal: %+v", q)
	}
	if _, err := st.AcceptSongProposal(ctx, id, coach, zoeBook, 0, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("accepting twice: %v", err)
	}
	// Proposed again (taken out of her songbook since): the same
	// arrangement, nothing new made.
	must(st.RemoveFromSongbook(ctx, zoeBook, got.SongID))
	id2, _, err := st.ProposeSong(ctx, c, zoe, own, "", nil)
	must(err)
	if id2 != id {
		t.Errorf("a new row for the same song: %d", id2)
	}
	if target, copied, ok, _ := st.ProposalTarget(ctx, c, zoe, own); !ok || copied || target != got.SongID {
		t.Errorf("the target the second time: %d %v %v", target, copied, ok)
	}
	again2, err := st.AcceptSongProposal(ctx, id, coach, zoeBook, -1, "")
	must(err)
	if again2.Copied || again2.SongID != got.SongID {
		t.Errorf("accepted again: %+v", again2)
	}

	// A public song: linked as it is.
	pid, _, err := st.ProposeSong(ctx, c, zoe, pub, "", nil)
	must(err)
	if r, _ := st.SongRightsFor(ctx, coach, pub); r.ViaProposal {
		t.Error("a public song is seen through the proposal")
	}
	pg, err := st.AcceptSongProposal(ctx, pid, coach, zoeBook, 0, "")
	must(err)
	if pg.Copied || pg.SongID != pub {
		t.Fatalf("a public song accepted: %+v", pg)
	}
	if ok, _ := st.RepertoireHas(ctx, c, pub); !ok {
		t.Error("the public song isn't linked in the repertoire")
	}

	// Declined with a note: nothing moves; the coach's sight goes.
	other, err := st.CreateVariant(ctx, OwnerPerson, zoe, "Other", "", zoe)
	must(err)
	oid, _, err := st.ProposeSong(ctx, c, zoe, other, "", nil)
	must(err)
	must(st.AnswerSongRequest(ctx, oid, SongRequestDeclined, coach, "Not for your voice yet"))
	q, _ = st.GetSongRequest(ctx, oid)
	if q.Status != SongRequestDeclined || q.Reply != "Not for your voice yet" {
		t.Errorf("declined: %+v", q)
	}
	if r, _ := st.SongRightsFor(ctx, coach, other); r.See {
		t.Error("the coach sees a declined proposal's song")
	}
	if ok, _ := st.RepertoireHas(ctx, c, other); ok {
		t.Error("a declined song is in the repertoire")
	}
	list, err := st.StudentProposals(ctx, c, zoe)
	must(err)
	if len(list) != 3 {
		t.Errorf("Zoé's proposals: %+v", list)
	}

	// An "Ask for it" of a repertoire song stays an ask.
	ask, _, err := st.RequestSong(ctx, c, zoe, pub)
	must(err)
	if q, _ := st.GetSongRequest(ctx, ask); q.Kind != SongRequestAsk || q.Note != "" {
		t.Errorf("an ask after a proposal of the same song: %+v", q)
	}
}

// TestKeepEntryKeysCatalogue: a catalogue song's key: header put right
// (its chart isn't transposed in place there) is a label fix: the keys
// people hold it in keep their semitones, so the recording and the chords
// they play sound as before; only the key's name follows the header.
func TestKeepEntryKeysCatalogue(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	ann, err := st.CreatePerson(ctx, "ann", "Ann", "ann@example.com")
	must(err)
	song, err := st.CreateVariant(ctx, OwnerCatalog, 0, "Cat", "", ann)
	must(err)
	const before = "title: Cat\nkey: C\n\n[V]\n[C]la [G]la\n"
	_, err = st.SaveVariantRaw(song, before, "", ann)
	must(err)
	mine, err := st.GetDefaultSongbook(ctx, "person", ann)
	must(err)
	must(st.AddToSongbook(ctx, mine, song, ann))
	must(st.SetEntrySemitones(ctx, mine, song, 3)) // Eb
	var version int
	must(st.db.QueryRow(ctx, `SELECT version FROM songs WHERE id = $1`, song).Scan(&version))
	_, err = st.ApplyCatalogueEdit(ctx, CatalogueEditInput{SongID: song, AuthorID: ann, BaseVersion: version, BaseBody: before,
		Title: "Cat", Body: "title: Cat\nkey: D\n\n[V]\n[C]la [G]la\n", Summary: "the key is D"})
	must(err)
	if n, _ := st.EntrySemitones(ctx, mine, song); n != 3 {
		t.Errorf("Ann's key after the header C → D: %d, want 3 (the same sound, named F now)", n)
	}
}
