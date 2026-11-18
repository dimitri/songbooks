package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"app/internal/httpapi"
	"app/internal/importer"
	"app/internal/song"
	"app/internal/songimport"
	"app/internal/store"
	"app/internal/ytaudio"
)

// Song commands, one song per run, the import wizard's steps from the
// command line (internal/songimport does the work for both). They print
// what they did — sections, keys, times, sources — never the lyrics.
const songUsage = `
  sbcoach add --db URL (--community ID | --band ID | --person USER) --actor USER --title T --artist A
              [--clip URL] [--chart FILE | --vocal] [--ireal] [--lyrics ug|url|text [--input X]]
              [--student USER] [--wait 20m]
  sbcoach sync   --db URL --song ID [--actor USER] [--overwrite] [--dry-run]
  sbcoach lyrics --db URL --song ID --actor USER [--source ug|url|text] [--input X] [--replace] [--dry-run]
  sbcoach ireal  --db URL --song ID --actor USER [--all] [--dry-run]
  sbcoach key    --db URL --song ID`

var songCommands = map[string]bool{"add": true, "sync": true, "lyrics": true, "ireal": true, "key": true}

func runSongCommand(cmd string, args []string) {
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	dbURL := fs.String("db", os.Getenv("DATABASE_URL"), "database connection string")
	actorName := fs.String("actor", "", "username doing it")
	communityID := fs.Int64("community", 0, "add: into this community's songbook")
	bandID := fs.Int64("band", 0, "add: into this band's songbook")
	personName := fs.String("person", "", "add: into this person's songbook")
	student := fs.String("student", "", "add: also into this student's songbook (with --community)")
	title := fs.String("title", "", "add: the song's title")
	artist := fs.String("artist", "", "add: the song's artist")
	clip := fs.String("clip", "", "add: the reference recording (a YouTube address); searched when empty")
	noClip := fs.Bool("no-clip", false, "add: no reference recording")
	chart := fs.String("chart", "", "add: a chart file (.song text) to start from, cleaned up")
	vocal := fs.Bool("vocal", false, "add: the singer's chart: lyrics from Ultimate Guitar without chords, grids from iReal Pro")
	ireal := fs.Bool("ireal", false, "add: apply iReal Pro's grids where they match")
	lyrics := fs.String("lyrics", "", "add: place lyrics from ug, url or text (with --input)")
	source := fs.String("source", songimport.LyricsFromUG, "lyrics: ug, url or text")
	input := fs.String("input", "", "lyrics: the page address (url) or a file of lyrics (text)")
	replace := fs.Bool("replace", false, "lyrics: replace lyrics already in sections")
	songID := fs.Int64("song", 0, "the song")
	all := fs.Bool("all", false, "ireal: apply every section, not only the safe matches")
	overwrite := fs.Bool("overwrite", false, "sync: replace an existing tempo: and time:")
	dryRun := fs.Bool("dry-run", false, "show, don't save")
	wait := fs.Duration("wait", 20*time.Minute, "add: how long to wait for the voice to be separated before syncing")
	fs.Parse(args)

	ctx := context.Background()
	st, err := store.New(ctx, *dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	actor := int64(0)
	if *actorName != "" {
		p, err := st.GetPersonByUsername(ctx, *actorName)
		if err != nil {
			log.Fatalf("%s: %v", *actorName, err)
		}
		actor = p.ID
	}
	lyricsInput := func() string {
		if (*source == songimport.LyricsFromText || *lyrics == songimport.LyricsFromText) && *input != "" {
			b, err := os.ReadFile(*input)
			if err != nil {
				log.Fatal(err)
			}
			return string(b)
		}
		return *input
	}

	switch cmd {
	case "add":
		if *title == "" || actor == 0 || (*communityID == 0 && *bandID == 0 && *personName == "") {
			log.Fatal("usage:" + songUsage)
		}
		id := createSong(ctx, st, *communityID, *bandID, *personName, *student, *title, *artist, actor)
		body := "title: " + *title + "\nartist: " + *artist + "\n\n[Verse]\n"
		switch {
		case *chart != "":
			b, err := os.ReadFile(*chart)
			if err != nil {
				log.Fatal(err)
			}
			text, notes := importer.CleanSongText(string(b), importer.CleanOptions{})
			body = text
			printNotes("cleanup", notes)
		case *vocal:
			d, err := songimport.VocalDraft(ctx, st, *title, *artist)
			if err != nil {
				fmt.Printf("singer's chart: %v (kept an empty chart)\n", err)
			} else {
				body = d.Text
				fmt.Printf("singer's chart from %s\n", d.URL)
				printNotes("draft", d.Notes)
			}
		}
		if *ireal && !*vocal {
			body = irealFix(ctx, st, body, *title, *artist, false)
		}
		if *lyrics != "" {
			body = placeLyrics(ctx, body, *lyrics, *title, *artist, lyricsInput(), *replace)
		}
		body = songimport.SetHeader(body, "title", *title, true)
		if *artist != "" {
			body = songimport.SetHeader(body, "artist", *artist, true)
		}
		ref := ""
		if !*noClip {
			ref = *clip
			if ref == "" {
				ref = findClip(ctx, *title, *artist)
			}
		}
		if ref != "" {
			body = songimport.SetHeader(body, "reference", ref, true)
		}
		if _, err := st.SaveVariantByID(ctx, id, *title, *artist, ref, body, "", actor); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("song %d: %s — %s\n", id, *title, *artist)
		if ref == "" {
			return
		}
		data, err := ytaudio.Download(ctx, ref)
		if err != nil {
			log.Fatalf("downloading %s: %v", ref, err)
		}
		sum := sha256.Sum256(data)
		audioID, err := st.SetSongAudio(ctx, id, store.AudioYouTube, data, "audio/mpeg", hex.EncodeToString(sum[:]), ref, actor)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("recording: %d KB, queued for its voice and backing parts\n", len(data)>>10)
		if waitForStems(ctx, st, audioID, *wait) {
			syncSong(ctx, st, id, actor, false, false)
		} else {
			fmt.Printf("not separated yet: run `sbcoach sync --song %d` once it is\n", id)
		}

	case "sync":
		needSong(*songID)
		syncSong(ctx, st, *songID, actor, *overwrite, *dryRun)

	case "lyrics":
		needSong(*songID)
		title, artist, ref, body, err := st.VariantContent(ctx, *songID)
		if err != nil {
			log.Fatal(err)
		}
		nb := placeLyrics(ctx, body, *source, title, artist, lyricsInput(), *replace)
		save(ctx, st, *songID, title, artist, ref, body, nb, actor, *dryRun)

	case "ireal":
		needSong(*songID)
		title, artist, ref, body, err := st.VariantContent(ctx, *songID)
		if err != nil {
			log.Fatal(err)
		}
		nb := irealFix(ctx, st, body, title, artist, *all)
		save(ctx, st, *songID, title, artist, ref, body, nb, actor, *dryRun)

	case "key":
		needSong(*songID)
		title, artist, _, body, err := st.VariantContent(ctx, *songID)
		if err != nil {
			log.Fatal(err)
		}
		irealKey := ""
		if _, info, err := songimport.FindIReal(ctx, st, title, artist); err == nil {
			irealKey = info.Key
		}
		audioKey, conf := "", 0.0
		if a, _, err := songimport.Recording(ctx, st, *songID, store.AudioYouTube); err == nil {
			audioKey, conf = a.Key, a.KeyConfidence
		}
		printKey(songimport.CheckKey(body, irealKey, audioKey, conf))
	}
}

func needSong(id int64) {
	if id == 0 {
		log.Fatal("usage:" + songUsage)
	}
}

func createSong(ctx context.Context, st *store.Store, communityID, bandID int64, personName, student, title, artist string, actor int64) int64 {
	switch {
	case communityID != 0:
		id, err := st.CreateCommunityVariant(ctx, communityID, title, artist, actor)
		if err != nil {
			log.Fatal(err)
		}
		if student != "" {
			sp, err := st.GetPersonByUsername(ctx, student)
			if err != nil {
				log.Fatalf("%s: %v", student, err)
			}
			sbID, err := st.StudentSongbook(ctx, communityID, sp.ID, sp.DisplayName+"’s songs", actor)
			if err != nil {
				log.Fatal(err)
			}
			if err := st.AddToSongbook(ctx, sbID, id, actor); err != nil {
				log.Fatal(err)
			}
		}
		return id
	case bandID != 0:
		id, err := st.CreateBandVariant(ctx, bandID, title, artist, actor)
		if err != nil {
			log.Fatal(err)
		}
		return id
	default:
		p, err := st.GetPersonByUsername(ctx, personName)
		if err != nil {
			log.Fatalf("%s: %v", personName, err)
		}
		id, err := st.CreateVariant(ctx, store.OwnerPerson, p.ID, title, artist, actor)
		if err != nil {
			log.Fatal(err)
		}
		if sbID, err := st.GetDefaultSongbook(ctx, "person", p.ID); err == nil {
			if err := st.AddToSongbook(ctx, sbID, id, actor); err != nil {
				log.Fatal(err)
			}
		}
		return id
	}
}

// findClip picks the best reference recording and says which, so the
// person can check it's the artist's own.
func findClip(ctx context.Context, title, artist string) string {
	list, err := ytaudio.Search(ctx, title, artist)
	if err != nil || len(list) == 0 {
		fmt.Printf("no reference recording found (%v): pass --clip\n", err)
		return ""
	}
	c := list[0]
	official := "not the artist's own channel: check it"
	if c.Official {
		official = "the artist's channel"
	}
	fmt.Printf("reference: %s — %q on %s (%s, %.0fs)\n", c.URL, c.Title, c.Channel, official, c.Duration)
	return c.URL
}

func waitForStems(ctx context.Context, st *store.Store, audioID int64, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		s, err := st.GetStemStatus(ctx, audioID)
		if err == nil && s.Status == "done" {
			return true
		}
		if err == nil && s.Status == "failed" {
			fmt.Println("separating the voice failed")
			return false
		}
		time.Sleep(15 * time.Second)
	}
	return false
}

