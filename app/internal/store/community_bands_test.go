package store

import (
	"context"
	"testing"
)

// TestLinkBandToCommunityGrantsExistingMembers covers LinkBandToCommunity's
// own sync step: linking a band that already has members immediately
// grants every one of those CURRENT members ordinary individual
// community membership, at the community's default_join_role, with
// joined_via_band_id set.
func TestLinkBandToCommunityGrantsExistingMembers(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	creatorID, err := st.CreatePerson(ctx, "creator", "Creator", "creator@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	staffID, err := st.CreatePerson(ctx, "staff", "Staff", "staff@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	bandID, err := st.CreateBand(ctx, "JamSpace Band", creatorID)
	if err != nil {
		t.Fatalf("CreateBand: %v", err)
	}
	communityID, err := st.CreateCommunity(ctx, "JamSpace", staffID)
	if err != nil {
		t.Fatalf("CreateCommunity: %v", err)
	}
	community, err := st.GetCommunity(ctx, communityID)
	if err != nil {
		t.Fatalf("GetCommunity: %v", err)
	}

	if _, err := st.LinkBandToCommunity(ctx, communityID, bandID, staffID); err != nil {
		t.Fatalf("LinkBandToCommunity: %v", err)
	}

	// creatorID was already a band member before the link existed —
	// confirms the sync step covers the band's EXISTING roster, not just
	// members added after the link.
	ok, err := st.IsCurrentCommunityMember(ctx, communityID, creatorID)
	if err != nil {
		t.Fatalf("IsCurrentCommunityMember: %v", err)
	}
	if !ok {
		t.Fatal("expected the band's existing creator to be granted community membership on link")
	}

	members, err := st.ListCommunityMembers(ctx, communityID, false)
	if err != nil {
		t.Fatalf("ListCommunityMembers: %v", err)
	}
	var creatorRow CommunityMember
	for _, m := range members {
		if m.PersonID == creatorID {
			creatorRow = m
		}
	}
	if creatorRow.JoinedViaBandID == nil || *creatorRow.JoinedViaBandID != bandID {
		t.Fatalf("expected creatorRow.JoinedViaBandID == %d, got %v", bandID, creatorRow.JoinedViaBandID)
	}
	if creatorRow.Role != community.DefaultJoinRole {
		t.Fatalf("expected role %q (the community's default_join_role), got %q", community.DefaultJoinRole, creatorRow.Role)
	}
}

// TestEnsureCommunityMembershipForBandMemberIdempotent covers the
// no-op-if-already-current behavior ensureCommunityMembershipForBandMember
// needs for both LinkBandToCommunity (a member in two overlapping bands
// linked to the same community) and AddBandMember's sync step.
func TestEnsureCommunityMembershipForBandMemberIdempotent(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	creatorID, err := st.CreatePerson(ctx, "creator", "Creator", "creator@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	personID, err := st.CreatePerson(ctx, "person", "Person", "person@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	communityID, err := st.CreateCommunity(ctx, "Community", creatorID)
	if err != nil {
		t.Fatalf("CreateCommunity: %v", err)
	}

	bandA, err := st.CreateBand(ctx, "Band A", creatorID)
	if err != nil {
		t.Fatalf("CreateBand: %v", err)
	}
	bandB, err := st.CreateBand(ctx, "Band B", creatorID)
	if err != nil {
		t.Fatalf("CreateBand: %v", err)
	}
	if _, err := st.LinkBandToCommunity(ctx, communityID, bandA, creatorID); err != nil {
		t.Fatalf("LinkBandToCommunity A: %v", err)
	}
	if _, err := st.LinkBandToCommunity(ctx, communityID, bandB, creatorID); err != nil {
		t.Fatalf("LinkBandToCommunity B: %v", err)
	}

	// personID joins bandA first (auto-grants community membership),
	// then bandB (already a current community member — must be a no-op,
	// not a second membership row or an error).
	if _, err := st.AddBandMember(ctx, bandA, personID, "core", creatorID); err != nil {
		t.Fatalf("AddBandMember A: %v", err)
	}
	if _, err := st.AddBandMember(ctx, bandB, personID, "core", creatorID); err != nil {
		t.Fatalf("AddBandMember B: %v", err)
	}

	members, err := st.ListCommunityMembers(ctx, communityID, true)
	if err != nil {
		t.Fatalf("ListCommunityMembers: %v", err)
	}
	count := 0
	for _, m := range members {
		if m.PersonID == personID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one community_members row for personID, got %d", count)
	}
}

// TestAddBandMemberSyncsToLinkedCommunity covers AddBandMember's own
// sync step directly: a brand-new band member joining a band that's
// ALREADY linked to a community is granted community membership as
// part of the same AddBandMember call, not a separate step the caller
// has to remember.
func TestAddBandMemberSyncsToLinkedCommunity(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	creatorID, err := st.CreatePerson(ctx, "creator", "Creator", "creator@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	newMemberID, err := st.CreatePerson(ctx, "new", "New Member", "new@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	bandID, err := st.CreateBand(ctx, "Band", creatorID)
	if err != nil {
		t.Fatalf("CreateBand: %v", err)
	}
	communityID, err := st.CreateCommunity(ctx, "Community", creatorID)
	if err != nil {
		t.Fatalf("CreateCommunity: %v", err)
	}
	if _, err := st.LinkBandToCommunity(ctx, communityID, bandID, creatorID); err != nil {
		t.Fatalf("LinkBandToCommunity: %v", err)
	}

	if _, err := st.AddBandMember(ctx, bandID, newMemberID, "guest", creatorID); err != nil {
		t.Fatalf("AddBandMember: %v", err)
	}

	ok, err := st.IsCurrentCommunityMember(ctx, communityID, newMemberID)
	if err != nil {
		t.Fatalf("IsCurrentCommunityMember: %v", err)
	}
	if !ok {
		t.Fatal("expected the new band member to be synced into the linked community")
	}
}

// TestRemoveBandMemberKeepsCommunityMembership covers the "keep on
// band-leave" decision: leaving a band linked to a community never
// auto-revokes the community membership that band-join granted — it's
// sticky/independent once granted, staff clean it up manually.
func TestRemoveBandMemberKeepsCommunityMembership(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	creatorID, err := st.CreatePerson(ctx, "creator", "Creator", "creator@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	memberID, err := st.CreatePerson(ctx, "member", "Member", "member@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	bandID, err := st.CreateBand(ctx, "Band", creatorID)
	if err != nil {
		t.Fatalf("CreateBand: %v", err)
	}
	communityID, err := st.CreateCommunity(ctx, "Community", creatorID)
	if err != nil {
		t.Fatalf("CreateCommunity: %v", err)
	}
	if _, err := st.LinkBandToCommunity(ctx, communityID, bandID, creatorID); err != nil {
		t.Fatalf("LinkBandToCommunity: %v", err)
	}
	if _, err := st.AddBandMember(ctx, bandID, memberID, "core", creatorID); err != nil {
		t.Fatalf("AddBandMember: %v", err)
	}

	if err := st.RemoveBandMember(ctx, bandID, memberID, creatorID); err != nil {
		t.Fatalf("RemoveBandMember: %v", err)
	}

	stillMember, err := st.IsCurrentBandMember(ctx, bandID, memberID)
	if err != nil {
		t.Fatalf("IsCurrentBandMember: %v", err)
	}
	if stillMember {
		t.Fatal("expected memberID to no longer be a current band member")
	}
	stillCommunityMember, err := st.IsCurrentCommunityMember(ctx, communityID, memberID)
	if err != nil {
		t.Fatalf("IsCurrentCommunityMember: %v", err)
	}
	if !stillCommunityMember {
		t.Fatal("expected leaving the band to NOT revoke community membership (sticky/keep decision)")
	}
}

// TestUnlinkBandFromCommunityKeepsMembersCurrent covers
// UnlinkBandFromCommunity's own side: closing the band↔community link
// touches only community_band_members, never any community_members row
// that link already caused to exist.
func TestUnlinkBandFromCommunityKeepsMembersCurrent(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	creatorID, err := st.CreatePerson(ctx, "creator", "Creator", "creator@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	communityID, err := st.CreateCommunity(ctx, "Community", creatorID)
	if err != nil {
		t.Fatalf("CreateCommunity: %v", err)
	}
	bandID, err := st.CreateBand(ctx, "Band", creatorID)
	if err != nil {
		t.Fatalf("CreateBand: %v", err)
	}
	if _, err := st.LinkBandToCommunity(ctx, communityID, bandID, creatorID); err != nil {
		t.Fatalf("LinkBandToCommunity: %v", err)
	}
	if ok, err := st.IsCurrentCommunityMember(ctx, communityID, creatorID); err != nil || !ok {
		t.Fatalf("expected creatorID to be a community member after linking: ok=%v err=%v", ok, err)
	}

	if err := st.UnlinkBandFromCommunity(ctx, communityID, bandID); err != nil {
		t.Fatalf("UnlinkBandFromCommunity: %v", err)
	}

	linked, err := st.ListLinkedCommunitiesForBand(ctx, bandID)
	if err != nil {
		t.Fatalf("ListLinkedCommunitiesForBand: %v", err)
	}
	if len(linked) != 0 {
		t.Fatalf("expected no currently-linked communities after unlink, got %d", len(linked))
	}
	if ok, err := st.IsCurrentCommunityMember(ctx, communityID, creatorID); err != nil || !ok {
		t.Fatalf("expected creatorID to remain a community member after unlink: ok=%v err=%v", ok, err)
	}
}

// TestListCommunityMembersGroupedByBand covers the read model: a
// band-granted member buckets under their band, a direct joiner buckets
// separately, and a member whose granting band has since unlinked shows
// up as orphaned rather than silently vanishing or crashing.
func TestListCommunityMembersGroupedByBand(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	creatorID, err := st.CreatePerson(ctx, "creator", "Creator", "creator@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	directID, err := st.CreatePerson(ctx, "direct", "Direct", "direct@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	viaBandID, err := st.CreatePerson(ctx, "via", "Via Band", "via@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	orphanID, err := st.CreatePerson(ctx, "orphan", "Orphan", "orphan@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	communityID, err := st.CreateCommunity(ctx, "Community", creatorID)
	if err != nil {
		t.Fatalf("CreateCommunity: %v", err)
	}
	bandA, err := st.CreateBand(ctx, "Band A", creatorID)
	if err != nil {
		t.Fatalf("CreateBand A: %v", err)
	}
	bandB, err := st.CreateBand(ctx, "Band B", creatorID)
	if err != nil {
		t.Fatalf("CreateBand B: %v", err)
	}
	if _, err := st.LinkBandToCommunity(ctx, communityID, bandA, creatorID); err != nil {
		t.Fatalf("LinkBandToCommunity A: %v", err)
	}
	if _, err := st.LinkBandToCommunity(ctx, communityID, bandB, creatorID); err != nil {
		t.Fatalf("LinkBandToCommunity B: %v", err)
	}
	if _, err := st.AddCommunityMember(ctx, communityID, directID, "member", creatorID); err != nil {
		t.Fatalf("AddCommunityMember direct: %v", err)
	}
	if _, err := st.AddBandMember(ctx, bandA, viaBandID, "core", creatorID); err != nil {
		t.Fatalf("AddBandMember via: %v", err)
	}
	if _, err := st.AddBandMember(ctx, bandB, orphanID, "core", creatorID); err != nil {
		t.Fatalf("AddBandMember orphan: %v", err)
	}
	// Unlinking bandB after orphanID already got auto-granted membership
	// through it is exactly how a real orphan appears (see migration
	// 0060 / community_bands.go's own doc comments) — the grant isn't
	// retroactively undone, but it no longer counts as a live band
	// grouping.
	if err := st.UnlinkBandFromCommunity(ctx, communityID, bandB); err != nil {
		t.Fatalf("UnlinkBandFromCommunity: %v", err)
	}

	groups, orphaned, err := st.ListCommunityMembersGroupedByBand(ctx, communityID)
	if err != nil {
		t.Fatalf("ListCommunityMembersGroupedByBand: %v", err)
	}

	if len(orphaned) != 1 || orphaned[0].PersonID != orphanID {
		t.Fatalf("expected exactly orphanID in orphaned, got %+v", orphaned)
	}

	var sawBandGroup, sawDirectGroup bool
	for _, g := range groups {
		switch {
		case g.BandID == bandA:
			sawBandGroup = true
			if len(g.Members) != 1 || g.Members[0].PersonID != viaBandID {
				t.Fatalf("expected bandA's group to contain only viaBandID, got %+v", g.Members)
			}
		case g.BandID == 0:
			sawDirectGroup = true
			// creatorID (the community's own staff creator, a direct
			// join via CreateCommunity) is also expected here alongside
			// directID — both have JoinedViaBandID == nil.
			found := false
			for _, m := range g.Members {
				if m.PersonID == directID {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected the direct group to contain directID, got %+v", g.Members)
			}
		}
	}
	if !sawBandGroup {
		t.Fatal("expected a group for bandA")
	}
	if !sawDirectGroup {
		t.Fatal("expected a direct-join group")
	}
}
