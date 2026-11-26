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

// TestRun: the dry run lists the arrangements of catalogue songs by
// owner, classed, and changes nothing; --apply folds back the identical
// ones only (their songbook entries back to the original), the fix and the
// real arrangement left as they are; a second run has nothing to fold.
func TestRun(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	ann, err := st.CreatePerson(ctx, "Ann", "ann@example.com")
	must(err)
	band, err := st.CreateBand(ctx, "Mercy", ann)
	must(err)
	orig, err := st.CreateVariant(ctx, store.OwnerCatalog, 0, "Le Vent", "Someone", ann)
	must(err)
	start := "title: Le Vent\nartist: Someone\n\n[Verse]\ngrid: C | G | Am | F\n"
	_, err = st.SaveVariantRaw(orig, start, "", ann)
	must(err)
	book, err := st.GetDefaultSongbook(ctx, "band", band)
	must(err)
	must(st.AddToSongbook(ctx, book, orig, ann))
	identical, err := st.MakeVersion(ctx, ann, orig, store.BandOwner(band), book, "")
	must(err)
	mine, err := st.GetDefaultSongbook(ctx, "person", ann)
	must(err)
	must(st.AddToSongbook(ctx, mine, orig, ann))
	fix, err := st.MakeVersion(ctx, ann, orig, store.PersonOwner(ann), mine, "")
	must(err)
	_, err = st.SaveVariantRaw(fix, strings.Replace(start, "Am | F", "Am7 | F", 1), "", ann)
	must(err)
	named, err := st.MakeVersion(ctx, ann, fix, store.PersonOwner(ann), mine, "slow")
	must(err)
	_ = named

	var out bytes.Buffer
	must(run(ctx, st, &out, false))
	got := out.String()
	for _, want := range []string{`band "Mercy"`, `user "Ann"`, "identical", "fix ", "would move 1 songbook entries", "nothing changed"} {
		if !strings.Contains(got, want) {
			t.Errorf("dry run: no %q in\n%s", want, got)
		}
	}
	if v, _ := st.GetVariant(ctx, identical); v == nil {
		t.Fatal("the dry run folded")
	}

	out.Reset()
	must(run(ctx, st, &out, true))
	if !strings.Contains(out.String(), "folded back 1") {
		t.Errorf("apply:\n%s", out.String())
	}
	if v, _ := st.GetVariant(ctx, identical); v != nil {
		t.Error("the identical arrangement is still there")
	}
	if _, in, _ := st.SongbookEntryKey(ctx, book, orig); !in {
		t.Error("the band's songbook doesn't link the original again")
	}
	if v, _ := st.GetVariant(ctx, fix); v == nil {
		t.Error("the fix was folded by the tool")
	}
	out.Reset()
	must(run(ctx, st, &out, true))
	if !strings.Contains(out.String(), "folded back 0") {
		t.Errorf("a second run:\n%s", out.String())
	}
}
