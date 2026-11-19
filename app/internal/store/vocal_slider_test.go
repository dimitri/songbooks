package store

import (
	"context"
	"errors"
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

	// On: its members get it on the songs it has, nobody else does.
	if err := st.SetCommunityPlayerVocalSlider(ctx, c, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetCommunity(ctx, c); !got.PlayerVocalSlider {
		t.Fatal("not on")
	}
	if err := st.SetCommunityPlayerVocalSlider(ctx, 999999, true); err == nil {
		t.Fatal("no such community, no error")
	}

	// The key per student (migration 0097): its own option, off until
	// turned on, apart from the voice's slider.
	if o, _ := st.CommunityPlayerOptions(ctx, c); !o.VocalSlider || o.CoachStudentKeys {
		t.Fatalf("options: %+v", o)
	}
	if err := st.SetCommunityCoachStudentKeys(ctx, c, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetCommunity(ctx, c); !got.CoachStudentKeys {
		t.Fatal("key per student not on")
	}
	if err := st.SetCommunityPlayerVocalSlider(ctx, c, false); err != nil {
		t.Fatal(err)
	}
	if o, _ := st.CommunityPlayerOptions(ctx, c); o.VocalSlider || !o.CoachStudentKeys {
		t.Fatalf("each its own: %+v", o)
	}
	if err := st.SetCommunityPlayerVocalSlider(ctx, c, true); err != nil {
		t.Fatal(err)
	}
	// Neither on for a community that isn't a coaching one; off is fine.
	band, _ := st.CreateCommunity(ctx, "Jam", coach)
	for _, set := range []func(context.Context, int64, bool) error{st.SetCommunityPlayerVocalSlider, st.SetCommunityCoachStudentKeys} {
		if err := set(ctx, band, true); !errors.Is(err, ErrNotCoaching) {
			t.Errorf("on for a plain community: %v", err)
		}
		if err := set(ctx, band, false); err != nil {
			t.Errorf("off for a plain community: %v", err)
		}
	}
	if o, _ := st.CommunityPlayerOptions(ctx, band); o.VocalSlider || o.CoachStudentKeys {
		t.Fatalf("a plain community: %+v", o)
	}
	// Even set behind its back, a plain community has neither.
	if _, err := st.db.Exec(ctx, `UPDATE communities SET player_vocal_slider = true, coach_student_keys = true WHERE id = $1`, band); err != nil {
		t.Fatal(err)
	}
	if o, _ := st.CommunityPlayerOptions(ctx, band); o.VocalSlider || o.CoachStudentKeys {
		t.Fatalf("a plain community, set by hand: %+v", o)
	}

	// A student's key, within the community only.
	if err := st.SetEntrySemitones(ctx, sb, song, -1); err != nil {
		t.Fatal(err)
	}
	if id, n, ok, _ := st.StudentKeyIn(ctx, poppy, c, song); !ok || n != -1 || id != sb {
		t.Fatalf("Poppy's key in her community: %d %d %v", id, n, ok)
	}
	if _, _, ok, _ := st.StudentKeyIn(ctx, poppy, band, song); ok {
		t.Fatal("Poppy's key in another community")
	}
	if _, _, ok, _ := st.StudentKeyIn(ctx, george, c, song); ok {
		t.Fatal("George has no key for it")
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
