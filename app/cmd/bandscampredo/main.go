// bandscampredo is a one-shot batch redo of Bands Camp's song catalog,
// per the user's explicit request after the real community-creation
// tools turned out to have been silently broken (migration 0042
// dropped school_catalog_entries; CreateSchoolVariant/ForkVariant's
// school case kept writing to it — see the commit that fixed this).
// Bands Camp's songbook_entries were cleared, leaving the underlying
// songs rows (visibility='global') in place but unlinked.
//
// Originally this fetched fresh content from Ultimate Guitar/iReal,
// but the user pointed at a much better source: v2/BandsCamp/ already
// holds the real, hand-curated .song files (and order.txt's canonical
// list) from the pre-web-app Typst pipeline — complete lyrics, real
// corrections ("Thanks to Omar..."), proper key/tempo/map metadata.
// 66 files, 66 order.txt lines, 66 of the original songbook_entries —
// this is unambiguously the authoritative source, not a guess.
//
// For each song in order.txt: resolve its work (title/artist, same
// normalization every other creation path uses), reuse the existing
// global-visibility variant for that work if one exists (overwriting
// its body — same id, so anything already pointing at it stays
// valid) or create a fresh one, then link it into the target
// songbook in order.txt's order.
//
// Run via a one-off container joined to the compose network, with the
// legacy directory mounted read-only (not baked into the image — it's
// reference data, not app code):
//
//	docker compose run --rm -v ~/dev/www/songbooks/v2/BandsCamp:/legacy:ro app \
//	  bandscampredo --db "$DATABASE_URL" --legacy-dir /legacy \
//	  --school-id 3 --songbook-id 6 --actor-id 1
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"app/internal/song"
	"app/internal/store"
)

func main() {
	dbURL := flag.String("db", "", "database connection string (required)")
	legacyDir := flag.String("legacy-dir", "", "path to the legacy book directory (contains order.txt and songs/*.song) (required)")
	songbookID := flag.Int64("songbook-id", 0, "target songbook id to link into (required)")
	actorID := flag.Int64("actor-id", 0, "person id to record as the actor for creates/saves/links (required)")
	dryRun := flag.Bool("dry-run", false, "list what would happen, without writing anything")
	flag.Parse()
	if *dbURL == "" || *legacyDir == "" || *songbookID == 0 || *actorID == 0 {
		log.Fatal("--db, --legacy-dir, --songbook-id, and --actor-id are all required")
	}

	orderPath := filepath.Join(*legacyDir, "order.txt")
	orderBytes, err := os.ReadFile(orderPath)
	if err != nil {
		log.Fatalf("reading %s: %v", orderPath, err)
	}
	var slugs []string
	for _, line := range strings.Split(string(orderBytes), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			slugs = append(slugs, line)
		}
	}
	fmt.Printf("=== %d songs in order.txt ===\n", len(slugs))

	ctx := context.Background()
	st, err := store.New(ctx, *dbURL)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}

	var created, reused, failed int
	for i, slug := range slugs {
		path := filepath.Join(*legacyDir, "songs", slug+".song")
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("[%d/%d] %s: READ FAILED: %v\n", i+1, len(slugs), slug, err)
			failed++
			continue
		}
		body := string(raw)
		s := song.Parse(body)
		title, artist := s.Get("title"), s.Get("artist")
		if title == "" {
			fmt.Printf("[%d/%d] %s: no title: header, skipping\n", i+1, len(slugs), slug)
			failed++
			continue
		}
		fmt.Printf("[%d/%d] %q by %q (%s)\n", i+1, len(slugs), title, artist, slug)

		if *dryRun {
			continue
		}

		workID, err := st.FindOrCreateWork(ctx, title, artist)
		if err != nil {
			fmt.Printf("  WORK FAILED: %v\n", err)
			failed++
			continue
		}
		variantID, found, err := st.FindGlobalVariantByWorkID(ctx, workID)
		if err != nil {
			fmt.Printf("  LOOKUP FAILED: %v\n", err)
			failed++
			continue
		}
		if !found {
			variantID, err = st.CreateVariant(ctx, store.OwnerCatalog, 0, title, artist, *actorID)
			if err != nil {
				fmt.Printf("  CREATE FAILED: %v\n", err)
				failed++
				continue
			}
			created++
			fmt.Printf("  created variant %d\n", variantID)
		} else {
			reused++
			fmt.Printf("  reusing existing variant %d\n", variantID)
		}

		if _, err := st.SaveVariantByID(ctx, variantID, title, artist, "", body, "", *actorID); err != nil {
			fmt.Printf("  SAVE FAILED: %v\n", err)
			failed++
			continue
		}
		if err := st.AddToSongbook(ctx, *songbookID, variantID, *actorID); err != nil {
			fmt.Printf("  LINK FAILED: %v\n", err)
			failed++
			continue
		}
		fmt.Printf("  OK\n")
	}

	fmt.Printf("\n=== done: %d created, %d reused, %d failed, %d total ===\n", created, reused, failed, len(slugs))
}
