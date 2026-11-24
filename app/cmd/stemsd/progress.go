package main

import (
	"context"
	"log"
	"time"

	"app/internal/stems"
	"app/internal/store"
)

// withProgress is ctx with the job's split reporting how far it is
// (Demucs's progress bar, every 5 %), for the song page's Melody view
// (migration 0134). A report that can't be saved is only logged: the
// split goes on.
func withProgress(ctx context.Context, st *store.Store, job store.StemJob) context.Context {
	return stems.WithProgress(ctx, 0.05, func(p float64) {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := st.SetStemJobProgress(sctx, job, p); err != nil {
			log.Printf("stemsd: recording %d: saving its progress: %v", job.SongAudioID, err)
		}
	})
}
