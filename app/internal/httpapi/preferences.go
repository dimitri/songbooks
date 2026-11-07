package httpapi

import (
	"encoding/json"
	"net/http"

	"app/internal/store"
)

// handleGetClickTrackPrefs/handleSetClickTrackPrefs back the click-track
// page's silent, per-person "remember my display settings" behavior —
// the client reads this once on load (falling back to its own
// localStorage/defaults if it's never been set) and writes it back,
// debounced, on every change, with no explicit save action of the
// person's own.
func (s *Server) handleGetClickTrackPrefs(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	prefs, err := s.store.GetClickTrackPrefs(r.Context(), person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (s *Server) handleSetClickTrackPrefs(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	var prefs store.ClickTrackPrefs
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&prefs); err != nil {
		httpError(w, http.StatusBadRequest, "invalid preferences body")
		return
	}
	if err := s.store.SetClickTrackPrefs(r.Context(), person.ID, prefs); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
