package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"app/internal/tab"
)

// handleGetSongScore is GET /api/songs/{id}/scores/{sectionIndex} — the
// stored LilyPond-subset source for one section's "score:" field, for
// the editor's score panel.
func (s *Server) handleGetSongScore(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	sectionIndex, err := strconv.Atoi(r.PathValue("sectionIndex"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid section index")
		return
	}
	sc, err := s.store.GetSongScore(r.Context(), variantID, sectionIndex)
	if err != nil {
		httpError(w, http.StatusNotFound, "no score for this section yet")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lySource": sc.LySource})
}

// handlePutSongScore is PUT /api/songs/{id}/scores/{sectionIndex} — save
// (or create) a section's score source. Validated against ParseScore
// before it's ever stored, so a save that would silently fail to render
// is rejected at save time instead — the same "catch it now, not on
// every later read" posture SetSongAudio's upload validation already
// has for the audio path.
func (s *Server) handlePutSongScore(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	sectionIndex, err := strconv.Atoi(r.PathValue("sectionIndex"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid section index")
		return
	}
	var req struct {
		LySource string `json:"lySource"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LySource == "" {
		httpError(w, http.StatusBadRequest, "missing lySource")
		return
	}
	if _, err := tab.ParseScore(req.LySource); err != nil {
		httpError(w, http.StatusBadRequest, "couldn't parse this as a LilyPond-subset score: "+err.Error())
		return
	}
	id, err := s.store.SetSongScore(r.Context(), variantID, sectionIndex, req.LySource, actorID(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// handlePreviewSongScore is POST /api/songs/{id}/scores/{sectionIndex}/preview
// — parses an UNSAVED lySource draft and returns the alphaTeX it would
// render as, for the score editor's live preview. Reuses tab.ToAlphaTex,
// the exact same encoder attachTabRiffs uses for a saved score, so the
// editor preview can never drift from what Save would actually produce.
// Chord labels are intentionally omitted here (nil): the editor is
// working on one section's note-level source in isolation, with no bar
// alignment against that section's own grid: chords in scope.
func (s *Server) handlePreviewSongScore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LySource string `json:"lySource"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	riff, err := tab.ParseScore(req.LySource)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	clef := ""
	if len(riff.Tuning) <= 4 {
		clef = "F4"
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "alphaTex": tab.ToAlphaTex(riff, clef, nil)})
}

// handleDeleteSongScore removes one section's score source entirely —
// the section's "score:" field itself (in the .song body) is left
// alone; the user edits that like any other header text.
func (s *Server) handleDeleteSongScore(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	sectionIndex, err := strconv.Atoi(r.PathValue("sectionIndex"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid section index")
		return
	}
	if err := s.store.DeleteSongScore(r.Context(), variantID, sectionIndex); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
