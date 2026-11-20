// stemsd splits songs' reference recordings into their parts in the
// background (internal/stems, Demucs): every recording added or replaced
// is queued by the database (migration 0078), and the recordings already
// there were queued when it ran. It runs in its own container, which has
// Python, PyTorch and Demucs (deploy/Dockerfile.stems).
//
// A recording added or replaced (no parts: someone is waiting) goes
// before one split again. With --backlog-threads, a second lane takes
// the splits done again, niced and on few threads, so that an upload
// never waits behind a backfill; without, one lane does everything,
// uploads first.
//
//	stemsd --db URL [--model htdemucs] [--threads 4 --jobs 4] [--backlog-threads 2] [--bitrate 128]
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
	threads := flag.Int("threads", 0, "PyTorch threads per Demucs segment (0: OMP_NUM_THREADS, or one per core)")
	jobs := flag.Int("jobs", 0, "segments Demucs separates at once (its --jobs), each on --threads threads (0: one at a time)")
	backlogThreads := flag.Int("backlog-threads", 0, "if > 0, a second, niced lane on this many threads splits recordings again")
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
	split := stems.Splitter{Demucs: strings.Fields(*demucs), Model: *model, Threads: *threads, Jobs: *jobs, Bitrate: *bitrate}
	w := worker{st: st, model: *model, split: split, timeout: *timeout, once: *once}
	if *backlogThreads <= 0 || *once {
		w.lane = store.LaneAll
		w.run(ctx)
		return
	}
	// Two lanes: uploads at once with every thread; the splits done again
	// beside them, niced (the kernel gives an upload's split the cores it
	// wants) and on few threads.
	up := w
	up.lane = store.LaneNew
	back := w
	back.lane = store.LaneBacklog
	back.split.Demucs = niced(split.Demucs)
	back.split.Threads, back.split.Jobs = *backlogThreads, 0
	log.Printf("stemsd: uploads on %d x %d threads, the backlog niced on %d", *threads, max(*jobs, 1), *backlogThreads)
	done := make(chan struct{})
	go func() {
		back.run(ctx)
		close(done)
	}()
	up.run(ctx)
	<-done
}

// niced runs a command at the lowest CPU priority.
func niced(cmd []string) []string {
	if len(cmd) == 0 {
		return nil
	}
	return append([]string{"nice", "-n", "19"}, cmd...)
}

// worker is one lane: it takes its lane's splits.
type worker struct {
	st      *store.Store
	lane    store.StemLane
	model   string
	split   stems.Splitter
	timeout time.Duration
	once    bool
}

func (w worker) run(ctx context.Context) {
	for ctx.Err() == nil {
		job, err := w.st.ClaimStemJob(ctx, w.model, w.lane)
		if err != nil {
			log.Printf("stemsd: claiming a job: %v", err)
			sleep(ctx, 30*time.Second)
			continue
		}
		if job == nil {
			if w.once {
				return
			}
			if err := w.st.WaitForStemJob(ctx, time.Minute); err != nil && ctx.Err() == nil {
				log.Printf("stemsd: waiting: %v", err)
				sleep(ctx, 30*time.Second)
			}
			continue
		}
		w.process(ctx, *job)
	}
}

func (w worker) process(ctx context.Context, job store.StemJob) {
	st := w.st
	start := time.Now()
	audio, _, err := st.GetSongAudioData(ctx, job.SongAudioID)
	if err != nil {
		fail(st, job, err)
		return
	}
	jctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()
	parts, err := w.split.Split(jctx, audio)
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
	log.Printf("stemsd: recording %d split in %s (%d KB, %s lane)", job.SongAudioID, time.Since(start).Round(time.Second), size>>10, w.lane)
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
