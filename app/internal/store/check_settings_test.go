package store

import (
	"context"
	"testing"
)

func TestCheckSettings(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	coach, _ := st.CreatePerson(ctx, "Coach", "coach@example.com")
	c, err := st.CreateCommunity(ctx, "Voice", coach)
	if err != nil {
		t.Fatal(err)
	}
	set, err := st.GetCheckSettings(ctx, c)
	if err != nil || set.Custom || len(set.Levels) != 3 || set.Level("intermediate").Cents != 25 {
		t.Fatalf("defaults: %+v %v", set, err)
	}
	err = st.SetCheckSettings(ctx, c, []CheckLevel{
		{Key: "beginner", Name: "Bronze", Cents: 60, Ms: 300},
		{Key: "advanced", Cents: 1, Ms: 9999}, // kept in bounds
		{Key: "expert", Cents: 5},             // not a level
	}, map[string]string{"flat": "Souris en chantant, {name}."})
	if err != nil {
		t.Fatal(err)
	}
	set, _ = st.GetCheckSettings(ctx, c)
	b, a := set.Level("beginner"), set.Level("advanced")
	if !set.Custom || b.Name != "Bronze" || b.Cents != 60 || a.Cents != 5 || a.Ms != 600 || set.Level("intermediate").Cents != 25 || set.Messages["flat"] == "" || len(set.Levels) != 3 {
		t.Fatalf("saved: %+v", set)
	}
}
