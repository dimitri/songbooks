// reflabels fills the empty reference labels of The Big Songbook's songs
// from their reference recordings' titles (docs/ownership-model.md §3,
// §11): the label (version_name: the album, "Live at …", "Cover"…) is
// the video's title cleaned by reftitle.Label — the upload's wrapping
// ("(Official Video)", "[HD]", "Lyrics"…) and what repeats the song's
// title or artist taken out —, left empty when nothing is left. A label
// someone typed is never touched: only empty ones, for the reference the
// title was found for.
//
// The title is the one kept for the song's reference (songs.
// reference_title, fetched when the song page was first shown) when it
// is that link's; else it is fetched now from the link's oEmbed endpoint
// (YouTube's, no key) or page — unless --no-fetch. With --apply the
// titles fetched are kept too.
//
// The dry run (the default) changes nothing and prints what --apply
// would do: counts and examples, ids and titles only. A second --apply
// finds nothing more to do (or only the songs whose title wasn't found).
// No revision and no new version: an empty label filled loses nothing.
//
//	reflabels --db "$DATABASE_URL"               # dry run
//	reflabels --db "$DATABASE_URL" -v            # dry run, every song
//	reflabels --db "$DATABASE_URL" --no-fetch    # only the titles kept
//	reflabels --db "$DATABASE_URL" --apply       # do it
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"app/internal/reftitle"
	"app/internal/store"
)

func main() {
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "Postgres connection string (or set DATABASE_URL)")
	apply := flag.Bool("apply", false, "set the labels (without it, only print what would be set)")
	noFetch := flag.Bool("no-fetch", false, "use only the titles already kept; never fetch one")
	verbose := flag.Bool("v", false, "print every song, not only examples")
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
	opts := options{Apply: *apply, Verbose: *verbose}
	if !*noFetch {
		opts.Fetcher = reftitle.New()
	}
	if err := run(ctx, st, os.Stdout, opts); err != nil {
		log.Fatal(err)
	}
}

type options struct {
	Apply, Verbose bool
	// Fetcher fetches the titles not kept; nil fetches none.
	Fetcher *reftitle.Fetcher
}

// line is one song's outcome.
type line struct {
	id                 int64
	video, label, from string
}

// examples is how many songs of each outcome are printed without -v.
const examples = 12

func run(ctx context.Context, st *store.Store, w io.Writer, o options) error {
	counts, err := st.CountReferenceLabels(ctx)
	if err != nil {
		return err
	}
	cands, err := st.ReferenceLabelCandidates(ctx)
	if err != nil {
		return err
	}
	var set, empty, notFound []line
	stored, fetched := 0, 0
	for _, c := range cands {
		title, from := c.Stored.For(c.ReferenceURL), "kept"
		if title == "" && o.Fetcher != nil {
			fctx, cancel := context.WithTimeout(ctx, reftitle.Timeout+time.Second)
			t, ferr := o.Fetcher.Title(fctx, c.ReferenceURL)
			cancel()
			title, from = strings.TrimSpace(t), "fetched"
			if o.Apply {
				// Kept, found or not, as the song page keeps it.
				if err := st.SetReferenceTitle(ctx, c.ID, c.ReferenceURL, title); err != nil {
					return err
				}
			}
			if ferr != nil && title == "" {
				notFound = append(notFound, line{id: c.ID, from: "not found"})
				continue
			}
		}
		if title == "" {
			notFound = append(notFound, line{id: c.ID, from: "no title kept"})
			continue
		}
		if from == "kept" {
			stored++
		} else {
			fetched++
		}
		l := line{id: c.ID, video: title, label: reftitle.Label(title, c.Title, c.Artist), from: from}
		if l.label == "" {
			empty = append(empty, l)
			continue
		}
		if o.Apply {
			ok, err := st.FillReferenceLabel(ctx, c.ID, c.ReferenceURL, l.label)
			if err != nil {
				return err
			}
			if !ok { // typed or changed meanwhile: left as it is
				continue
			}
		}
		set = append(set, l)
	}

	verb := func(would, did string) string {
		if o.Apply {
			return did
		}
		return would
	}
	fmt.Fprintf(w, "The Big Songbook: %d songs; %d with a label already (left as they are), %d without a reference, %d with a reference and no label.\n",
		counts.Songs, counts.Labelled, counts.NoReference, len(cands))
	fetch := "not fetched (--no-fetch)"
	if o.Fetcher != nil {
		fetch = fmt.Sprintf("%d fetched now (oEmbed or the page)", fetched)
	}
	fmt.Fprintf(w, "Titles: %d kept for their reference, %s, %d not found.\n", stored, fetch, len(notFound))
	fmt.Fprintf(w, "Labels: %s %d; %d left empty (the title only repeats the song and its wrapping).\n",
		verb("would set", "set"), len(set), len(empty))

	show := func(head string, ls []line, f func(line) string) {
		if len(ls) == 0 {
			return
		}
		fmt.Fprintf(w, "\n%s\n", head)
		for i, l := range ls {
			if i == examples && !o.Verbose {
				fmt.Fprintf(w, "  … and %d more (-v lists them)\n", len(ls)-examples)
				break
			}
			fmt.Fprintf(w, "  #%d %s\n", l.id, f(l))
		}
	}
	show(verb("Would set (song, video title → label):", "Set (song, video title → label):"), set, func(l line) string {
		return fmt.Sprintf("%q → %q", l.video, l.label)
	})
	show("Left empty (song, video title):", empty, func(l line) string { return fmt.Sprintf("%q", l.video) })
	show("No title (song):", notFound, func(l line) string { return l.from })
	return nil
}
