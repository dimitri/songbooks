// migratescores is a one-off tool: ports every song_scores row (the
// Postgres-backed "score:" mechanism migration 0057_retire_song_scores.sql
// drops outright) into its owning section's own "score[label]: <body>"
// line, inline in the song's body text — the storage the merged
// drummer-view branch replaced it with (see internal/tab.LoadScores,
// internal/song.ScoreVoice).
//
// Deliberately does NOT import app/internal/store: store.New runs
// pending migrations on connect (see internal/store/migrate.go), and
// 0057_retire_song_scores.sql — already embedded in this same binary's
// source tree — would DROP TABLE song_scores before this tool got a
// chance to read it. This tool talks to Postgres directly via pgx,
// nothing else, so it's safe to run against a database that hasn't had
// that migration applied yet (the whole point of running it).
//
// Usage:
//
//	migratescores --db "$DATABASE_URL" [--apply]
//
// Without --apply, only prints what it would do (dry run) — no writes.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"app/internal/song"

	"github.com/jackc/pgx/v5/pgxpool"
)

type scoreRow struct {
	variantID    int64
	sectionIndex int
	lySource     string
}

func labelFor(lySource string) string {
	var probe struct {
		Tuning []int
	}
	if json.Unmarshal([]byte(lySource), &probe) == nil && len(probe.Tuning) > 0 && len(probe.Tuning) <= 4 {
		return "Bass"
	}
	return "Guitar"
}

func main() {
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "Postgres connection string")
	apply := flag.Bool("apply", false, "actually write; without this flag, only prints what would happen")
	flag.Parse()

	if *dbURL == "" {
		log.Fatal("usage: migratescores --db <url> [--apply]")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		log.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	// Fails loudly (rather than silently proceeding against the wrong
	// schema state) if song_scores is already gone — this tool only
	// makes sense BEFORE 0057_retire_song_scores.sql has run.
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'song_scores')`).Scan(&exists); err != nil {
		log.Fatalf("checking song_scores existence: %v", err)
	}
	if !exists {
		log.Fatal("song_scores doesn't exist — already migrated/dropped, or wrong database. Nothing to do.")
	}

	rows, err := pool.Query(ctx, `SELECT variant_id, section_index, ly_source FROM song_scores ORDER BY variant_id, section_index`)
	if err != nil {
		log.Fatalf("querying song_scores: %v", err)
	}
	byVariant := map[int64][]scoreRow{}
	var order []int64
	for rows.Next() {
		var r scoreRow
		if err := rows.Scan(&r.variantID, &r.sectionIndex, &r.lySource); err != nil {
			log.Fatalf("scanning song_scores row: %v", err)
		}
		if _, ok := byVariant[r.variantID]; !ok {
			order = append(order, r.variantID)
		}
		byVariant[r.variantID] = append(byVariant[r.variantID], r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Fatalf("iterating song_scores: %v", err)
	}

	fmt.Printf("%d variant(s), %d score row(s) total.\n", len(order), func() int {
		n := 0
		for _, v := range byVariant {
			n += len(v)
		}
		return n
	}())

	var ok, failed int
	for _, variantID := range order {
		scores := byVariant[variantID]
		var title, body string
		var version int
		if err := pool.QueryRow(ctx, `SELECT title, body, version FROM songs WHERE id = $1`, variantID).Scan(&title, &body, &version); err != nil {
			fmt.Printf("variant %d: ERROR loading: %v\n", variantID, err)
			failed++
			continue
		}
		sg := song.Parse(body)
		changed := false
		for _, sc := range scores {
			if sc.sectionIndex < 0 || sc.sectionIndex >= len(sg.Sections) {
				fmt.Printf("variant %d (%s): section index %d out of range (%d sections) — SKIPPED\n", variantID, title, sc.sectionIndex, len(sg.Sections))
				failed++
				continue
			}
			label := labelFor(sc.lySource)
			sg.Sections[sc.sectionIndex].Scores = append(sg.Sections[sc.sectionIndex].Scores, song.ScoreVoice{Label: label, Body: sc.lySource})
			changed = true
			fmt.Printf("variant %d (%s): section %d (%q) <- score[%s] (%d bytes)\n", variantID, title, sc.sectionIndex, sg.Sections[sc.sectionIndex].Label, label, len(sc.lySource))
		}
		if !changed {
			continue
		}
		newBody := song.Write(sg)
		if !*apply {
			ok++
			continue
		}
		tag, err := pool.Exec(ctx, `UPDATE songs SET body = $1, version = version + 1 WHERE id = $2 AND version = $3`, newBody, variantID, version)
		if err != nil {
			fmt.Printf("variant %d: ERROR writing: %v\n", variantID, err)
			failed++
			continue
		}
		if tag.RowsAffected() == 0 {
			fmt.Printf("variant %d: version changed concurrently (expected %d) — SKIPPED, rerun\n", variantID, version)
			failed++
			continue
		}
		ok++
	}

	fmt.Printf("\n%d variant(s) %s, %d failed/skipped.\n", ok, map[bool]string{true: "updated", false: "would be updated"}[*apply], failed)
	if !*apply {
		fmt.Println("(dry run — pass --apply to actually write)")
	}
}
