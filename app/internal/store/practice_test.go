package store

import (
	"context"
	"testing"
	"time"
)

func TestPractice(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	coach, _ := st.CreatePerson(ctx, "coach", "Coach", "coach@example.com")
	emma, _ := st.CreatePerson(ctx, "emma", "Emma", "emma@example.com")
	leo, _ := st.CreatePerson(ctx, "leo", "Leo", "leo@example.com")
	c, err := st.CreateCommunity(ctx, "Voice", coach)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetCommunityKind(ctx, c, "coaching"); err != nil {
		t.Fatal(err)
	}
	song, err := st.CreateVariant(ctx, OwnerPerson, coach, "A song", "", coach)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []int64{emma, leo} {
		if _, err := st.AddCommunityMember(ctx, c, p, "member", coach); err != nil {
			t.Fatal(err)
		}
	}
	sb, err := st.StudentSongbook(ctx, c, emma, "songs", coach)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddToSongbook(ctx, sb, song, coach); err != nil {
		t.Fatal(err)
	}
	g, err := st.CreateCoachingGroup(ctx, c, "Tuesday", "year", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetGroupMembers(ctx, g, []int64{leo}, coach); err != nil {
		t.Fatal(err)
	}

	if _, err := st.CreatePracticeAssignment(ctx, NewPracticeAssignment{CommunityID: c, PersonID: emma}); err == nil {
		t.Fatal("an assignment with nothing to practise")
	}
	a, err := st.CreatePracticeAssignment(ctx, NewPracticeAssignment{CommunityID: c, PersonID: emma, VariantID: song, Notes: "the bridge", PerWeek: 3, By: coach})
	if err != nil {
		t.Fatal(err)
	}
	ga, err := st.CreatePracticeAssignment(ctx, NewPracticeAssignment{CommunityID: c, GroupID: g, Title: "Lip trills, 5 minutes", PerWeek: 9, By: coach})
	if err != nil {
		t.Fatal(err)
	}

	// Played from her songbook: counted on the assignment, one row a day.
	for range 2 {
		if ok, err := st.LogPlayed(ctx, emma, sb, song, 90); err != nil || !ok {
			t.Fatalf("played: %v %v", ok, err)
		}
	}
	// The coach playing it, or someone else's songbook: nothing.
	if ok, _ := st.LogPlayed(ctx, coach, sb, song, 90); ok {
		t.Fatal("the coach's playing was logged")
	}
	if ok, _ := st.LogPlayed(ctx, leo, sb, song, 90); ok {
		t.Fatal("someone else's songbook was logged")
	}
	logs, err := st.ListPracticeLogs(ctx, c, emma, time.Now().AddDate(0, 0, -7))
	if err != nil || len(logs) != 1 || logs[0].Seconds != 180 || logs[0].AssignmentID == nil || *logs[0].AssignmentID != a || logs[0].Label != "A song" {
		t.Fatalf("logs: %+v %v", logs, err)
	}

	// Leo's group assignment, told; his list has it, Emma's doesn't.
	if _, err := st.LogPractice(ctx, c, leo, ga, time.Now(), 300, "felt easier"); err != nil {
		t.Fatal(err)
	}
	la, err := st.ListStudentAssignments(ctx, c, leo, false)
	if err != nil || len(la) != 1 || la[0].ID != ga || la[0].DaysThisWeek != 1 || la[0].PerWeek != 7 || la[0].GroupName != "Tuesday" || la[0].LastPracticed == nil {
		t.Fatalf("leo's: %+v %v", la, err)
	}
	ea, _ := st.ListStudentAssignments(ctx, c, emma, false)
	if len(ea) != 1 || ea[0].ID != a || ea[0].SongbookID != sb || ea[0].DaysThisWeek != 1 {
		t.Fatalf("emma's: %+v", ea)
	}

	sum, err := st.CommunityPracticeSummaries(ctx, c)
	if err != nil || sum[emma].Seconds != 180 || sum[emma].Open != 1 || sum[leo].Minutes() != 5 {
		t.Fatalf("summaries: %+v %v", sum, err)
	}

	// Done: off the open list, still in the full one.
	if err := st.SetPracticeAssignmentDone(ctx, c, a, true); err != nil {
		t.Fatal(err)
	}
	if ea, _ = st.ListStudentAssignments(ctx, c, emma, false); len(ea) != 0 {
		t.Fatalf("done still open: %+v", ea)
	}
	if ea, _ = st.ListStudentAssignments(ctx, c, emma, true); len(ea) != 1 || ea[0].DoneAt == nil {
		t.Fatalf("done: %+v", ea)
	}
}
