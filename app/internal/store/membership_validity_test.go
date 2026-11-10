package store

import (
	"context"
	"strconv"
	"testing"
)

// TestBandMembershipValidity exercises the validity tstzrange column
// (migration 0059) through the exact same paths the old joined_at/
// left_at pair used to cover: current-membership checks, rejoin
// producing a brand-new row rather than reviving the old one, and
// RemoveBandMember correctly closing the range on the row it removed
// while leaving the newest (rejoined) row open-ended.
func TestBandMembershipValidity(t *testing.T) {
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
	bandID, err := st.CreateBand(ctx, "Test Band", creatorID)
	if err != nil {
		t.Fatalf("CreateBand: %v", err)
	}

	firstRowID, err := st.AddBandMember(ctx, bandID, memberID, "core", creatorID)
	if err != nil {
		t.Fatalf("AddBandMember: %v", err)
	}
	ok, err := st.IsCurrentBandMember(ctx, bandID, memberID)
	if err != nil {
		t.Fatalf("IsCurrentBandMember: %v", err)
	}
	if !ok {
		t.Fatal("expected memberID to be a current band member after AddBandMember")
	}

	if err := st.RemoveBandMember(ctx, bandID, memberID, creatorID); err != nil {
		t.Fatalf("RemoveBandMember: %v", err)
	}
	ok, err = st.IsCurrentBandMember(ctx, bandID, memberID)
	if err != nil {
		t.Fatalf("IsCurrentBandMember after remove: %v", err)
	}
	if ok {
		t.Fatal("expected memberID to no longer be a current band member after RemoveBandMember")
	}

	members, err := st.ListBandMembers(ctx, bandID, true)
	if err != nil {
		t.Fatalf("ListBandMembers: %v", err)
	}
	var firstRow BandMember
	for _, m := range members {
		if m.ID == firstRowID {
			firstRow = m
		}
	}
	if firstRow.LeftAt == nil {
		t.Fatal("expected the removed row's LeftAt to be set (validity range closed)")
	}

	// Rejoin: a new row, not a revival of the old one.
	secondRowID, err := st.AddBandMember(ctx, bandID, memberID, "guest", creatorID)
	if err != nil {
		t.Fatalf("AddBandMember (rejoin): %v", err)
	}
	if secondRowID == firstRowID {
		t.Fatal("expected rejoin to insert a new row, not reuse the old one")
	}
	ok, err = st.IsCurrentBandMember(ctx, bandID, memberID)
	if err != nil {
		t.Fatalf("IsCurrentBandMember after rejoin: %v", err)
	}
	if !ok {
		t.Fatal("expected memberID to be current again after rejoining")
	}

	members, err = st.ListBandMembers(ctx, bandID, true)
	if err != nil {
		t.Fatalf("ListBandMembers after rejoin: %v", err)
	}
	var old, cur BandMember
	for _, m := range members {
		switch m.ID {
		case firstRowID:
			old = m
		case secondRowID:
			cur = m
		}
	}
	if old.LeftAt == nil {
		t.Fatal("expected the original (left) row's LeftAt to still be set after rejoining")
	}
	if cur.LeftAt != nil {
		t.Fatal("expected the new (rejoined) row's LeftAt to be nil (open-ended)")
	}
}

// TestCommunityMembershipValidity is TestBandMembershipValidity's
// community counterpart.
func TestCommunityMembershipValidity(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	staffID, err := st.CreatePerson(ctx, "staff", "Staff", "staff@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	memberID, err := st.CreatePerson(ctx, "member", "Member", "member@example.com")
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	communityID, err := st.CreateCommunity(ctx, "Test Community", staffID)
	if err != nil {
		t.Fatalf("CreateCommunity: %v", err)
	}

	firstRowID, err := st.AddCommunityMember(ctx, communityID, memberID, "member", staffID)
	if err != nil {
		t.Fatalf("AddCommunityMember: %v", err)
	}
	if ok, err := st.IsCurrentCommunityMember(ctx, communityID, memberID); err != nil || !ok {
		t.Fatalf("IsCurrentCommunityMember: ok=%v err=%v", ok, err)
	}

	if err := st.RemoveCommunityMember(ctx, communityID, memberID); err != nil {
		t.Fatalf("RemoveCommunityMember: %v", err)
	}
	if ok, err := st.IsCurrentCommunityMember(ctx, communityID, memberID); err != nil || ok {
		t.Fatalf("expected memberID to no longer be current: ok=%v err=%v", ok, err)
	}

	rows, err := st.ListCommunityMembers(ctx, communityID, true)
	if err != nil {
		t.Fatalf("ListCommunityMembers: %v", err)
	}
	var closed CommunityMember
	for _, r := range rows {
		if r.ID == firstRowID {
			closed = r
		}
	}
	if closed.LeftAt == nil {
		t.Fatal("expected the removed row's LeftAt to be set")
	}

	secondRowID, err := st.AddCommunityMember(ctx, communityID, memberID, "member", staffID)
	if err != nil {
		t.Fatalf("AddCommunityMember (rejoin): %v", err)
	}
	if secondRowID == firstRowID {
		t.Fatal("expected rejoin to insert a new row")
	}
}

// TestBandMemberRevisionUndoRestoresValidity exercises revisions.go's
// per-stint undo path against the validity column: reverting the
// revision RemoveBandMember recorded must restore that SAME row to
// current (open-ended validity), not touch any other row.
func TestBandMemberRevisionUndoRestoresValidity(t *testing.T) {
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
	bandID, err := st.CreateBand(ctx, "Test Band", creatorID)
	if err != nil {
		t.Fatalf("CreateBand: %v", err)
	}
	rowID, err := st.AddBandMember(ctx, bandID, memberID, "core", creatorID)
	if err != nil {
		t.Fatalf("AddBandMember: %v", err)
	}
	if err := st.RemoveBandMember(ctx, bandID, memberID, creatorID); err != nil {
		t.Fatalf("RemoveBandMember: %v", err)
	}

	revisions, err := st.ListRevisions(ctx, "band_member", strconv.FormatInt(rowID, 10))
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(revisions) == 0 {
		t.Fatal("expected at least one revision for this band_member row")
	}
	// Newest first: revisions[0] is the pre-RemoveBandMember snapshot
	// (still current, LeftAt nil) — reverting to it undoes the removal.
	removeRevision := revisions[0]

	if err := st.RevertRevision(ctx, removeRevision.ID, creatorID); err != nil {
		t.Fatalf("RevertRevision: %v", err)
	}

	ok, err := st.IsCurrentBandMember(ctx, bandID, memberID)
	if err != nil {
		t.Fatalf("IsCurrentBandMember after undo: %v", err)
	}
	if !ok {
		t.Fatal("expected the undo to restore current membership")
	}

	members, err := st.ListBandMembers(ctx, bandID, true)
	if err != nil {
		t.Fatalf("ListBandMembers: %v", err)
	}
	var restored BandMember
	for _, m := range members {
		if m.ID == rowID {
			restored = m
		}
	}
	if restored.LeftAt != nil {
		t.Fatal("expected the SAME row's LeftAt to be cleared by the undo")
	}
}
