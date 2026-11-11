package httpapi

import (
	"net/http"
	"strconv"

	"app/internal/tab"
)

type mixerTrackJSON struct {
	Name  string `json:"name"`
	Drums bool   `json:"drums"`
}

// handleGetSectionMixerScore is GET /api/songs/{id}/sections/{index}/mixer
// — the Mixer view's own alphaTeX source: every named track this
// section's "score:" field carries (guitar, bass, drums, ...), combined
// into ONE multi-track document (tab.ToAlphaTexMulti) so AlphaSynth plays
// them on one shared clock and the view's own mute/solo buttons can
// isolate each — unlike every other alphaTab mount in this app, which
// renders one riff's notation on its own with no playback at all.
func (s *Server) handleGetSectionMixerScore(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	sectionIndex, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid section index")
		return
	}
	sg, _, err := s.store.LoadVariant(variantID)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such song")
		return
	}
	if sectionIndex < 0 || sectionIndex >= len(sg.Sections) {
		httpError(w, http.StatusNotFound, "no such section")
		return
	}
	riffs, _ := tab.LoadScores(sg)
	tracks := riffs[sectionIndex]
	if len(tracks) == 0 {
		httpError(w, http.StatusNotFound, "this section has no score to play")
		return
	}
	tempo, _ := strconv.Atoi(sg.Get("tempo"))
	alphaTex, meta := tab.ToAlphaTexMulti(tracks, tempo)
	out := make([]mixerTrackJSON, len(meta))
	for i, m := range meta {
		out[i] = mixerTrackJSON{Name: m.Name, Drums: m.Drums}
	}
	writeJSON(w, http.StatusOK, map[string]any{"alphaTex": alphaTex, "tracks": out})
}
