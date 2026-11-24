package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/internal/song"
)

// TestAnonymizedDatabase checks a database sbdemo anonymize ran on, when
// SBDEMO_TEST_DB names one (it only reads): no lyric line but the
// invented ones, no reference link, no known real name left (nor the old
// usernames' column, migration 0136). It prints
// counts, never a song's content.
func TestAnonymizedDatabase(t *testing.T) {
	url := os.Getenv("SBDEMO_TEST_DB")
	if url == "" {
		t.Skip("SBDEMO_TEST_DB not set")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	invented := map[string]bool{}
	for _, set := range [][][]string{verses, choruses, bridges} {
		for _, ls := range set {
			for _, l := range ls {
				invented[l] = true
			}
		}
	}
	rows, err := db.Query(ctx, `SELECT body FROM songs`)
	if err != nil {
		t.Fatal(err)
	}
	var songs, withLyrics, foreign, refs int
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			t.Fatal(err)
		}
		songs++
		s := song.Parse(body)
		if s.Get("reference") != "" {
			refs++
		}
		has := false
		for _, sec := range s.Sections {
			for _, l := range sec.Lyrics {
				has = true
				var text strings.Builder
				for _, seg := range l.Segments {
					text.WriteString(seg.Text)
				}
				if !invented[strings.TrimSpace(text.String())] {
					foreign++
				}
			}
		}
		if has {
			withLyrics++
		}
	}
	rows.Close()
	t.Logf("songs: %d, with (invented) lyrics: %d, lyric lines not invented: %d, reference lines: %d", songs, withLyrics, foreign, refs)
	if foreign > 0 || refs > 0 || withLyrics > lyricSongs {
		t.Errorf("lyrics or references left")
	}

	var n int
	if err := db.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM communities WHERE name = ANY($1)) + (SELECT count(*) FROM songs WHERE reference_url <> '')
		     + (SELECT count(*) FROM song_audio WHERE source_url <> '') + (SELECT count(*) FROM band_recording_sources)
		     + (SELECT count(*) FROM communities WHERE brand ? 'logoURL' OR brand ? 'website')
		     + (SELECT count(*) FROM bands WHERE brand ? 'logoURL' OR brand ? 'website')
		     + (SELECT count(*) FROM lessons WHERE video_url <> '')
		     + (SELECT count(*) FROM information_schema.columns
		        WHERE table_schema = current_schema() AND table_name = 'people' AND column_name = 'username')`,
		[]string{"Bands Camp", "Florent Passamonti", "Chloé Paul", "JamSpace"}).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d real names, links, logos or usernames left", n)
	}
}
