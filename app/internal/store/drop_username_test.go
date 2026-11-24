package store

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"
)

// TestMigration0136DropsUsername: on a database whose people still have
// their old usernames (as every one before 0136 has), 0136 drops the
// column and nothing else: the people, their emails and names stay. Run
// again (a second app starting), it changes nothing.
func TestMigration0136DropsUsername(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	raw, err := migrationsFS.ReadFile("migrations/0136_drop_username.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// Back to before 0136, in a transaction rolled back after: the column
	// as 0135 left it, filled in, and a placeholder without one.
	if _, err := tx.Exec(ctx, `ALTER TABLE people ADD COLUMN username text UNIQUE`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO people (display_name, email, username, is_shell) VALUES
		  ('Lena', 'lena@example.test', 'lena', false),
		  ('Marc', 'marc@example.test', 'marcus', false),
		  ('Roland', NULL, NULL, true)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := tx.Exec(ctx, string(raw)); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	var cols int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'people' AND column_name = 'username'`).Scan(&cols); err != nil {
		t.Fatal(err)
	}
	if cols != 0 {
		t.Errorf("people.username is still there")
	}
	var n, emails int
	var names string
	if err := tx.QueryRow(ctx, `SELECT count(*), count(email), string_agg(display_name, ',' ORDER BY display_name) FROM people`).Scan(&n, &emails, &names); err != nil {
		t.Fatal(err)
	}
	if n != 3 || emails != 2 || names != "Lena,Marc,Roland" {
		t.Errorf("after 0136: %d people, %d emails, %q", n, emails, names)
	}
}

// TestClaimPersonalInviteAs: a placeholder added by name, taken by
// someone who has an account, merges into it: memberships, roles and
// instruments become the account's (where the account has one already,
// its own stays), the placeholder and its link are gone; a link can't be
// taken twice, nor by a placeholder.
func TestClaimPersonalInviteAs(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	core, err := st.CreatePerson(ctx, "Core", "core@example.test")
	if err != nil {
		t.Fatal(err)
	}
	rita, err := st.CreatePerson(ctx, "Rita", "rita@example.test")
	if err != nil {
		t.Fatal(err)
	}
	both, err := st.CreateBand(ctx, "Both", core) // Rita's band already
	if err != nil {
		t.Fatal(err)
	}
	newBand, err := st.CreateBand(ctx, "New", core)
	if err != nil {
		t.Fatal(err)
	}
	jam, err := st.CreateCommunity(ctx, "Jam", core)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBandMember(ctx, both, rita, "core", core); err != nil {
		t.Fatal(err)
	}
	if err := st.CollectInstruments(ctx, rita, []string{"bass"}, InstrumentSource{Kind: InstrumentFromSelf}); err != nil {
		t.Fatal(err)
	}

	shell, err := st.CreateShellPerson(ctx, "Roland")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []int64{both, newBand} {
		if _, err := st.AddBandMember(ctx, b, shell, "guest", core); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.AddCommunityMember(ctx, jam, shell, "member", core); err != nil {
		t.Fatal(err)
	}
	if err := st.CollectInstruments(ctx, shell, []string{"bass", "drums"}, InstrumentSource{Kind: InstrumentFromBand, ID: newBand}); err != nil {
		t.Fatal(err)
	}
	token, err := st.CreatePersonalInvite(ctx, shell, core, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	// A placeholder can't take another's place.
	other, err := st.CreateShellPerson(ctx, "Other")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimPersonalInviteAs(ctx, token, other); !errors.Is(err, ErrConflict) {
		t.Errorf("a placeholder taking the link: %v, want ErrConflict", err)
	}

	if err := st.ClaimPersonalInviteAs(ctx, token, rita); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetPersonByID(ctx, shell); !errors.Is(err, ErrNotFound) {
		t.Errorf("the placeholder is still there: %v", err)
	}
	for _, b := range []int64{both, newBand} {
		if ok, err := st.IsCurrentBandMember(ctx, b, rita); err != nil || !ok {
			t.Errorf("band %d: Rita isn't a member: %v", b, err)
		}
	}
	ms, err := st.GetPersonMemberships(ctx, rita)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range ms {
		got[m.Name] = m.Status
	}
	// Her own role in Both stays (core, not the placeholder's guest).
	if len(ms) != 3 || got["Both"] != "core" || got["New"] != "guest" || got["Jam"] != "member" {
		t.Errorf("Rita's memberships: %+v", ms)
	}
	ins, err := st.ListPersonInstruments(ctx, rita)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, i := range ins {
		names = append(names, i.Instrument)
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != "bass" || names[1] != "drums" {
		t.Errorf("Rita's instruments: %v", names)
	}
	if _, err := st.GetPersonalInvite(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Errorf("the link still works: %v", err)
	}
	if err := st.ClaimPersonalInviteAs(ctx, token, rita); !errors.Is(err, ErrNotFound) {
		t.Errorf("taking the link again: %v, want ErrNotFound", err)
	}
}
