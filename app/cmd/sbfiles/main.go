// sbfiles adds files to a songbook's download area
// (docs/community-downloads.md), reading them from stdin: one file, or a
// zip unpacked the way an upload of a zip on the download page is.
//
//	sbfiles add --db URL --songbook 12 [--path F] [--song 251] [--title T] [--kind K] [--actor NAME] --name "01_Mon premier blues.gp" < file
//	sbfiles import --db URL --songbook 12 [--path F] [--actor NAME] --name "Lessons.zip" < Lessons.zip
//	sbfiles course --db URL --songbook 12
//
// course makes the songbook a course (docs/courses.md) and its lessons
// from its files; a course's import does the latter by itself.
//
// A file of the same name in the same folder is replaced. In production it
// runs inside the app container:
//
//	docker compose exec -T app sbfiles add --db "$DATABASE_URL" --songbook 12 --name "x.pdf" < x.pdf
package main

import (
	"archive/zip"
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"app/internal/store"
)

const usage = `usage:
  sbfiles add --db URL --songbook ID [--path F] [--song ID] [--title T] [--kind K] [--actor NAME] --name FILE < file
  sbfiles import --db URL --songbook ID [--path F] [--actor NAME] --name ZIP < zip
  sbfiles course --db URL --songbook ID`

func main() {
	if len(os.Args) < 2 || (os.Args[1] != "add" && os.Args[1] != "import" && os.Args[1] != "course") {
		log.Fatal(usage)
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	dbURL := fs.String("db", "", "database connection string (required)")
	songbookID := fs.Int64("songbook", 0, "the songbook whose download area gets the files (required)")
	folder := fs.String("path", "", "the folder the files go in")
	songID := fs.Int64("song", 0, "a song of the songbook the file goes with")
	name := fs.String("name", "", "the file's name (required)")
	title := fs.String("title", "", "what the file is called in the list, when not its name")
	actor := fs.String("actor", "", "username the files are added as")
	kind := fs.String("kind", "", "book, gp, audio or other (default: from the name)")
	fs.Parse(os.Args[2:])
	if *dbURL == "" || *songbookID == 0 || (*name == "" && cmd != "course") {
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
	base := filepath.Base(*name)

	if cmd == "course" {
		if err := st.SetSongbookKind(ctx, *songbookID, store.SongbookKindCourse); err != nil {
			log.Fatal(err)
		}
		created, placed, err := st.LessonsFromFiles(ctx, *songbookID)
		if err != nil {
			log.Fatal(err)
		}
		lessons, loose, err := st.ListLessons(ctx, *songbookID)
		if err != nil {
			log.Fatal(err)
		}
		for _, l := range lessons {
			var songs []string
			for _, s := range l.Songs {
				songs = append(songs, s.Title)
			}
			fmt.Printf("%s\t%s\n", l.Label(), strings.Join(songs, " | "))
		}
		for _, s := range loose {
			fmt.Printf("(no lesson)\t%s\n", s.Title)
		}
		fmt.Printf("%d lessons made, %d songs and files placed\n", created, placed)
		return
	}

	if cmd == "import" {
		// A zip is read from its end: keep it on disk while it's read.
		tmp, err := os.CreateTemp("", "sbfiles-*.zip")
		if err != nil {
			log.Fatal(err)
		}
		defer os.Remove(tmp.Name())
		size, err := io.Copy(tmp, os.Stdin)
		if err != nil {
			log.Fatal(err)
		}
		zr, err := zip.NewReader(tmp, size)
		if err != nil {
			log.Fatalf("%s: %v", base, err)
		}
		res, err := st.ImportSongbookZip(ctx, *songbookID, zr, base, *folder, actorID)
		if err != nil {
			log.Fatalf("%s: %v", base, err)
		}
		for _, f := range res {
			song := "-"
			if f.SongID != nil {
				song = fmt.Sprintf("%d %s", *f.SongID, f.SongTitle)
			}
			fmt.Printf("%-9s %s\t%s%s\n", f.Status, filepath.Join(f.Path, f.Name), song, map[bool]string{true: "\t" + f.Note}[f.Note != ""])
		}
		return
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
	id, status, err := st.AddSongbookFile(ctx, store.NewSongbookFile{
		SongbookID: *songbookID, VariantID: variantID, Kind: *kind, Path: *folder, Filename: base, Title: *title, CreatedByID: actorID,
	}, bufio.NewReaderSize(os.Stdin, store.FileChunkSize))
	if err != nil {
		log.Fatalf("%s: %v", base, err)
	}
	fmt.Printf("%s: %s (file %d)\n", base, status, id)
}
