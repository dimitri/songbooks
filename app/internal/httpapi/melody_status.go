package httpapi

import (
	"context"
	"math"
	"net/http"
	"time"

	"app/internal/audioanalysis"
	"app/internal/i18n"
	"app/internal/song"
	"app/internal/store"
)

// Where a song's melody stands while its Melody view can't show it yet
// (docs/melody-sheet.md, "Until it's ready"): the view is always on the
// song page, disabled with the reason and how far along it is until the
// melody can be drawn.
//
//	GET /api/songs/{id}/melody/status   → {state, message, progress, eta, etaSource, position, canRetry, poll}
//
// The view asks it again while the state can still change (poll: when),
// and draws the melody as soon as it's "ready", without a reload.

// melodyStatusResponse is a song's melody's state:
//
//	none         no recording a melody can come from
//	unavailable  not split, and no stems worker at work on this server
//	queued       its split is waiting (position: splits before it)
//	running      its split is being made (progress, from the worker)
//	failed       its split failed for good (canRetry: an editor may queue it again)
//	notes        the melody's notes are being found in the voice
//	analysis     the recording's beats and bars are being found (no sync saved)
//	unsung       too little singing to write a melody
//	sync         no bars for the recording: save its sync
//	ready        the melody can be drawn
type melodyStatusResponse struct {
	State   string `json:"state"`
	Message string `json:"message"`
	// Progress 0..1 (queued and running), ETA in seconds until the
	// melody can be drawn; ETASource: "worker" (from the worker's
	// progress) or "estimate" (the recording's size × the worker's speed
	// on its recent splits).
	Progress  *float64 `json:"progress,omitempty"`
	ETA       *int     `json:"eta,omitempty"`
	ETASource string   `json:"etaSource,omitempty"`
	Position  int      `json:"position,omitempty"`
	Category  string   `json:"category,omitempty"`
	CanRetry  bool     `json:"canRetry,omitempty"`
	// Poll: when to ask again, in seconds; 0: the state won't change by
	// itself.
	Poll int `json:"poll,omitempty"`
}

// melodyStateDone: states that don't change by themselves.
var melodyStateDone = map[string]bool{"none": true, "unavailable": true, "failed": true, "unsung": true, "sync": true, "ready": true}

// melodyProgress reads where the melody stands from its status, without
// the bars (ok: the notes are there; whether there are bars for them is
// the caller's to find). now is the database's clock.
func melodyProgress(m *store.MelodyStatus) melodyStatusResponse {
	var out melodyStatusResponse
	if m == nil {
		out.State = "none"
		return out
	}
	out.Category = string(m.Category)
	now := m.Now
	if now.IsZero() {
		now = time.Now()
	}
	est := func(bytes int64) float64 { return m.SecondsPerMB * float64(bytes) / 1e6 }
	// What's left of a running split: from its progress when the worker
	// reports it (Demucs's share, plus the parts' encoding after it),
	// else from its estimated length.
	left := func(started time.Time, bytes int64, p *float64) (float64, bool) {
		elapsed := math.Max(0, now.Sub(started).Seconds())
		if p != nil && *p >= 0.03 {
			total := elapsed / *p
			return total*(1-*p) + math.Max(10, 0.08*total), true
		}
		if e := est(bytes); e > 0 {
			return math.Max(15, e-elapsed), false
		}
		return 0, false
	}
	setETA := func(s float64, source string) {
		if s <= 0 {
			return
		}
		v := int(math.Round(s))
		out.ETA, out.ETASource = &v, source
	}
	notes := m.NotesSeconds
	if notes <= 0 {
		notes = 5
	}
	alive := workerAlive(m)
	switch {
	case m.Split && m.Found && m.OK:
		out.State = "ready"
	case m.Split && m.Found:
		out.State = "unsung"
	case m.Split:
		out.State, out.Poll = "notes", 5
		setETA(notes*float64(1+m.NotesAhead), "estimate")
	case m.Job == "failed":
		out.State = "failed"
	case !alive:
		out.State = "unavailable"
	case m.Job == "running":
		out.State, out.Poll = "running", 5
		s, worker := left(m.Started, m.ByteSize, m.Progress)
		switch {
		case worker:
			p := math.Round(*m.Progress*1000) / 1000
			out.Progress = &p
			setETA(s+notes, "worker")
		case s > 0:
			// No report: the time it's had of what it should take.
			e := est(m.ByteSize)
			p := math.Round(math.Min(0.95, now.Sub(m.Started).Seconds()/e)*1000) / 1000
			out.Progress = &p
			setETA(s+notes, "estimate")
		}
	default:
		// Queued (or no job yet, which the worker takes as queued).
		out.State, out.Poll = "queued", 15
		out.Position = m.Ahead
		if m.SecondsPerMB > 0 {
			wait := est(m.AheadBytes) + est(m.ByteSize) + notes
			for _, r := range m.AheadRunning {
				s, _ := left(r.Started, r.ByteSize, r.Progress)
				wait += s
			}
			setETA(wait, "estimate")
		}
	}
	return out
}

