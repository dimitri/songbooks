package httpapi

import (
	"context"
	"log"
	"time"

	"app/internal/audioanalysis"
	"app/internal/store"
)

// Each recording's melody range (docs/coaching.md, "The song's melody
// over the student's range"): found in the background by the app, one
// vocals part at a time, once per split (store.NextMelodyRange), never
// while a page waits. cmd/songbooks starts it.

// melodyAnalyzer finds a vocals part's range; audioanalysis's, a fake in
// tests.
type melodyAnalyzer func(ctx context.Context, vocals []byte) (audioanalysis.MelodyRange, bool, error)

// melodyIdle is how long the finder rests when there's nothing to do: a
// recording split since is found within that.
const melodyIdle = 2 * time.Minute

// RunMelodyRanges finds the melody ranges not found yet, until ctx ends.
func RunMelodyRanges(ctx context.Context, st *store.Store) {
	for {
		did, err := findMelodyRange(ctx, st, audioanalysis.AnalyzeMelodyRange)
		if err != nil {
			log.Printf("melody ranges: %v", err)
		}
		if did && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(melodyIdle):
		}
	}
}

// findMelodyRange finds one: false when there was none to find. A part
// that can't be read is kept as "no range", so it isn't tried again until
// it's split again.
func findMelodyRange(ctx context.Context, st *store.Store, analyze melodyAnalyzer) (bool, error) {
	j, err := st.NextMelodyRange(ctx, audioanalysis.MelodyRangeAnalyzer)
	if err != nil || j == nil {
		return false, err
	}
	start := time.Now()
	var r audioanalysis.MelodyRange
	ok := false
	vocals, _, _, err := st.GetStemData(ctx, j.SongAudioID, "vocals")
	if err != nil {
		return false, err // split again meanwhile, or the database: next time
	}
	actx, cancel := context.WithTimeout(ctx, audioanalysis.DecodeTimeout)
	r, ok, err = analyze(actx, vocals)
	cancel()
	if err != nil {
		log.Printf("melody range of recording %d: %v", j.SongAudioID, err)
		ok = false
	}
	took := time.Since(start).Seconds()
	if err := st.SaveMelodyRange(ctx, *j, audioanalysis.MelodyRangeAnalyzer, ok, r.Low, r.High, r.Voiced, took); err != nil {
		return false, err
	}
	log.Printf("melody range of recording %d: %v %d–%d (%.0f s sung), in %.1f s", j.SongAudioID, ok, r.Low, r.High, r.Voiced, took)
	return true, nil
}
