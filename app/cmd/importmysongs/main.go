// importmysongs is a one-shot bulk importer for the legacy LaTeX
// "songs"-package tree under MySongs/ (one \beginsong per <slug>/<slug>.tex,
// see internal/latexsong) into the modern database: every song lands as a
// visibility='global' catalogue entry (publicly reusable, same as any other
// catalogue song), with a songbook_entries pointer added into --songbook-id
// so it also shows up in that songbook's listing.
//
//	go run ./cmd/importmysongs --db "$DATABASE_URL" --dir /path/to/MySongs --songbook-id 2 [--dry-run]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/internal/latexsong"
	"app/internal/song"
)

func main() {
	dbURL := flag.String("db", "", "database connection string (required unless --dry-run)")
	dir := flag.String("dir", "", "path to the MySongs/ tree (required)")
	songbookID := flag.Int64("songbook-id", 0, "songbook to add an entry to for each imported song (required unless --dry-run)")
	personID := flag.Int64("person-id", 0, "person id recorded as added_by_person_id (required unless --dry-run)")
	dryRun := flag.Bool("dry-run", false, "parse and print what would be imported, without touching the database")
	flag.Parse()

	if *dir == "" {
		log.Fatal("--dir is required")
	}
	if !*dryRun && (*dbURL == "" || *songbookID == 0 || *personID == 0) {
		log.Fatal("--db, --songbook-id and --person-id are required unless --dry-run")
	}

	entries, err := os.ReadDir(*dir)
	if err != nil {
		log.Fatalf("reading %s: %v", *dir, err)
	}

	type job struct {
		dir   string
		title string
		body  string
	}
	var jobs []job
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		texPath := filepath.Join(*dir, e.Name(), e.Name()+".tex")
		if _, err := os.Stat(texPath); err != nil {
			continue // not a <slug>/<slug>.tex song directory — tmp-ly/, etc.
		}
		parsed, err := latexsong.ParseFile(texPath)
		if err != nil {
			log.Fatalf("%s: %v", texPath, err)
		}
		for _, ps := range parsed {
			if ps.Song.Get("title") == "" {
				log.Fatalf("%s: parsed with no title — refusing to import a nameless song", texPath)
			}
			jobs = append(jobs, job{dir: e.Name(), title: ps.Song.Get("title"), body: song.Write(ps.Song)})
		}
	}

	fmt.Printf("found %d song(s) under %s\n", len(jobs), *dir)
	for _, j := range jobs {
		fmt.Printf("  %-40s %s\n", j.dir, j.title)
	}
	if *dryRun {
		return
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	for _, j := range jobs {
		parsed := song.Parse(j.body)
		title, artist := parsed.Get("title"), parsed.Get("artist")

		tx, err := pool.Begin(ctx)
		if err != nil {
			log.Fatal(err)
		}

		var workID int64
		if err := tx.QueryRow(ctx, `SELECT id FROM works WHERE title = $1 AND artist = $2`, title, artist).Scan(&workID); err != nil {
			if err := tx.QueryRow(ctx, `INSERT INTO works (title, artist) VALUES ($1, $2) RETURNING id`, title, artist).Scan(&workID); err != nil {
				log.Fatalf("%s: creating work: %v", j.dir, err)
			}
		}

		var variantID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO songs (title, artist, body, work_id, created_by_person_id)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			title, artist, j.body, workID, *personID).Scan(&variantID); err != nil {
			log.Fatalf("%s: inserting song: %v", j.dir, err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO songbook_entries (songbook_id, variant_id, added_by_person_id) VALUES ($1, $2, $3)`,
			*songbookID, variantID, *personID); err != nil {
			log.Fatalf("%s: adding songbook entry: %v", j.dir, err)
		}

		if err := tx.Commit(ctx); err != nil {
			log.Fatalf("%s: commit: %v", j.dir, err)
		}
		fmt.Printf("imported %-40s -> variant id %d\n", j.dir, variantID)
	}
}
