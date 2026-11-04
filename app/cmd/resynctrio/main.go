// resynctrio re-parses the Trio/ tree and overwrites the body of each
// already-imported song (matched by title, band_id) with freshly parsed
// content — a one-shot repair tool for when internal/latexsong gets a
// bugfix after an import already ran (see Couleur Acoustique's
// repertoire, imported before the \chordsoff-stray-markup fix).
//
//	go run ./cmd/resynctrio --db "$DATABASE_URL" --dir /path/to/Trio --band-id 17 [--dry-run]
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
	dir := flag.String("dir", "", "path to the Trio/ tree (required)")
	bandID := flag.Int64("band-id", 0, "band whose repertoire to resync (required unless --dry-run)")
	dryRun := flag.Bool("dry-run", false, "print what would change, without touching the database")
	flag.Parse()

	if *dir == "" {
		log.Fatal("--dir is required")
	}
	if !*dryRun && (*dbURL == "" || *bandID == 0) {
		log.Fatal("--db and --band-id are required unless --dry-run")
	}

	entries, err := os.ReadDir(*dir)
	if err != nil {
		log.Fatalf("reading %s: %v", *dir, err)
	}

	type job struct {
		dir, title, body string
	}
	var jobs []job
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		texPath := filepath.Join(*dir, e.Name(), e.Name()+".tex")
		if _, err := os.Stat(texPath); err != nil {
			continue
		}
		parsed, err := latexsong.ParseFile(texPath)
		if err != nil {
			log.Fatalf("%s: %v", texPath, err)
		}
		for _, ps := range parsed {
			jobs = append(jobs, job{dir: e.Name(), title: ps.Song.Get("title"), body: song.Write(ps.Song)})
		}
	}
	fmt.Printf("found %d song(s) under %s\n", len(jobs), *dir)
	if *dryRun {
		for _, j := range jobs {
			fmt.Printf("  %-30s %s\n", j.dir, j.title)
		}
		return
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	for _, j := range jobs {
		tag, err := pool.Exec(ctx, `UPDATE songs SET body = $1 WHERE owner_band_id = $2 AND title = $3 AND visibility = 'band'`,
			j.body, *bandID, j.title)
		if err != nil {
			log.Fatalf("%s: %v", j.dir, err)
		}
		if tag.RowsAffected() != 1 {
			log.Fatalf("%s (title %q): expected to update exactly 1 row, updated %d", j.dir, j.title, tag.RowsAffected())
		}
		fmt.Printf("resynced %-30s %s\n", j.dir, j.title)
	}
}
