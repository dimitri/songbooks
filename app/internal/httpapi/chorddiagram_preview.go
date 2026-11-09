package httpapi

import (
	"encoding/json"
	"net/http"

	"app/internal/chorddiagram"
)

// handleChordDiagramPreview is POST /api/chord-diagrams/preview — renders
// one "[fret:]StringSpec" (the same grammar a "chord:" header line's
// second word uses, see chorddiagram.ParseSpec) to SVG, for the score
// editor's clickable fretboard widget. ParseSpec is lenient by design
// (best-effort on partial/malformed input), so this never errors — a
// chord being built one click at a time is normal, not a parse failure.
func (s *Server) handleChordDiagramPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Spec string `json:"spec"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	d := chorddiagram.ParseSpec(req.Spec)
	writeJSON(w, http.StatusOK, map[string]any{"svg": chorddiagram.RenderSVG(d, req.Name)})
}
