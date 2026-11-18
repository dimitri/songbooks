package store

import (
	"context"
	"testing"
	"time"
)

func TestLiveRoomAndHeadphones(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	coach, _ := st.CreatePerson(ctx, "coach", "Coach", "coach@example.com")
	c, err := st.CreateCommunity(ctx, "Voice", coach)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := st.CreateEvent(ctx, "community", c, "lesson", "Lesson", "", "", time.Now(), nil, "members", coach)
	if err != nil {
		t.Fatal(err)
	}
	room, err := st.LiveRoom(ctx, ev)
	if err != nil || len(room) != 36 {
		t.Fatalf("room %q, %v", room, err)
	}
	if again, _ := st.LiveRoom(ctx, ev); again != room {
		t.Fatalf("the room changed: %q, then %q", room, again)
	}
	if on, _ := st.LiveHeadphones(ctx, coach); on {
		t.Fatal("headphones on by default")
	}
	if err := st.SetLiveHeadphones(ctx, coach, true); err != nil {
		t.Fatal(err)
	}
	if on, _ := st.LiveHeadphones(ctx, coach); !on {
		t.Fatal("headphones not remembered")
	}
}
