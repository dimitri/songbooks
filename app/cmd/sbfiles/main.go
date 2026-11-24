// sbfiles adds files to a songbook's download area
// (docs/community-downloads.md), reading them from stdin: one file, or a
// zip unpacked the way an upload of a zip on the download page is.
//
//	sbfiles add --db URL --songbook 12 [--path F] [--song 251] [--title T] [--kind K] [--actor PERSON] --name "01_Mon premier blues.gp" < file
//	sbfiles import --db URL --songbook 12 [--path F] [--actor PERSON] --name "Lessons.zip" < Lessons.zip
//	sbfiles course --db URL --songbook 12
//	sbfiles covers --db URL --songbook 12 [--again] [--set-look]
//	sbfiles recordings --db URL --band 15
//
// course makes the songbook a course (docs/courses.md) and its lessons
// from its files; a course's import does the latter by itself. covers
// looks for the covers of the songbook's songs not looked for yet (every
// song with --again) and prints the look they make (docs/covers.md).
// recordings reads a band's recordings folder again, as its Rehearsals
// tab's "Read it again" does (docs/band-recordings.md).
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
	"time"

	"app/internal/covers"
	"app/internal/store"
)

const usage = `usage:
  sbfiles add --db URL --songbook ID [--path F] [--song ID] [--title T] [--kind K] [--actor PERSON] --name FILE < file
  sbfiles import --db URL --songbook ID [--path F] [--actor PERSON] --name ZIP < zip
  sbfiles course --db URL --songbook ID
  sbfiles covers --db URL --songbook ID [--again] [--set-look]
  sbfiles recordings --db URL --band ID`

func main() {
	if len(os.Args) < 2 || (os.Args[1] != "add" && os.Args[1] != "import" && os.Args[1] != "course" && os.Args[1] != "covers" && os.Args[1] != "recordings") {
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
	actor := fs.String("actor", "", "who the files are added as: an email or a person id")
	kind := fs.String("kind", "", "book, gp, audio or other (default: from the name)")
	bandID := fs.Int64("band", 0, "recordings: the band whose folder is read again")
	again := fs.Bool("again", false, "covers: look again for the songs that got no album cover")
	setLook := fs.Bool("set-look", false, "covers: make the look of all the owner's songs its brand (the brand form's \"From our songs\"), keeping its logo, tagline and website")
	fs.Parse(os.Args[2:])
	if cmd == "recordings" {
		if *dbURL == "" || *bandID == 0 {
			log.Fatal("--db and --band are required")
		}
		ctx := context.Background()
		st, err := store.New(ctx, *dbURL)
		if err != nil {
			log.Fatal(err)
		}
		defer st.Close()
		if err := st.SyncBandRecordings(ctx, *bandID); err != nil {
			log.Fatal(err)
		}
		titles, err := st.UnmatchedTitles(ctx, *bandID)
		if err != nil {
			log.Fatal(err)
		}
		files, err := st.UnmatchedFiles(ctx, *bandID, false)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("band %d read again: %d titles to match, %d files with no song\n", *bandID, len(titles), len(files))
		return
	}
	if *dbURL == "" || *songbookID == 0 || (*name == "" && cmd != "course" && cmd != "covers") {
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
		p, err := st.FindPerson(ctx, *actor)
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
		videos, err := st.LessonVideosFromBooks(ctx, *songbookID)
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
			fmt.Printf("%s\t%s\t%s\n", l.Label("en"), l.VideoURL, strings.Join(songs, " | "))
		}
		for _, s := range loose {
			fmt.Printf("(no lesson)\t%s\n", s.Title)
		}
		fmt.Printf("%d lessons made, %d songs and files placed, %d videos from the book\n", created, placed, videos)
		return
	}

	if cmd == "covers" {
		found, missing, err := st.FillCovers(ctx, covers.NewFinder(), *songbookID, *again, 3*time.Second, func(sc store.SongCover) {
			if sc.Cover == nil {
				fmt.Printf("none\t%s — %s\n", sc.Title, sc.Artist)
				return
			}
			fmt.Printf("%s\t%s — %s\t%s (%d, %s)\n", sc.Cover.Source, sc.Title, sc.Artist, sc.Cover.Album, sc.Cover.Year, sc.Cover.Genre)
		})
		if err != nil {
			log.Printf("some songs to look for again (sbfiles covers again later): %v", err)
		}
		songs, err := st.SongbookCovers(ctx, *songbookID)
		if err != nil {
			log.Fatal(err)
		}
		look := store.CoverLook(songs)
		fmt.Printf("%d found, %d not found\nlook: %s %+v\ngenres: %s\n", found, missing, look.Mood, look.Brand, strings.Join(look.Genres, ", "))
		if *setLook {
			sb, err := st.GetSongbook(ctx, *songbookID)
			if err != nil {
				log.Fatal(err)
			}
			kind, owner := sb.OwnerKind, sb.OwnerBandID
			if kind == "community" {
				owner = sb.OwnerCommunityID
			} else if kind != "band" {
				log.Fatalf("songbook %d is a person's: only a band or a community has a look", *songbookID)
			}
			all, err := st.OwnerCovers(ctx, kind, owner)
			if err != nil {
				log.Fatal(err)
			}
			old, name, err := st.GetBrand(ctx, kind, owner)
			if err != nil {
				log.Fatal(err)
			}
			b := store.CoverLook(all).Brand
			if b.IsZero() {
				log.Fatalf("no covers for %s's songs yet", name)
			}
			b.LogoURL, b.Tagline, b.Website, b.Mark = old.LogoURL, old.Tagline, old.Website, old.Mark
			if err := st.SetBrand(ctx, kind, owner, b); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("%s's look is now its songs': %+v\n", name, b)
		}
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
	// A course's book: its lessons' videos.
	if sb, err := st.GetSongbook(ctx, *songbookID); err == nil && sb.IsCourse() && store.FileKind(base, variantID != nil) == "book" {
		if n, err := st.LessonVideosFromBooks(ctx, *songbookID); err != nil {
			log.Print(err)
		} else {
			fmt.Printf("%d lesson videos from the book\n", n)
		}
	}
}
