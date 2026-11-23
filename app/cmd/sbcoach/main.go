// sbcoach sets up a coaching community (docs/coaching.md) from the
// command line, while its pages are the coach's to use:
//
//	sbcoach community --db URL --name "Chloé Paul" --coach chloepaul [--student tapoueh] [--look look.json]
//	sbcoach songs --db URL --community 21 --student tapoueh --songs 206,215,228 [--clips clips.tsv]
//	sbcoach locale --db URL (--community ID | --band ID | --person USER) --locale fr
//
// community makes a coaching community, its coach its first member (an
// account made for them when there's none yet: no password, no email,
// theirs once they're invited), a student with it, and a look (a
// brand.Brand as JSON). songs puts songs in the community's songbook
// (the coach's reference) and the student's — linked, or the community's
// own song or arrangement, never a copy per student (communitySong); with
// --clips (lines "songID<TAB>YouTube URL"), it sets each song's reference
// video and downloads its sound, which the stems worker then splits
// (voice and backing): another recording than a linked song's makes the
// community's arrangement of it.
//
// add, sync, lyrics, ireal and key (songcmds.go) are the import wizard's
// steps for one song: a new song with its reference recording, chart,
// lyrics and sections synced to the recording; or one of those steps on
// an existing song. check (read-only) says, for one song or a whole
// songbook, whether each is new, worth improving or worth restarting,
// and which of those steps it needs. In production it runs inside the
// app container:
//
//	docker compose exec -T app sbcoach songs --db "$DATABASE_URL" ...
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"app/internal/brand"
	"app/internal/store"
	"app/internal/ytaudio"
)

const usage = `usage:
  sbcoach community --db URL --name NAME --coach USER [--student USER] [--look FILE]
  sbcoach songs --db URL --community ID --student USER --songs ID,ID,... [--actor USER] [--clips FILE]
  sbcoach locale --db URL (--community ID | --band ID | --person USER) --locale LANG`

