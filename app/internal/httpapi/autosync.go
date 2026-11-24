package httpapi

import (
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"strings"

	"app/internal/chartalign"
	"app/internal/i18n"
	"app/internal/song"
	"app/internal/songimport"
	"app/internal/store"
)

// The recording's own analysis (migration 0106, cmd/stemsd's analysis
// stage) laid on the chart: the Sync sheet's "Analysed" panel.
//
//	POST /api/songs/{id}/audio/{category}/analysis        {text?, hints}
//	POST /api/songs/{id}/audio/{category}/analysis/queue  listen again
//	POST /api/songs/{id}/audio/{category}/analysis/apply  {version, hints, fix, sync, dryRun}

type analysisRequest struct {
	Text  string             `json:"text"`
	Hints chartalign.Hints   `json:"hints"`
	Fix   songimport.AutoFix `json:"fix"`
	// Sync: write the alignment's section starts and per-bar points, as
	// estimates ("auto"), after the fixes.
	Sync    bool   `json:"sync"`
	DryRun  bool   `json:"dryRun"`
	Version string `json:"version"`
	// Replace: with sync, the starts set by ear go too (otherwise they
	// stay and only the others are placed).
	Replace bool `json:"replace"`
}

type analysisResponse struct {
	// Status: the analysis job's ("queued", "running", "done", "failed",
	// "" none yet); StemStatus: the split's, which comes first.
	Status     string               `json:"status"`
	StemStatus string               `json:"stemStatus"`
	Error      string               `json:"error,omitempty"`
	Analyzer   string               `json:"analyzer,omitempty"`
	CreatedAt  string               `json:"createdAt,omitempty"`
	Proposal   *songimport.AutoSync `json:"proposal,omitempty"`
	// While it waits: since when (queued or running), and how many
	// analyses come before it.
	Since string `json:"since,omitempty"`
	Ahead int    `json:"ahead,omitempty"`
}

// analysisOf loads the recording and its analysis; ok false when the
// response is written.
func (s *Server) analysisOf(w http.ResponseWriter, r *http.Request) (*store.SongAudio, store.AnalysisStatus, bool) {
	meta, ok := s.audioMetaFor(w, r)
	if !ok {
		return nil, store.AnalysisStatus{}, false
	}
	st, err := s.store.GetAnalysis(r.Context(), meta.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return nil, st, false
	}
	return meta, st, true
}

func (s *Server) syncSectionsOf(text string) []songimport.SyncSection {
	return SyncSections(song.Parse(text))
}

func (s *Server) localizeAuto(r *http.Request, p *songimport.AutoSync) {
	lang := s.langOfRequest(r)
	p.Localize(func(msg string, args ...any) string { return i18n.T(lang, msg, args...) })
}

