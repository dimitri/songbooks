package store

import (
	"context"
	"testing"
	"time"
)

func TestCoaching(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	coach, err := st.CreatePerson(ctx, "coach", "Coach", "coach@example.com")
	if err != nil {
		t.Fatal(err)
	}
	student, err := st.CreatePerson(ctx, "student", "Student", "student@example.com")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreatePerson(ctx, "other", "Other", "other@example.com")
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.CreateCommunity(ctx, "Voice", coach)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetCommunityKind(ctx, c, "coaching"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []int64{student, other} {
		if _, err := st.AddCommunityMember(ctx, c, p, "member", coach); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := st.GetCommunity(ctx, c); !got.IsCoaching() {
		t.Fatalf("kind: %q", got.Kind)
	}

	// Ranges: three registers, noted or not, low and high in order.
	lo, hi := 64, 50
	if err := st.SetStudentRange(ctx, c, student, VoiceRange{Register: "chest", Low: &lo, High: &hi, Note: "warm"}, coach); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStudentRange(ctx, c, student, VoiceRange{Register: "falsetto"}, coach); err == nil {
		t.Fatal("an unknown register was taken")
	}
	rs, err := st.StudentRanges(ctx, c, student)
	if err != nil || len(rs) != 3 || rs[0].Register != "chest" || *rs[0].Low != 50 || *rs[0].High != 64 || rs[1].Low != nil {
		t.Fatalf("ranges: %+v, %v", rs, err)
	}

	// The student's songbook: theirs and the coach's, a song in their key.
	song, err := st.CreateVariant(ctx, OwnerPerson, coach, "Song", "", coach)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := st.StudentSongbook(ctx, c, student, "Student’s songs", coach)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := st.StudentSongbook(ctx, c, student, "x", coach); again != sb {
		t.Fatalf("a second songbook: %d, %d", sb, again)
	}
	if ok, _ := st.CanAccessSongbook(ctx, student, sb); !ok {
		t.Fatal("the student can't open their songbook")
	}
	if ok, _ := st.CanAccessSongbook(ctx, other, sb); ok {
		t.Fatal("another student opens it")
	}
	if err := st.AddToSongbook(ctx, sb, song, coach); err != nil {
		t.Fatal(err)
	}
	if err := st.SetEntrySemitones(ctx, sb, song, -3); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.EntrySemitones(ctx, sb, song); n != -3 {
		t.Fatalf("key: %d", n)
	}
	if err := st.SetEntrySemitones(ctx, sb, song, 13); err == nil {
		t.Fatal("a key out of range was taken")
	}

	// Lessons: the student's, not the other's.
	start := time.Now().Add(24 * time.Hour)
	end := start.Add(time.Hour)
	ev, err := st.CreateEvent(ctx, "community", c, "lesson", "Lesson", "", "Studio", start, &end, "members", coach)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RegisterForEvent(ctx, ev, student, 0); err != nil {
		t.Fatal(err)
	}
	if ls, _ := st.ListCoachingLessons(ctx, c, time.Now(), student); len(ls) != 1 {
		t.Fatalf("the student's lessons: %d", len(ls))
	}
	if ls, _ := st.ListCoachingLessons(ctx, c, time.Now(), other); len(ls) != 0 {
		t.Fatalf("the other's lessons: %d", len(ls))
	}
	cal, err := st.ListCalendarForPerson(ctx, other, time.Now(), time.Now().Add(48*time.Hour))
	if err != nil || len(cal) != 0 {
		t.Fatalf("another student's calendar: %+v, %v", cal, err)
	}
	cal, _ = st.ListCalendarForPerson(ctx, student, time.Now(), time.Now().Add(48*time.Hour))
	if len(cal) != 1 || cal[0].Kind != "lesson" {
		t.Fatalf("the student's calendar: %+v", cal)
	}
}
