package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestNormalizeInstruments(t *testing.T) {
	got := NormalizeInstruments([]string{"bass, Vocals", " vocals ", "", "  slide   guitar ", ","})
	if want := []string{"bass", "Vocals", "slide guitar"}; !reflect.DeepEqual(got, want) {
		t.Errorf("NormalizeInstruments = %q, want %q", got, want)
	}
	if got := NormalizeInstruments(nil); got == nil || len(got) != 0 {
		t.Errorf("NormalizeInstruments(nil) = %#v, want an empty non-nil list", got)
	}
	long := NormalizeInstruments([]string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	if len([]rune(long[0])) != maxInstrumentLength {
		t.Errorf("a long name is %d runes, want %d", len([]rune(long[0])), maxInstrumentLength)
	}
	var many []string
	for i := 0; i < 30; i++ {
		many = append(many, string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	if n := len(NormalizeInstruments(many)); n != maxInstruments {
		t.Errorf("kept %d instruments, want %d", n, maxInstruments)
	}
}

// TestSetBandMemberInstruments: any member edits a placeholder's
// instruments; a real account's only by a core member, the person
// themselves, or a site admin.
func TestSetBandMemberInstruments(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	core, _ := st.CreatePerson(ctx, "core", "Core", "core@example.com")
	guest, _ := st.CreatePerson(ctx, "guest", "Guest", "guest@example.com")
	other, _ := st.CreatePerson(ctx, "other", "Other", "other@example.com")
	outsider, _ := st.CreatePerson(ctx, "outsider", "Outsider", "outsider@example.com")
	bandID, err := st.CreateBand(ctx, "Test Band", core)
	if err != nil {
		t.Fatalf("CreateBand: %v", err)
	}
	shell, err := st.CreateShellPerson(ctx, "Marc")
	if err != nil {
		t.Fatalf("CreateShellPerson: %v", err)
	}
	for _, id := range []int64{guest, other, shell} {
		if _, err := st.AddBandMember(ctx, bandID, id, "guest", core); err != nil {
			t.Fatalf("AddBandMember: %v", err)
		}
	}

	instruments := func(id int64) []string {
		p, err := st.GetPersonByID(ctx, id)
		if err != nil {
			t.Fatalf("GetPersonByID: %v", err)
		}
		return p.Instruments
	}

	cases := []struct {
		name          string
		actor, target int64
		admin         bool
		wantErr       error
	}{
		{"guest edits a placeholder", guest, shell, false, nil},
		{"guest edits another real member", guest, other, false, ErrForbidden},
		{"guest edits themselves", guest, guest, false, nil},
		{"core edits a real member", core, other, false, nil},
		{"admin outside the band edits a real member", outsider, other, true, nil},
		{"outsider (not admin) edits a real member", outsider, other, false, ErrForbidden},
		{"target not in the band", core, outsider, false, ErrNotFound},
	}
	for _, c := range cases {
		saved, err := st.SetBandMemberInstruments(ctx, bandID, c.actor, c.target, c.admin, []string{"bass, vocals", "Bass"})
		if c.wantErr != nil {
			if !errors.Is(err, c.wantErr) {
				t.Errorf("%s: err = %v, want %v", c.name, err, c.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		want := []string{"bass", "vocals"}
		if !reflect.DeepEqual(saved, want) || !reflect.DeepEqual(instruments(c.target), want) {
			t.Errorf("%s: saved %q, stored %q, want %q", c.name, saved, instruments(c.target), want)
		}
		// Reset for the next case.
		if err := st.SetInstruments(ctx, c.target, []string{}); err != nil {
			t.Fatal(err)
		}
	}

	// A refused edit leaves the instruments alone.
	if err := st.SetInstruments(ctx, other, []string{"drums"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetBandMemberInstruments(ctx, bandID, guest, other, false, []string{"kazoo"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if got := instruments(other); !reflect.DeepEqual(got, []string{"drums"}) {
		t.Errorf("after a refused edit: %q", got)
	}
	// Clearing them all works too.
	if saved, err := st.SetBandMemberInstruments(ctx, bandID, guest, shell, false, nil); err != nil || len(saved) != 0 {
		t.Errorf("clearing: %q, %v", saved, err)
	}
}

// TestPersonalInviteNewLink: a placeholder always gets a new link, the
// old one stops working, and only a placeholder can have one.
func TestPersonalInviteNewLink(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	core, _ := st.CreatePerson(ctx, "core", "Core", "core@example.com")
	bandID, err := st.CreateBand(ctx, "Test Band", core)
	if err != nil {
		t.Fatalf("CreateBand: %v", err)
	}
	shell, err := st.CreateShellPerson(ctx, "Marc")
	if err != nil {
		t.Fatalf("CreateShellPerson: %v", err)
	}
	if _, err := st.AddBandMember(ctx, bandID, shell, "core", core); err != nil {
		t.Fatalf("AddBandMember: %v", err)
	}

	first, err := st.CreatePersonalInvite(ctx, shell, core, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("CreatePersonalInvite: %v", err)
	}
	second, err := st.CreatePersonalInvite(ctx, shell, core, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("CreatePersonalInvite while a link is live: %v", err)
	}
	if _, err := st.GetPersonalInvite(ctx, first); !errors.Is(err, ErrNotFound) {
		t.Errorf("the old link still works after a new one: %v", err)
	}
	if _, err := st.GetPersonalInvite(ctx, second); err != nil {
		t.Errorf("the new link doesn't work: %v", err)
	}

	// An expired link is replaced the same way.
	if _, err := st.db.Exec(ctx, `UPDATE invites SET expires_at = now() - interval '1 day' WHERE token = $1`, second); err != nil {
		t.Fatal(err)
	}
	third, err := st.CreatePersonalInvite(ctx, shell, core, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("CreatePersonalInvite after expiry: %v", err)
	}
	var n int
	if err := st.db.QueryRow(ctx, `SELECT count(*) FROM invites WHERE person_id = $1`, shell).Scan(&n); err != nil || n != 1 {
		t.Errorf("%d invite rows for the placeholder (%v), want 1", n, err)
	}

	// The roster sees the link and its expiry, and who hasn't joined.
	shells, err := st.ListShellPeopleForBand(ctx, bandID)
	if err != nil || len(shells) != 1 {
		t.Fatalf("ListShellPeopleForBand: %v, %d rows", err, len(shells))
	}
	if shells[0].OpenInviteToken != third {
		t.Errorf("roster token %q, want %q", shells[0].OpenInviteToken, third)
	}
	if d := time.Until(shells[0].OpenInviteUntil); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Errorf("link expires in %v, want about 30 days", d)
	}
	members, err := st.ListBandMembers(ctx, bandID, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		if m.IsShell != (m.PersonID == shell) {
			t.Errorf("member %d: IsShell = %v", m.PersonID, m.IsShell)
		}
	}

	// Never for a real account, nor for no one.
	if _, err := st.CreatePersonalInvite(ctx, core, core, time.Hour); !errors.Is(err, ErrNotShell) {
		t.Errorf("a link for a real account: err = %v, want ErrNotShell", err)
	}
	if _, err := st.CreatePersonalInvite(ctx, 999999, core, time.Hour); !errors.Is(err, ErrNotFound) {
		t.Errorf("a link for no one: err = %v, want ErrNotFound", err)
	}

	// Once claimed, the account is real: no new link, and a link that
	// somehow points at a real account can't take it over.
	if _, err := st.ClaimPersonalInvite(ctx, third, "marc", "marc@example.com", ""); err != nil {
		t.Fatalf("ClaimPersonalInvite: %v", err)
	}
	if _, err := st.CreatePersonalInvite(ctx, shell, core, time.Hour); !errors.Is(err, ErrNotShell) {
		t.Errorf("a link after joining: err = %v, want ErrNotShell", err)
	}
	if _, err := st.db.Exec(ctx, `INSERT INTO invites (token, person_id, expires_at) VALUES ('stale-token', $1, now() + interval '1 day')`, core); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimPersonalInvite(ctx, "stale-token", "taken", "taken@example.com", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("claiming a real account: err = %v, want ErrNotFound", err)
	}
}
