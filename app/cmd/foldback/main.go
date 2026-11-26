// foldback lists the arrangements of The Big Songbook's songs that are
// only fixes, and folds back the ones identical to their original (the
// edit model, docs/catalogue-edit-review.md §4.7; store/foldback.go).
//
// Each arrangement of a catalogue song (a band's, a person's, a
// community's) is classed by what it changed itself since it was made:
// identical (nothing, or nothing the original hasn't now: an accidental
// copy), fix (a few chords or words, the same sections and bars, no
// name), or arrangement (left alone). An arrangement with recordings or a
// Guitar Pro file of its own is never folded.
//
// The dry run (the default) changes nothing: it prints, by owner, each
// arrangement with its kind, ids and titles, and what a fold would move
// (it does each fold and rolls it back). --apply folds back the identical
// ones: their songbook entries (keys kept), set list items, notes and
// practice move to the original, the arrangement goes — its last state
// kept as a revision under its id, listed on the original's history
// (song_merges, reason 'foldback'), its old link opening the original.
// The fixes are never folded by the tool: their owners get the offer in
// the app ("Send the fix and use the shared chart" / "It's our
// arrangement — keep it"), the fix weighed like any edit.
//
//	foldback --db "$DATABASE_URL"            # dry run
//	foldback --db "$DATABASE_URL" --apply    # fold back the identical ones
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"app/internal/store"
)

func main() {
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "Postgres connection string (or set DATABASE_URL)")
	apply := flag.Bool("apply", false, "fold back the identical arrangements (without it, only print what would be done)")
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
	if err := run(ctx, st, os.Stdout, *apply); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, st *store.Store, w io.Writer, apply bool) error {
	cs, err := st.FoldCandidates(ctx, 0)
	if err != nil {
		return err
	}
	count := map[string]int{}
	folded, owner := 0, ""
	mode := "dry run"
	if apply {
		mode = "apply"
	}
	fmt.Fprintf(w, "foldback (%s): %d arrangements of The Big Songbook's songs\n", mode, len(cs))
	for _, c := range cs {
		count[c.Kind]++
		if o := c.OwnerKind + " " + c.OwnerName; o != owner {
			owner = o
			fmt.Fprintf(w, "\n%s %q (%d)\n", c.OwnerKind, c.OwnerName, c.OwnerID)
		}
		name := c.Title
		if c.VersionName != "" {
			name += " — " + c.VersionName
		}
		note := ""
		switch {
		case c.OwnMedia:
			note = "; has recordings or a Guitar Pro file of its own: kept"
		case c.Kept:
			note = "; kept by its owner"
		case c.Kind == store.FoldFix:
			note = fmt.Sprintf("; %d change(s): offered in the app, not folded here", len(c.Changes))
		case c.Kind == store.FoldArrangement:
			note = fmt.Sprintf("; %d change(s): a real arrangement, left alone", len(c.Changes))
		}
		fmt.Fprintf(w, "  %-11s song %d %q → original %d %q%s\n", c.Kind, c.ForkID, name, c.OriginalID, c.OriginalTitle, note)
		if c.Kind != store.FoldIdentical || c.OwnMedia || c.Kept {
			continue
		}
		r, err := st.FoldBack(ctx, 0, c.ForkID, !apply, store.FoldIdentical)
		if err != nil {
			fmt.Fprintf(w, "              not folded: %v\n", err)
			continue
		}
		verb := "would move"
		if apply {
			verb = "moved"
			folded++
		}
		fmt.Fprintf(w, "              %s %d songbook entries, %d set list items, %d recordings, %d other rows\n",
			verb, r.Moved.Entries, r.Moved.SetlistItems, r.Moved.Recordings, r.Moved.Other)
	}
	fmt.Fprintf(w, "\nidentical %d, fix %d, arrangement %d", count[store.FoldIdentical], count[store.FoldFix], count[store.FoldArrangement])
	if apply {
		fmt.Fprintf(w, "; folded back %d\n", folded)
	} else {
		fmt.Fprintln(w, "; nothing changed (--apply folds back the identical ones)")
	}
	return nil
}
