// ownership maps today's songs onto the ownership model
// (docs/ownership-model.md, §9 and §10): every song gets its one owner and
// is public or private, the band copies of November 2026 that nobody
// changed are undone, the songs that are catalogue material move into the
// catalogue, the paid songbooks are closed (migration 0114 does the
// mechanical rows: the catalogue's owner, the closed songbooks; this
// reports them). It prints, per row of §9's
// table, what it would change (with --apply, what it changed); -v lists
// every song. It never prints a song's text.
//
// The dry run does the whole mapping in one transaction and rolls it back,
// so what it prints is what --apply does. A second run finds nothing to
// do. It doesn't migrate the database's schema (store.OpenCurrent): it
// runs against a database the app of the same version has migrated.
//
//	ownership --db "$DATABASE_URL"            # dry run
//	ownership --db "$DATABASE_URL" -v         # dry run, every song
//	ownership --db "$DATABASE_URL" --apply    # do it
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"app/internal/store"
)

func main() {
	def := store.DefaultOwnershipOptions()
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "Postgres connection string (or set DATABASE_URL)")
	apply := flag.Bool("apply", false, "make the changes (without it, only print what would change)")
	verbose := flag.Bool("v", false, "print every song (its id and what happens to it), not only the totals")
	bands := flag.String("catalogue-bands", strings.Join(def.CatalogueBands, ","), "bands whose songs move into the catalogue (comma-separated names)")
	wins := flag.String("band-song-wins", strings.Join(def.BandSongWins, ","), `of those, the songs ("Title — Artist", comma-separated) that replace the catalogue's own`)
	communities := flag.String("catalogue-communities", strings.Join(def.CatalogueCommunities, ","), "communities whose charts replace the catalogue's (comma-separated names)")
	flag.Parse()
	if *dbURL == "" {
		log.Fatal("--db (or DATABASE_URL) is required")
	}

	ctx := context.Background()
	st, err := store.OpenCurrent(ctx, *dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	if err := run(ctx, st, os.Stdout, store.OwnershipOptions{
		Apply:                *apply,
		CatalogueBands:       splitList(*bands),
		BandSongWins:         splitList(*wins),
		CatalogueCommunities: splitList(*communities),
	}, *verbose); err != nil {
		log.Fatal(err)
	}
}

// run maps the ownership and prints the report to w.
func run(ctx context.Context, st *store.Store, w io.Writer, opts store.OwnershipOptions, verbose bool) error {
	r, err := st.MapOwnership(ctx, opts)
	if err != nil {
		return err
	}
	if verbose {
		for _, l := range r.Lines {
			fmt.Fprintln(w, l)
		}
		fmt.Fprintln(w)
	}
	report(w, r)
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func report(w io.Writer, r *store.OwnershipReport) {
	verb := func(would, did string) string {
		if r.Applied {
			return did
		}
		return would
	}
	fmt.Fprintf(w, "The Big Songbook is community %d.\n\n", r.BigSongbookID)

	fmt.Fprintf(w, "global (catalogue): %s %s to The Big Songbook and %s %d more public; the catalogue has %s, all public, at the end\n",
		verb("would give", "gave"), songs(r.CatalogueGiven), verb("would make", "made"), r.CatalogueMadePublic, songs(r.CatalogueSongs))

	fmt.Fprintf(w, "user: %s, each its person's, private: nothing to change\n", songs(r.PersonalSongs))

	removed, kept := 0, 0
	var moved store.RepointCounts
	for _, b := range r.BandCopies {
		fmt.Fprintf(w, "bandsongs copies, %s (band %d): %s %d unchanged copies (the band links the catalogue's songs again: %s); %d changed since, the band's versions\n",
			b.BandName, b.BandID, verb("would remove", "removed"), b.Removed, b.Moved, b.Kept)
		removed += b.Removed
		kept += b.Kept
		moved.Add(b.Moved)
	}
	fmt.Fprintf(w, "bandsongs copies, every band: %s %d, kept %d (%s)\n", verb("would remove", "removed"), removed, kept, moved)

	for _, b := range r.BandSongs {
		fmt.Fprintf(w, "band %d (%s): %s, the band's, private\n", b.ID, b.Name, songs(b.N))
	}

	byOwner := map[string]int{}
	var owners []string
	for _, m := range r.ToCatalogue {
		if byOwner[m.Owner] == 0 {
			owners = append(owners, m.Owner)
		}
		byOwner[m.Owner]++
	}
	for _, o := range owners {
		fmt.Fprintf(w, "%s: %s %s to the catalogue (public, The Big Songbook); the band links to them\n", o, verb("would move", "moved"), songs(byOwner[o]))
	}
	if len(r.ToCatalogue) == 0 {
		fmt.Fprintln(w, "catalogue bands: no song to move")
	}
	for _, m := range r.Merged {
		fmt.Fprintf(w, "%s — %s: the catalogue's song %d %s into song %d, the catalogue's from now on (%s)\n",
			m.Title, m.Artist, m.SongID, verb("would be merged", "merged"), m.TargetID, m.Moved)
	}
	for _, d := range r.Duplicates {
		fmt.Fprintf(w, "duplicate left for the de-duplication review: %s\n", d)
	}

	var rep store.RepointCounts
	for _, m := range r.Replaced {
		rep.Add(m.Moved)
	}
	if len(r.Replaced)+len(r.CommunityToCatalogue) > 0 {
		fmt.Fprintf(w, "catalogue communities: %d charts %s the catalogue's (new revisions; the community's copies go; %s), %d songs the catalogue lacked %s it\n",
			len(r.Replaced), verb("would replace", "replaced"), rep, len(r.CommunityToCatalogue), verb("would move into", "moved into"))
	} else {
		fmt.Fprintln(w, "catalogue communities: no chart to replace, no song to move")
	}

	for _, c := range r.CommunitySongs {
		fmt.Fprintf(w, "community %d (%s): %s, the community's, private\n", c.ID, c.Name, songs(c.N))
	}

	closed := []string{}
	for _, c := range r.ClosedSongbooks {
		closed = append(closed, fmt.Sprintf("%d (%s, %s)", c.ID, c.Name, songs(c.N)))
	}
	if len(closed) == 0 {
		closed = append(closed, "none")
	}
	fmt.Fprintf(w, "songbooks shared → public / closed: the ones not shared are closed (migration 0114): %s; public: %d\n",
		strings.Join(closed, ", "), r.PublicSongbooks)

	fmt.Fprintln(w, "setlist, jam: the values are gone (migration 0114)")
	fmt.Fprintln(w, "set list bodies → setlist_items: step 3 (not this command)")

	switch {
	case r.Nothing():
		fmt.Fprintln(w, "\nnothing to do: the data already follows the ownership model")
	case !r.Applied:
		fmt.Fprintln(w, "\ndry run: nothing changed (--apply to do it)")
	}
}

func songs(n int) string {
	if n == 1 {
		return "1 song"
	}
	return fmt.Sprintf("%d songs", n)
}
