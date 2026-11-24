package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"
)

// Band roles, round 2 (docs/band-roles.md, migration 0132): invites into
// any of a band's roles, archiving closes the invites, the guest
// defaults, curating Discover and Influences, the votes list.

// roleOf is personID's current role id in bandID (0: not a member).
func (f *bandRolesFixture) roleOf(t *testing.T, personID int64) int64 {
	t.Helper()
	var id int64
	err := f.st.db.QueryRow(context.Background(), `
		SELECT coalesce((SELECT role_id FROM band_members WHERE band_id = $1 AND person_id = $2 AND upper_inf(validity)), 0)`,
		f.band, personID).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *bandRolesFixture) newPerson(t *testing.T, name string) int64 {
	t.Helper()
	id, err := f.st.CreatePerson(context.Background(), name, name, name+"@example.test")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestInviteIntoCustomRole: every way of inviting gives one of the
// band's own roles — the open link, a roster slot, a one-off link, an
// account added by name, a placeholder (personal invite) — and the claim
// assigns it; another band's role is refused.
func TestInviteIntoCustomRole(t *testing.T) {
	f := newBandRolesFixture(t)
	ctx := context.Background()
	roadie, err := f.st.GroupRoles("band").Create(ctx, f.band, "Roadie", []string{BandPrivManageEvents})
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.st.CreateBand(ctx, "Other Band", f.other)
	if err != nil {
		t.Fatal(err)
	}
	otherCore, err := builtinBandRoleID(ctx, f.st.db, other, "core")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.ResolveBandInviteRole(ctx, f.band, otherCore, ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("another band's role: %v, want ErrInvalid", err)
	}
	if _, err := f.st.ResolveBandInviteRole(ctx, f.band, 0, "boss"); !errors.Is(err, ErrInvalid) {
		t.Errorf("an unknown status: %v, want ErrInvalid", err)
	}
	if _, _, err := f.st.CreateBandLink(ctx, f.band, f.core, otherCore); !errors.Is(err, ErrInvalid) {
		t.Errorf("a link for another band's role: %v", err)
	}

	// The open link.
	_, token, err := f.st.CreateBandLink(ctx, f.band, f.core, roadie)
	if err != nil {
		t.Fatal(err)
	}
	a := f.newPerson(t, "viaLink")
	if _, err := f.st.ClaimGroupInviteUnlisted(ctx, token, a, "A"); err != nil {
		t.Fatal(err)
	}
	if got := f.roleOf(t, a); got != roadie {
		t.Errorf("joined with the Roadie link: role %d, want %d", got, roadie)
	}
	if ok, _ := f.st.HasBandPrivilege(ctx, f.band, a, BandPrivManageEvents); !ok {
		t.Error("the Roadie doesn't manage events")
	}
	if ok, _ := f.st.HasBandPrivilege(ctx, f.band, a, BandPrivEditSongbook); ok {
		t.Error("the Roadie edits the songbook")
	}

	// A roster slot.
	_, token, err = f.st.CreateGroupInvite(ctx, "band", f.band, f.core, []GroupInviteSlotInput{{DisplayName: "Bo", RoleID: roadie}}, "")
	if err != nil {
		t.Fatal(err)
	}
	view, err := f.st.GetGroupInviteByToken(ctx, token)
	if err != nil || len(view.Slots) != 1 || view.Slots[0].RoleID != roadie || view.Slots[0].Status != "guest" {
		t.Fatalf("the slot: %+v %v", view, err)
	}
	b := f.newPerson(t, "viaSlot")
	if _, err := f.st.ClaimGroupInviteSlot(ctx, token, view.Slots[0].ID, b); err != nil {
		t.Fatal(err)
	}
	if got := f.roleOf(t, b); got != roadie {
		t.Errorf("claimed a Roadie slot: role %d", got)
	}

	// A one-off link.
	tok, err := f.st.CreateBandInvite(ctx, f.band, roadie, f.core, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	open, err := f.st.ListOpenBandInvites(ctx, f.band)
	if err != nil || len(open) != 1 || open[0].RoleID != roadie {
		t.Errorf("open one-off links: %+v %v", open, err)
	}
	c := f.newPerson(t, "viaOneOff")
	if _, err := f.st.AcceptInvite(ctx, tok, c, "C"); err != nil {
		t.Fatal(err)
	}
	if got := f.roleOf(t, c); got != roadie {
		t.Errorf("accepted a Roadie one-off link: role %d", got)
	}

	// An account added by name; a placeholder.
	d := f.newPerson(t, "byName")
	if _, err := f.st.AddBandMemberWithRole(ctx, f.band, d, roadie, f.core); err != nil {
		t.Fatal(err)
	}
	if got := f.roleOf(t, d); got != roadie {
		t.Errorf("added as a Roadie: role %d", got)
	}
	shell, err := f.st.CreateShellPerson(ctx, "Placeholder")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.AddBandMemberWithRole(ctx, f.band, shell, roadie, f.core); err != nil {
		t.Fatal(err)
	}
	if got := f.roleOf(t, shell); got != roadie {
		t.Errorf("a Roadie placeholder: role %d", got)
	}
	if _, err := f.st.AddBandMemberWithRole(ctx, f.band, f.other, otherCore, f.core); !errors.Is(err, ErrInvalid) {
		t.Errorf("added with another band's role: %v", err)
	}

	// The role deleted before a link is used: the link gives guest.
	gone, err := f.st.GroupRoles("band").Create(ctx, f.band, "Gone", []string{BandPrivPublish})
	if err != nil {
		t.Fatal(err)
	}
	_, token, err = f.st.CreateBandLink(ctx, f.band, f.core, gone)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.GroupRoles("band").Delete(ctx, f.band, gone); err != nil {
		t.Fatal(err)
	}
	e := f.newPerson(t, "afterDelete")
	if _, err := f.st.ClaimGroupInviteUnlisted(ctx, token, e, "E"); err != nil {
		t.Fatal(err)
	}
	if got := f.roleOf(t, e); got != f.role(t, "guest").ID {
		t.Errorf("a deleted role's link: role %d, want guest", got)
	}
}

// TestOldInvitesKeepWorking: links made before roles (a status, no role)
// give the built-in role of their status.
func TestOldInvitesKeepWorking(t *testing.T) {
	f := newBandRolesFixture(t)
	ctx := context.Background()
	// As migration 0132 found them: band_role_id NULL.
	_, coreLink, err := f.st.CreateGroupInvite(ctx, "band", f.band, f.core, nil, "core")
	if err != nil {
		t.Fatal(err)
	}
	_, roster, err := f.st.CreateGroupInvite(ctx, "band", f.band, f.core, []GroupInviteSlotInput{{DisplayName: "Old", Status: "core"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := f.st.CreateBandInvite(ctx, f.band, f.role(t, "core").ID, f.core, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE group_invites SET band_role_id = NULL WHERE band_id = $1`,
		`UPDATE group_invite_slots s SET band_role_id = NULL FROM group_invites gi WHERE gi.id = s.group_invite_id AND gi.band_id = $1`,
		`UPDATE invites SET band_role_id = NULL WHERE band_id = $1`,
	} {
		if _, err := f.st.db.Exec(ctx, q, f.band); err != nil {
			t.Fatal(err)
		}
	}
	core := f.role(t, "core").ID
	guest := f.role(t, "guest").ID
	a, b, c, d := f.newPerson(t, "a"), f.newPerson(t, "b"), f.newPerson(t, "c"), f.newPerson(t, "d")
	if _, err := f.st.ClaimGroupInviteUnlisted(ctx, coreLink, a, "A"); err != nil {
		t.Fatal(err)
	}
	view, _ := f.st.GetGroupInviteByToken(ctx, roster)
	if _, err := f.st.ClaimGroupInviteSlot(ctx, roster, view.Slots[0].ID, b); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.ClaimGroupInviteUnlisted(ctx, roster, c, "C"); err != nil { // someone else on a roster: guest
		t.Fatal(err)
	}
	if _, err := f.st.AcceptInvite(ctx, tok, d, "D"); err != nil {
		t.Fatal(err)
	}
	for who, want := range map[int64]int64{a: core, b: core, c: guest, d: core} {
		if got := f.roleOf(t, who); got != want {
			t.Errorf("person %d: role %d, want %d", who, got, want)
		}
	}
}

// TestArchiveClosesInvites: archiving closes the band's open links, its
// roster links and its one-off links; unarchiving reopens none.
func TestArchiveClosesInvites(t *testing.T) {
	f := newBandRolesFixture(t)
	ctx := context.Background()
	_, link, err := f.st.CreateBandLink(ctx, f.band, f.core, f.role(t, "guest").ID)
	if err != nil {
		t.Fatal(err)
	}
	_, roster, err := f.st.CreateGroupInvite(ctx, "band", f.band, f.core, []GroupInviteSlotInput{{DisplayName: "Zed"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	oneOff, err := f.st.CreateBandInvite(ctx, f.band, f.role(t, "core").ID, f.core, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// Another band's link stays open.
	other, err := f.st.CreateBand(ctx, "Still Going", f.other)
	if err != nil {
		t.Fatal(err)
	}
	otherGuest, _ := builtinBandRoleID(ctx, f.st.db, other, "guest")
	_, otherLink, err := f.st.CreateBandLink(ctx, other, f.other, otherGuest)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.st.SetBandArchived(ctx, f.band, true); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetBandArchived(ctx, f.band, false); err != nil {
		t.Fatal(err)
	}
	p := f.newPerson(t, "late")
	if _, err := f.st.ClaimGroupInviteUnlisted(ctx, link, p, "Late"); !errors.Is(err, ErrForbidden) {
		t.Errorf("the open link after archiving: %v, want closed", err)
	}
	if _, err := f.st.ClaimGroupInviteUnlisted(ctx, roster, p, "Late"); !errors.Is(err, ErrForbidden) {
		t.Errorf("the roster link after archiving: %v, want closed", err)
	}
	if _, err := f.st.AcceptInvite(ctx, oneOff, p, "Late"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the one-off link after archiving: %v, want gone", err)
	}
	if open, _ := f.st.ListOpenBandInvites(ctx, f.band); len(open) != 0 {
		t.Errorf("one-off links still listed: %+v", open)
	}
	if f.roleOf(t, p) != 0 {
		t.Error("joined an archived (since unarchived) band through an old link")
	}
	if _, err := f.st.ClaimGroupInviteUnlisted(ctx, otherLink, p, "Late"); err != nil {
		t.Errorf("another band's link closed too: %v", err)
	}
}

// TestGuestDefaults: a guest doesn't edit the band's songs nor remove
// members, still proposes (a song made for the proposal included: they
// fix it in the check step), adds to Discover and decides; their notes
// on a song are their own (section annotations: viewers').
func TestGuestDefaults(t *testing.T) {
	f := newBandRolesFixture(t)
	ctx := context.Background()
	for p, want := range map[string]bool{
		BandPrivEditSongbook: false, BandPrivRemoveMembers: false, BandPrivVote: false, BandPrivCurateListen: false,
		BandPrivPropose: true, BandPrivDecideProposals: true, BandPrivManageSetlists: true,
	} {
		if got := f.can(t, f.guest, p); got != want {
			t.Errorf("guest %s: %v, want %v", p, got, want)
		}
	}
	song, err := f.st.CreateProposedSong(ctx, f.guest, f.band, "Guest's Pick", "Them", "")
	if err != nil {
		t.Fatalf("a guest makes the song they propose: %v", err)
	}
	if ok, _ := f.st.CanEditVariant(ctx, f.guest, song); ok {
		t.Error("a guest edits the band's song")
	}
	if ok, err := f.st.MayFixProposedSong(ctx, f.band, song, f.guest); err != nil || !ok {
		t.Errorf("a guest fixes the song they're proposing: %v %v", ok, err)
	}
	if ok, _ := f.st.MayFixProposedSong(ctx, f.band, song, f.core2); ok {
		t.Error("someone else fixes it as its proposer")
	}
	if _, err := f.st.CreateProposal(ctx, f.band, f.guest, "new_song", song, nil); err != nil {
		t.Fatalf("a guest proposes: %v", err)
	}
	if ok, _ := f.st.MayFixProposedSong(ctx, f.band, song, f.guest); ok {
		t.Error("a sent proposal's song still fixed as a draft")
	}
	if _, err := f.st.AddBandListen(ctx, f.band, f.guest, NewBandListen{Pool: PoolDiscover, URL: "https://youtu.be/dQw4w9WgXcQ"}); err != nil {
		t.Errorf("a guest shares to Discover: %v", err)
	}
	// Notes: anyone who sees the song, the guest's own (the HTTP side:
	// httpapi's TestGuestKeepsNotes).
	if _, err := f.st.CreateSectionAnnotation(ctx, song, 0, f.guest, "solo: sax", "private"); err != nil {
		t.Errorf("a guest's note: %v", err)
	}
}

// TestCurateListens: whoever added a song removes it; a member whose
// role grants curate_listen (core) removes anyone's; no one in an
// archived band.
func TestCurateListens(t *testing.T) {
	f := newBandRolesFixture(t)
	ctx := context.Background()
	add := func(who int64) int64 {
		id, err := f.st.AddBandListen(ctx, f.band, who, NewBandListen{Pool: PoolInfluence, URL: "https://youtu.be/dQw4w9WgXcQ"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	coresOwn, guests := add(f.core2), add(f.guest)
	if err := f.st.DeleteBandListen(ctx, f.band, coresOwn, f.guest, f.can(t, f.guest, BandPrivCurateListen)); !errors.Is(err, ErrForbidden) {
		t.Errorf("a guest removes a core member's: %v", err)
	}
	if err := f.st.DeleteBandListen(ctx, f.band, guests, f.core, f.can(t, f.core, BandPrivCurateListen)); err != nil {
		t.Errorf("core curates a guest's: %v", err)
	}
	mine := add(f.guest)
	if err := f.st.SetBandArchived(ctx, f.band, true); err != nil {
		t.Fatal(err)
	}
	if err := f.st.DeleteBandListen(ctx, f.band, mine, f.guest, false); !errors.Is(err, ErrForbidden) {
		t.Errorf("the author removes from an archived band: %v", err)
	}
	if err := f.st.SetBandArchived(ctx, f.band, false); err != nil {
		t.Fatal(err)
	}
	if err := f.st.DeleteBandListen(ctx, f.band, mine, f.guest, false); err != nil {
		t.Errorf("the author removes their own: %v", err)
	}
}

// TestTurnedDownLast: in every order, the turned-down proposals come
// last.
func TestTurnedDownLast(t *testing.T) {
	f := newBandRolesFixture(t)
	ctx := context.Background()
	propose := func(title string) int64 {
		song, err := f.st.CreateVariant(ctx, OwnerCatalog, 0, title, "X", f.core)
		if err != nil {
			t.Fatal(err)
		}
		id, err := f.st.CreateProposal(ctx, f.band, f.core, "new_song", song, nil)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	down, top, added := propose("Down"), propose("Top"), propose("Added")
	if _, err := f.st.CastVote(ctx, down, f.core, Vote{Stars: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CastVote(ctx, down, f.core2, Vote{NoThanks: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CastVote(ctx, top, f.core, Vote{Stars: 2}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.RejectProposal(ctx, down, f.core); err != nil {
		t.Fatal(err)
	}
	if err := f.st.AcceptProposal(ctx, added, f.core); err != nil {
		t.Fatal(err)
	}
	for _, sortBy := range []string{"stars", "no_thanks", "newest", ""} {
		for _, viewer := range []int64{0, f.core, f.guest} {
			list, err := f.st.ListProposals(ctx, f.band, ProposalFilter{Sort: sortBy, Viewer: viewer})
			if err != nil {
				t.Fatal(err)
			}
			var titles []string
			for _, p := range list {
				titles = append(titles, p.TargetTitle)
			}
			if len(titles) != 3 || titles[2] != "Down" {
				t.Errorf("sort %q, viewer %d: %v, want Down last", sortBy, viewer, titles)
			}
		}
	}
}

// TestMigration0132: the guest role of every band loses edit_songbook
// and remove_members, nothing else of a band's own choices; core and the
// staff's own roles get curate_listen; invites get their role column.
func TestMigration0132(t *testing.T) {
	f := newBandRolesFixture(t)
	ctx := context.Background()
	roles := f.st.GroupRoles("band")
	// A second band whose guest role was edited: publishing given,
	// the calendar taken, the songbook already taken.
	edited, err := f.st.CreateBand(ctx, "Edited", f.other)
	if err != nil {
		t.Fatal(err)
	}
	staff, err := roles.Create(ctx, f.band, "Staff", []string{BandPrivManageRoles, BandPrivDecideProposals})
	if err != nil {
		t.Fatal(err)
	}
	fans, err := roles.Create(ctx, f.band, "Fans", []string{BandPrivPropose})
	if err != nil {
		t.Fatal(err)
	}
	editedGuest, _ := builtinBandRoleID(ctx, f.st.db, edited, "guest")
	guestRole, coreRole := f.role(t, "guest").ID, f.role(t, "core").ID // before the transaction's locks
	raw, err := migrationsFS.ReadFile("migrations/0132_band_roles_invites.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.st.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// Back to before 0132, in a transaction rolled back after.
	for _, q := range []string{
		`ALTER TABLE invites DROP COLUMN band_role_id`,
		`ALTER TABLE group_invites DROP COLUMN band_role_id`,
		`ALTER TABLE group_invite_slots DROP COLUMN band_role_id`,
		`DELETE FROM band_role_privileges WHERE privilege = 'curate_listen'`,
		`INSERT INTO band_role_privileges (role_id, privilege)
		   SELECT id, p FROM band_roles, unnest(ARRAY['edit_songbook', 'remove_members']) p WHERE builtin_key = 'guest'
		   ON CONFLICT DO NOTHING`,
	} {
		if _, err := tx.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	for _, q := range []string{
		`DELETE FROM band_role_privileges WHERE role_id = $1 AND privilege IN ('manage_events', 'edit_songbook')`,
		`INSERT INTO band_role_privileges (role_id, privilege) VALUES ($1, 'publish')`,
	} {
		if _, err := tx.Exec(ctx, q, editedGuest); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(ctx, string(raw)); err != nil {
		t.Fatal(err)
	}
	privs := func(roleID int64) string {
		t.Helper()
		var out []string
		rows, err := tx.Query(ctx, `SELECT privilege FROM band_role_privileges WHERE role_id = $1`, roleID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				t.Fatal(err)
			}
			out = append(out, p)
		}
		sort.Strings(out)
		return strings.Join(out, " ")
	}
	want := func(list ...string) string {
		sort.Strings(list)
		return strings.Join(list, " ")
	}
	if got := privs(guestRole); got != want(BandRoleDefaults["guest"]...) {
		t.Errorf("a default guest role: %s", got)
	}
	if got, w := privs(editedGuest), want(BandPrivManageSetlists, BandPrivManageRecordings, BandPrivPropose, BandPrivDecideProposals, BandPrivEditBand, BandPrivPublish); got != w {
		t.Errorf("an edited guest role: %s, want %s", got, w)
	}
	if got := privs(coreRole); got != want(AllBandPrivileges...) {
		t.Errorf("core: %s", got)
	}
	if got := privs(staff); got != want(BandPrivManageRoles, BandPrivDecideProposals, BandPrivCurateListen) {
		t.Errorf("a staff role: %s", got)
	}
	if got := privs(fans); got != want(BandPrivPropose) {
		t.Errorf("another own role: %s", got)
	}
	var cols int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE column_name = 'band_role_id' AND table_schema = current_schema()
		AND table_name IN ('invites', 'group_invites', 'group_invite_slots')`).Scan(&cols); err != nil || cols != 3 {
		t.Errorf("band_role_id columns: %d %v", cols, err)
	}
}
