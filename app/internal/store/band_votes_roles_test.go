package store_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"app/internal/store"
)

// The band votes asked through the band's roles (docs/band-roles.md):
// guests unchanged by default; a custom Staff alone decides, through the
// HTTP routes; a custom role with vote votes and counts in the tally.

func bandRole(t *testing.T, st *store.Store, band int64, key string) store.BandRole {
	t.Helper()
	roles, err := st.ListBandRoles(context.Background(), band)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range roles {
		if r.BuiltinKey == key || r.Name == key {
			return r
		}
	}
	t.Fatalf("no role %q", key)
	return store.BandRole{}
}

func without(privs []string, drop string) []string {
	var out []string
	for _, p := range privs {
		if p != drop {
			out = append(out, p)
		}
	}
	return out
}

func decide(t *testing.T, st *store.Store, f votesBand, person, proposal int64, how string) int {
	t.Helper()
	return as(t, st, person).do("POST", fmt.Sprintf("/api/bands/%d/proposals/%d/%s", f.Band, proposal, how), nil).Code
}

// TestBandVotesDefaultRoles: by default a guest proposes and decides,
// doesn't vote, and isn't counted.
func TestBandVotesDefaultRoles(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newVotesBand(t, st)
	p := f.propose(t, st, "Alpha")
	// A guest votes, listed apart: not counted.
	if g := vote(t, st, f, f.Guest, p, map[string]any{"stars": 4}, http.StatusOK); g.Tally.Voted != 0 || g.Tally.Ratings != 0 || len(g.Tally.Guests) != 1 {
		t.Errorf("a guest's vote: %+v", g.Tally)
	}
	ans := vote(t, st, f, f.Core, p, map[string]any{"stars": 3}, http.StatusOK)
	if ans.Tally.Members != 3 || ans.Tally.Voted != 1 || ans.Tally.AvgText != "3.0" || len(ans.Tally.Guests) != 1 {
		t.Errorf("tally %d/%d voted, average %s, want 1/3 (the core members), 3.0, the guest's apart", ans.Tally.Voted, ans.Tally.Members, ans.Tally.AvgText)
	}
	song, err := st.CreateVariant(ctx, store.OwnerCatalog, 0, "Guest's pick", "Someone", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateProposal(ctx, f.Band, f.Guest, "new_song", song, nil); err != nil {
		t.Errorf("a guest proposes: %v", err)
	}
	if code := decide(t, st, f, f.Guest, p, "accept"); code != http.StatusOK {
		t.Errorf("a guest accepts: %d", code)
	}
}

// TestBandStaffAloneDecidesHTTP: a band keeps deciding to a custom
// "Staff" role; the accept and turn-down routes follow, proposing stays
// everyone's.
func TestBandStaffAloneDecidesHTTP(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newVotesBand(t, st)
	roles := st.GroupRoles("band")
	staff, err := roles.Create(ctx, f.Band, "Staff", store.BandRoleDefaults["core"])
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"core", "guest"} {
		r := bandRole(t, st, f.Band, key)
		if err := roles.SetPrivileges(ctx, f.Band, r.ID, without(r.Privileges, store.BandPrivDecideProposals)); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetBandMemberRole(ctx, f.Band, f.Cores[1], staff, f.Core); err != nil {
		t.Fatal(err)
	}
	a, b := f.propose(t, st, "Alpha"), f.propose(t, st, "Bravo")
	for _, who := range []int64{f.Core, f.Guest, f.Outsider} {
		if code := decide(t, st, f, who, a, "accept"); code != http.StatusForbidden {
			t.Errorf("person %d accepts without decide_proposals: %d", who, code)
		}
		if code := decide(t, st, f, who, b, "reject"); code != http.StatusForbidden {
			t.Errorf("person %d turns down without decide_proposals: %d", who, code)
		}
	}
	if code := decide(t, st, f, f.Cores[1], a, "accept"); code != http.StatusOK {
		t.Errorf("Staff accepts: %d", code)
	}
	if code := decide(t, st, f, f.Cores[1], b, "reject"); code != http.StatusOK {
		t.Errorf("Staff turns down: %d", code)
	}
	// The store refuses too, whatever the route.
	c := f.propose(t, st, "Charlie")
	if err := st.AcceptProposal(ctx, c, f.Guest); err == nil {
		t.Error("store: a guest accepts")
	}
	song, err := st.CreateVariant(ctx, store.OwnerCatalog, 0, "Guest's pick", "Someone", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateProposal(ctx, f.Band, f.Guest, "new_song", song, nil); err != nil {
		t.Errorf("a guest no longer proposes: %v", err)
	}
}

// TestBandCustomRoleVotes: a band's own role with vote votes and is
// counted in "n of m voted"; a core role without it isn't.
func TestBandCustomRoleVotes(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newVotesBand(t, st)
	roles := st.GroupRoles("band")
	listener, err := roles.Create(ctx, f.Band, "Listener", []string{store.BandPrivPropose, store.BandPrivVote})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetBandMemberRole(ctx, f.Band, f.Guest, listener, f.Core); err != nil {
		t.Fatal(err)
	}
	p := f.propose(t, st, "Alpha")
	ans := vote(t, st, f, f.Guest, p, map[string]any{"stars": 4}, http.StatusOK)
	if ans.Tally.Members != 4 || ans.Tally.Voted != 1 || ans.Tally.Ratings != 1 {
		t.Errorf("tally %d/%d voted, %d ratings; want 1/4, 1", ans.Tally.Voted, ans.Tally.Members, ans.Tally.Ratings)
	}
	// Core loses vote: its members aren't counted, their ballots drop out.
	vote(t, st, f, f.Core, p, map[string]any{"stars": 1}, http.StatusOK)
	core := bandRole(t, st, f.Band, "core")
	if err := roles.SetPrivileges(ctx, f.Band, core.ID, without(core.Privileges, store.BandPrivVote)); err != nil {
		t.Fatal(err)
	}
	ans = vote(t, st, f, f.Guest, p, map[string]any{"stars": 3}, http.StatusOK)
	if ans.Tally.Members != 1 || ans.Tally.Voted != 1 || ans.Tally.AvgText != "3.0" {
		t.Errorf("after core lost vote: %d/%d voted, average %q; want 1/1, 3.0", ans.Tally.Voted, ans.Tally.Members, ans.Tally.AvgText)
	}
	list, err := st.ListProposals(ctx, f.Band, store.ProposalFilter{Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Ratings != 1 || list[0].StarSum != 3 {
		t.Errorf("listed: %d ratings, %d stars; want the Listener's alone", list[0].Ratings, list[0].StarSum)
	}
	// Core without vote still rates it, listed apart.
	if a := vote(t, st, f, f.Core, p, map[string]any{"stars": 2}, http.StatusOK); a.Tally.AvgText != "3.0" || a.Tally.Members != 1 || len(a.Tally.Guests) != 1 || a.Tally.Guests[0].PersonID != f.Core {
		t.Errorf("core without vote: %+v", a.Tally)
	}
}
