// Idempotent production backfill: links the Mercy and Other Lies bands to
// their matching songbook projects by NAME (not by hardcoded id, since
// production's row ids don't match dev's), and creates their
// titlepage-derived setlists only if a setlist with that exact title
// doesn't already exist for that band.
//
// Build and run from the app module root:
//
//	go run ./cmd/prodbackfill --db "$DATABASE_URL" --actor tapoueh
//
// --actor is the username to attribute the setlist-creation revision to
// (defaults to "tapoueh" — pass the real production admin username if
// different). Safe to run more than once: both the band->project link
// and the setlist creation check "is this already done?" first.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/internal/setlist"
	"app/internal/store"
)

type bandLink struct {
	bandName    string
	projectName string
}

type bandSetlist struct {
	bandName string
	title    string
	notes    string
	sets     []setlist.Set
}

func main() {
	dbURL := flag.String("db", "", "database connection string (required)")
	actorUsername := flag.String("actor", "tapoueh", "username to attribute the setlist-creation revision to")
	dryRun := flag.Bool("dry-run", false, "print what would change, without writing anything")
	flag.Parse()
	if *dbURL == "" {
		log.Fatal("--db is required")
	}

	ctx := context.Background()
	st, err := store.New(ctx, *dbURL) // also applies any pending migrations, including 0032
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	// A second, direct pool for the few plain by-name lookups/updates
	// below (band id/project_id by name) that have no store-layer
	// method of their own — avoids adding new store API surface just
	// for this one-time backfill.
	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	actor, err := st.GetPersonByUsername(ctx, *actorUsername)
	if err != nil {
		log.Fatalf("looking up actor %q: %v", *actorUsername, err)
	}

	links := []bandLink{
		{bandName: "Mercy", projectName: "Mercy"},
		{bandName: "Other Lies", projectName: "OtherLies"},
	}
	for _, l := range links {
		linkBandToProject(ctx, pool, l, *dryRun)
	}

	setlists := []bandSetlist{
		{
			bandName: "Mercy",
			title:    "Mercy — full set (from the legacy titlepage)",
			notes:    "imported from the legacy LaTeX titlepage",
			sets: []setlist.Set{
				{Label: "Set 1", Entries: []setlist.Entry{
					{Book: "Mercy", Stem: "mercy"},
					{Book: "Mercy", Stem: "id-rather-go-blind"},
					{Book: "Mercy", Stem: "the-thrill-is-gone"},
					{Book: "Mercy", Stem: "ill-play-the-blues-for-you"},
					{Book: "Mercy", Stem: "falling-in-love-again"},
					{Book: "Mercy", Stem: "colors"},
					{Book: "Mercy", Stem: "wake-up-alone"},
					{Book: "Mercy", Stem: "its-too-late"},
					{Book: "Mercy", Stem: "worried-life-blues"},
					{Book: "Mercy", Stem: "the-sky-is-crying"},
					{Book: "Mercy", Stem: "whos-been-talking"},
				}},
				{Label: "Set 2", Entries: []setlist.Entry{
					{Book: "Mercy", Stem: "back-to-black"},
					{Book: "Mercy", Stem: "you-were-mine"},
					{Book: "Mercy", Stem: "shine-a-light"},
					{Book: "Mercy", Stem: "too-much-of-a-woman"},
					{Book: "Mercy", Stem: "travelin-blues"},
					{Book: "Mercy", Stem: "you-know-im-no-good"},
					{Book: "Mercy", Stem: "kill-or-be-kind"},
					{Book: "Mercy", Stem: "soulful-dress"},
					{Book: "Mercy", Stem: "valerie"},
					{Book: "Mercy", Stem: "the-best-youve-had"},
					{Book: "Mercy", Stem: "whisper-in-a-hurricane"},
					{Book: "Mercy", Stem: "black-magic-woman"},
					{Book: "Mercy", Stem: "aint-no-love"},
				}},
				{Label: "Encore", Entries: []setlist.Entry{
					{Book: "Mercy", Stem: "my-yard"},
					{Book: "Mercy", Stem: "give-me-one-reason"},
					{Book: "Mercy", Stem: "gone"},
				}},
			},
		},
		{
			bandName: "Other Lies",
			title:    "Set List 2025 (from the legacy titlepage)",
			notes:    "imported from the legacy LaTeX titlepage",
			sets: []setlist.Set{
				{Label: "Set 1", Entries: []setlist.Entry{
					{Book: "OtherLies", Stem: "Belle-of-the-Ball"},
					{Book: "OtherLies", Stem: "In-Candesenza"},
					{Book: "OtherLies", Stem: "The-Whirlwind-of-Lovers"},
					{Book: "OtherLies", Stem: "Stars-On-Black"},
					{Book: "OtherLies", Stem: "My-Favourite-Scarlet"},
					{Book: "OtherLies", Stem: "Mottainai"},
					{Book: "OtherLies", Stem: "Bones"},
					{Book: "OtherLies", Stem: "GinLane"},
					{Book: "OtherLies", Stem: "Kawasaki"},
					{Book: "OtherLies", Stem: "The-Obsession-With-Control"},
					{Book: "OtherLies", Stem: "Vanity-Rewards"},
					{Book: "OtherLies", Stem: "Vibrator"},
					{Book: "OtherLies", Stem: "Voiles"},
					{Book: "OtherLies", Stem: "Of-Human-Bondage"},
					{Book: "OtherLies", Stem: "Teens4Today"},
					{Book: "OtherLies", Stem: "The-Last-Embrace"},
				}},
			},
		},
	}
	setlists = append(setlists, bandSetlist{
		bandName: "Other Lies",
		title:    "Bastille (Live) — 2 Oct 2026",
		notes:    "live album, apple music: music.apple.com/fr/album/bastille-live/6804827698 — \"Intro (Live)\" track skipped, not a catalog song",
		sets: []setlist.Set{
			{Label: "Set 1", Entries: []setlist.Entry{
				{Book: "OtherLies", Stem: "Belle-of-the-Ball"},
				{Book: "OtherLies", Stem: "In-Candesenza"},
				{Book: "OtherLies", Stem: "Bones"},
				{Book: "OtherLies", Stem: "GinLane"},
				{Book: "OtherLies", Stem: "Mottainai"},
				{Book: "OtherLies", Stem: "Kawasaki"},
				{Book: "OtherLies", Stem: "Dead-End-For-Delhia"},
				{Book: "OtherLies", Stem: "Vanity-Rewards"},
				{Book: "OtherLies", Stem: "Teens4Today"},
			}},
		},
	})

	for _, s := range setlists {
		createBandSetlistIfMissing(ctx, st, pool, s, actor.ID, *dryRun)
	}

	fmt.Println("done")
}

