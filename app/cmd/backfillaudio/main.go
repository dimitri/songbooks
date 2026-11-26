// backfillaudio queues the YouTube audio of the songs there already are
// (docs/mp3-reference-audio.md "Automatic"): every song whose reference
// is a YouTube video and that has no audio from it yet. The app fetches
// it, as it does for a reference set or a song opened
// (httpapi/yt_audio_jobs.go): this only queues, slowly.
//
// A dry run by default: it counts — the songs with a YouTube video, with
// its audio, with a job queued, running, failed (and failed in the last
// day, not tried again), removed by an editor, and those it would queue.
// With --apply it queues them one at a time, waiting for each to be
// fetched (or to fail) and then --pause before the next, so YouTube sees
// one fetch at a time and the songs people open come first. Stop it at
// any time; run again, it carries on (a song with its audio, or one under
// way, is skipped).
//
//	docker compose exec app backfillaudio                 # counts
//	docker compose exec app backfillaudio --apply --limit 20
//
// The app must be running (its workers fetch). --db defaults to
// $DATABASE_URL.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"time"

	"app/internal/store"
)

func main() {
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "database connection string (default $DATABASE_URL)")
	apply := flag.Bool("apply", false, "queue the fetches (without it: count only)")
	_ = flag.Bool("dry-run", true, "count only (the default; kept for older scripts)")
	limit := flag.Int("limit", 0, "queue at most this many (0: all)")
	pause := flag.Duration("pause", 30*time.Second, "the wait between one song's fetch finishing and the next queued")
	wait := flag.Duration("wait", 20*time.Minute, "the longest wait for one song's fetch before moving on")
	flag.Parse()
	if *dbURL == "" {
		log.Fatal("--db (or $DATABASE_URL) is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	st, err := store.New(ctx, *dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if err := run(ctx, st, os.Stdout, *apply, *limit, *pause, *wait); err != nil {
		log.Fatal(err)
	}
}

// run counts, and with apply queues; out gets the report.
func run(ctx context.Context, st *store.Store, out io.Writer, apply bool, limit int, pause, wait time.Duration) error {
	b, err := st.CountYouTubeAudioBackfill(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "songs with a YouTube video as their reference: %d\n", b.WithVideo)
	fmt.Fprintf(out, "  with its audio already:                      %d\n", b.WithAudio)
	fmt.Fprintf(out, "  queued:                                      %d\n", b.Queued)
	fmt.Fprintf(out, "  being fetched:                               %d\n", b.Running)
	fmt.Fprintf(out, "  failed:                                      %d (%d in the last day: not tried again yet)\n", b.Failed, b.FailedRecently)
	fmt.Fprintf(out, "  removed by an editor (not fetched again):    %d\n", b.Removed)
	fmt.Fprintf(out, "  done, audio gone since:                      %d\n", b.Done)
	n := len(b.Candidates)
	if limit > 0 && n > limit {
		n = limit
	}
	fmt.Fprintf(out, "to queue: %d", len(b.Candidates))
	if n != len(b.Candidates) {
		fmt.Fprintf(out, " (%d with --limit)", n)
	}
	fmt.Fprintln(out)
	if !apply {
		fmt.Fprintln(out, "dry run: nothing queued (--apply to queue them, one at a time)")
		return nil
	}
	ok, failed, skipped := 0, 0, 0
	for i, id := range b.Candidates[:n] {
		if ctx.Err() != nil {
			break
		}
		queued, err := st.QueueYouTubeAudioBackfill(ctx, id)
		if err != nil {
			return err
		}
		if !queued {
			skipped++
			continue
		}
		fmt.Fprintf(out, "[%d/%d] song %d queued… ", i+1, n, id)
		start := time.Now()
		status := ""
		for time.Since(start) < wait && ctx.Err() == nil {
			if status, err = st.YouTubeAudioJobStatus(ctx, id); err != nil {
				return err
			}
			if status != store.YTAudioQueued && status != store.YTAudioRunning {
				break
			}
			sleep(ctx, min(5*time.Second, wait))
		}
		switch status {
		case store.YTAudioDone:
			ok++
		case store.YTAudioFailed:
			failed++
		}
		fmt.Fprintf(out, "%s (%s)\n", orStr(status, "gone"), time.Since(start).Round(time.Second))
		if i < n-1 {
			sleep(ctx, pause)
		}
	}
	fmt.Fprintf(out, "done: %d fetched, %d failed, %d skipped (fetched meanwhile)\n", ok, failed, skipped)
	return nil
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
