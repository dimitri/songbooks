// stemsd splits songs' reference recordings into their parts in the
// background (internal/stems, Demucs), one at a time: every recording
// added or replaced is queued by the database (migration 0078), and the
// recordings already there were queued when it ran. It runs in its own
// container, which has Python, PyTorch and Demucs (deploy/Dockerfile.stems).
//
// Right after a split, while it still has the WAVs, it listens to the
// recording (the analysis stage, migration 0106): beats, downbeats,
// metre, tempo, chords and where the voice is, from Beat This! and
// lv-chordia (deploy/analysis/sbanalyze.py) and internal/audioanalysis.
// A recording split before, or whose analysis was queued again, is
// analysed from its stored parts once no split is waiting. Its parts are
// saved before it's listened to: the player has them a minute sooner.
//
// A recording added or replaced (no parts: someone is waiting) goes
// before one split again. With --backlog-threads, a second lane takes
// the splits done again and the analyses queued apart, niced and on few
// threads, so that an upload never waits behind a backfill; without, one
// lane does everything, uploads first.
//
//	stemsd --db URL [--model htdemucs] [--threads 4 --jobs 4] [--backlog-threads 2] [--bitrate 128]
//	stemsd --db URL --once            # split and analyse what's queued, then stop
//	stemsd --db URL --reanalyze all   # queue every split recording's analysis again
//	stemsd --db URL --reanalyze 42    # one recording (song_audio id)
//	stemsd --analyze-files mix.wav,no_vocals.wav,vocals.wav [--raw out.json]
//	                                  # no database: print the analysis
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"app/internal/chartalign"
	"app/internal/stems"
	"app/internal/store"
)

