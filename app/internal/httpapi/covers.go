package httpapi

import (
	"context"
	"log"
	"sync"
	"time"

	"app/internal/covers"
)

// Covers looks songs' covers up (docs/covers.md); cmd/songbooks sets it,
// nil (tests) never looks anything up.
var Covers *covers.Finder

// coverFills is the songbooks whose covers are being looked for.
var coverFills sync.Map

// fillCovers looks for the covers of a songbook's songs not looked for
// yet, in the background, once at a time per songbook: a page shows the
// covers there are, the next one shows more.
func (s *Server) fillCovers(songbookID int64) {
	if Covers == nil {
		return
	}
	if _, busy := coverFills.LoadOrStore(songbookID, true); busy {
		return
	}
	go func() {
		defer coverFills.Delete(songbookID)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		found, missing, err := s.store.FillCovers(ctx, Covers, songbookID, false, 3*time.Second, nil)
		if err != nil {
			log.Printf("covers of songbook %d: %v", songbookID, err)
		}
		if found+missing > 0 {
			log.Printf("covers of songbook %d: %d found, %d not", songbookID, found, missing)
		}
	}()
}
