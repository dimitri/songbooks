package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"app/internal/store"
	"app/internal/ytaudio"
)

// The YouTube reference's audio, fetched in the background
// (docs/mp3-reference-audio.md "Automatic"; migration 0140,
// store/youtube_audio_jobs.go). The song's MP3 is what the click, the
// sync, the parts and the melody start from, so it's fetched by itself:
// when the reference is set or changed (the songs trigger), when the song
// is opened without it (the song page, EnsureYouTubeAudio), when an
// editor asks again; by a small pool of workers in the app (yt-dlp is in
// the image), one or two at a time, with yt-dlp's progress for the page.
//
//	GET  /api/songs/{id}/audio/youtube/status  where it is (anyone who may see the song)
//	POST /api/songs/{id}/audio/youtube         fetch it (again): an editor's "Try again", "Again"
//
// Under the video (play.html "yt-audio", static/js/yt-audio.js): getting
// it (a progress bar, the % and the time left), ready (its length; Again
// and Remove for editors), failed (why; Try again and Upload an MP3 for
// editors).

// YouTubeAudioFetch is the background fetch (ytaudio.Fetch); tests set
// their own, never reaching YouTube.
var YouTubeAudioFetch = ytaudio.Fetch

const (
	// ytAudioIdle: a worker with nothing to do looks again this often
	// (a job due after its backoff), or when one is queued (NOTIFY).
	ytAudioIdle = 30 * time.Second
	// ytAudioStale: a running job whose worker said nothing for this long
	// is queued again (a worker gone with its process).
	ytAudioStale = 3 * time.Minute
	// ytAudioHeartbeat: a running job says it's alive this often, even
	// when yt-dlp prints nothing (the conversion to MP3).
	ytAudioHeartbeat = 10 * time.Second
	// ytAudioReportEvery: progress written at most this often.
	ytAudioReportEvery = time.Second
)

// RunYouTubeAudio runs the workers (workers of them, at most 2) until
// ctx ends. At start-up, the jobs a worker was on when the app stopped
// are queued again: this process is the only one fetching.
func RunYouTubeAudio(ctx context.Context, st *store.Store, workers int) {
	if workers <= 0 {
		return
	}
	if workers > 2 {
		workers = 2
	}
	if n, err := st.RequeueYouTubeAudio(ctx, 0); err != nil {
		log.Printf("youtube audio: requeueing: %v", err)
	} else if n > 0 {
		log.Printf("youtube audio: %d fetch(es) cut short by the last stop, queued again", n)
	}
	wake := make(chan struct{}, workers)
	go func() {
		for ctx.Err() == nil {
			if err := st.WaitForYouTubeAudioJob(ctx, time.Minute); err != nil {
				if ctx.Err() != nil {
					return
				}
				time.Sleep(5 * time.Second)
			}
			for i := 0; i < workers; i++ {
				select {
				case wake <- struct{}{}:
				default:
				}
			}
		}
	}()
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				did, err := RunYouTubeAudioJob(ctx, st)
				if err != nil {
					log.Printf("youtube audio: %v", err)
				}
				if did && err == nil {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case <-wake:
				case <-time.After(ytAudioIdle):
				}
				if _, err := st.RequeueYouTubeAudio(ctx, ytAudioStale); err != nil && ctx.Err() == nil {
					log.Printf("youtube audio: requeueing stale jobs: %v", err)
				}
			}
		}()
	}
	wg.Wait()
}

// RunYouTubeAudioJob fetches one job's audio: false when none was due.
func RunYouTubeAudioJob(ctx context.Context, st *store.Store) (bool, error) {
	j, err := st.ClaimYouTubeAudioJob(ctx)
	if err != nil || j == nil {
		return false, err
	}
	start := time.Now()
	fctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Progress, at most every second; and alive every few seconds while
	// yt-dlp is quiet. A job taken away meanwhile (another link, the
	// audio removed) stops the fetch.
	var mu sync.Mutex
	last := time.Time{}
	lastP, lastETA := 0.0, (*int)(nil)
	report := func(force bool) {
		mu.Lock()
		if !force && time.Since(last) < ytAudioReportEvery {
			mu.Unlock()
			return
		}
		last = time.Now()
		p, eta := lastP, lastETA
		mu.Unlock()
		ok, err := st.ReportYouTubeAudioProgress(ctx, j, p, eta)
		if err == nil && !ok {
			cancel()
		}
	}
	beat := time.NewTicker(ytAudioHeartbeat)
	defer beat.Stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-done:
				return
			case <-beat.C:
				report(true)
			}
		}
	}()
	res, err := YouTubeAudioFetch(fctx, j.URL, func(p ytaudio.Progress) {
		mu.Lock()
		lastP = math.Round(p.Fraction*1000) / 1000
		lastETA = nil
		if p.ETA >= 0 {
			e := p.ETA
			lastETA = &e
		}
		mu.Unlock()
		report(false)
	})
	if err == nil && len(res.Data) > maxAudioUpload {
		err = &ytaudio.Error{Kind: ytaudio.KindTooLong, Detail: "more than 50 MB of audio"}
	}
	if err != nil {
		if ctx.Err() != nil {
			return true, ctx.Err() // stopping: queued again at the next start
		}
		e := ytaudio.AsError(err)
		status, ferr := st.FailYouTubeAudioJob(ctx, j, string(e.Kind), e.Detail, e.Transient)
		if ferr != nil {
			return true, ferr
		}
		if status != "" {
			log.Printf("youtube audio: song %d, attempt %d: %s (%s), %s", j.VariantID, j.Attempts, e.Kind, oneLine(e.Detail), status)
		}
		return true, nil
	}
	stored, err := st.FinishYouTubeAudioJob(ctx, j, res.Data, res.Duration)
	if err != nil {
		return true, err
	}
	if stored {
		log.Printf("youtube audio: song %d, %d bytes in %s", j.VariantID, len(res.Data), time.Since(start).Round(time.Second))
	}
	return true, nil
}

