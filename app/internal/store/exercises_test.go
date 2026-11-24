package store

import (
	"context"
	"encoding/json"
	"testing"
)

func TestExercises(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	coach, _ := st.CreatePerson(ctx, "Coach", "coach@example.com")
	emma, _ := st.CreatePerson(ctx, "Emma", "emma@example.com")
	c, err := st.CreateCommunity(ctx, "Voice", coach)
	if err != nil {
		t.Fatal(err)
	}
	st.SetCommunityKind(ctx, c, "coaching")
	if _, err := st.AddCommunityMember(ctx, c, emma, "member", coach); err != nil {
		t.Fatal(err)
	}
	pat := json.RawMessage(`{"tonic":"C4","scale":"major","notes":"1 2 3 4 5 4 3 2 1"}`)
	if _, err := st.CreateExercise(ctx, c, ExerciseInput{Name: "", Pattern: pat}, coach); err == nil {
		t.Fatal("an exercise without a name")
	}
	x, err := st.CreateExercise(ctx, c, ExerciseInput{Name: "Five notes", Pattern: pat, Messages: map[string]string{"flat": "Plus haut, {name}."}}, coach)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetExerciseDemo(ctx, c, x, []byte("webm"), "audio/webm"); err != nil {
		t.Fatal(err)
	}
	e, err := st.GetExercise(ctx, c, x)
	if err != nil || !e.HasDemo || e.Kind != "pitch" || e.Messages["flat"] == "" {
		t.Fatalf("exercise: %+v %v", e, err)
	}
	if err := st.SetExercisePath(ctx, x, emma, []int{-2, 40, -2}); err != nil {
		t.Fatal(err)
	}
	if w, _ := st.ExercisePath(ctx, x, emma); len(w) != 3 || w[1] != 24 {
		t.Fatalf("path: %v", w)
	}
	// A goal on the exercise, reached by a take.
	if _, err := st.CreatePracticeAssignment(ctx, NewPracticeAssignment{CommunityID: c, PersonID: emma, ExerciseID: x, PitchTarget: 80, By: coach}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := st.RecordExerciseTake(ctx, coach, x, Take{Level: "beginner", Pitch: 90}); ok {
		t.Fatal("the coach's take was kept")
	}
	if ok, err := st.RecordExerciseTake(ctx, emma, x, Take{Level: "beginner", Pitch: 85, Rhythm: 70, Seconds: 60}); err != nil || !ok {
		t.Fatalf("take: %v %v", ok, err)
	}
	p, err := st.ExerciseProgressOf(ctx, emma, x)
	if err != nil || p == nil || !p.Reached || p.Best.Pitch != 85 || p.Settings.Messages["flat"] != "Plus haut, {name}." || p.Streak != 1 {
		t.Fatalf("progress: %+v %v", p, err)
	}
	if list, _ := st.ListStudentAssignments(ctx, c, emma, false); len(list) != 1 || list[0].Label() != "Five notes" || list[0].ReachedAt == nil {
		t.Fatalf("assignment: %+v", list)
	}
	// A session of the community's exercises only.
	if _, err := st.SaveExerciseSession(ctx, c, 0, "Tuesday", []int64{x, 999999}); err == nil {
		t.Fatal("a session with another community's exercise")
	}
	sid, err := st.SaveExerciseSession(ctx, c, 0, "Tuesday", []int64{x})
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := st.GetExerciseSession(ctx, c, sid); s == nil || len(s.ExerciseIDs) != 1 {
		t.Fatalf("session: %+v", s)
	}
}
