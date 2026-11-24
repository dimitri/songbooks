package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestEventRoutesStayInTheirCommunity: an organiser of their own community
// can't reach another community's event by putting its id under their own
// community's path — not to make themselves its manager, nor to touch its
// program — while the same calls on their own community's event still work.
func TestEventRoutesStayInTheirCommunity(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	ownerID, owner := e.person("Olga")
	intruderID, intruder := e.person("Ivan")

	victim, err := e.st.CreateCommunity(ctx, "Victim Jam "+e.sfx, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	theirEvent, err := e.st.CreateEvent(ctx, "community", victim, "jam", "Their Night", "", "", time.Now().Add(48*time.Hour), nil, "members", ownerID)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := e.st.CreateCommunity(ctx, "Intruder Jam "+e.sfx, intruderID)
	if err != nil {
		t.Fatal(err)
	}
	myEvent, err := e.st.CreateEvent(ctx, "community", mine, "jam", "My Night", "", "", time.Now().Add(48*time.Hour), nil, "members", intruderID)
	if err != nil {
		t.Fatal(err)
	}

	body := fmt.Sprintf(`{"personId":%d}`, intruderID)
	if w := e.do("POST", fmt.Sprintf("/api/communities/%d/events/%d/managers", mine, theirEvent), body, intruder); w.Code != http.StatusNotFound {
		t.Errorf("manager of another community's event: %d, want 404", w.Code)
	}
	managers, err := e.st.ListEventManagers(ctx, theirEvent)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range managers {
		if m.PersonID == intruderID {
			t.Fatalf("the intruder became a manager of the other community's event")
		}
	}
	if w := e.do("DELETE", fmt.Sprintf("/api/communities/%d/events/%d/managers/%d", mine, theirEvent, ownerID), "", intruder); w.Code != http.StatusNotFound {
		t.Errorf("removing a manager of another community's event: %d, want 404", w.Code)
	}
	if w := e.do("GET", fmt.Sprintf("/communities/%d/events/%d/program", mine, theirEvent), "", intruder); w.Code != http.StatusNotFound {
		t.Errorf("another community's program page: %d, want 404", w.Code)
	}

	// Their own community's event: as before.
	if w := e.do("POST", fmt.Sprintf("/api/communities/%d/events/%d/managers", mine, myEvent), body, intruder); w.Code != http.StatusOK {
		t.Errorf("manager of own event: %d, want 200", w.Code)
	}
	if w := e.do("POST", fmt.Sprintf("/api/communities/%d/events/%d/managers", victim, theirEvent), fmt.Sprintf(`{"personId":%d}`, ownerID), owner); w.Code != http.StatusOK {
		t.Errorf("owner assigning on their own event: %d, want 200", w.Code)
	}
}