func main() {
	if len(os.Args) >= 2 && songCommands[os.Args[1]] {
		runSongCommand(os.Args[1], os.Args[2:])
		return
	}
	if len(os.Args) < 2 || (os.Args[1] != "community" && os.Args[1] != "songs" && os.Args[1] != "locale") {
		log.Fatal(usage + songUsage)
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	dbURL := fs.String("db", os.Getenv("DATABASE_URL"), "database connection string")
	name := fs.String("name", "", "the community's name")
	actor := fs.String("actor", "", "username doing it")
	coach := fs.String("coach", "", "community: the coach's username (made when there's none)")
	look := fs.String("look", "", "community: a JSON file with its look (brand.Brand)")
	communityID := fs.Int64("community", 0, "songs: the coaching community")
	student := fs.String("student", "", "a student's username")
	songs := fs.String("songs", "", "songs: song ids, comma separated")
	clips := fs.String("clips", "", "songs: a file of songID<TAB>YouTube URL lines")
	bandID := fs.Int64("band", 0, "locale: a band")
	personName := fs.String("person", "", "locale: a person's username")
	locale := fs.String("locale", "", "locale: the language, en or fr (\"\" for automatic); community: the community's and its coach's")
	fs.Parse(os.Args[2:])
	ctx := context.Background()
	st, err := store.New(ctx, *dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	switch cmd {
	case "community":
		if *name == "" || *coach == "" {
			log.Fatal(usage)
		}
		coachID := int64(0)
		if p, err := st.GetPersonByUsername(ctx, *coach); err == nil {
			coachID = p.ID
		} else if coachID, err = st.CreatePerson(ctx, *coach, *name, ""); err != nil {
			log.Fatalf("making %s: %v", *coach, err)
		} else {
			fmt.Printf("account %s (%s) made for the coach: invite them to it\n", *coach, *name)
		}
		id, err := st.CreateCommunity(ctx, *name, coachID)
		if err != nil {
			log.Fatal(err)
		}
		if err := st.SetCommunityKind(ctx, id, "coaching"); err != nil {
			log.Fatal(err)
		}
		// The coach's songbook: the songs they teach from.
		if sb, err := st.GetDefaultSongbook(ctx, "community", id); err == nil {
			if err := st.RenameSongbook(ctx, sb, "Repertoire"); err != nil {
				log.Fatal(err)
			}
		}
		if *student != "" {
			p, err := st.GetPersonByUsername(ctx, *student)
			if err != nil {
				log.Fatalf("%s: %v", *student, err)
			}
			if _, err := st.AddCommunityMember(ctx, id, p.ID, "member", coachID); err != nil {
				log.Fatal(err)
			}
		}
		if *look != "" {
			raw, err := os.ReadFile(*look)
			if err != nil {
				log.Fatal(err)
			}
			var b brand.Brand
			if err := json.Unmarshal(raw, &b); err != nil {
				log.Fatalf("%s: %v", *look, err)
			}
			if err := st.SetBrand(ctx, "community", id, b); err != nil {
				log.Fatal(err)
			}
		}
		if *locale != "" {
			if err := st.SetCommunityLocale(ctx, id, *locale); err != nil {
				log.Fatal(err)
			}
			if err := st.SetPersonLocale(ctx, coachID, *locale); err != nil {
				log.Fatal(err)
			}
		}
		fmt.Printf("community %d: %s\n", id, *name)

	case "locale":
		// The language of a community's or band's pages, or a person's.
		switch {
		case *communityID != 0:
			err = st.SetCommunityLocale(ctx, *communityID, *locale)
		case *bandID != 0:
			err = st.SetBandLocale(ctx, *bandID, *locale)
		case *personName != "":
			var p *store.Person
			if p, err = st.GetPersonByUsername(ctx, *personName); err == nil {
				err = st.SetPersonLocale(ctx, p.ID, *locale)
			}
		default:
			log.Fatal(usage)
		}
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("language set to %q\n", *locale)

	case "songs":
		if *communityID == 0 || *student == "" || *songs == "" {
			log.Fatal(usage)
		}
		sp, err := st.GetPersonByUsername(ctx, *student)
		if err != nil {
			log.Fatalf("%s: %v", *student, err)
		}
		by := sp
		if *actor != "" {
			if by, err = st.GetPersonByUsername(ctx, *actor); err != nil {
				log.Fatalf("%s: %v", *actor, err)
			}
		}
		urls := map[int64]string{}
		if *clips != "" {
			f, err := os.Open(*clips)
			if err != nil {
				log.Fatal(err)
			}
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				id, url, ok := strings.Cut(strings.TrimSpace(sc.Text()), "\t")
				if n, err := strconv.ParseInt(id, 10, 64); ok && err == nil {
					urls[n] = strings.TrimSpace(url)
				}
			}
			f.Close()
		}
		sbID, err := st.StudentSongbook(ctx, *communityID, sp.ID, sp.DisplayName+"’s songs", by.ID)
		if err != nil {
			log.Fatal(err)
		}
		repertoire, err := st.GetDefaultSongbook(ctx, "community", *communityID)
		if err != nil {
			log.Fatal(err)
		}
		for _, field := range strings.Split(*songs, ",") {
			src, err := strconv.ParseInt(strings.TrimSpace(field), 10, 64)
			if err != nil {
				log.Fatalf("song %q: %v", field, err)
			}
			url := urls[src]
			copyID, how, err := communitySong(ctx, st, *communityID, src, url, by.ID)
			if err != nil {
				log.Fatalf("song %d: %v", src, err)
			}
			for _, book := range []int64{repertoire, sbID} {
				if err := st.AddToSongbook(ctx, book, copyID, by.ID); err != nil {
					log.Fatal(err)
				}
			}
			title, artist, ref, body, err := st.VariantContent(ctx, copyID)
			if err != nil {
				log.Fatal(err)
			}
			if url == "" || (how == "linked" && url == ref) {
				fmt.Printf("%d → %d %s (%s, no clip)\n", src, copyID, title, how)
				continue
			}
			// Its reference: the "reference:" line of the song's header.
			if _, err := st.SaveVariantByID(ctx, copyID, title, artist, url, withReference(body, url), "", by.ID); err != nil {
				log.Fatal(err)
			}
			dctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			data, err := ytaudio.Download(dctx, url)
			cancel()
			if err != nil {
				fmt.Printf("%d → %d %s: download failed: %v\n", src, copyID, title, err)
				continue
			}
			sum := sha256.Sum256(data)
			if _, err := st.SetSongAudio(ctx, copyID, store.AudioYouTube, data, "audio/mpeg", hex.EncodeToString(sum[:]), url, by.ID); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("%d → %d %s: %d KB, queued for its parts\n", src, copyID, title, len(data)>>10)
		}
		fmt.Printf("student songbook %d\n", sbID)
	}
}

// communitySong is the song a coaching community's songbooks get for
// src (docs/coaching.md, "A group works on one song"): one song the coach
// and the students share, never a copy per student or per run. The
// community's own song, or its arrangement of src when it has one; else
// src itself, linked, when clip doesn't change its recording; else
// (another recording of it) the community's arrangement made from it,
// once. Someone else's private song is refused (store.LinkRefusal). how
// says which: "own", "linked" or "arrangement".
func communitySong(ctx context.Context, st *store.Store, communityID, src int64, clip string, actor int64) (int64, string, error) {
	owner := store.CommunityOwner(communityID)
	id, err := st.CommunityVersionOf(ctx, communityID, src)
	if err != nil {
		return 0, "", err
	}
	r, err := st.SongRightsFor(ctx, actor, id)
	if err != nil {
		return 0, "", err
	}
	if r.Owner == owner {
		return id, "own", nil
	}
	_, _, ref, _, err := st.VariantContent(ctx, id)
	if err != nil {
		return 0, "", err
	}
	if r.CanLinkFrom(owner) && (clip == "" || clip == ref) {
		return id, "linked", nil
	}
	id, err = st.ForkVariant(ctx, id, actor, "community", communityID)
	return id, "arrangement", err
}

// withReference sets the "reference:" line of a song's header (the lines
// before its first blank line), after its artist when it has none yet.
func withReference(body, url string) string {
	lines := strings.Split(body, "\n")
	end := len(lines)
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			end = i
			break
		}
	}
	at := 0
	for i := 0; i < end; i++ {
		if strings.HasPrefix(lines[i], "reference:") {
			lines[i] = "reference: " + url
			return strings.Join(lines, "\n")
		}
		if strings.HasPrefix(lines[i], "artist:") || strings.HasPrefix(lines[i], "title:") {
			at = i + 1
		}
	}
	lines = append(lines[:at], append([]string{"reference: " + url}, lines[at:]...)...)
	return strings.Join(lines, "\n")
}
