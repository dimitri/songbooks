// sbfiles adds a file to a songbook's download area
// (docs/community-downloads.md), reading it from stdin:
//
//	sbfiles add --db URL --songbook 12 [--song 251] [--title T] [--kind K] [--actor NAME] --name "01_Mon premier blues.gp" < file
//
// The same file added twice to a songbook is kept once. In production it
// runs inside the app container:
//
//	docker compose exec -T app sbfiles add --db "$DATABASE_URL" --songbook 12 --name "x.pdf" < x.pdf
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"app/internal/store"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "add" {
		log.Fatal("usage: sbfiles add --db URL --songbook ID [--song ID] [--title T] [--kind K] [--actor NAME] --name FILE < file")
	}
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	dbURL := fs.String("db", "", "database connection string (required)")
	songbookID := fs.Int64("songbook", 0, "the songbook whose download area gets the file (required)")
	songID := fs.Int64("song", 0, "a song of the songbook the file goes with")
	name := fs.String("name", "", "the file's name (required)")
	title := fs.String("title", "", "what the file is called in the list, when not its name")
	actor := fs.String("actor", "", "username the file is added as")
	kind := fs.String("kind", "", "book, gp, audio or other (default: from the name)")
	fs.Parse(os.Args[2:])
	if *dbURL == "" || *songbookID == 0 || *name == "" {
		log.Fatal("--db, --songbook and --name are required")
	}
	ctx := context.Background()
	st, err := store.New(ctx, *dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	var actorID int64
	if *actor != "" {
		p, err := st.GetPersonByUsername(ctx, *actor)
		if err != nil {
			log.Fatalf("actor %q: %v", *actor, err)
		}
		actorID = p.ID
	}
	var variantID *int64
	if *songID != 0 {
		in, err := st.SongbookHasVariant(ctx, *songbookID, *songID)
		if err != nil {
			log.Fatal(err)
		}
		if !in {
			log.Fatalf("song %d isn't in songbook %d", *songID, *songbookID)
		}
		variantID = songID
	}
	base := filepath.Base(*name)
	if *kind == "" {
		*kind = store.FileKind(base, variantID != nil)
	}
	id, existed, err := st.AddSongbookFile(ctx, *songbookID, variantID, *kind, base, *title,
		store.FileContentType(base), bufio.NewReaderSize(os.Stdin, store.FileChunkSize), actorID)
	if err != nil {
		log.Fatalf("%s: %v", base, err)
	}
	if existed {
		fmt.Printf("%s: already there (file %d)\n", base, id)
		return
	}
	fmt.Printf("%s: added (file %d)\n", base, id)
}
