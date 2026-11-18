// stemsd splits songs' reference recordings into their parts in the
// background (internal/stems, Demucs), one at a time: every recording
// added or replaced is queued by the database (migration 0078), and the
// recordings already there were queued when it ran. It runs in its own
// container, which has Python, PyTorch and Demucs (deploy/Dockerfile.stems).
//
//	stemsd --db URL [--model htdemucs] [--threads 8] [--bitrate 128]
//	stemsd --db URL --once   # split what's queued, then stop
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"app/internal/stems"
	"app/internal/store"
)

func main() {
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "database connection string")
	demucs := flag.String("demucs", "demucs", "the command that runs Demucs (words split on spaces)")
	model := flag.String("model", "htdemucs", "the Demucs model")
	threads := flag.Int("threads", 0, "threads Demucs may use (0: its default)")
	bitrate := flag.Int("bitrate", 128, "the parts' MP3 bitrate, in kbit/s")
	timeout := flag.Duration("timeout", 30*time.Minute, "the longest one recording may take")
	once := flag.Bool("once", false, "split what's queued, then stop")
	flag.Parse()
	if *dbURL == "" {
		log.Fatal("stemsd: --db (or DATABASE_URL) is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	st, err := store.New(ctx, *dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if err := st.ResetRunningStemJobs(ctx); err != nil {
		log.Fatal(err)
	}
	split := stems.Splitter{Demucs: strings.Fields(*demucs), Model: *model, Threads: *threads, Bitrate: *bitrate}

	for ctx.Err() == nil {
		job, err := st.ClaimStemJob(ctx, *model)
		if err != nil {
			log.Printf("stemsd: claiming a job: %v", err)
			sleep(ctx, 30*time.Second)
			continue
		}
		if job == nil {
			if *once {
				return
			}
			if err := st.WaitForStemJob(ctx, time.Minute); err != nil && ctx.Err() == nil {
				log.Printf("stemsd: waiting: %v", err)
				sleep(ctx, 30*time.Second)
			}
			continue
		}
		process(ctx, st, split, *job, *timeout)
	}
}

func process(ctx context.Context, st *store.Store, split stems.Splitter, job store.StemJob, timeout time.Duration) {
	start := time.Now()
	audio, _, err := st.GetSongAudioData(ctx, job.SongAudioID)
	if err != nil {
		fail(st, job, err)
		return
	}
	jctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	parts, err := split.Split(jctx, audio)
	if ctx.Err() != nil {
		return // stopping: the job goes back to the queue at the next start
	}
	if err != nil {
		fail(st, job, err)
		return
	}
	saved, err := st.SaveStems(ctx, job, "audio/mpeg", parts)
	if err != nil {
		fail(st, job, err)
		return
	}
	if !saved {
		log.Printf("stemsd: recording %d was replaced while it was split; its new version is queued", job.SongAudioID)
		return
	}
	size := 0
	for _, p := range parts {
		size += len(p)
	}
	log.Printf("stemsd: recording %d split in %s (%d KB)", job.SongAudioID, time.Since(start).Round(time.Second), size>>10)
}

func fail(st *store.Store, job store.StemJob, err error) {
	log.Printf("stemsd: recording %d (try %d): %v", job.SongAudioID, job.Attempts, err)
	msg := err.Error()
	if len(msg) > 4000 {
		msg = msg[len(msg)-4000:]
	}
	if err := st.FailStemJob(context.Background(), job, msg); err != nil {
		log.Printf("stemsd: recording the failure: %v", err)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
