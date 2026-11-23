package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"app/internal/reftitle"
	"app/internal/store"
)

// testStore opens the test database (SONGBOOKS_TEST_DATABASE_URL), emptied
// as the store's own tests empty it, or skips.
func testStore(t *testing.T) (*store.Store, *pgx.Conn) {
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
	return st, lock
}

// TestRun: the dry run prints what it would set and sets nothing; --apply
// fills the empty labels of the catalogue's songs from their references'
// titles (kept, or fetched), leaves a typed label, a song whose title
// only repeats it, a song without a reference and a band's song as they
// are; a second run has nothing to set.
func TestRun(t *testing.T) {
	st, db := testStore(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	ann, err := st.CreatePerson(ctx, "ann", "Ann", "ann@example.com")
	must(err)
	song := func(title, artist, ref string) int64 {
		t.Helper()
		id, err := st.CreateVariant(ctx, store.OwnerCatalog, 0, title, artist, ann)
		must(err)
		if ref != "" {
			must(st.SetCatalogueReference(ctx, id, ref))
		}
		return id
	}
	live := song("Someone Like You", "Adele", "https://www.youtube.com/watch?v=live")
	must(st.SetReferenceTitle(ctx, live, "https://www.youtube.com/watch?v=live", "Adele - Someone Like You (Live at The Royal Albert Hall)"))
	plain := song("Wonderwall", "Oasis", "https://www.youtube.com/watch?v=plain")
	must(st.SetReferenceTitle(ctx, plain, "https://www.youtube.com/watch?v=plain", "Oasis - Wonderwall (Official HD Video)"))
	typed := song("Hurt", "Johnny Cash", "https://www.youtube.com/watch?v=typed")
	must(st.SetReferenceTitle(ctx, typed, "https://www.youtube.com/watch?v=typed", "Johnny Cash - Hurt (Official Video)"))
	must(st.SetReferenceLabel(ctx, typed, ann, "American IV", ""))
	fetched := song("Layla", "Eric Clapton", "https://www.youtube.com/watch?v=fetch")
	stale := song("Hallelujah", "Leonard Cohen", "https://www.youtube.com/watch?v=new")
	// A title kept for the link before: not this one's.
	must(st.SetReferenceTitle(ctx, stale, "https://www.youtube.com/watch?v=old", "Leonard Cohen - Hallelujah (Live in London)"))
	none := song("Le Vent", "Someone", "")
	band, err := st.CreateBand(ctx, "Mercy", ann)
	must(err)
	bandSong, err := st.CreateVariant(ctx, store.OwnerBand, band, "Mine", "Mercy", ann)
	must(err)
	_, err = db.Exec(ctx, `UPDATE songs SET reference_url = 'https://www.youtube.com/watch?v=band', reference_title = 'Mercy - Mine (Live at home)', reference_title_url = 'https://www.youtube.com/watch?v=band', reference_title_at = now() WHERE id = $1`, bandSong)
	must(err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Query().Get("url"), "v=fetch"):
			w.Write([]byte(`{"title":"Eric Clapton - Layla (Unplugged) [HD]"}`))
		case strings.Contains(r.URL.Query().Get("url"), "v=new"):
			w.Write([]byte(`{"title":"Leonard Cohen - Hallelujah (Official Audio)"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := &reftitle.Fetcher{Client: srv.Client(), Providers: map[string]string{"youtube.com": srv.URL + "/oembed?format=json"}}

	label := func(id int64) string {
		t.Helper()
		var l string
		must(db.QueryRow(ctx, `SELECT version_name FROM songs WHERE id = $1`, id).Scan(&l))
		return l
	}
	labels := func() map[int64]string {
		out := map[int64]string{}
		for _, id := range []int64{live, plain, typed, fetched, stale, none, bandSong} {
			out[id] = label(id)
		}
		return out
	}
	before := labels()

	var out bytes.Buffer
	must(run(ctx, st, &out, options{Fetcher: f}))
	got := out.String()
	for _, want := range []string{
		"The Big Songbook: 6 songs; 1 with a label already (left as they are), 1 without a reference, 4 with a reference and no label.",
		"Titles: 2 kept for their reference, 2 fetched now (oEmbed or the page), 0 not found.",
		"Labels: would set 2; 2 left empty",
		`"Adele - Someone Like You (Live at The Royal Albert Hall)" → "Live at The Royal Albert Hall"`,
		`"Eric Clapton - Layla (Unplugged) [HD]" → "Unplugged"`,
		`"Oasis - Wonderwall (Official HD Video)"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the dry run doesn't say %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Mine") || strings.Contains(got, "American IV") {
		t.Errorf("the dry run lists a band's song or a typed label:\n%s", got)
	}
	if after := labels(); after[live] != before[live] || after[fetched] != "" {
		t.Errorf("the dry run changed labels: %v", after)
	}

	out.Reset()
	must(run(ctx, st, &out, options{Fetcher: f, Apply: true}))
	if !strings.Contains(out.String(), "Labels: set 2; 2 left empty") {
		t.Errorf("--apply says:\n%s", out.String())
	}
	want := map[int64]string{live: "Live at The Royal Albert Hall", plain: "", typed: "American IV", fetched: "Unplugged", stale: "", none: "", bandSong: ""}
	for id, w := range want {
		if g := label(id); g != w {
			t.Errorf("song %d labelled %q, want %q", id, g, w)
		}
	}
	// The titles fetched are kept for their link.
	if rt, err := st.ReferenceTitle(ctx, fetched); err != nil || rt.For("https://www.youtube.com/watch?v=fetch") != "Eric Clapton - Layla (Unplugged) [HD]" {
		t.Errorf("the fetched title isn't kept: %+v, %v", rt, err)
	}

	// Again, without fetching: nothing more to set.
	out.Reset()
	must(run(ctx, st, &out, options{Apply: true}))
	if !strings.Contains(out.String(), "Labels: set 0;") || !strings.Contains(out.String(), "not fetched (--no-fetch)") {
		t.Errorf("a second run:\n%s", out.String())
	}
}