func main() {
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "database connection string")
	demucs := flag.String("demucs", "demucs", "the command that runs Demucs (words split on spaces)")
	model := flag.String("model", "htdemucs", "the Demucs model")
	threads := flag.Int("threads", 0, "PyTorch threads per Demucs segment; the analysis gets threads x jobs (0: OMP_NUM_THREADS, or one per core)")
	jobs := flag.Int("jobs", 0, "segments Demucs separates at once (its --jobs), each on --threads threads (0: one at a time)")
	backlogThreads := flag.Int("backlog-threads", 0, "if > 0, a second, niced lane on this many threads splits recordings again and runs the queued analyses")
	bitrate := flag.Int("bitrate", 128, "the parts' MP3 bitrate, in kbit/s")
	timeout := flag.Duration("timeout", 30*time.Minute, "the longest one recording may take")
	once := flag.Bool("once", false, "split and analyse what's queued, then stop")
	analyze := flag.String("analyze", "", "the command that runs sbanalyze.py (words split on spaces); empty: no analysis")
	reanalyze := flag.String("reanalyze", "", `queue analyses again: "all" (every split recording not analysed by the current models), "force" (every one), or a song_audio id; then stop`)
	files := flag.String("analyze-files", "", "mix.wav[,no_vocals.wav[,vocals.wav]]: analyse these files, print the result, stop (no database)")
	rawOut := flag.String("raw", "", "with --analyze-files: read the models' output from this file instead of running them")
	flag.Parse()

	// The analysis runs once Demucs is done: it may have all its threads
	// (on 16, a 3:30 song's in 75 s; 103 s on 4).
	an := stems.Analyzer{Command: strings.Fields(*analyze), Threads: *threads * max(*jobs, 1)}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *files != "" {
		if err := analyzeFiles(ctx, an, *files, *rawOut); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *dbURL == "" {
		log.Fatal("stemsd: --db (or DATABASE_URL) is required")
	}
	st, err := store.New(ctx, *dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if *reanalyze != "" {
		if err := queueAnalyses(ctx, st, an, *reanalyze); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := st.ResetRunningStemJobs(ctx); err != nil {
		log.Fatal(err)
	}
	if err := st.ResetRunningAnalysisJobs(ctx); err != nil {
		log.Fatal(err)
	}
	split := stems.Splitter{Demucs: strings.Fields(*demucs), Model: *model, Threads: *threads, Jobs: *jobs, Bitrate: *bitrate}
	w := worker{st: st, model: *model, split: split, an: an, timeout: *timeout, once: *once}
	if len(an.Command) > 0 {
		w.analyzer = "sbanalyze"
	}
	if *backlogThreads <= 0 || *once {
		w.lane = store.LaneAll
		w.run(ctx)
		return
	}
	// Two lanes: uploads at once with every thread; the rest beside them,
	// niced (the kernel gives an upload's split the cores it wants) and
	// on few threads.
	up := w
	up.lane = store.LaneNew
	back := w
	back.lane = store.LaneBacklog
	back.split.Demucs = niced(split.Demucs)
	back.split.Threads, back.split.Jobs = *backlogThreads, 0
	back.an.Command = niced(an.Command)
	back.an.Threads = *backlogThreads
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

// worker is one lane: it takes its lane's splits, and, but for the
// uploads' lane, the analyses queued apart.
type worker struct {
	st       *store.Store
	lane     store.StemLane
	model    string
	split    stems.Splitter
	an       stems.Analyzer
	analyzer string // "" when there's no analysis
	timeout  time.Duration
	once     bool
}

func (w worker) run(ctx context.Context) {
	for ctx.Err() == nil {
		job, err := w.st.ClaimStemJob(ctx, w.model, w.lane)
		if err != nil {
			log.Printf("stemsd: claiming a job: %v", err)
			sleep(ctx, 30*time.Second)
			continue
		}
		if job != nil {
			w.process(ctx, *job)
			continue
		}
		// Splits first: a new recording's parts shouldn't wait for a
		// backfill of analyses.
		if w.analyzer != "" && w.lane != store.LaneNew {
			aj, err := w.st.ClaimAnalysisJob(ctx, w.analyzer)
			if err != nil {
				log.Printf("stemsd: claiming an analysis: %v", err)
				sleep(ctx, 30*time.Second)
				continue
			}
			if aj != nil {
				w.analyzeStored(ctx, *aj)
				continue
			}
		}
		if w.once {
			return
		}
		if err := w.st.WaitForStemJob(ctx, time.Minute); err != nil && ctx.Err() == nil {
			log.Printf("stemsd: waiting: %v", err)
			sleep(ctx, 30*time.Second)
		}
	}
}

// process splits a recording and saves its parts, then, while it still
// has the WAVs, analyses it (the job saving the parts queued).
func (w worker) process(ctx context.Context, job store.StemJob) {
	st := w.st
	start := time.Now()
	audio, _, err := st.GetSongAudioData(ctx, job.SongAudioID)
	if err != nil {
		fail(st, job, err)
		return
	}
	dir, err := os.MkdirTemp("", "stems-")
	if err != nil {
		fail(st, job, err)
		return
	}
	defer os.RemoveAll(dir)
	jctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()
	parts, wavs, err := w.split.SplitIn(jctx, dir, audio)
	if ctx.Err() != nil {
		return // stopping: the job goes back to the queue at the next start
	}
	if err != nil {
		fail(st, job, err)
		return
	}
	// Saved first, the analysis claimed with them: the player has the
	// parts while the recording is listened to.
	saved, aj, err := st.SaveStemsClaimAnalysis(ctx, job, "audio/mpeg", parts, w.analyzer)
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
	if aj == nil {
		return // no analysis here, or it wasn't queued
	}
	w.analyze(ctx, jctx, *aj, wavs, time.Now())
}

// analyzeStored analyses a recording split earlier, from its stored
// recording and parts.
func (w worker) analyzeStored(ctx context.Context, job store.AnalysisJob) {
	st := w.st
	start := time.Now()
	mix, _, err := st.GetSongAudioData(ctx, job.SongAudioID)
	if err != nil {
		failAnalysis(st, job, err)
		return
	}
	backing, _, _, err := st.GetStemData(ctx, job.SongAudioID, "no_vocals")
	if err != nil {
		failAnalysis(st, job, fmt.Errorf("no backing part: %w", err))
		return
	}
	vocals, _, _, _ := st.GetStemData(ctx, job.SongAudioID, "vocals")
	dir, err := os.MkdirTemp("", "analysis-")
	if err != nil {
		failAnalysis(st, job, err)
		return
	}
	defer os.RemoveAll(dir)
	jctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()
	wavs, err := stems.DecodeWAVs(jctx, dir, mix, backing, vocals)
	if err != nil {
		if ctx.Err() == nil {
			failAnalysis(st, job, err)
		}
		return
	}
	w.analyze(ctx, jctx, job, wavs, start)
}

// analyze runs the analysis of a claimed job on its WAVs and saves it.
func (w worker) analyze(ctx, jctx context.Context, job store.AnalysisJob, wavs stems.WAVs, start time.Time) {
	rec, err := w.an.Analyze(jctx, wavs)
	if err == nil {
		var saved bool
		if saved, err = w.st.SaveAnalysis(ctx, job.SongAudioID, job.SHA256, rec); err == nil {
			if saved {
				logAnalysis(job.SongAudioID, rec, time.Since(start))
			}
			return
		}
	}
	if ctx.Err() != nil {
		return // stopping: the job goes back to the queue at the next start
	}
	failAnalysis(w.st, job, err)
}

func failAnalysis(st *store.Store, job store.AnalysisJob, err error) {
	log.Printf("stemsd: analysing recording %d (try %d): %v", job.SongAudioID, job.Attempts, err)
	msg := err.Error()
	if len(msg) > 4000 {
		msg = msg[len(msg)-4000:]
	}
	if err := st.FailAnalysisJob(context.Background(), job.SongAudioID, job.SHA256, msg); err != nil {
		log.Printf("stemsd: recording the failure: %v", err)
	}
}

func logAnalysis(id int64, r *chartalign.Recording, took time.Duration) {
	log.Printf("stemsd: recording %d analysed in %s: %.1f bpm (cv %.3f), %s (%.2f), beats from %s, %d chords, key %s",
		id, took.Round(time.Second), r.Tempo, r.TempoCV, r.TimeSig, r.TimeSigConf, r.BeatSource, len(r.Chords), r.Key)
}

func queueAnalyses(ctx context.Context, st *store.Store, an stems.Analyzer, what string) error {
	switch what {
	case "all", "force":
		analyzer := ""
		if what == "all" {
			// Not analysed by the models installed here: ask them their name.
			if len(an.Command) > 0 {
				analyzer = currentAnalyzer(ctx, an)
			}
		}
		n, err := st.QueueAllAnalysisJobs(ctx, analyzer)
		if err != nil {
			return err
		}
		log.Printf("stemsd: %d recordings queued for analysis", n)
		return nil
	}
	id, err := strconv.ParseInt(what, 10, 64)
	if err != nil {
		return fmt.Errorf(`--reanalyze: "all", "force" or a recording's id, not %q`, what)
	}
	return st.QueueAnalysisJob(ctx, id)
}

// currentAnalyzer is the name sbanalyze.py gives its models ("" when it
// can't say: then everything is queued).
func currentAnalyzer(ctx context.Context, an stems.Analyzer) string {
	cmd := append([]string{}, an.Command...)
	cmd = append(cmd, "--name")
	out, err := execOutput(ctx, cmd)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func execOutput(ctx context.Context, cmd []string) (string, error) {
	out, err := exec.CommandContext(ctx, cmd[0], cmd[1:]...).Output()
	return string(out), err
}

func analyzeFiles(ctx context.Context, an stems.Analyzer, files, rawPath string) error {
	f := strings.Split(files, ",")
	w := stems.WAVs{Mix: f[0]}
	if len(f) > 1 {
		w.Backing = f[1]
	}
	if len(f) > 2 {
		w.Vocals = f[2]
	}
	var raw chartalign.Raw
	if rawPath != "" {
		b, err := os.ReadFile(rawPath)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return err
		}
	} else {
		var err error
		if raw, err = an.Raw(ctx, w); err != nil {
			return err
		}
	}
	rec, err := stems.Summarize(ctx, raw, w)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(rec)
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
