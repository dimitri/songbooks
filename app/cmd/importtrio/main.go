// importtrio is a one-shot setup script: creates a new band from the
// legacy LaTeX "songs"-package tree under Trio/ (see internal/latexsong),
// imports every song as that band's own repertoire (visibility='band',
// pointed into its default songbook), adds the given founder as a core
// member, and opens a group-invite roster link with pre-typed,
// instrument-tagged slots for the other named members to join by.
//
//	go run ./cmd/importtrio --db "$DATABASE_URL" --dir /path/to/Trio \
//	  --band-name "Couleur Acoustique" --music-type covers \
//	  --founder-id 1 \
//	  --slot "Philippe:bassist:core" --slot "Gaëlle:singer:core" \
//	  [--dry-run]
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/internal/latexsong"
	"app/internal/song"
)

// randomToken mirrors internal/store's own unexported randomToken (32
// random bytes, URL-safe base64, no padding) — duplicated here rather
// than exported across a package boundary for one one-shot script.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type slotSpec struct {
	name, instrument, status string
}

type slotFlags []slotSpec

func (s *slotFlags) String() string { return "" }
func (s *slotFlags) Set(v string) error {
	parts := strings.SplitN(v, ":", 3)
	if len(parts) < 2 {
		return fmt.Errorf("--slot must be Name:instrument[:status], got %q", v)
	}
	status := "core"
	if len(parts) == 3 && parts[2] != "" {
		status = parts[2]
	}
	*s = append(*s, slotSpec{name: parts[0], instrument: parts[1], status: status})
	return nil
}

func main() {
	dbURL := flag.String("db", "", "database connection string (required unless --dry-run)")
	dir := flag.String("dir", "", "path to the Trio/ tree (required)")
	bandName := flag.String("band-name", "", "name of the new band (required)")
	musicType := flag.String("music-type", "covers", "band music_type: covers or original")
	founderID := flag.Int64("founder-id", 0, "person id to add as the band's first core member (required unless --dry-run)")
	dryRun := flag.Bool("dry-run", false, "parse and print what would be done, without touching the database")
	var slots slotFlags
	flag.Var(&slots, "slot", "Name:instrument[:status] roster slot, repeatable")
	flag.Parse()

	if *dir == "" || *bandName == "" {
		log.Fatal("--dir and --band-name are required")
	}
	if !*dryRun && (*dbURL == "" || *founderID == 0) {
		log.Fatal("--db and --founder-id are required unless --dry-run")
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
			continue
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

	fmt.Printf("band: %q (music_type=%s)\n", *bandName, *musicType)
	fmt.Printf("found %d song(s) under %s\n", len(jobs), *dir)
	for _, j := range jobs {
		fmt.Printf("  %-30s %s\n", j.dir, j.title)
	}
	fmt.Printf("roster slots:\n")
	for _, sl := range slots {
		fmt.Printf("  %-15s %-12s %s\n", sl.name, sl.instrument, sl.status)
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

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(ctx)

	var bandID int64
	if err := tx.QueryRow(ctx, `INSERT INTO bands (name, music_type) VALUES ($1, $2) RETURNING id`,
		*bandName, *musicType).Scan(&bandID); err != nil {
		log.Fatalf("creating band: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO band_members (band_id, person_id, status) VALUES ($1, $2, 'core')`,
		bandID, *founderID); err != nil {
		log.Fatalf("adding founder: %v", err)
	}
	var songbookID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO songbooks (owner_kind, owner_band_id, name, is_default) VALUES ('band', $1, 'Songbook', true) RETURNING id`,
		bandID).Scan(&songbookID); err != nil {
		log.Fatalf("creating default songbook: %v", err)
	}
	fmt.Printf("created band id %d, default songbook id %d\n", bandID, songbookID)

	for _, j := range jobs {
		parsed := song.Parse(j.body)
		title, artist := parsed.Get("title"), parsed.Get("artist")

		var workID int64
		if err := tx.QueryRow(ctx, `SELECT id FROM works WHERE title = $1 AND artist = $2`, title, artist).Scan(&workID); err != nil {
			if err := tx.QueryRow(ctx, `INSERT INTO works (title, artist) VALUES ($1, $2) RETURNING id`, title, artist).Scan(&workID); err != nil {
				log.Fatalf("%s: creating work: %v", j.dir, err)
			}
		}

		var variantID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO songs (title, artist, body, work_id, visibility, owner_band_id, created_by_person_id)
			VALUES ($1, $2, $3, $4, 'band', $5, $6) RETURNING id`,
			title, artist, j.body, workID, bandID, *founderID).Scan(&variantID); err != nil {
			log.Fatalf("%s: inserting song: %v", j.dir, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO songbook_entries (songbook_id, variant_id, added_by_person_id) VALUES ($1, $2, $3)`,
			songbookID, variantID, *founderID); err != nil {
			log.Fatalf("%s: adding songbook entry: %v", j.dir, err)
		}
		fmt.Printf("imported %-30s -> variant id %d\n", j.dir, variantID)
	}

	var groupInviteID int64
	token, err := randomToken()
	if err != nil {
		log.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO group_invites (token, target_kind, band_id, created_by_person_id)
		VALUES ($1, 'band', $2, $3) RETURNING id`,
		token, bandID, *founderID).Scan(&groupInviteID); err != nil {
		log.Fatalf("creating group invite: %v", err)
	}
	for _, sl := range slots {
		status := sl.status
		if status != "core" && status != "guest" {
			status = "guest"
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO group_invite_slots (group_invite_id, display_name, instrument, status)
			VALUES ($1, $2, $3, $4)`, groupInviteID, sl.name, sl.instrument, status); err != nil {
			log.Fatalf("adding slot %q: %v", sl.name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("commit: %v", err)
	}
	fmt.Printf("group invite token: %s (share as /join/%s)\n", token, token)
}