func syncSong(ctx context.Context, st *store.Store, id, actor int64, overwrite, dryRun bool) {
	title, artist, _, body, err := st.VariantContent(ctx, id)
	if err != nil {
		log.Fatal(err)
	}
	a, ready, err := songimport.Recording(ctx, st, id, store.AudioYouTube)
	if err != nil {
		log.Fatal(err)
	}
	irealSig, irealKey := "", ""
	if _, info, err := songimport.FindIReal(ctx, st, title, artist); err == nil {
		irealSig, irealKey = info.TimeSig, info.Key
	}
	p, err := songimport.ProposeSync(a, ready, body, httpapi.SyncSections(song.Parse(body)), irealSig, irealKey)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("recording: %.1fs, sound from %.2fs; its own estimate %.0f bpm in %s\n", p.Duration, p.AudioStart, p.AudioTempo, p.AudioTimeSig)
	fmt.Printf("chart: tempo %d, time %s (from %s), fit %.0f%%\n", p.Tempo, p.TimeSig, p.TimeSigFrom, p.Score*100)
	for _, s := range p.Sections {
		fmt.Printf("  %6.2fs  %s\n", s.Time, s.Label)
	}
	printKey(p.Key)
	printNotes("sync", p.Notes)
	if dryRun {
		return
	}
	if _, _, err := songimport.ApplySync(ctx, st, id, store.AudioYouTube, p, "", overwrite, actor); err != nil {
		log.Fatal(err)
	}
	fmt.Println("saved: tempo, time and section markers")
}

