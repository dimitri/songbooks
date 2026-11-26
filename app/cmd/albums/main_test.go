package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"app/internal/setlist"
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

// TestRun: a band of two songs, one in a set list; the dry run prints the
// plan and writes nothing; --apply makes the album, the rename, the live
// version, the set list's move and the setting; a second run has nothing
// more to do. The mappings beside the command read.
func TestRun(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	core, err := st.CreatePerson(ctx, "Core", "core@example.com")
	if err != nil {
		t.Fatal(err)
	}
	band, err := st.CreateBand(ctx, "Other Lies", core)
	if err != nil {
		t.Fatal(err)
	}
	bones, _ := st.CreateVariant(ctx, store.OwnerBand, band, "Bones", "Other Lies", core)
	color, _ := st.CreateVariant(ctx, store.OwnerBand, band, "The Color of Thunder", "Other Lies", core)
	sl, err := st.CreateSetlist(ctx, "band", "Supersonic", "", nil, &band, nil, core)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveSetlistByID(ctx, sl, &setlist.Setlist{Sets: []setlist.Set{{Label: "Set 1", Entries: []setlist.Entry{{VariantID: bones, Note: "tape first"}}}}}, "Supersonic", "", "", core); err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf(`{"band": %d, "expectBand": "Other Lies", "liveVersions": "setlists", "setlistVersions": "live",
	  "albums": [{"title": "Ultrablanc", "releasedOn": "2026-04-10", "tracks": [
	    {"song": %d, "songTitle": "The Color of Thunder", "title": "Colors of Thunder", "rename": true, "duration": "3:50", "tempo": 115},
	    {"song": %d, "songTitle": "Bones", "duration": "2:23", "tempo": 90, "isrc": "FR-9W1-25-34056"}]}]}`, band, color, bones)
	var m store.AlbumsMapping
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(ctx, st, &out, m, store.AlbumsOptions{ActorID: core}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"DRY RUN", `create album "Ultrablanc"`, `rename song`, `"Colors of Thunder"`, `live version of "Bones"`, "1 set list line(s) moved", "set lists use: song → live",
		"1 albums, 2 tracks, 1 songs renamed, 1 live versions"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("dry run lacks %q:\n%s", want, out.String())
		}
	}
	if albums, _ := st.BandAlbums(ctx, band); len(albums) != 0 {
		t.Fatal("the dry run wrote")
	}
	out.Reset()
	if err := run(ctx, st, &out, m, store.AlbumsOptions{Apply: true, ActorID: core}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "APPLIED") {
		t.Fatal(out.String())
	}
	albums, _ := st.BandAlbums(ctx, band)
	if len(albums) != 1 || len(albums[0].Tracks) != 2 || albums[0].Tracks[0].Title != "Colors of Thunder" || albums[0].Tracks[1].LiveID == 0 {
		t.Fatalf("albums: %+v", albums)
	}
	out.Reset()
	if err := run(ctx, st, &out, m, store.AlbumsOptions{Apply: true, ActorID: core}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "0 songs renamed, 0 live versions") {
		t.Fatalf("second run:\n%s", out.String())
	}

	// The mappings kept beside the command are well formed.
	for _, f := range []string{"otherlies.json", "testdata/velvet-hour.json"} {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var m store.AlbumsMapping
		if err := json.Unmarshal(raw, &m); err != nil || m.Band == 0 || len(m.Albums) != 7 {
			t.Fatalf("%s: %v (%d albums)", f, err, len(m.Albums))
		}
	}
}
