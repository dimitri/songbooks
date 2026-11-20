package store

import (
	"context"
	"testing"
	"time"
)

func TestQuietStudents(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	coach, _ := st.CreatePerson(ctx, "coach", "Coach", "coach@example.com")
	c, err := st.CreateCommunity(ctx, "Voice", coach)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetCommunityKind(ctx, c, "coaching"); err != nil {
		t.Fatal(err)
	}
	var students []int64
	for _, name := range []string{"emma", "leo", "zoe", "max", "new"} {
		p, err := st.CreatePerson(ctx, name, name, name+"@example.com")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.AddCommunityMember(ctx, c, p, "member", coach); err != nil {
			t.Fatal(err)
		}
		students = append(students, p)
	}
	emma, leo, zoe, newcomer := students[0], students[1], students[2], students[4]
	// Everyone but the newcomer (and the coach) joined a month ago.
	if _, err := st.db.Exec(ctx, `UPDATE community_members SET validity = tstzrange(now() - interval '30 days', NULL)
		WHERE community_id = $1 AND person_id <> $2`, c, newcomer); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	since := now.AddDate(0, 0, -14)
	// Emma practised this week; Leo came to a lesson last week; Zoé's
	// last lesson was a month ago and her next one is tomorrow; Max did
	// nothing.
	a, err := st.CreatePracticeAssignment(ctx, NewPracticeAssignment{CommunityID: c, PersonID: emma, Title: "Lip trills", PerWeek: 3, By: coach})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.LogPractice(ctx, c, emma, a, now, 300, ""); err != nil {
		t.Fatal(err)
	}
	lesson := func(at time.Time, who int64) {
		end := at.Add(time.Hour)
		ev, err := st.CreateEvent(ctx, "community", c, "lesson", "Lesson", "", "", at, &end, "members", coach)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.RegisterForEvent(ctx, ev, who, 0); err != nil {
			t.Fatal(err)
		}
	}
	lesson(now.AddDate(0, 0, -6), leo)
	lesson(now.AddDate(0, 0, -30), zoe)
	lesson(now.AddDate(0, 0, 1), zoe)

	n, err := st.QuietStudents(ctx, c, since, now)
	if err != nil {
		t.Fatal(err)
	}
	// Zoé and Max: not the coach (staff), not the newcomer.
	if n != 2 {
		t.Fatalf("quiet students: got %d, want 2", n)
	}
}
