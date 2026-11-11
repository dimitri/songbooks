package httpapi

import (
	"encoding/json"
	"net/http"

	"app/internal/tab"
)

// handleScoreRiff is POST /api/score/riff — decodes one "score[label]:"
// voice body (LilyPond-subset text or a GP-imported riff's JSON) into the
// Riff the tab editor edits. The editor renders and edits Riffs only, so
// a hand-written LilyPond voice comes through here first; it is stored
// as Riff JSON from its first edit on. A body that doesn't parse answers
// 400 with the parser's own message, for the editor to show.
func (s *Server) handleScoreRiff(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label string `json:"label"`
		Body  string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	riff, err := tab.ParseVoice(req.Label, req.Body)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"riff": riff})
}
