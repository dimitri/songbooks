package store

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"testing"
	"time"
)

// bandRolesFixture: a band with a core member (its creator), a guest, a
// second core member, and someone outside it.
type bandRolesFixture struct {
	st                        *Store
	band                      int64
	core, core2, guest, other int64
}

func newBandRolesFixture(t *testing.T) *bandRolesFixture {
	t.Helper()
	st := newTestStore(t)
	ctx := context.Background()
	f := &bandRolesFixture{st: st}
	mk := func(name string) int64 {
		id, err := st.CreatePerson(ctx, name, name+"@example.test")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.core, f.core2, f.guest, f.other = mk("core"), mk("core2"), mk("guest"), mk("other")
	var err error
	if f.band, err = st.CreateBand(ctx, "Roles Band", f.core); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBandMember(ctx, f.band, f.core2, "core", f.core); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBandMember(ctx, f.band, f.guest, "guest", f.core); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *bandRolesFixture) role(t *testing.T, key string) BandRole {
	t.Helper()
	roles, err := f.st.ListBandRoles(context.Background(), f.band)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range roles {
		if r.BuiltinKey == key || (key != "core" && key != "guest" && r.Name == key) {
			return r
		}
	}
	t.Fatalf("no role %q", key)
	return BandRole{}
}

func (f *bandRolesFixture) can(t *testing.T, person int64, priv string) bool {
	t.Helper()
	ok, err := f.st.HasBandPrivilege(context.Background(), f.band, person, priv)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

func equalSets(a, b []string) bool {
	a, b = sorted(a), sorted(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestBandRoleDefaultsMatchSeed: a new band gets core and guest, granting
// BandRoleDefaults — migration 0130's seed_band_roles and the Go list
// say the same; members point at the built-in of their status.
func TestBandRoleDefaultsMatchSeed(t *testing.T) {
	f := newBandRolesFixture(t)
	roles, err := f.st.ListBandRoles(context.Background(), f.band)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 2 || roles[0].BuiltinKey != "core" || roles[1].BuiltinKey != "guest" {
		t.Fatalf("roles = %+v, want core then guest", roles)
	}
	for _, r := range roles {
		if !equalSets(r.Privileges, BandRoleDefaults[r.BuiltinKey]) {
			t.Errorf("%s grants %v, want %v", r.Name, sorted(r.Privileges), sorted(BandRoleDefaults[r.BuiltinKey]))
		}
	}
	members, err := f.st.ListBandMembers(context.Background(), f.band, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		if want := f.role(t, m.Status).ID; m.RoleID != want {
			t.Errorf("%s (%s) has role %d, want %d", m.DisplayName, m.Status, m.RoleID, want)
		}
	}
}

// TestBandBuiltinRolesReproduceTodaysRights: every inventoried action
// (docs/band-roles.md), as the built-in roles leave it — core may do
// everything; guest set lists, the calendar, recordings, proposing and
// deciding, but not the songs (migration 0132), what the band plays
// (migration 0137),
// removing people, the look, inviting, roles, publishing, voting (counted)
// nor tidying Discover; someone outside nothing — through the store's
// own checks, not only HasBandPrivilege.
func TestBandBuiltinRolesReproduceTodaysRights(t *testing.T) {
	f := newBandRolesFixture(t)
	ctx := context.Background()
	guestOnlyNot := map[string]bool{
		BandPrivPublish: true, BandPrivVote: true, BandPrivManageMembers: true,
		BandPrivManageRoles: true, BandPrivEditLook: true, BandPrivRenameArchive: true,
		BandPrivEditSongbook: true, BandPrivRemoveMembers: true, BandPrivCurateListen: true,
		BandPrivEditBand: true,
	}
	for _, p := range AllBandPrivileges {
		if !f.can(t, f.core, p) {
			t.Errorf("core can't %s", p)
		}
		if got := f.can(t, f.guest, p); got == guestOnlyNot[p] {
			t.Errorf("guest %s: %v, want %v", p, got, !guestOnlyNot[p])
		}
		if f.can(t, f.other, p) {
			t.Errorf("someone outside the band can %s", p)
		}
	}

	// The songbook: core adds, edits and publishes songs; a guest sees
	// them.
	if _, err := f.st.CreateBandVariant(ctx, f.band, "Guest's", "Them", f.guest); !errors.Is(err, ErrForbidden) {
		t.Errorf("a guest adds a song: %v, want ErrForbidden", err)
	}
	song, err := f.st.CreateBandVariant(ctx, f.band, "A Song", "Them", f.core)
	if err != nil {
		t.Fatalf("core adds a song: %v", err)
	}
	for _, c := range []struct {
		who           int64
		edit, publish bool
	}{{f.core, true, true}, {f.guest, false, false}, {f.other, false, false}} {
		r, err := f.st.SongRightsFor(ctx, c.who, song)
		if err != nil {
			t.Fatal(err)
		}
		if r.Edit != c.edit || r.Publish != c.publish {
			t.Errorf("person %d: edit %v publish %v, want %v %v", c.who, r.Edit, r.Publish, c.edit, c.publish)
		}
		if ok, _ := f.st.OwnerMayChange(ctx, c.who, Owner{Kind: "band", ID: f.band}); ok != c.edit {
			t.Errorf("person %d: OwnerMayChange %v, want %v", c.who, ok, c.edit)
		}
	}
	sb, err := f.st.GetDefaultSongbook(ctx, "band", f.band)
	if err != nil {
		t.Fatal(err)
	}
	for who, want := range map[int64][2]bool{f.core: {true, true}, f.guest: {false, false}, f.other: {false, false}} {
		edit, _ := f.st.CanEditSongbook(ctx, who, sb)
		r, err := f.st.SongbookRightsFor(ctx, who, sb)
		if err != nil {
			t.Fatal(err)
		}
		if edit != want[0] || r.Publish != want[1] {
			t.Errorf("person %d: songbook edit %v publish %v, want %v", who, edit, r.Publish, want)
		}
	}

	// Set lists and events: any member.
	band := f.band
	sl, err := f.st.CreateSetlist(ctx, "band", "Gig", "", nil, &band, nil, f.core)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := f.st.CreateEvent(ctx, "band", f.band, "rehearsal", "Rehearsal", "", "", time.Now().Add(24*time.Hour), nil, "members", f.core)
	if err != nil {
		t.Fatal(err)
	}
	for who, want := range map[int64]bool{f.core: true, f.guest: true, f.other: false} {
		if ok, _ := f.st.CanEditSetlistByID(ctx, who, sl); ok != want {
			t.Errorf("person %d edits the set list: %v, want %v", who, ok, want)
		}
		if ok, _ := f.st.CanManageEvent(ctx, ev, who); ok != want {
			t.Errorf("person %d manages the event: %v, want %v", who, ok, want)
		}
	}

	// Inviting, others' instruments: core only.
	shell, err := f.st.CreateShellPerson(ctx, "Placeholder")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.AddBandMember(ctx, f.band, shell, "guest", f.core); err != nil {
		t.Fatal(err)
	}
	for who, want := range map[int64]bool{f.core: true, f.guest: false, f.other: false} {
		if ok, _ := f.st.CanInvitePerson(ctx, who, shell); ok != want {
			t.Errorf("person %d invites the placeholder: %v, want %v", who, ok, want)
		}
	}
	if _, err := f.st.SetBandMemberInstruments(ctx, f.band, f.core, f.guest, false, []string{"bass"}); err != nil {
		t.Errorf("core sets a guest's instruments: %v", err)
	}
	if _, err := f.st.SetBandMemberInstruments(ctx, f.band, f.guest, f.core2, false, []string{"bass"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("guest sets a core member's instruments: %v, want ErrForbidden", err)
	}
}

// TestBandStaffAloneDecides: a band makes a "Staff" role, the only one
// that decides proposals; its holder counts as core, the others no
// longer decide.
func TestBandStaffAloneDecides(t *testing.T) {
	f := newBandRolesFixture(t)
	ctx := context.Background()
	roles := f.st.GroupRoles("band")
	staff, err := roles.Create(ctx, f.band, "Staff", BandRoleDefaults["core"])
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"core", "guest"} {
		r := f.role(t, key)
		var keep []string
		for _, p := range r.Privileges {
			if p != BandPrivDecideProposals {
				keep = append(keep, p)
			}
		}
		if err := roles.SetPrivileges(ctx, f.band, r.ID, keep); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.st.SetBandMemberRole(ctx, f.band, f.core2, staff, f.core); err != nil {
		t.Fatal(err)
	}
	for who, want := range map[int64]bool{f.core2: true, f.core: false, f.guest: false, f.other: false} {
		if got := f.can(t, who, BandPrivDecideProposals); got != want {
			t.Errorf("person %d decides: %v, want %v", who, got, want)
		}
	}
	// Proposing stays everyone's.
	if !f.can(t, f.guest, BandPrivPropose) {
		t.Error("a guest no longer proposes")
	}
	var status string
	if err := f.st.db.QueryRow(ctx, `SELECT status::text FROM band_members WHERE band_id = $1 AND person_id = $2 AND upper_inf(validity)`, f.band, f.core2).Scan(&status); err != nil || status != "core" {
		t.Errorf("the staff member's status is %q (%v), want core", status, err)
	}
	// Back to guest through the old switch: the guest role.
	if err := f.st.SetBandMemberStatus(ctx, f.band, f.core2, "guest", f.core); err != nil {
		t.Fatal(err)
	}
	if f.can(t, f.core2, BandPrivDecideProposals) {
		t.Error("made a guest, still decides")
	}
}

// TestBandGuestLosesPrivilege: a band takes editing the songbook from
// its guests; the store's checks follow.
func TestBandGuestLosesPrivilege(t *testing.T) {
	f := newBandRolesFixture(t)
	ctx := context.Background()
	guest := f.role(t, "guest")
	var keep []string
	for _, p := range guest.Privileges {
		if p != BandPrivEditSongbook {
			keep = append(keep, p)
		}
	}
	if err := f.st.GroupRoles("band").SetPrivileges(ctx, f.band, guest.ID, keep); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CreateBandVariant(ctx, f.band, "Not Mine", "Them", f.guest); !errors.Is(err, ErrForbidden) {
		t.Errorf("guest adds a song: %v, want ErrForbidden", err)
	}
	song, err := f.st.CreateBandVariant(ctx, f.band, "Core's", "Them", f.core)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := f.st.SongRightsFor(ctx, f.guest, song); r.Edit || !r.See {
		t.Errorf("guest: edit %v see %v, want no edit, still sees", r.Edit, r.See)
	}
	sb, _ := f.st.GetDefaultSongbook(ctx, "band", f.band)
	if ok, _ := f.st.CanEditSongbook(ctx, f.guest, sb); ok {
		t.Error("guest still edits the songbook")
	}
	if ok, _ := f.st.CanAccessSongbook(ctx, f.guest, sb); !ok {
		t.Error("guest no longer sees the songbook")
	}
	if !f.can(t, f.guest, BandPrivManageSetlists) {
		t.Error("the other privileges went too")
	}
}

// TestBandRolesHeldToTheBand: a role is its own band's; someone must
// keep managing roles; built-ins stay; a role in use stays; a deleted
// role's past holders keep their membership, as core.
func TestBandRolesHeldToTheBand(t *testing.T) {
	f := newBandRolesFixture(t)
	ctx := context.Background()
	roles := f.st.GroupRoles("band")
	otherBand, err := f.st.CreateBand(ctx, "Other Band", f.other)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := roles.Create(ctx, otherBand, "Theirs", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := roles.SetPrivileges(ctx, f.band, theirs, AllBandPrivileges); !errors.Is(err, ErrNotFound) {
		t.Errorf("another band's role through this band: %v, want ErrNotFound", err)
	}
	if err := roles.Rename(ctx, f.band, theirs, "Mine"); err == nil {
		t.Error("renamed another band's role")
	}
	if err := roles.Delete(ctx, f.band, theirs); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted another band's role: %v", err)
	}
	if err := f.st.SetBandMemberRole(ctx, f.band, f.guest, theirs, f.core); !errors.Is(err, ErrNotFound) {
		t.Errorf("gave another band's role: %v, want ErrNotFound", err)
	}
	if err := roles.SetPrivileges(ctx, f.band, f.role(t, "core").ID, []string{"fly"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("an unknown privilege: %v, want ErrInvalid", err)
	}

	// Someone keeps manage_roles.
	core := f.role(t, "core")
	if err := roles.SetPrivileges(ctx, f.band, core.ID, []string{BandPrivEditSongbook}); !errors.Is(err, ErrLastKeeper) {
		t.Errorf("took manage_roles from everyone: %v, want ErrLastKeeper", err)
	}
	if err := f.st.SetBandMemberStatus(ctx, f.band, f.core2, "guest", f.core); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetBandMemberStatus(ctx, f.band, f.core, "guest", f.core); !errors.Is(err, ErrLastKeeper) {
		t.Errorf("demoted the last core member: %v, want ErrLastKeeper", err)
	}
	if !f.can(t, f.core, BandPrivManageRoles) {
		t.Error("the refused change was made anyway")
	}

	// Built-ins and roles in use stay.
	if err := roles.Delete(ctx, f.band, core.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("deleted core: %v, want ErrForbidden", err)
	}
	if err := roles.Rename(ctx, f.band, core.ID, "Boss"); !errors.Is(err, ErrForbidden) {
		t.Errorf("renamed core: %v, want ErrForbidden", err)
	}
	sub, err := roles.Create(ctx, f.band, "Sub", []string{BandPrivPropose})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roles.Create(ctx, f.band, "Sub", nil); !errors.Is(err, ErrConflict) {
		t.Errorf("a second Sub: %v, want ErrConflict", err)
	}
	if err := f.st.SetBandMemberRole(ctx, f.band, f.guest, sub, f.core); err != nil {
		t.Fatal(err)
	}
	if err := roles.Delete(ctx, f.band, sub); !errors.Is(err, ErrConflict) {
		t.Errorf("deleted a role in use: %v, want ErrConflict", err)
	}
	if err := f.st.RemoveBandMember(ctx, f.band, f.guest, f.core); err != nil {
		t.Fatal(err)
	}
	if err := roles.Delete(ctx, f.band, sub); err != nil {
		t.Fatalf("deleting a role only past members held: %v", err)
	}
	past, err := f.st.ListBandMembers(ctx, f.band, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range past {
		if m.PersonID == f.guest && (m.RoleID != core.ID || m.Status != "core" || m.IsCurrent()) {
			t.Errorf("past member after the delete: role %d status %s current %v", m.RoleID, m.Status, m.IsCurrent())
		}
	}
}

// TestBandRoleRevisionRevert: undoing a role change gives the role back,
// a custom one included.
func TestBandRoleRevisionRevert(t *testing.T) {
	f := newBandRolesFixture(t)
	ctx := context.Background()
	staff, err := f.st.GroupRoles("band").Create(ctx, f.band, "Staff", []string{BandPrivDecideProposals})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetBandMemberRole(ctx, f.band, f.guest, staff, f.core); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetBandMemberStatus(ctx, f.band, f.guest, "guest", f.core); err != nil {
		t.Fatal(err)
	}
	members, _ := f.st.ListBandMembers(ctx, f.band, false)
	var rowID int64
	for _, m := range members {
		if m.PersonID == f.guest {
			rowID = m.ID
		}
	}
	revs, err := f.st.ListRevisions(ctx, "band_member", strconv.FormatInt(rowID, 10))
	if err != nil || len(revs) == 0 {
		t.Fatalf("revisions: %v %d", err, len(revs))
	}
	// Newest first: the snapshot before the switch back to guest.
	if err := f.st.RevertRevision(ctx, revs[0].ID, f.core); err != nil {
		t.Fatal(err)
	}
	members, _ = f.st.ListBandMembers(ctx, f.band, false)
	for _, m := range members {
		if m.PersonID == f.guest && (m.RoleID != staff || m.Status != "core") {
			t.Errorf("after the undo: role %d status %s, want %d core", m.RoleID, m.Status, staff)
		}
	}
}