func oneLine(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// ytAudioStatus is GET .../audio/youtube/status's answer, and the song
// page's first one (data-status on the "yt-audio" partial).
//
//	none     no YouTube video as the reference (nothing shown)
//	queued   waiting its turn; with reason and retryIn: a try failed in a
//	         way that may pass, the next in retryIn seconds
//	running  being fetched: progress 0..1, eta seconds (when yt-dlp knows)
//	ready    there: duration seconds when known
//	failed   failed for good: reason (age_restricted, unavailable,
//	         blocked, too_long, other)
//	removed  an editor removed it (not fetched again by itself)
//	missing  none and no job (an editor can ask for it)
type ytAudioStatus struct {
	State    string   `json:"state"`
	Progress *float64 `json:"progress,omitempty"`
	ETA      *int     `json:"eta,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	RetryIn  *int     `json:"retryIn,omitempty"`
	Duration *float64 `json:"duration,omitempty"`
	CanEdit  bool     `json:"canEdit,omitempty"`
	// Poll: when to ask again, in seconds; 0: it won't change by itself.
	Poll int `json:"poll,omitempty"`
}

// ytAudioStatusOf reads where the song's YouTube audio is.
func ytAudioStatusOf(s *store.YouTubeAudioState) ytAudioStatus {
	var out ytAudioStatus
	j := s.Job
	now := s.Now
	if now.IsZero() {
		now = time.Now()
	}
	switch {
	case j != nil && j.Status == store.YTAudioRunning:
		out.State, out.Poll = "running", 2
		out.Progress = j.Progress
		if out.Progress == nil {
			z := 0.0
			out.Progress = &z
		}
		out.ETA = j.ETASeconds
	case j != nil && j.Status == store.YTAudioQueued:
		out.State, out.Poll = "queued", 5
		if j.Reason != "" {
			out.Reason = j.Reason
			in := int(math.Max(0, math.Ceil(j.RunAfter.Sub(now).Seconds())))
			out.RetryIn = &in
			out.Poll = 15
		}
	// The latest link asked for failed, and the audio there is (if any)
	// isn't that link's: failed. One fetched again that failed keeps its
	// audio: ready.
	case j != nil && j.Status == store.YTAudioFailed && !(s.HasAudio && s.AudioURL == j.URL):
		out.State, out.Reason = "failed", j.Reason
		if out.Reason == "" {
			out.Reason = string(ytaudio.KindOther)
		}
	case s.HasAudio:
		out.State = "ready"
		out.Duration = s.Duration
	case !s.IsVideo:
		out.State = "none"
	case j != nil && j.Status == store.YTAudioRemoved && j.URL == s.Reference:
		out.State = "removed"
	default:
		out.State = "missing"
	}
	return out
}

// ytAudioForPage is the song page's: the song opened queues its audio
// when it has none (EnsureYouTubeAudio: not after an editor removed it,
// nor within a day of a failure), then where it is, as JSON for the
// page's script; "" when the reference isn't a YouTube video.
func (s *Server) ytAudioForPage(ctx context.Context, variantID int64, canEdit bool) string {
	if variantID == 0 {
		return ""
	}
	if _, err := s.store.EnsureYouTubeAudio(ctx, variantID); err != nil {
		log.Printf("youtube audio: song %d opened: %v", variantID, err)
	}
	state, err := s.store.GetYouTubeAudioState(ctx, variantID)
	if err != nil || !state.IsVideo {
		return ""
	}
	out := ytAudioStatusOf(state)
	out.CanEdit = canEdit
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(b)
}

// handleGetYouTubeAudioStatus is GET /api/songs/{id}/audio/youtube/status,
// to anyone who may see the song; canEdit: whether they may ask again,
// remove it, upload one.
func (s *Server) handleGetYouTubeAudioStatus(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	state, err := s.store.GetYouTubeAudioState(r.Context(), variantID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such song")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := ytAudioStatusOf(state)
	if p := personFromContext(r); p != nil {
		out.CanEdit, _ = s.store.CanEditVariant(r.Context(), p.ID, variantID)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

// handlePostSongAudioYouTube is POST /api/songs/{id}/audio/youtube, for
// an editor: the YouTube audio fetched (again) in the background — the
// song's reference's, or the body's url (a YouTube video: the import
// wizard's Recording step, before the chart naming it is saved). 202
// with where it is (the status API's answer); 409 when there's no video
// to fetch it from.
func (s *Server) handlePostSongAudioYouTube(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}
	state, err := s.store.GetYouTubeAudioState(r.Context(), variantID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	url, trigger := strings.TrimSpace(req.URL), "retry"
	switch {
	case url == "" && !state.IsVideo:
		httpError(w, http.StatusConflict, "this song's reference isn't a YouTube video")
		return
	case url == "":
		url = state.Reference
	case store.YouTubeID(url) == "":
		httpError(w, http.StatusBadRequest, "not a YouTube video link")
		return
	case url != state.Reference:
		trigger = "wizard"
	}
	if _, err := s.store.QueueYouTubeAudio(r.Context(), variantID, url, trigger, actorID(r)); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	state, err = s.store.GetYouTubeAudioState(r.Context(), variantID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := ytAudioStatusOf(state)
	out.CanEdit = true
	writeJSON(w, http.StatusAccepted, out)
}
