// gpmigrate moves songs off Riff JSON score lines ("score[Track]: {...}",
// notes copied out of a Guitar Pro file) onto "gp[Track]: bars" lines
// pointing into the song's attached file (docs/gp-file-model.md §8).
//
//	gpmigrate dump  --db URL > songs.jsonl     every song with Riff JSON
//	gpmigrate dump  --db URL --ids 251-284     these songs, whatever they hold
//	node tools/gpmigrate/plan.cjs ...           works out the new texts and files
//	gpmigrate apply --db URL --actor PERSON < plan.jsonl
//
// apply saves each song through the app's own save path, so every change
// is a revision in the song's history; a song edited since the dump
// (another version) is skipped and reported, never overwritten. A new
// Guitar Pro file is stored with its facts, before the text that points
// into it.
//
// In production it runs inside the app container:
//
//	docker compose exec -T app gpmigrate apply --db "$DATABASE_URL" --actor 1 < plan.jsonl
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"app/internal/gpfacts"
	"app/internal/store"
)

// planItem is one line of plan.cjs's output.
type planItem struct {
	ID      int64           `json:"id"`
	Version string          `json:"version"`
	Body    string          `json:"body"`
	File    *planFile       `json:"file,omitempty"`
	Notes   []string        `json:"notes,omitempty"`
	Facts   json.RawMessage `json:"facts,omitempty"` // facts of the song's existing file, when it had none
	SHA256  string          `json:"sha256,omitempty"`
}

type planFile struct {
	Name  string          `json:"name"`
	Data  []byte          `json:"data"`
	Facts json.RawMessage `json:"facts"`
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: gpmigrate dump|apply --db URL [--actor PERSON] [--dry-run]")
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	dbURL := fs.String("db", "", "database connection string (required)")
	actor := fs.String("actor", "", "who the changes are made as (apply): an email or a person id")
	dryRun := fs.Bool("dry-run", false, "apply: check every item, write nothing")
	idList := fs.String("ids", "", "dump: these songs instead (\"251-284,297\")")
	fs.Parse(os.Args[2:])
	if *dbURL == "" {
		log.Fatal("--db is required")
	}
	ctx := context.Background()
	st, err := store.New(ctx, *dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	switch cmd {
	case "dump":
		var songs []store.RiffSong
		if *idList != "" {
			ids, perr := parseIDs(*idList)
			if perr != nil {
				log.Fatal(perr)
			}
			songs, err = st.ListSongsForMigration(ctx, ids)
		} else {
			songs, err = st.ListRiffSongs(ctx)
		}
		if err != nil {
			log.Fatal(err)
		}
		enc := json.NewEncoder(os.Stdout)
		for _, s := range songs {
			if err := enc.Encode(s); err != nil {
				log.Fatal(err)
			}
		}
		if *idList != "" {
			log.Printf("%d songs", len(songs))
		} else {
			log.Printf("%d songs with Riff JSON or a file without facts", len(songs))
		}
	case "apply":
		var actorID int64
		if *actor != "" {
			p, err := st.FindPerson(ctx, *actor)
			if err != nil {
				log.Fatalf("actor %q: %v", *actor, err)
			}
			actorID = p.ID
		}
		apply(ctx, st, actorID, *dryRun)
	default:
		log.Fatalf("unknown command %q", cmd)
	}
}

func apply(ctx context.Context, st *store.Store, actorID int64, dryRun bool) {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 1<<20), 256<<20)
	var done, skipped int
	for sc.Scan() {
		var it planItem
		if err := json.Unmarshal(sc.Bytes(), &it); err != nil {
			log.Fatalf("bad plan line: %v", err)
		}
		current, version, err := st.LoadVariantRaw(it.ID)
		if err != nil {
			log.Printf("song %d: %v — skipped", it.ID, err)
			skipped++
			continue
		}
		if version != it.Version {
			log.Printf("song %d: edited since the dump (version %s, plan made on %s) — skipped", it.ID, version, it.Version)
			skipped++
			continue
		}
		if it.File != nil {
			if _, err := gpfacts.Parse(it.File.Facts); err != nil {
				log.Fatalf("song %d: bad facts: %v", it.ID, err)
			}
		}
		for _, n := range it.Notes {
			log.Printf("song %d: %s", it.ID, n)
		}
		if dryRun {
			log.Printf("song %d: would be saved (%d bytes of text%s)", it.ID, len(it.Body), fileNote(it.File))
			done++
			continue
		}
		if it.File != nil {
			sum := sha256.Sum256(it.File.Data)
			if err := st.SetSongGPSourceFile(ctx, it.ID, it.File.Name, "application/octet-stream", it.File.Data, hex.EncodeToString(sum[:]), actorID, it.File.Facts); err != nil {
				log.Fatalf("song %d: storing the file: %v", it.ID, err)
			}
		} else if len(it.Facts) > 0 && it.SHA256 != "" {
			if _, err := st.SetSongGPSourceFileFacts(ctx, it.ID, it.SHA256, it.Facts); err != nil {
				log.Fatalf("song %d: storing facts: %v", it.ID, err)
			}
		}
		if it.Body == current {
			if it.File != nil {
				log.Printf("song %d: new Guitar Pro file %q, text unchanged", it.ID, it.File.Name)
			} else {
				log.Printf("song %d: facts only", it.ID)
			}
			done++
			continue
		}
		if _, err := st.SaveVariantRaw(it.ID, it.Body, it.Version, actorID); err != nil {
			if errors.Is(err, store.ErrConflict) {
				log.Printf("song %d: edited meanwhile — skipped", it.ID)
				skipped++
				continue
			}
			log.Fatalf("song %d: saving: %v", it.ID, err)
		}
		log.Printf("song %d: saved%s", it.ID, fileNote(it.File))
		done++
	}
	if err := sc.Err(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d saved, %d skipped\n", done, skipped)
}

// parseIDs reads "251-284,297" as the song ids it names.
func parseIDs(s string) ([]int64, error) {
	var out []int64
	for _, part := range strings.Split(s, ",") {
		a, b, isRange := strings.Cut(strings.TrimSpace(part), "-")
		lo, err := strconv.ParseInt(a, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("--ids: %q is not a song id", part)
		}
		hi := lo
		if isRange {
			if hi, err = strconv.ParseInt(b, 10, 64); err != nil || hi < lo {
				return nil, fmt.Errorf("--ids: %q is not a range of song ids", part)
			}
		}
		for id := lo; id <= hi; id++ {
			out = append(out, id)
		}
	}
	return out, nil
}

func fileNote(f *planFile) string {
	if f == nil {
		return ""
	}
	return fmt.Sprintf(", with a new Guitar Pro file %q (%d bytes)", f.Name, len(f.Data))
}
