// bandsongs makes every song a band uses the band's own: each catalogue
// ('global') or personal ('user') song in a band's songbooks or set lists
// gets a copy for the band (store.AdoptIntoBand: content and media
// copied, forked_from_id the original), and the band's songbook entries,
// set list items and recordings point at the copy. The original stays as
// it is, with its history; each set list changed gets a revision. A band
// that already has its copy of a song (forked_from_id) gets its pointers
// moved to it, no second copy.
//
// Another band's song in a band's songbook or set list is left as it is
// and reported: a band never takes another band's song (an original
// band's songs stay theirs; covers come from the catalogue), so a person
// decides — take it out, or take the catalogue's version instead. A
// community's song is left too: its songbooks may be private or sold.
//
// It only reads unless --apply is given, and prints per band what it would
// change (with --apply, what it changed). It never prints a song's text.
// It doesn't migrate the database's schema (store.OpenCurrent): it runs
// against a database the app of the same version has already migrated.
//
//	bandsongs --db "$DATABASE_URL"                 # dry run, every band
//	bandsongs --db "$DATABASE_URL" --band 14 -v    # one band, each song
//	bandsongs --db "$DATABASE_URL" --apply         # do it
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"

	"app/internal/store"
)

type bandTotals struct {
	name                              string
	copies, reused, skipped           int
	entries, setlistItems, recordings int
	mediaBytes                        int64
	skippedByKind                     map[string]int
}

func main() {
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "Postgres connection string (or set DATABASE_URL)")
	apply := flag.Bool("apply", false, "make the changes (without it, only print what would change)")
	band := flag.Int64("band", 0, "only this band's songs")
	verbose := flag.Bool("v", false, "print each song (its id and what happens to it), not only the totals")
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

	plan, err := st.PlanBandAdoptions(ctx)
	if err != nil {
		log.Fatal(err)
	}

	totals := map[int64]*bandTotals{}
	var order []int64
	for _, a := range plan {
		if *band != 0 && a.BandID != *band {
			continue
		}
		bt := totals[a.BandID]
		if bt == nil {
			bt = &bandTotals{name: a.BandName, skippedByKind: map[string]int{}}
			totals[a.BandID] = bt
			order = append(order, a.BandID)
		}
		if a.Action == "skip" {
			bt.skipped++
			bt.skippedByKind[a.Visibility]++
			// Always reported, -v or not: a person decides about each.
			why := "a community's song"
			if a.Visibility == "band" {
				why = "another band's song: take it out, or the catalogue's version instead"
			}
			fmt.Printf("band %d: song %d left as it is (%s; %d songbook entries, %d set list items)\n",
				a.BandID, a.VariantID, why, a.Entries, a.SetlistItems)
			continue
		}
		bt.entries += a.Entries
		bt.setlistItems += a.SetlistItems
		bt.recordings += a.Recordings
		if a.Action == "copy" {
			bt.mediaBytes += a.MediaBytes
		}
		if *apply {
			newID, created, err := st.AdoptIntoBand(ctx, a.BandID, a.VariantID, 0)
			if err != nil {
				log.Fatalf("band %d, song %d: %v", a.BandID, a.VariantID, err)
			}
			if created {
				bt.copies++
			} else {
				bt.reused++
			}
			if *verbose {
				fmt.Printf("band %d: song %d (%s) -> band's song %d (%d songbook entries, %d set list items, %d recordings)\n",
					a.BandID, a.VariantID, a.Visibility, newID, a.Entries, a.SetlistItems, a.Recordings)
			}
			continue
		}
		if a.Action == "copy" {
			bt.copies++
		} else {
			bt.reused++
		}
		if *verbose {
			what := "a copy for the band"
			if a.Action == "reuse" {
				what = fmt.Sprintf("the band's copy %d", a.BandSongID)
			}
			fmt.Printf("band %d: song %d (%s) -> %s (%d songbook entries, %d set list items, %d recordings)\n",
				a.BandID, a.VariantID, a.Visibility, what, a.Entries, a.SetlistItems, a.Recordings)
		}
	}

	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	verb := "would"
	if *apply {
		verb = "did"
	}
	if len(order) == 0 {
		fmt.Println("every band's songbooks and set lists hold only the band's own songs: nothing to do")
		return
	}
	var all bandTotals
	for _, id := range order {
		bt := totals[id]
		fmt.Printf("band %d (%s): %s make %d copies (%s of media copied), reuse %d, repoint %d songbook entries, %d set list items, %d recordings; left %d",
			id, bt.name, verb, bt.copies, mib(bt.mediaBytes), bt.reused, bt.entries, bt.setlistItems, bt.recordings, bt.skipped)
		if bt.skipped > 0 {
			fmt.Printf(" (%v)", bt.skippedByKind)
		}
		fmt.Println()
		all.copies += bt.copies
		all.reused += bt.reused
		all.entries += bt.entries
		all.setlistItems += bt.setlistItems
		all.recordings += bt.recordings
		all.skipped += bt.skipped
		all.mediaBytes += bt.mediaBytes
	}
	fmt.Printf("all bands: %s make %d copies (%s of media copied), reuse %d, repoint %d songbook entries, %d set list items, %d recordings; left %d\n",
		verb, all.copies, mib(all.mediaBytes), all.reused, all.entries, all.setlistItems, all.recordings, all.skipped)
	if !*apply {
		fmt.Println("dry run: nothing changed (--apply to do it)")
	}
}

func mib(n int64) string { return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20)) }