func linkBandToProject(ctx context.Context, pool *pgxpool.Pool, l bandLink, dryRun bool) {
	var bandID int64
	var projectID *int64
	if err := pool.QueryRow(ctx, `SELECT id, project_id FROM bands WHERE name = $1`, l.bandName).Scan(&bandID, &projectID); err != nil {
		log.Fatalf("looking up band %q: %v", l.bandName, err)
	}
	if projectID != nil {
		fmt.Printf("band %q already linked to project id %d — skipping\n", l.bandName, *projectID)
		return
	}
	var newProjectID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM projects WHERE name = $1`, l.projectName).Scan(&newProjectID); err != nil {
		log.Fatalf("looking up project %q: %v", l.projectName, err)
	}
	if dryRun {
		fmt.Printf("[dry-run] would link band %q (id %d) -> project %q (id %d)\n", l.bandName, bandID, l.projectName, newProjectID)
		return
	}
	// Idempotent: the WHERE project_id IS NULL guard means a re-run is
	// a no-op (and the early "already linked" check above makes it an
	// explicit, logged no-op rather than a silent one).
	if _, err := pool.Exec(ctx, `UPDATE bands SET project_id = $1 WHERE id = $2 AND project_id IS NULL`, newProjectID, bandID); err != nil {
		log.Fatalf("linking band %q to project %q: %v", l.bandName, l.projectName, err)
	}
	fmt.Printf("linked band %q (id %d) -> project %q (id %d)\n", l.bandName, bandID, l.projectName, newProjectID)
}

func createBandSetlistIfMissing(ctx context.Context, st *store.Store, pool *pgxpool.Pool, s bandSetlist, actorID int64, dryRun bool) {
	var bandID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM bands WHERE name = $1`, s.bandName).Scan(&bandID); err != nil {
		log.Fatalf("looking up band %q: %v", s.bandName, err)
	}
	existing, err := st.ListSetlistsForBand(ctx, bandID)
	if err != nil {
		log.Fatalf("listing setlists for band %q: %v", s.bandName, err)
	}
	for _, e := range existing {
		if e.Title == s.title {
			fmt.Printf("setlist %q already exists for band %q (id %d) — skipping\n", s.title, s.bandName, e.ID)
			return
		}
	}
	if dryRun {
		n := 0
		for _, set := range s.sets {
			n += len(set.Entries)
		}
		fmt.Printf("[dry-run] would create setlist %q for band %q (%d sets, %d songs)\n", s.title, s.bandName, len(s.sets), n)
		return
	}
	id, err := st.CreateSetlist(ctx, "band", s.title, s.notes, nil, &bandID, actorID)
	if err != nil {
		log.Fatalf("creating setlist %q for band %q: %v", s.title, s.bandName, err)
	}
	l := &setlist.Setlist{Sets: s.sets}
	l.Set("title", s.title)
	if _, err := st.SaveSetlistByID(ctx, id, l, s.title, s.notes, "", actorID); err != nil {
		log.Fatalf("saving setlist %d (%q): %v", id, s.title, err)
	}
	fmt.Printf("created setlist %d %q for band %q (id %d) with %d sets\n", id, s.title, s.bandName, bandID, len(s.sets))
}
