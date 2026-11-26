// albums makes a band's albums and live versions from what it has
// (docs/albums-and-live-versions.md §9; store/albums_mapping.go): its
// albums and their tracks from a mapping (Other Lies': otherlies.json
// beside this file) and/or the masters of its recordings folder, the
// songs renamed to the release's printed titles where the mapping says
// so, the live versions of the songs its set lists play (made from the
// songs as they are now: the live version keeps their chart, tempo and
// sync, and the set list lines move to it; the album song takes the
// album's tempo when the master's name says it), and the band's set
// lists set to use them.
//
// The dry run does it all in one transaction and rolls it back, so what
// it prints is what --apply does. It never prints a song's text. It
// doesn't migrate the database (store.OpenCurrent): run it against a
// database the app of the same version has migrated.
//
//	albums --db "$DATABASE_URL" --mapping cmd/albums/otherlies.json                # dry run
//	albums --db "$DATABASE_URL" --mapping cmd/albums/otherlies.json --artwork      # dry run, the artwork fetched
//	albums --db "$DATABASE_URL" --mapping cmd/albums/otherlies.json --artwork --apply --as 1
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"app/internal/brand"
	"app/internal/store"
)

func main() {
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "Postgres connection string (or set DATABASE_URL)")
	mapping := flag.String("mapping", "", "the band's mapping (JSON: its band, albums, tracks, live versions)")
	band := flag.Int64("band", 0, "the band (overrides the mapping's)")
	fromFolder := flag.Bool("from-folder", false, "also make an album of each Albums/N - Title/ folder the mapping doesn't name")
	apply := flag.Bool("apply", false, "make the changes (without it, only print what would change)")
	artwork := flag.Bool("artwork", false, "fetch each album's artwork from the mapping's address once, to keep it in the app")
	actor := flag.Int64("as", 0, "the person the changes are recorded as (revisions); 0: nobody")
	ignoreTitles := flag.Bool("ignore-titles", false, "go on when a song's title or the band's name isn't the mapping's (a copy of the data under other names): those songs aren't renamed")
	flag.Parse()
	if *dbURL == "" {
		log.Fatal("--db (or DATABASE_URL) is required")
	}
	var m store.AlbumsMapping
	if *mapping != "" {
		raw, err := os.ReadFile(*mapping)
		if err != nil {
			log.Fatal(err)
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			log.Fatalf("%s: %v", *mapping, err)
		}
	}
	if *band != 0 {
		m.Band = *band
	}
	m.FromFolder = m.FromFolder || *fromFolder
	if m.Band == 0 {
		log.Fatal("no band: --band, or the mapping's \"band\"")
	}
	ctx := context.Background()
	opts := store.AlbumsOptions{Apply: *apply, ActorID: *actor, IgnoreTitles: *ignoreTitles}
	if *artwork {
		opts.Artwork = fetchArtwork(ctx, m, os.Stderr)
	}
	st, err := store.OpenCurrent(ctx, *dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if err := run(ctx, st, os.Stdout, m, opts); err != nil {
		log.Fatal(err)
	}
}

// run maps the band's albums and prints the report to w.
func run(ctx context.Context, st *store.Store, w io.Writer, m store.AlbumsMapping, opts store.AlbumsOptions) error {
	rep, err := st.MapAlbums(ctx, m, opts)
	if err != nil {
		return err
	}
	head := "DRY RUN (nothing written; --apply to write it)"
	if opts.Apply {
		head = "APPLIED"
	}
	fmt.Fprintln(w, head)
	for _, l := range rep.Lines {
		fmt.Fprintln(w, l)
	}
	if len(rep.Warnings) > 0 {
		fmt.Fprintln(w, "\nWarnings:")
		for _, l := range rep.Warnings {
			fmt.Fprintln(w, "  "+l)
		}
	}
	fmt.Fprintf(w, "\n%d albums, %d tracks, %d songs renamed, %d live versions (%d set list lines moved), %d album tempos set\n",
		rep.Albums, rep.Tracks, rep.Renamed, rep.Live, rep.SetlistLines, rep.Tempos)
	return nil
}

// fetchArtwork fetches each album's artwork once and prepares it as the
// app keeps it (brand.PrepareArtwork); one that fails is reported and
// left out.
func fetchArtwork(ctx context.Context, m store.AlbumsMapping, log io.Writer) map[string]brand.LogoImage {
	client := &http.Client{Timeout: 60 * time.Second}
	out := map[string]brand.LogoImage{}
	for _, a := range m.Albums {
		if a.Artwork == "" {
			continue
		}
		img, err := fetchOne(ctx, client, a.Artwork)
		if err != nil {
			fmt.Fprintf(log, "artwork of %q: %v (left out)\n", a.Title, err)
			continue
		}
		out[a.Title] = img
	}
	return out
}

func fetchOne(ctx context.Context, client *http.Client, url string) (brand.LogoImage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return brand.LogoImage{}, err
	}
	req.Header.Set("User-Agent", "songbooks-albums/1")
	res, err := client.Do(req)
	if err != nil {
		return brand.LogoImage{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return brand.LogoImage{}, fmt.Errorf("%s answered %s", url, res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, brand.MaxArtworkUpload+1))
	if err != nil {
		return brand.LogoImage{}, err
	}
	return brand.PrepareArtwork(data)
}
