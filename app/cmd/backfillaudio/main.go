// backfillaudio downloads reference audio (via yt-dlp — see
// app/docs/mp3-reference-audio.md §1) for every song that already has a
// youtube.com/youtu.be reference: URL but no song_audio row yet. Idempotent:
// a song that already has audio is skipped, so this is safe to re-run (e.g.
// after adding more reference: links).
//
// Must run where yt-dlp is installed — inside the deploy container, not a
// plain dev machine:
//
//	docker compose exec app backfillaudio --db "$DATABASE_URL"
//
// (the Dockerfile builds this alongside the main `songbooks` binary).
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/internal/ytaudio"
)

var youtubeRE = regexp.MustCompile(`(?m)^reference:\s*(\S*(?:youtube\.com|youtu\.be)\S*)\s*$`)

func main() {
	dbURL := flag.String("db", "", "database connection string (required)")
	dryRun := flag.Bool("dry-run", false, "list what would be downloaded, without downloading or writing anything")
	flag.Parse()
	if *dbURL == "" {
		log.Fatal("--db is required")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	rows, err := pool.Query(ctx, `
		SELECT s.id, p.slug, s.stem, s.body
		FROM songs s
		JOIN projects p ON p.id = s.project_id
		WHERE NOT EXISTS (SELECT 1 FROM song_audio sa WHERE sa.variant_id = s.id AND sa.category = 'youtube')
		ORDER BY s.id`)
	if err != nil {
		log.Fatal(err)
	}
	type candidate struct {
		id         int64
		book, stem string
		url        string
	}
	var candidates []candidate
	for rows.Next() {
		var id int64
		var book, stem, body string
		if err := rows.Scan(&id, &book, &stem, &body); err != nil {
			log.Fatal(err)
		}
		m := youtubeRE.FindStringSubmatch(body)
		if m == nil {
			continue
		}
		candidates = append(candidates, candidate{id: id, book: book, stem: stem, url: m[1]})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%d song(s) with a YouTube reference and no audio yet\n", len(candidates))
	for _, c := range candidates {
		fmt.Printf("  %s/%s -> %s\n", c.book, c.stem, c.url)
	}
	if *dryRun {
		fmt.Println("--dry-run: not downloading anything")
		return
	}

	ok, failed := 0, 0
	for _, c := range candidates {
		fmt.Printf("downloading %s/%s ... ", c.book, c.stem)
		start := time.Now()
		data, err := ytaudio.Download(ctx, c.url)
		if err != nil {
			fmt.Printf("FAILED: %v\n", err)
			failed++
			continue
		}
		sum := sha256.Sum256(data)
		if _, err := pool.Exec(ctx, `
			INSERT INTO song_audio (variant_id, category, content_type, byte_size, sha256, source_url, data)
			VALUES ($1, 'youtube', 'audio/mpeg', $2, $3, $4, $5)
			ON CONFLICT (variant_id, category) DO NOTHING`,
			c.id, len(data), hex.EncodeToString(sum[:]), c.url, data); err != nil {
			fmt.Printf("FAILED to store: %v\n", err)
			failed++
			continue
		}
		fmt.Printf("ok (%d bytes, %s)\n", len(data), time.Since(start).Round(time.Second))
		ok++
	}
	fmt.Printf("done: %d ok, %d failed\n", ok, failed)
}