// handlePostAudioAnalysis is POST /api/songs/{id}/audio/{category}/analysis:
// the analysis' state and, once there's one, the chart (the editor's
// text, else the saved song) laid on it with the hints given. Nothing is
// written.
func (s *Server) handlePostAudioAnalysis(w http.ResponseWriter, r *http.Request) {
	var req analysisRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	meta, st, ok := s.analysisOf(w, r)
	if !ok {
		return
	}
	out := analysisResponse{Status: st.Status, Error: st.Error, Analyzer: st.Analyzer, Ahead: st.Ahead}
	if !st.Since.IsZero() {
		out.Since = st.Since.UTC().Format("2006-01-02T15:04:05Z")
	}
	if ss, err := s.store.GetStemStatus(r.Context(), meta.ID); err == nil {
		out.StemStatus = ss.Status
	}
	if st.Analysis != nil {
		out.Status = "done"
		out.CreatedAt = st.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")
		text := req.Text
		if strings.TrimSpace(text) == "" {
			_, _, _, body, err := s.store.VariantContent(r.Context(), meta.VariantID)
			if err != nil {
				httpError(w, http.StatusNotFound, err.Error())
				return
			}
			text = body
		}
		out.Proposal = songimport.ProposeAutoSync(st.Analysis, text, s.syncSectionsOf(text), req.Hints)
		s.localizeAuto(r, out.Proposal)
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePostAudioAnalysisQueue is POST .../analysis/queue: listen to the
// recording again (after a model update, or a failure).
func (s *Server) handlePostAudioAnalysisQueue(w http.ResponseWriter, r *http.Request) {
	meta, ok := s.audioMetaFor(w, r)
	if !ok {
		return
	}
	if err := s.store.QueueAnalysisJob(r.Context(), meta.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}

// handlePostAudioAnalysisApply is POST .../analysis/apply: the editor's
// picks among the proposals. dryRun returns the new text and its changed
// lines to review; otherwise the text is saved as a revision (when it
// changed; version must be the saved one) and, with sync, the sections'
// starts and every bar's are written as estimates, from the text as
// saved.
func (s *Server) handlePostAudioAnalysisApply(w http.ResponseWriter, r *http.Request) {
	var req analysisRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	meta, st, ok := s.analysisOf(w, r)
	if !ok {
		return
	}
	if st.Analysis == nil {
		httpError(w, http.StatusConflict, "the recording isn't analysed yet")
		return
	}
	// A Guitar Pro file has its own sync for every bar: the analysis'
	// never replaces it.
	if req.Sync && meta.Category == store.AudioGP {
		httpError(w, http.StatusBadRequest, "a Guitar Pro file has its own sync for every bar")
		return
	}
	title, artist, ref, body, err := s.store.VariantContent(r.Context(), meta.VariantID)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	text, changes := songimport.ApplyAutoFix(st.Analysis, body, s.syncSectionsOf, req.Hints, req.Fix)
	if changes == nil {
		changes = []songimport.Change{}
	}
	if req.DryRun {
		writeJSON(w, http.StatusOK, map[string]any{"text": text, "changes": changes})
		return
	}
	// The sync is the text's as it will be saved; an unsure alignment is
	// never written, not even as estimates.
	var p *songimport.AutoSync
	if req.Sync {
		p = songimport.ProposeAutoSync(st.Analysis, text, s.syncSectionsOf(text), req.Hints)
		if p.State == "unsure" {
			httpError(w, http.StatusConflict, "the recording's analysis is unsure of this chart: place the sections by ear")
			return
		}
	}
	version := req.Version
	if text != body {
		if req.Version == "" {
			httpError(w, http.StatusBadRequest, "version is required to save the chart")
			return
		}
		if version, err = s.store.SaveVariantByID(r.Context(), meta.VariantID, title, artist, ref, text, req.Version, actorID(r)); err != nil {
			httpError(w, http.StatusConflict, err.Error())
			return
		}
	}
	var saved []jsonAudioMark
	kept := 0
	if req.Sync {
		markers, points, k, err := s.mergeAutoSync(r, meta, p, req.Replace)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.store.ReplaceSync(r.Context(), meta.ID, markers, actorID(r), points); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		kept = k
		saved = make([]jsonAudioMark, 0, len(markers))
		for _, m := range markers {
			saved = append(saved, jsonAudioMark{SectionIndex: m.SectionIndex, TimeSeconds: m.TimeSeconds, Origin: m.Origin})
		}
		s.localizeAuto(r, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": text, "version": version, "changes": changes, "proposal": p, "saved": text != body, "markers": saved, "kept": kept})
}

// mergeAutoSync is the sync "Use this sync" writes: the analysis' starts
// as estimates, except (unless replace) the starts a person set, which
// stay — the analysis' starts out of order with them go — and the
// analysis' every bar, unless the per-bar points are already set by
// ear (then they stay) or a start kept disagrees with them (then the
// starts alone place the bars). kept: how many starts set by ear stayed.
func (s *Server) mergeAutoSync(r *http.Request, meta *store.SongAudio, p *songimport.AutoSync, replace bool) ([]store.AudioMarker, *[]store.GPBarSyncPoint, int, error) {
	if replace {
		m, pts, k := mergeSync(nil, nil, p)
		return m, pts, k, nil
	}
	old, err := s.store.ListAudioMarkers(r.Context(), meta.ID)
	if err != nil {
		return nil, nil, 0, err
	}
	pts, err := s.store.ListGPBarSyncPoints(r.Context(), meta.ID)
	if err != nil {
		return nil, nil, 0, err
	}
	m, out, k := mergeSync(old, pts, p)
	return m, out, k, nil
}

// mergeSync is mergeAutoSync on the recording's sync as it is (old
// markers and points).
func mergeSync(old []store.AudioMarker, oldPoints []store.GPBarSyncPoint, p *songimport.AutoSync) ([]store.AudioMarker, *[]store.GPBarSyncPoint, int) {
	set := map[int]store.AudioMarker{}
	pointsSet := false
	for _, m := range old {
		if m.Origin == store.SyncSet {
			set[m.SectionIndex] = m
		}
	}
	for _, pt := range oldPoints {
		pointsSet = pointsSet || pt.Origin == store.SyncSet
	}
	proposed := map[int]float64{}
	for _, m := range p.Markers {
		proposed[m.SectionIndex] = m.TimeSeconds
	}
	var idx []int
	for i := range set {
		idx = append(idx, i)
	}
	for i := range proposed {
		if _, ok := set[i]; !ok {
			idx = append(idx, i)
		}
	}
	slices.Sort(idx)
	var markers []store.AudioMarker
	agree := true
	for _, i := range idx {
		if m, ok := set[i]; ok {
			markers = append(markers, m)
			if t, ok := proposed[i]; ok && math.Abs(t-m.TimeSeconds) > 0.06 {
				agree = false
			}
			continue
		}
		markers = append(markers, store.AudioMarker{SectionIndex: i, TimeSeconds: proposed[i], Origin: store.SyncAuto})
	}
	// An estimate between two starts set by ear that doesn't fit between
	// them goes.
	var out []store.AudioMarker
	for k, m := range markers {
		if m.Origin != store.SyncSet {
			prev, next := math.Inf(-1), math.Inf(1)
			for j := k - 1; j >= 0; j-- {
				if markers[j].Origin == store.SyncSet {
					prev = markers[j].TimeSeconds
					break
				}
			}
			for j := k + 1; j < len(markers); j++ {
				if markers[j].Origin == store.SyncSet {
					next = markers[j].TimeSeconds
					break
				}
			}
			if m.TimeSeconds <= prev || m.TimeSeconds >= next {
				continue
			}
		}
		out = append(out, m)
	}
	switch {
	case pointsSet:
		return out, nil, len(set)
	case !agree:
		none := []store.GPBarSyncPoint{}
		return out, &none, len(set)
	}
	points := make([]store.GPBarSyncPoint, 0, len(p.BarPoints))
	for _, b := range p.BarPoints {
		points = append(points, store.GPBarSyncPoint{BarIndex: b.Bar, TimeSeconds: b.Time, Origin: store.SyncAuto})
	}
	return out, &points, len(set)
}

// reconcileBarPoints keeps a recording's per-bar points (which the
// player follows before its markers) in step with markers saved from the
// Sync sheet: estimated points the new markers contradict are dropped
// (the markers win), markers all confirmed by ear at the points' own
// times confirm the points too, and a sync cleared of every start drops
// them. It returns the points to save with the markers (nil: as they
// are). A Guitar Pro file's own sync is never touched.
func (s *Server) reconcileBarPoints(r *http.Request, meta *store.SongAudio, markers []store.AudioMarker) (*[]store.GPBarSyncPoint, error) {
	if meta.Category == store.AudioGP {
		return nil, nil
	}
	points, err := s.store.ListGPBarSyncPoints(r.Context(), meta.ID)
	if err != nil || len(points) == 0 {
		return nil, err
	}
	if len(markers) == 0 {
		return reconcile(points, nil, nil, nil), nil
	}
	// A marker agrees with the points when it's on its own section's
	// first bar, or where the analysis puts that section (the voice can
	// start it a bar before the chart's first bar of it): anything else is
	// a person's correction, and the markers win.
	_, _, _, body, err := s.store.VariantContent(r.Context(), meta.VariantID)
	if err != nil {
		return nil, err
	}
	sections := s.syncSectionsOf(body)
	start := map[int]int{}
	for _, sec := range sections {
		start[sec.Index] = sec.StartBar
	}
	proposed := map[int]float64{}
	if st, err := s.store.GetAnalysis(r.Context(), meta.ID); err == nil && st.Analysis != nil {
		for _, m := range songimport.ProposeAutoSync(st.Analysis, body, sections, chartalign.Hints{}).Markers {
			proposed[m.SectionIndex] = m.TimeSeconds
		}
	}
	return reconcile(points, markers, start, proposed), nil
}

// reconcile is reconcileBarPoints' decision: points as they are, the
// markers saved, each section's first bar (start) and where the analysis
// puts each section (proposed).
func reconcile(points []store.GPBarSyncPoint, markers []store.AudioMarker, start map[int]int, proposed map[int]float64) *[]store.GPBarSyncPoint {
	none := []store.GPBarSyncPoint{}
	if len(points) == 0 {
		return nil
	}
	// No start left: no sync, and the bars don't stay "set" on their own.
	if len(markers) == 0 {
		return &none
	}
	at := map[int]float64{}
	auto := false
	for _, p := range points {
		at[p.BarIndex] = p.TimeSeconds
		auto = auto || p.Origin == store.SyncAuto
	}
	near := func(a, b float64) bool { return math.Abs(a-b) <= 0.06 }
	agree, allSet := true, true
	for _, m := range markers {
		t, has := at[start[m.SectionIndex]]
		pt, hasP := proposed[m.SectionIndex]
		agree = agree && ((has && near(t, m.TimeSeconds)) || (hasP && near(pt, m.TimeSeconds)))
		allSet = allSet && m.Origin == store.SyncSet
	}
	switch {
	case !agree:
		return &none
	case allSet && auto:
		out := slices.Clone(points)
		for i := range out {
			out[i].Origin = store.SyncSet
		}
		return &out
	}
	return nil
}

// handlePutBarSyncPoints is PUT /api/songs/{id}/audio/{category}/sync-points:
// every bar's time in one request, for any recording (the Guitar Pro
// path, PUT …/audio/gp/sync-points, is the same with category gp).
func (s *Server) handlePutBarSyncPoints(w http.ResponseWriter, r *http.Request) {
	meta, ok := s.audioMetaFor(w, r)
	if !ok {
		return
	}
	var req struct {
		Points []struct {
			BarIndex    int     `json:"barIndex"`
			TimeSeconds float64 `json:"timeSeconds"`
		} `json:"points"`
		Origin string `json:"origin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Origin != "" && req.Origin != store.SyncAuto && req.Origin != store.SyncSet {
		httpError(w, http.StatusBadRequest, `origin must be "auto" or "set"`)
		return
	}
	points := make([]store.GPBarSyncPoint, len(req.Points))
	for i, p := range req.Points {
		points[i] = store.GPBarSyncPoint{BarIndex: p.BarIndex, TimeSeconds: p.TimeSeconds, Origin: req.Origin}
	}
	if err := s.store.SetGPBarSyncPoints(r.Context(), meta.ID, points); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(points)})
}
