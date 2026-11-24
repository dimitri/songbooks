package main

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"app/internal/store"
)

// testStore opens the test database (SONGBOOKS_TEST_DATABASE_URL), emptied
// as the store's own tests empty it, or skips.
func testStore(t *testing.T) *store.Store {
	t.Helper()
	conn := os.Getenv("SONGBOOKS_TEST_DATABASE_URL")
	if conn == "" {
		conn = "postgres://localhost/songbooks_test?sslmode=disable"
	}
	ctx := context.Background()
	lock, err := pgx.Connect(ctx, conn)
	if err != nil {
		t.Skipf("no test database at %s: %v", conn, err)
	}
	t.Cleanup(func() { lock.Close(context.Background()) })
	if _, err := lock.Exec(ctx, `SELECT pg_advisory_lock(7215)`); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(ctx, conn)
	if err != nil {
		t.Skipf("no test database at %s: %v", conn, err)
	}
	t.Cleanup(st.Close)
	if _, err := lock.Exec(ctx, `TRUNCATE people, bands, communities, file_blobs RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	return st
}

// TestCommunitySong: sbcoach songs gives a coaching community one song
// per song, never a copy per student or per run (docs/coaching.md, "A
// group works on one song"): the catalogue's song linked; the
// community's own as it is; the catalogue's song with another
// recording, the community's arrangement — made once, the same one the
// next time; someone's private song refused.
func TestCommunitySong(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	coach, err := st.CreatePerson(ctx, "Inès", "ines@example.com")
	if err != nil {
		t.Fatal(err)
	}
	community, err := st.CreateCommunity(ctx, "Studio Lumen", coach)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetCommunityKind(ctx, community, "coaching"); err != nil {
		t.Fatal(err)
	}
	catalogue, err := st.CreateVariant(ctx, store.OwnerCatalog, 0, "Small Plane", "Saltwater Radio", coach)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetCatalogueReference(ctx, catalogue, "https://www.youtube.com/watch?v=plane111111"); err != nil {
		t.Fatal(err)
	}
	own, err := st.CreateCommunityVariant(ctx, community, "Le Quai", "Studio", coach)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := st.CreateVariant(ctx, store.OwnerPerson, coach, "Her Own", "Inès", coach)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		what     string
		src      int64
		clip     string
		how      string
		sameAsIn bool
	}{
		{"the catalogue's song", catalogue, "", "linked", true},
		{"the catalogue's song, its own recording", catalogue, "https://www.youtube.com/watch?v=plane111111", "linked", true},
		{"the community's own", own, "https://www.youtube.com/watch?v=quai2222222", "own", true},
	} {
		id, how, err := communitySong(ctx, st, community, x.src, x.clip, coach)
		if err != nil {
			t.Fatalf("%s: %v", x.what, err)
		}
		if how != x.how || (id == x.src) != x.sameAsIn {
			t.Errorf("%s: %d %s (want %s)", x.what, id, how, x.how)
		}
		// Again: the same song, nothing new.
		again, _, err := communitySong(ctx, st, community, x.src, x.clip, coach)
		if err != nil || again != id {
			t.Errorf("%s, again: %d %v (want %d)", x.what, again, err, id)
		}
	}
	// Her private song: refused, as a link or an arrangement would be.
	if _, _, err := communitySong(ctx, st, community, mine, "", coach); err == nil {
		t.Error("her private song: not refused")
	}
	// Another recording of the catalogue's song: the community's
	// arrangement of it, which every later run gives, with a clip or not.
	arr, how, err := communitySong(ctx, st, community, catalogue, "https://www.youtube.com/watch?v=live3333333", coach)
	if err != nil || how != "arrangement" || arr == catalogue {
		t.Fatalf("another recording: %d %s %v", arr, how, err)
	}
	if again, how, err := communitySong(ctx, st, community, catalogue, "", coach); err != nil || again != arr || how != "own" {
		t.Errorf("the catalogue's song after its arrangement: %d %s %v (want %d own)", again, how, err, arr)
	}
}
