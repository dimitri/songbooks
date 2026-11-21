// stemsd splits songs' reference recordings into their parts in the
// background (internal/stems, Demucs), one at a time: every recording
// added or replaced is queued by the database (migration 0078), and the
// recordings already there were queued when it ran. It runs in its own
// container, which has Python, PyTorch and Demucs (deploy/Dockerfile.stems).
//
// Right after a split, its parts saved, while it still has the WAVs, it
// listens to the recording (the analysis stage, migration 0106, on its
// own --analysis-timeout): beats, downbeats,
// metre, tempo, chords and where the voice is, from Beat This! and
// lv-chordia (deploy/analysis/sbanalyze.py) and internal/audioanalysis.
// A recording split before, or whose analysis was queued again, is
// analysed from its stored parts once no split is waiting.
//
//	stemsd --db URL [--model htdemucs] [--threads 8] [--bitrate 128]
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
	threads := flag.Int("threads", 0, "threads Demucs and the analysis may use (0: their default)")
	bitrate := flag.Int("bitrate", 128, "the parts' MP3 bitrate, in kbit/s")
	timeout := flag.Duration("timeout", 30*time.Minute, "the longest one recording's split may take")
	flag.DurationVar(&analysisTimeout, "analysis-timeout", analysisTimeout, "the longest one recording's analysis may take")
	once := flag.Bool("once", false, "split and analyse what's queued, then stop")
	analyze := flag.String("analyze", "", "the command that runs sbanalyze.py (words split on spaces); empty: no analysis")
	reanalyze := flag.String("reanalyze", "", `queue analyses again: "all" (every split recording not analysed by the current models), "force" (every one), or a song_audio id; then stop`)
	files := flag.String("analyze-files", "", "mix.wav[,no_vocals.wav[,vocals.wav]]: analyse these files, print the result, stop (no database)")
	rawOut := flag.String("raw", "", "with --analyze-files: read the models' output from this file instead of running them")
	flag.Parse()

	an := stems.Analyzer{Command: strings.Fields(*analyze), Threads: *threads}
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
	split := stems.Splitter{Demucs: strings.Fields(*demucs), Model: *model, Threads: *threads, Bitrate: *bitrate}
	analyzer := ""
	if len(an.Command) > 0 {
		analyzer = "sbanalyze"
	}

	for ctx.Err() == nil {
		// Splits first: a new recording's parts shouldn't wait for a
		// backfill of analyses.
		job, err := st.ClaimStemJob(ctx, *model)
		if err != nil {
			log.Printf("stemsd: claiming a job: %v", err)
			sleep(ctx, 30*time.Second)
			continue
		}
		if job != nil {
			process(ctx, st, split, an, *job, *timeout)
			continue
		}
		if len(an.Command) > 0 {
			aj, err := st.ClaimAnalysisJob(ctx, analyzer)
			if err != nil {
				log.Printf("stemsd: claiming an analysis: %v", err)
				sleep(ctx, 30*time.Second)
				continue
			}
			if aj != nil {
				analyzeStored(ctx, st, an, *aj, analysisTimeout)
				continue
			}
		}
		if *once {
			return
		}
		if err := st.WaitForStemJob(ctx, time.Minute); err != nil && ctx.Err() == nil {
			log.Printf("stemsd: waiting: %v", err)
			sleep(ctx, 30*time.Second)
		}
	}
}

func process(ctx context.Context, st *store.Store, split stems.Splitter, an stems.Analyzer, job store.StemJob, timeout time.Duration) {
	start := time.Now()
	audio, _, err := st.GetSongAudioData(ctx, job.SongAudioID)
	if err != nil {
		fail(st, job, err)
		return
	}
	jctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// The parts are saved as soon as they're made (the player shows them,
	// and their saving queues the analysis); then, while the WAVs are
	// still there, the recording is analysed, on its own clock: a hung
	// analysis never holds the parts back.
	var saveErr error
	saved := false
	then := func(parts map[string][]byte, w stems.WAVs) error {
		if saved, saveErr = st.SaveStems(ctx, job, "audio/mpeg", parts); saveErr != nil || !saved {
			return nil
		}
		size := 0
		for _, p := range parts {
			size += len(p)
		}
		log.Printf("stemsd: recording %d split in %s (%d KB)", job.SongAudioID, time.Since(start).Round(time.Second), size>>10)
		if len(an.Command) > 0 {
			analyzeSplit(ctx, st, an, job, w, analysisTimeout)
		}
		return nil
	}
	_, err = split.SplitThen(jctx, audio, then)
	if ctx.Err() != nil {
		return // stopping: the job goes back to the queue at the next start
	}
	if err == nil {
		err = saveErr
	}
	if err != nil {
		fail(st, job, err)
		return
	}
	if !saved {
		log.Printf("stemsd: recording %d was replaced while it was split; its new version is queued", job.SongAudioID)
	}
}

