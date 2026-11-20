package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func instrumentIDs(list []PersonInstrument) []string {
	out := []string{}
	for _, pi := range list {
		if pi.Instrument == "other" {
			out = append(out, "other:"+pi.OtherName)
		} else {
			out = append(out, pi.Instrument)
		}
	}
	return out
}

func peopleInstruments(t *testing.T, st *Store, id int64) []string {
	t.Helper()
	var list []string
	if err := st.db.QueryRow(context.Background(), `SELECT instruments FROM people WHERE id = $1`, id).Scan(&list); err != nil {
		t.Fatal(err)
	}
	return list
}

func TestPersonInstrumentsOwn(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, _ := st.CreatePerson(ctx, "me", "Me", "")

	gid, err := st.AddInstrument(ctx, me, "guitar", "", "advanced")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddInstrument(ctx, me, "other", "  Theremin ", ""); err != nil {
		t.Fatal(err)
	}
	// "Other" typed as a known instrument is filed under it.
	if _, err := st.AddInstrument(ctx, me, "other", "Chant", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddInstrument(ctx, me, "kazoo", "", ""); err == nil {
		t.Error("unknown instrument ID accepted")
	}
	if _, err := st.AddInstrument(ctx, me, "bass", "", "pro"); err == nil {
		t.Error("unknown level accepted")
	}
	list, _ := st.ListPersonInstruments(ctx, me)
	if got, want := instrumentIDs(list), []string{"voice", "guitar", "other:Theremin"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("instruments = %q, want %q", got, want)
	}
	if list[1].Level != "advanced" || list[1].Source != "self" {
		t.Errorf("guitar = %+v", list[1])
	}
	// people.instruments follows, for the pages that read it.
	if got, want := peopleInstruments(t, st, me), []string{"voice", "guitar", "Theremin"}; !reflect.DeepEqual(got, want) {
		t.Errorf("people.instruments = %q, want %q", got, want)
	}

	if err := st.SetInstrumentLevel(ctx, me, gid, "expert"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetInstrumentLevel(ctx, me, gid, ""); err != nil {
		t.Fatal(err)
	}
	other, _ := st.CreatePerson(ctx, "other", "Other", "")
	if err := st.SetInstrumentLevel(ctx, other, gid, "beginner"); err == nil {
		t.Error("someone else set my level")
	}
	if err := st.RemoveInstrument(ctx, other, gid); err == nil {
		t.Error("someone else removed my instrument")
	}
	if err := st.RemoveInstrument(ctx, me, gid); err != nil {
		t.Fatal(err)
	}
	list, _ = st.ListPersonInstruments(ctx, me)
	if got := instrumentIDs(list); !reflect.DeepEqual(got, []string{"voice", "other:Theremin"}) {
		t.Errorf("after removing guitar: %q", got)
	}
	if got := peopleInstruments(t, st, me); !reflect.DeepEqual(got, []string{"voice", "Theremin"}) {
		t.Errorf("people.instruments after removing guitar: %q", got)
	}
	// Adding it back restores it.
	if _, err := st.AddInstrument(ctx, me, "guitar", "", "beginner"); err != nil {
		t.Fatal(err)
	}
	list, _ = st.ListPersonInstruments(ctx, me)
	if len(list) != 3 || list[1].Instrument != "guitar" || list[1].Level != "beginner" {
		t.Errorf("guitar back: %+v", list)
	}
}

func TestCollectInstrumentsKeepsLevelsAndRemovals(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, _ := st.CreatePerson(ctx, "me", "Me", "")
	band, _ := st.CreateBand(ctx, "Mercy", me)

	if _, err := st.AddInstrument(ctx, me, "bass", "", "intermediate"); err != nil {
		t.Fatal(err)
	}
	dID, _ := st.AddInstrument(ctx, me, "drums", "", "")
	if err := st.RemoveInstrument(ctx, me, dID); err != nil {
		t.Fatal(err)
	}
	src := InstrumentSource{InstrumentFromBand, band}
	if err := st.CollectInstruments(ctx, me, []string{"bassist", "drummer", "guitar & singer"}, src); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListPersonInstruments(ctx, me)
	if got, want := instrumentIDs(list), []string{"voice", "guitar", "bass"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("instruments = %q, want %q (drums stays removed)", got, want)
	}
	for _, pi := range list {
		switch pi.Instrument {
		case "bass":
			if pi.Level != "intermediate" || pi.Source != "self" {
				t.Errorf("bass overwritten: %+v", pi)
			}
		default:
			if pi.Source != "band" || pi.SourceName != "Mercy" || pi.SourceID != band || pi.Original != "guitar & singer" {
				t.Errorf("collected %s: %+v", pi.Instrument, pi)
			}
		}
	}
}

func TestBackfillPersonInstruments(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	admin, _ := st.CreatePerson(ctx, "admin", "Admin", "")
	band, _ := st.CreateBand(ctx, "Mercy", admin)
	a, _ := st.CreatePerson(ctx, "a", "A", "")
	b, _ := st.CreatePerson(ctx, "b", "B", "")
	c, _ := st.CreatePerson(ctx, "c", "C", "")
	// Free text as found in production, written straight in as before.
	for id, list := range map[int64][]string{
		a: {"guitar & singer"},
		b: {"bassist", "guitarist", "singer", "guitar"},
		c: {"Basse", "theremin"},
	} {
		if _, err := st.db.Exec(ctx, `UPDATE people SET instruments = $1 WHERE id = $2`, list, id); err != nil {
			t.Fatal(err)
		}
	}
	// A claimed roster slot.
	gi, _, err := st.CreateGroupInvite(ctx, "band", band, admin, []GroupInviteSlotInput{{DisplayName: "C", Instrument: "drums"}}, "guest")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(ctx, `UPDATE group_invite_slots SET claimed_by_person_id = $1, claimed_at = now() WHERE group_invite_id = $2`, c, gi); err != nil {
		t.Fatal(err)
	}

	n, err := st.BackfillPersonInstruments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2+3+3 {
		t.Errorf("backfill added %d rows, want 8", n)
	}
	got := map[int64][]string{}
	for _, id := range []int64{a, b, c} {
		list, _ := st.ListPersonInstruments(ctx, id)
		got[id] = instrumentIDs(list)
	}
	want := map[int64][]string{
		a: {"voice", "guitar"},
		b: {"voice", "guitar", "bass"},
		c: {"bass", "drums", "other:theremin"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("backfilled %v, want %v", got, want)
	}
	cl, _ := st.ListPersonInstruments(ctx, c)
	if cl[1].Source != "band" || cl[1].SourceName != "Mercy" {
		t.Errorf("drums from the roster slot: %+v", cl[1])
	}
	if cl[0].Source != "self" || cl[0].Original != "Basse" {
		t.Errorf("bass from people.instruments: %+v", cl[0])
	}
	// The text people typed stays where it's shown today.
	if got := peopleInstruments(t, st, a); !reflect.DeepEqual(got, []string{"guitar & singer"}) {
		t.Errorf("people.instruments rewritten by the backfill: %q", got)
	}

	// Idempotent, removals included.
	if err := st.RemoveInstrument(ctx, a, mustFind(t, st, a, "voice")); err != nil {
		t.Fatal(err)
	}
	if n, err := st.BackfillPersonInstruments(ctx); err != nil || n != 0 {
		t.Errorf("second backfill: %d, %v; want 0", n, err)
	}
	list, _ := st.ListPersonInstruments(ctx, a)
	if got := instrumentIDs(list); !reflect.DeepEqual(got, []string{"guitar"}) {
		t.Errorf("a removed voice, backfill brought it back: %q", got)
	}
}

func mustFind(t *testing.T, st *Store, personID int64, instrument string) int64 {
	t.Helper()
	list, _ := st.ListPersonInstruments(context.Background(), personID)
	for _, pi := range list {
		if pi.Instrument == instrument {
			return pi.ID
		}
	}
	t.Fatalf("%d has no %s", personID, instrument)
	return 0
}

// The welcome wizard adds what's ticked or typed, and takes off only the
// icons it showed ticked that the person unticked: never an "other",
// never one collected from a band or an event, never one it didn't show.
func TestUpdateOwnInstruments(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, _ := st.CreatePerson(ctx, "me", "Me", "")
	band, _ := st.CreateBand(ctx, "Mercy", me)
	if _, err := st.AddInstrument(ctx, me, "bass", "", "advanced"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddInstrument(ctx, me, "other", "theremin", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.CollectInstruments(ctx, me, []string{"drummer"}, InstrumentSource{InstrumentFromBand, band}); err != nil {
		t.Fatal(err)
	}

	// Ticked and typed: added; nothing taken off.
	if err := st.UpdateOwnInstruments(ctx, me, []string{"bass", "guitar", "Trompette, kazoo"}, nil); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListPersonInstruments(ctx, me)
	if got, want := instrumentIDs(list), []string{"guitar", "bass", "drums", "trumpet", "other:kazoo", "other:theremin"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("instruments = %q, want %q", got, want)
	}
	if list[1].Level != "advanced" || list[1].Source != "self" {
		t.Errorf("bass level or source lost: %+v", list[1])
	}

	// An empty answer (nothing ticked, nothing had) changes nothing.
	if err := st.UpdateOwnInstruments(ctx, me, nil, nil); err != nil {
		t.Fatal(err)
	}
	if list, _ := st.ListPersonInstruments(ctx, me); len(list) != 6 {
		t.Errorf("an empty answer took something off: %q", instrumentIDs(list))
	}

	// Unticking bass and guitar takes those two off, and only those:
	// "other" and an instrument it didn't show (sax) are ignored, drums
	// from the band and the typed ones stay. One both unticked and
	// ticked stays (voice).
	if err := st.UpdateOwnInstruments(ctx, me, []string{"voice"}, []string{"bass", "guitar", "other", "sax", "voice", "nope"}); err != nil {
		t.Fatal(err)
	}
	list, _ = st.ListPersonInstruments(ctx, me)
	if got, want := instrumentIDs(list), []string{"voice", "drums", "trumpet", "other:kazoo", "other:theremin"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after unticking bass and guitar: %q, want %q", got, want)
	}
	if got, want := peopleInstruments(t, st, me), []string{"voice", "drums", "trumpet", "kazoo", "theremin"}; !reflect.DeepEqual(got, want) {
		t.Errorf("people.instruments = %q, want %q", got, want)
	}
	// Unticked is like removed: not collected again.
	if err := st.CollectInstruments(ctx, me, []string{"bass"}, InstrumentSource{InstrumentFromBand, band}); err != nil {
		t.Fatal(err)
	}
	if list, _ := st.ListPersonInstruments(ctx, me); len(list) != 5 {
		t.Errorf("unticked bass collected again: %q", instrumentIDs(list))
	}
	// Ticked again, it's back with its level.
	if err := st.UpdateOwnInstruments(ctx, me, []string{"bass"}, nil); err != nil {
		t.Fatal(err)
	}
	if pi := find(t, st, me, "bass"); pi.Level != "advanced" {
		t.Errorf("bass back without its level: %+v", pi)
	}

	if err := st.UpdateOwnInstruments(ctx, 999999, []string{"bass"}, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateOwnInstruments on nobody: %v", err)
	}
}

func find(t *testing.T, st *Store, personID int64, instrument string) PersonInstrument {
	t.Helper()
	list, _ := st.ListPersonInstruments(context.Background(), personID)
	for _, pi := range list {
		if pi.Instrument == instrument {
			return pi
		}
	}
	t.Fatalf("%d has no %s", personID, instrument)
	return PersonInstrument{}
}

// Collected instruments stay on the profile when what they came from
// goes: leaving the band, the roster slot changing, the registration
// cancelled or changed, the event, band or community deleted, and the
// start-up backfill run again. Only the person takes one off.
func TestCollectedInstrumentsStay(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	admin, _ := st.CreatePerson(ctx, "admin", "Admin", "")
	me, _ := st.CreatePerson(ctx, "me", "Me", "")

	// A roster slot I claim: bass, from Mercy.
	mercy, _ := st.CreateBand(ctx, "Mercy", admin)
	gi, token, err := st.CreateGroupInvite(ctx, "band", mercy, admin, []GroupInviteSlotInput{{DisplayName: "Me", Instrument: "bass"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	var slot int64
	if err := st.db.QueryRow(ctx, `SELECT id FROM group_invite_slots WHERE group_invite_id = $1`, gi).Scan(&slot); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimGroupInviteSlot(ctx, token, slot, me); err != nil {
		t.Fatal(err)
	}
	// Keys from another band, which will be deleted.
	velvet, _ := st.CreateBand(ctx, "Velvet Hour", admin)
	if err := st.CollectInstruments(ctx, me, []string{"keys"}, InstrumentSource{InstrumentFromBand, velvet}); err != nil {
		t.Fatal(err)
	}
	// Two jams: sax at the first (registration cancelled), harmonica at
	// the second (what I'll play changed, then the event deleted).
	comm, _ := st.CreateCommunity(ctx, "JamSpace", admin)
	jam1, err := st.CreateEvent(ctx, "community", comm, "jam", "Jam du jeudi", "", "", time.Now().Add(24*time.Hour), nil, "members", admin)
	if err != nil {
		t.Fatal(err)
	}
	jam2, err := st.CreateEvent(ctx, "community", comm, "jam", "Jam du vendredi", "", "", time.Now().Add(48*time.Hour), nil, "members", admin)
	if err != nil {
		t.Fatal(err)
	}
	for ev, inst := range map[int64]string{jam1: "sax", jam2: "harmonica"} {
		if err := st.RegisterForEvent(ctx, ev, me, 0); err != nil {
			t.Fatal(err)
		}
		if err := st.SetEventRegistrationInstruments(ctx, ev, me, []string{inst}); err != nil {
			t.Fatal(err)
		}
	}
	// Banjo from a community, which will be deleted.
	other, _ := st.CreateCommunity(ctx, "Folk Club", admin)
	if err := st.CollectInstruments(ctx, me, []string{"banjo"}, InstrumentSource{InstrumentFromCommunity, other}); err != nil {
		t.Fatal(err)
	}
	want := []string{"bass", "banjo", "keys", "sax", "harmonica"}
	if got := instrumentIDs(mustList(t, st, me)); !reflect.DeepEqual(got, want) {
		t.Fatalf("collected %q, want %q", got, want)
	}

	if err := st.RemoveBandMember(ctx, mercy, me, admin); err != nil {
		t.Fatal(err)
	}
	// The claimed slot's instrument changes (a roster edit).
	if _, err := st.db.Exec(ctx, `UPDATE group_invite_slots SET instrument = 'drums' WHERE id = $1`, slot); err != nil {
		t.Fatal(err)
	}
	if err := st.UnregisterFromEvent(ctx, jam1, me); err != nil {
		t.Fatal(err)
	}
	if err := st.SetEventRegistrationInstruments(ctx, jam2, me, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteEvent(ctx, jam2); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteBand(ctx, velvet); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(ctx, `DELETE FROM communities WHERE id = $1`, other); err != nil {
		t.Fatal(err)
	}
	if got := instrumentIDs(mustList(t, st, me)); !reflect.DeepEqual(got, want) {
		t.Errorf("after leaving, cancelling and deleting: %q, want %q", got, want)
	}
	// The backfill only adds: drums from the changed slot.
	if _, err := st.BackfillPersonInstruments(ctx); err != nil {
		t.Fatal(err)
	}
	list := mustList(t, st, me)
	if got, want := instrumentIDs(list), []string{"bass", "banjo", "drums", "keys", "sax", "harmonica"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after the backfill: %q, want %q", got, want)
	}
	if got, want := peopleInstruments(t, st, me), []string{"bass", "banjo", "keys", "sax", "harmonica"}; !reflect.DeepEqual(got, want) {
		t.Errorf("people.instruments = %q, want %q", got, want)
	}
	// Where they came from: still named while it exists, "a band" or
	// "an event" once it's gone.
	for _, pi := range list {
		switch pi.Instrument {
		case "bass":
			if pi.Source != "band" || pi.SourceName != "Mercy" {
				t.Errorf("bass: %+v", pi)
			}
		case "keys":
			if pi.Source != "band" || pi.SourceName != "" || pi.SourceID != 0 {
				t.Errorf("keys, its band deleted: %+v", pi)
			}
		case "harmonica":
			if pi.Source != "event" || pi.SourceName != "" || pi.SourceID != 0 {
				t.Errorf("harmonica, its event deleted: %+v", pi)
			}
		case "sax":
			if pi.Source != "event" || pi.SourceName != "Jam du jeudi" {
				t.Errorf("sax, registration cancelled: %+v", pi)
			}
		case "banjo":
			if pi.Source != "community" || pi.SourceName != "" {
				t.Errorf("banjo, its community deleted: %+v", pi)
			}
		}
	}
	// No path deleted a row: the only way off is the person's own.
	var rows int
	if err := st.db.QueryRow(ctx, `SELECT count(*) FROM person_instruments WHERE person_id = $1 AND removed_at IS NULL`, me).Scan(&rows); err != nil || rows != 6 {
		t.Errorf("rows = %d, %v; want 6", rows, err)
	}
}

func mustList(t *testing.T, st *Store, personID int64) []PersonInstrument {
	t.Helper()
	list, err := st.ListPersonInstruments(context.Background(), personID)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestEventRegistrationAndProgramCollect(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	org, _ := st.CreatePerson(ctx, "org", "Org", "")
	comm, _ := st.CreateCommunity(ctx, "JamSpace", org)
	ev, err := st.CreateEvent(ctx, "community", comm, "jam", "Jam du jeudi", "", "", time.Now().Add(24*time.Hour), nil, "members", org)
	if err != nil {
		t.Fatal(err)
	}
	me, _ := st.CreatePerson(ctx, "me", "Me", "")
	if err := st.SetEventRegistrationInstruments(ctx, ev, me, []string{"sax"}); err == nil {
		t.Error("instruments saved for an event I'm not registered for")
	}
	if err := st.RegisterForEvent(ctx, ev, me, 0); err != nil {
		t.Fatal(err)
	}
	if err := st.SetEventRegistrationInstruments(ctx, ev, me, []string{"sax", "nope", "voice", "sax"}); err != nil {
		t.Fatal(err)
	}
	regs, _ := st.ListEventRegistrations(ctx, ev)
	if len(regs) != 1 || !reflect.DeepEqual(regs[0].Instruments, []string{"voice", "sax"}) {
		t.Errorf("registration = %+v", regs)
	}
	list, _ := st.ListPersonInstruments(ctx, me)
	if got := instrumentIDs(list); !reflect.DeepEqual(got, []string{"voice", "sax"}) {
		t.Fatalf("collected %q", got)
	}
	if list[1].Source != "event" || list[1].SourceName != "Jam du jeudi" {
		t.Errorf("sax source: %+v", list[1])
	}
}

func TestInstrumentLevelVisibility(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, _ := st.CreatePerson(ctx, "me", "Me", "")
	mate, _ := st.CreatePerson(ctx, "mate", "Mate", "")
	org, _ := st.CreatePerson(ctx, "org", "Org", "")
	member, _ := st.CreatePerson(ctx, "member", "Member", "")
	stranger, _ := st.CreatePerson(ctx, "stranger", "Stranger", "")
	admin, _ := st.CreatePerson(ctx, "admin", "Admin", "")
	evManager, _ := st.CreatePerson(ctx, "evm", "Event manager", "")
	_ = st.SetAdmin(ctx, admin, true)

	band, _ := st.CreateBand(ctx, "Mercy", me)
	if _, err := st.AddBandMember(ctx, band, mate, "guest", me); err != nil {
		t.Fatal(err)
	}
	comm, _ := st.CreateCommunity(ctx, "JamSpace", org) // org is its staff
	for _, p := range []int64{me, member, evManager} {
		if _, err := st.AddCommunityMember(ctx, comm, p, "member", org); err != nil {
			t.Fatal(err)
		}
	}
	ev, err := st.CreateEvent(ctx, "community", comm, "jam", "Jam", "", "", time.Now().Add(time.Hour), nil, "members", org)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddEventManager(ctx, ev, evManager, org); err != nil {
		t.Fatal(err)
	}
	if err := st.RegisterForEvent(ctx, ev, me, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddInstrument(ctx, me, "guitar", "", "advanced"); err != nil {
		t.Fatal(err)
	}

	check := func(label string, wantInstr, wantLevels map[int64]bool) {
		t.Helper()
		for _, v := range []int64{0, me, mate, org, member, stranger, admin, evManager} {
			instr, _ := st.CanSeeInstruments(ctx, v, me)
			lv, _ := st.CanSeeInstrumentLevels(ctx, v, me)
			if instr != wantInstr[v] {
				t.Errorf("%s: viewer %d sees instruments = %v, want %v", label, v, instr, wantInstr[v])
			}
			if lv != wantLevels[v] {
				t.Errorf("%s: viewer %d sees levels = %v, want %v", label, v, lv, wantLevels[v])
			}
			m, _ := st.InstrumentsSeenBy(ctx, v, []int64{me})
			if got := m[me][0].Level != ""; got != wantLevels[v] {
				t.Errorf("%s: InstrumentsSeenBy(%d) shows the level = %v, want %v", label, v, got, wantLevels[v])
			}
		}
	}
	instr := map[int64]bool{me: true, mate: true, org: true, member: true, admin: true, evManager: true}
	check("shared", instr, map[int64]bool{me: true, mate: true, org: true, admin: true, evManager: true})

	if err := st.SetInstrumentLevelsShared(ctx, me, false); err != nil {
		t.Fatal(err)
	}
	check("not shared", instr, map[int64]bool{me: true, admin: true})
}
