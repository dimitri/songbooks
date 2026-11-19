package store

import (
	"context"
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

func TestReplaceOwnInstruments(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, _ := st.CreatePerson(ctx, "me", "Me", "")
	if _, err := st.AddInstrument(ctx, me, "bass", "", "advanced"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetInstruments(ctx, me, []string{"bass", "drums", "Trompette, kazoo"}); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListPersonInstruments(ctx, me)
	if got, want := instrumentIDs(list), []string{"bass", "drums", "trumpet", "other:kazoo"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("instruments = %q, want %q", got, want)
	}
	if list[0].Level != "advanced" {
		t.Errorf("bass level lost: %+v", list[0])
	}
	if err := st.SetInstruments(ctx, me, []string{"bass"}); err != nil {
		t.Fatal(err)
	}
	list, _ = st.ListPersonInstruments(ctx, me)
	if got := instrumentIDs(list); !reflect.DeepEqual(got, []string{"bass"}) {
		t.Errorf("after unticking: %q", got)
	}
	if err := st.SetInstruments(ctx, 999999, []string{"bass"}); err == nil {
		t.Error("SetInstruments on nobody")
	}
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
