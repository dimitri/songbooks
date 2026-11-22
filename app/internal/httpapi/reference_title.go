package httpapi

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"app/internal/reftitle"
)

// RefTitles finds the titles of songs' references (internal/reftitle);
// cmd/songbooks sets it, nil (tests) never fetches anything.
var RefTitles *reftitle.Fetcher

// refTitleRetry is how long a link whose title wasn't found waits before
// it's asked again.
const refTitleRetry = 6 * time.Hour

// refTitleFills is the songs whose reference's title is being fetched.
var refTitleFills sync.Map

// fillRefTitle fetches the title of a song's reference in the background,
// once at a time per song, and keeps it (store.SetReferenceTitle): the
// page that asked shows the link's site, the next one shows the title. A
// page never waits for it.
func (s *Server) fillRefTitle(variantID int64, ref string) {
	if RefTitles == nil || strings.TrimSpace(ref) == "" {
		return
	}
	if _, busy := refTitleFills.LoadOrStore(variantID, true); busy {
		return
	}
	go func() {
		defer refTitleFills.Delete(variantID)
		s.fetchRefTitle(context.Background(), variantID, ref)
	}()
}

// fetchRefTitle fetches and keeps the title of a song's reference link
// ref, "" when none was found (kept too, so it isn't asked again at once).
func (s *Server) fetchRefTitle(ctx context.Context, variantID int64, ref string) string {
	if RefTitles == nil || strings.TrimSpace(ref) == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, reftitle.Timeout+time.Second)
	defer cancel()
	title, err := RefTitles.Title(ctx, ref)
	if err != nil {
		log.Printf("reference title of song %d: %v", variantID, err)
	}
	if err := s.store.SetReferenceTitle(context.WithoutCancel(ctx), variantID, ref, title); err != nil {
		log.Printf("reference title of song %d: %v", variantID, err)
	}
	return title
}
