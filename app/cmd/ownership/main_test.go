package main

import (
	"bytes"
	"context"
	"os"
	"strings"
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

// TestRun: a catalogue song, a band's unchanged copy of it, What's Up's
// song; the dry run says what it would do and does nothing, --apply does
// it, a second run has nothing to do.
func TestRun(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	admin, err := st.CreatePerson(ctx, "Admin", "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	song, err := st.CreateVariant(ctx, store.OwnerCatalog, 0, "Le Vent", "Someone", admin)
	if err != nil {
		t.Fatal(err)
	}
	mercy, err := st.CreateBand(ctx, "Mercy", admin)
	if err != nil {
		t.Fatal(err)
	}
	// The band's unchanged copy, as cmd/bandsongs made them.
	if _, err := st.MakeVersion(ctx, admin, song, store.BandOwner(mercy), 0, ""); err != nil {
		t.Fatal(err)
	}
	whatsUp, err := st.CreateBand(ctx, "What's Up", admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateVariant(ctx, store.OwnerBand, whatsUp, "Zombie", "Yungblud", admin); err != nil {
		t.Fatal(err)
	}

	out := func(apply, verbose bool) string {
		t.Helper()
		opts := store.DefaultOwnershipOptions()
		opts.Apply = apply
		var b bytes.Buffer
		if err := run(ctx, st, &b, opts, verbose); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}

	dry := out(false, true)
	for _, want := range []string{
		"global (catalogue): would give 0 songs to The Big Songbook",
		"bandsongs copies, Mercy (band 1): would remove 1 unchanged copies",
		"What's Up: would move 1 song to the catalogue",
		"(Le Vent — Someone, Mercy's unchanged copy of",
		"dry run: nothing changed",
	} {
		if !strings.Contains(dry, want) {
			t.Errorf("dry run: no %q in\n%s", want, dry)
		}
	}
	if again := out(false, false); again != strings.SplitN(dry, "\n\n", 2)[1] {
		t.Errorf("a second dry run differs:\n%s\nvs\n%s", again, dry)
	}

	applied := out(true, false)
	if !strings.Contains(applied, "removed 1 unchanged copies") || strings.Contains(applied, "dry run") {
		t.Errorf("--apply:\n%s", applied)
	}
	if last := out(true, false); !strings.Contains(last, "nothing to do") {
		t.Errorf("a second --apply:\n%s", last)
	}
}
