package store

import (
	"context"
	"testing"
	"time"
)

// TestVocalSlider: a community's player for singers, off until its staff
// turn it on, and whose songs get it; the coming week's lessons of its
// students and groups; and only the community's staff coach.
func TestVocalSlider(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	coach, err := st.CreatePerson(ctx, "coach", "Coach", "coach@example.com")
	if err != nil {
		t.Fatal(err)
	}
	poppy, _ := st.CreatePerson(ctx, "poppy", "Poppy", "poppy@example.com")
	george, _ := st.CreatePerson(ctx, "george", "George", "george@example.com")
	outsider, _ := st.CreatePerson(ctx, "outsider", "Outsider", "outsider@example.com")
	c, err := st.CreateCommunity(ctx, "Voice", coach)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetCommunityKind(ctx, c, "coaching"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []int64{poppy, george} {
		if _, err := st.AddCommunityMember(ctx, c, p, "member", coach); err != nil {
			t.Fatal(err)
		}
	}

	// Off by default (migration 0093 turns it on for one community only).
	if got, _ := st.GetCommunity(ctx, c); got.PlayerVocalSlider {
		t.Fatal("on by default")
	}
	if on, _ := st.CommunityPlayerVocalSlider(ctx, c); on {
		t.Fatal("on by default")
	}
	song, err := st.CreateVariant(ctx, OwnerPerson, coach, "Song", "", coach)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := st.StudentSongbook(ctx, c, poppy, "Poppy’s songs", coach)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddToSongbook(ctx, sb, song, coach); err != nil {
		t.Fatal(err)
	}
	if id, _ := st.MemberVocalSliderCommunity(ctx, poppy, song); id != 0 {
		t.Fatalf("a community with it off: %d", id)
	}

	// On: its members get it on the songs it has, nobody else does.
	if err := st.SetCommunityPlayerVocalSlider(ctx, c, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetCommunity(ctx, c); !got.PlayerVocalSlider {
		t.Fatal("not on")
	}
	for who, want := range map[int64]int64{poppy: c, george: c, coach: c, outsider: 0} {
		if id, err := st.MemberVocalSliderCommunity(ctx, who, song); err != nil || id != want {
			t.Errorf("person %d: %d, %v (want %d)", who, id, err, want)
		}
	}
	other, _ := st.CreateVariant(ctx, OwnerPerson, coach, "Elsewhere", "", coach)
	if id, _ := st.MemberVocalSliderCommunity(ctx, poppy, other); id != 0 {
		t.Fatalf("a song the community hasn't: %d", id)
	}
	if err := st.SetCommunityPlayerVocalSlider(ctx, 999999, true); err == nil {
		t.Fatal("no such community, no error")
	}

	// Who coaches: the staff, not a student.
	if ok, _ := st.HasAnyCommunityManagePrivilege(ctx, c, coach); !ok {
		t.Fatal("the coach can't give keys")
	}
	if ok, _ := st.HasAnyCommunityManagePrivilege(ctx, c, poppy); ok {
		t.Fatal("a student can give keys")
	}

	// The coming week: George on Saturday, Poppy on Tuesday and again
	// later (the soonest counts), a group's lesson; one next month and
	// one already over don't count.
	now := time.Date(2026, 11, 19, 15, 0, 0, 0, time.UTC)
	lesson := func(name string, at time.Time, who ...int64) int64 {
		end := at.Add(time.Hour)
		id, err := st.CreateEvent(ctx, "community", c, "lesson", name, "", "", at, &end, "members", coach)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range who {
			if err := st.RegisterForEvent(ctx, id, p, 0); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	lesson("George", now.Add(46*time.Hour), george)
	lesson("Poppy", now.Add(5*24*time.Hour), poppy)
	lesson("Poppy again", now.Add(6*24*time.Hour), poppy)
	lesson("Poppy next month", now.Add(30*24*time.Hour), poppy)
	lesson("Over", now.Add(-3*time.Hour), george)
	lesson("Under way", now.Add(-30*time.Minute), poppy) // ends in half an hour
	gid, err := st.CreateCoachingGroup(ctx, c, "Tuesday", "year", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	gl := lesson("Tuesday", now.Add(4*24*time.Hour))
	if err := st.SetLessonDetails(ctx, gl, "in_person", &gid); err != nil {
		t.Fatal(err)
	}
	people, groups, err := st.UpcomingSessions(ctx, c, now, now.Add(7*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if at := people[george]; !at.Equal(now.Add(46 * time.Hour)) {
		t.Errorf("George: %v", at)
	}
	if at := people[poppy]; !at.Equal(now.Add(-30 * time.Minute)) {
		t.Errorf("Poppy: %v (the lesson under way is the soonest)", at)
	}
	if _, ok := people[outsider]; ok || len(people) != 2 {
		t.Errorf("people: %v", people)
	}
	if at := groups[gid]; len(groups) != 1 || !at.Equal(now.Add(4*24*time.Hour)) {
		t.Errorf("groups: %v", groups)
	}
	// From tomorrow on, Poppy's next one is Tuesday's.
	people, _, _ = st.UpcomingSessions(ctx, c, now.Add(24*time.Hour), now.Add(8*24*time.Hour))
	if at := people[poppy]; !at.Equal(now.Add(5 * 24 * time.Hour)) {
		t.Errorf("Poppy from tomorrow: %v", at)
	}
}