// workerAlive: a stems worker was at work lately on this server (it
// looks for a job every minute; a split runs at most its 30 min timeout).
func workerAlive(m *store.MelodyStatus) bool {
	now := m.Now
	if now.IsZero() {
		now = time.Now()
	}
	return !m.WorkerSeen.IsZero() && now.Sub(m.WorkerSeen) < store.StemWorkerFresh ||
		m.Job == "running" && now.Sub(m.Started) < 40*time.Minute
}

// melodyMessage is the state's one line, in lang.
func melodyMessage(lang string, r melodyStatusResponse) string {
	T := func(msg string, args ...any) string { return i18n.T(lang, msg, args...) }
	eta := ""
	if r.ETA != nil {
		eta = " " + etaText(lang, *r.ETA, r.ETASource == "estimate")
	}
	switch r.State {
	case "none":
		return T("Melody: this song has no reference recording to find it in. Add one (YouTube or the band’s).")
	case "unavailable":
		return T("Melody: voice separation isn’t available on this server, so the melody can’t be found here.")
	case "queued":
		if r.Position > 0 {
			return i18n.N(lang, r.Position, "Melody: voice separation queued, %d recording before this one.", "Melody: voice separation queued, %d recordings before this one.") + eta
		}
		return T("Melody: voice separation queued, next in line.") + eta
	case "running":
		if r.Progress != nil {
			return T("Melody: separating the voice from the recording, %d%%.", int(math.Round(*r.Progress*100))) + eta
		}
		return T("Melody: separating the voice from the recording.") + eta
	case "failed":
		return T("Melody: voice separation failed for this recording.")
	case "notes":
		return T("Melody: finding the melody in the voice.") + eta
	case "analysis":
		return T("Melody: finding the recording’s beats and bars.")
	case "unsung":
		return T("Melody: too little singing in this recording to write a melody.")
	case "sync":
		return T("Melody: available once the recording is synced with the chart.")
	}
	return ""
}

// etaText is the wait in words: "About 4 min left." (estimated: "…
// (estimated).").
func etaText(lang string, s int, estimated bool) string {
	T := func(msg string, args ...any) string { return i18n.T(lang, msg, args...) }
	min, h := int(math.Round(float64(s)/60)), int(math.Round(float64(s)/3600))
	switch {
	case s < 60 && estimated:
		return T("Less than a minute left (estimated).")
	case s < 60:
		return T("Less than a minute left.")
	case s < 90*60 && estimated:
		return T("About %d min left (estimated).", min)
	case s < 90*60:
		return T("About %d min left.", min)
	case estimated:
		return T("About %d h left (estimated).", h)
	}
	return T("About %d h left.", h)
}

// melodyStatus is where the song's melody stands. full: whether there
// are bars for its notes is found too (the recording's sync, else its
// analysis laid on the chart): the API's answer; the song page's first
// drawing skips it ("ready" then means the notes are there).
func (s *Server) melodyStatus(ctx context.Context, variantID int64, full bool) (melodyStatusResponse, error) {
	m, err := s.store.GetMelodyStatus(ctx, variantID, audioanalysis.MelodyNotesAnalyzer)
	if err != nil {
		return melodyStatusResponse{}, err
	}
	out := melodyProgress(m)
	if out.State != "ready" || !full {
		return out, nil
	}
	sm, err := s.store.GetSongMelody(ctx, variantID, audioanalysis.MelodyNotesAnalyzer)
	if err != nil || sm == nil || !sm.OK {
		return out, err
	}
	_, _, _, body, err := s.store.VariantContent(ctx, variantID)
	if err != nil {
		return out, err
	}
	sg := song.Parse(body)
	an, err := s.store.GetAnalysis(ctx, sm.AudioID)
	if err != nil {
		return out, err
	}
	g, _, _, err := s.melodyGrid(ctx, sm, sg, buildSections(sg), body, an.Analysis)
	if err != nil || g != nil {
		return out, err
	}
	// No bars: the recording's analysis may still give them, when a
	// worker is there to make it.
	if (an.Status == "queued" || an.Status == "running") && an.Analysis == nil && workerAlive(m) {
		out.State, out.Poll = "analysis", 15
		return out, nil
	}
	out.State = "sync"
	return out, nil
}

// handleGetSongMelodyStatus is GET /api/songs/{id}/melody/status, to
// anyone who may see the song.
func (s *Server) handleGetSongMelodyStatus(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	out, err := s.melodyStatus(r.Context(), variantID, true)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.finishMelodyStatus(r, variantID, &out)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

// finishMelodyStatus says the state in the requester's language, and
// whether they may queue a failed split again (who edits the song).
func (s *Server) finishMelodyStatus(r *http.Request, variantID int64, out *melodyStatusResponse) {
	if out.State == "failed" {
		if p := personFromContext(r); p != nil {
			out.CanRetry, _ = s.store.CanEditVariant(r.Context(), p.ID, variantID)
		}
	}
	out.Message = melodyMessage(s.langOfRequest(r), *out)
}
