package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// TestCommunityMembersVisibility: who may open a community's Members page,
// for each role, with the list shown to admins only (the default) and to
// every member.
func TestCommunityMembersVisibility(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	person := func(handle string) int64 {
		id, err := st.CreatePerson(ctx, handle, handle+"@example.com")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	staff, coordinator, member, guest, former, outsider := person("staff"), person("coordinator"), person("member"), person("guest"), person("former"), person("outsider")
	c, err := st.CreateCommunity(ctx, "Jam", staff)
	if err != nil {
		t.Fatal(err)
	}
	// A custom role with one management privilege counts as an admin.
	if _, err := st.CreateCommunityRole(ctx, c, "Events coordinator", []string{PrivManageEvents}); err != nil {
		t.Fatal(err)
	}
	for p, role := range map[int64]string{coordinator: "Events coordinator", member: "member", guest: "guest", former: "member"} {
		if _, err := st.AddCommunityMember(ctx, c, p, role, staff); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.RemoveCommunityMember(ctx, c, former); err != nil {
		t.Fatal(err)
	}

	if v, err := st.CommunityMembersVisibility(ctx, c); err != nil || v != MembersVisibleToAdmins {
		t.Fatalf("default visibility: %q, %v", v, err)
	}
	check := func(setting string, want map[int64]bool) {
		t.Helper()
		for p, ok := range want {
			got, err := st.CanSeeCommunityMembers(ctx, c, p)
			if err != nil {
				t.Fatal(err)
			}
			if got != ok {
				t.Errorf("%s: person %d sees the list: %v, want %v", setting, p, got, ok)
			}
		}
	}
	check("admins", map[int64]bool{staff: true, coordinator: true, member: false, guest: false, former: false, outsider: false})

	if err := st.SetCommunityMembersVisibility(ctx, c, MembersVisibleToMembers); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.CommunityMembersVisibility(ctx, c); v != MembersVisibleToMembers {
		t.Fatalf("visibility after setting it: %q", v)
	}
	check("members", map[int64]bool{staff: true, coordinator: true, member: true, guest: true, former: false, outsider: false})

	if err := st.SetCommunityMembersVisibility(ctx, c, "everyone"); err == nil {
		t.Error("an unknown visibility was taken")
	}
	if err := st.SetCommunityMembersVisibility(ctx, c+100, MembersVisibleToMembers); !errors.Is(err, ErrNotFound) {
		t.Errorf("no such community: %v", err)
	}
	if _, err := st.CommunityMembersVisibility(ctx, c+100); !errors.Is(err, ErrNotFound) {
		t.Errorf("no such community: %v", err)
	}
}

// TestListCommunityRoster: current members staff first then by name, with
// their instruments; past ones only when asked for, after them.
func TestListCommunityRoster(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	zoe, _ := st.CreatePerson(ctx, "Zoé Martin", "zoe@example.com")
	ana, _ := st.CreatePerson(ctx, "ana Lopez", "ana@example.com")
	bob, _ := st.CreatePerson(ctx, "Bob", "bob@example.com")
	c, err := st.CreateCommunity(ctx, "Jam", zoe)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []int64{bob, ana} {
		if _, err := st.AddCommunityMember(ctx, c, p, "member", zoe); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.UpdateOwnInstruments(ctx, ana, []string{"bass", "vocals"}, nil); err != nil {
		t.Fatal(err)
	}
	shell, err := st.CreateShellPerson(ctx, "Carla")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddCommunityMember(ctx, c, shell, "guest", zoe); err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveCommunityMember(ctx, c, bob); err != nil {
		t.Fatal(err)
	}

	roster, err := st.ListCommunityRoster(ctx, c, false)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range roster {
		names = append(names, e.DisplayName)
	}
	if len(roster) != 3 || names[0] != "Zoé Martin" || names[1] != "ana Lopez" || names[2] != "Carla" {
		t.Fatalf("current roster: %q", names)
	}
	// A member's instruments come in the instruments' display order
	// (instruments.Sort: voice first), not in the order they were typed,
	// and "vocals" is read as voice: people.instruments is rewritten from
	// person_instruments on every change (syncPeopleInstruments).
	if roster[0].RoleKey != "staff" || roster[1].Role != "member" || !reflect.DeepEqual(roster[1].Instruments, []string{"voice", "bass"}) {
		t.Errorf("rows: %+v", roster[:2])
	}
	if !roster[2].IsShell || roster[2].RoleKey != "guest" || roster[0].IsShell {
		t.Errorf("shell: %+v", roster[2])
	}

	all, err := st.ListCommunityRoster(ctx, c, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 || all[3].DisplayName != "Bob" || all[3].IsCurrent() || !all[0].IsCurrent() {
		t.Fatalf("with past members: %+v", all)
	}
}