// analysisTimeout is the longest one recording's analysis may take
// (--analysis-timeout): a few minutes for Beat This! and lv-chordia on a
// song; a hung one is failed and tried again from the stored parts.
var analysisTimeout = 5 * time.Minute

// analyzeSplit analyses a recording just split, from its WAVs: its job
// (queued by the parts' saving) is taken as running, done or failed.
func analyzeSplit(ctx context.Context, st *store.Store, an stems.Analyzer, job store.StemJob, w stems.WAVs, timeout time.Duration) {
	start := time.Now()
	ok, err := st.StartAnalysisJob(ctx, job.SongAudioID, job.SHA256, "sbanalyze")
	if err != nil || !ok {
		if err != nil {
			log.Printf("stemsd: recording %d: starting its analysis: %v", job.SongAudioID, err)
		}
		return // another worker has it, or it's done
	}
	actx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	rec, err := an.Analyze(actx, w)
	if err == nil {
		var saved bool
		if saved, err = st.SaveAnalysis(ctx, job.SongAudioID, job.SHA256, rec); err == nil {
			if saved {
				logAnalysis(job.SongAudioID, rec, time.Since(start))
			}
			return
		}
	}
	if ctx.Err() != nil {
		return // stopping: put back in the queue at the next start
	}
	log.Printf("stemsd: recording %d: analysis: %v", job.SongAudioID, err)
	msg := err.Error()
	if len(msg) > 4000 {
		msg = msg[len(msg)-4000:]
	}
	if err := st.FailAnalysisJob(context.Background(), job.SongAudioID, job.SHA256, msg); err != nil {
		log.Printf("stemsd: recording the failure: %v", err)
	}
}

// analyzeStored analyses a recording split earlier, from its stored
// recording and parts.
func analyzeStored(ctx context.Context, st *store.Store, an stems.Analyzer, job store.AnalysisJob, timeout time.Duration) {
	start := time.Now()
	failed := func(err error) {
		log.Printf("stemsd: analysing recording %d (try %d): %v", job.SongAudioID, job.Attempts, err)
		msg := err.Error()
		if len(msg) > 4000 {
			msg = msg[len(msg)-4000:]
		}
		if err := st.FailAnalysisJob(context.Background(), job.SongAudioID, job.SHA256, msg); err != nil {
			log.Printf("stemsd: recording the failure: %v", err)
		}
	}
	mix, _, err := st.GetSongAudioData(ctx, job.SongAudioID)
	if err != nil {
		failed(err)
		return
	}
	backing, _, _, err := st.GetStemData(ctx, job.SongAudioID, "no_vocals")
	if err != nil {
		failed(fmt.Errorf("no backing part: %w", err))
		return
	}
	vocals, _, _, _ := st.GetStemData(ctx, job.SongAudioID, "vocals")
	dir, err := os.MkdirTemp("", "analysis-")
	if err != nil {
		failed(err)
		return
	}
	defer os.RemoveAll(dir)
	jctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	w, err := stems.DecodeWAVs(jctx, dir, mix, backing, vocals)
	if err == nil {
		var rec *chartalign.Recording
		if rec, err = an.Analyze(jctx, w); err == nil {
			var saved bool
			if saved, err = st.SaveAnalysis(ctx, job.SongAudioID, job.SHA256, rec); err == nil {
				if saved {
					logAnalysis(job.SongAudioID, rec, time.Since(start))
				}
				return
			}
		}
	}
	if ctx.Err() != nil {
		return
	}
	failed(err)
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
