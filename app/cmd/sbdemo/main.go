// sbdemo turns a copy of production's database into demo data, for the
// public website's screenshots and for review on staging
// (docs/demo-data.md):
//
//	sbdemo anonymize --db URL --confirm DB@HOST
//	sbdemo fixtures  --db URL --confirm DB@HOST
//
// anonymize keeps the data's structure (songs, songbooks, communities,
// bands, who may open what, looks, courses) and replaces every name a
// visitor could recognise with an invented one: people, communities,
// bands, songs and artists, songbooks, lessons, set lists, events,
// files. It removes every lyric line, every reference video and album
// cover, and what is nobody's business in a copy (sessions, passkeys,
// mail, history), then writes invented lyrics into a few songs.
//
// fixtures, on an anonymized copy, makes the coaching and calendar
// features look alive: students with their voices, songbooks, groups and
// notes, and eight weeks of lessons, jams, an open mic, rehearsals and
// gigs. It can run again: what it made is found, not made twice.
//
// Both refuse to run unless the database is on this machine (localhost,
// 127.0.0.1) or staging's compose database (host db), and --confirm names
// it: "songbooks@db". Production's compose database is called db too:
// they also refuse where SONGBOOKS_WEBAUTHN_RP_ID is production's
// hostname, and on db where it isn't set. In staging's app container:
//
//	docker compose exec -T app sbdemo anonymize --confirm songbooks@db
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/internal/store"
)

const usage = `usage:
  sbdemo anonymize --db URL --confirm DB@HOST
  sbdemo fixtures  --db URL --confirm DB@HOST
  sbdemo remove    --db URL --confirm DB@HOST   (the fixtures, not the anonymizing)

--db defaults to $DATABASE_URL. Only a database on localhost, 127.0.0.1
or db (staging's compose database) is accepted, and --confirm must name
it: the database, @, the host.`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sbdemo:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 1 || (args[0] != "anonymize" && args[0] != "fixtures" && args[0] != "remove") {
		return fmt.Errorf("%s", usage)
	}
	cmd := args[0]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	dbURL := fs.String("db", os.Getenv("DATABASE_URL"), "database connection string")
	confirm := fs.String("confirm", "", "the database and host to change, as DB@HOST")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	database, host, err := checkConfirm(*dbURL, *confirm)
	if err != nil {
		return err
	}
	if err := checkDeployment(host, os.Getenv); err != nil {
		return err
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := checkConnected(ctx, db, database); err != nil {
		return err
	}
	st, err := store.New(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()

	var c counts
	switch cmd {
	case "anonymize":
		c, err = anonymize(ctx, db, st)
	case "fixtures":
		c, err = fixtures(ctx, db, st, time.Now())
	case "remove":
		c, err = removeFixtures(ctx, db)
	}
	if err != nil {
		return err
	}
	c.print(fmt.Sprintf("%s on %s@%s, rows per table:", cmd, database, host))
	return nil
}
