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

// refLabelNow fills catalogue song songID's reference label from its
// reference's title (reftitle.Label: the wrapping and what repeats the
// song taken out) when it has a reference and no label — at a new
// catalogue song or a new reference, never over a label someone typed
// (store.FillReferenceLabel). The title kept for that link is used, else
// it is fetched now (a few seconds at most, reftitle.Timeout) and kept.
// It returns the label set, "" for none; a failure is only logged.
func (s *Server) refLabelNow(ctx context.Context, songID int64) string {
	c, err := s.store.ReferenceLabelCandidate(ctx, songID)
	if err != nil {
		log.Printf("reference label of song %d: %v", songID, err)
	}
	if c == nil {
		return ""
	}
	title := c.Stored.For(c.ReferenceURL)
	if title == "" && c.Stored.Due(c.ReferenceURL, 0, time.Now()) {
		title = s.fetchRefTitle(ctx, songID, c.ReferenceURL)
	}
	label := reftitle.Label(title, c.Title, c.Artist)
	if label == "" {
		return ""
	}
	ok, err := s.store.FillReferenceLabel(context.WithoutCancel(ctx), songID, c.ReferenceURL, label)
	if err != nil || !ok {
		if err != nil {
			log.Printf("reference label of song %d: %v", songID, err)
		}
		return ""
	}
	return label
}