func placeLyrics(ctx context.Context, body, source, title, artist, input string, replace bool) string {
	res, err := songimport.PlaceLyrics(ctx, body, source, title, artist, input, replace)
	if err != nil {
		fmt.Printf("lyrics: %v\n", err)
		return body
	}
	fmt.Printf("lyrics from %s: %d stanzas\n", res.From, res.Stanzas)
	for _, p := range res.Placements {
		fmt.Printf("  %-14s %s\n", p.Section, p.Status)
	}
	if len(res.Unused) > 0 {
		fmt.Printf("  %d stanzas not placed: check the song's sections\n", len(res.Unused))
	}
	return res.Text
}

func irealFix(ctx context.Context, st *store.Store, body, title, artist string, all bool) string {
	nb, cands, info, err := songimport.IRealFix(ctx, st, body, title, artist, all)
	if err != nil {
		fmt.Printf("iReal Pro: %v\n", err)
		return body
	}
	fmt.Printf("iReal Pro chart: %s, key %s, %s\n", info.Title, info.Key, info.SourceURL)
	for _, c := range cands {
		state := "kept"
		switch {
		case c.Safe || all:
			state = "iReal grid"
		case c.Offset != 0:
			state = fmt.Sprintf("kept (iReal's is %+d semitones away)", c.Offset)
		}
		fmt.Printf("  %-14s %s\n", c.Label, state)
	}
	return nb
}

func save(ctx context.Context, st *store.Store, id int64, title, artist, ref, old, body string, actor int64, dryRun bool) {
	if body == old {
		fmt.Println("nothing changed")
		return
	}
	if dryRun {
		fmt.Println("dry run: not saved")
		return
	}
	if actor == 0 {
		log.Fatal("--actor is required to save")
	}
	if _, err := st.SaveVariantByID(ctx, id, title, artist, ref, body, "", actor); err != nil {
		log.Fatal(err)
	}
	fmt.Println("saved (a new revision: the song's history has the previous one)")
}

func printKey(k songimport.KeyReport) {
	fmt.Printf("key: chart %s (from %s", k.Chart, k.ChartFrom)
	if k.Capo != 0 {
		fmt.Printf(", capo %d, sounding %s", k.Capo, k.Sounding)
	}
	fmt.Print(")")
	if k.IReal != "" {
		fmt.Printf(", iReal Pro %s", k.IReal)
	}
	if k.Audio != "" {
		fmt.Printf(", recording %s (%.0f%% sure)", k.Audio, k.AudioConf*100)
	}
	fmt.Println()
	if k.ShiftToAudio != 0 {
		fmt.Printf("  transpose the chart %+d semitones to match the recording\n", k.ShiftToAudio)
	}
	printNotes("key", k.Notes)
}

func printNotes(what string, notes []string) {
	for _, n := range notes {
		fmt.Printf("  %s: %s\n", what, strings.TrimSpace(n))
	}
}
