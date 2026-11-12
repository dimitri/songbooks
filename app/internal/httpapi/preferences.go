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

// handleGetNotationPrefs/handleSetNotationPrefs: how this person reads
// guitar and bass parts (tab, staff or both) on every song — a personal
// choice, changed from the chart, the Scores tab or a Mixer lane.
func (s *Server) handleGetNotationPrefs(w http.ResponseWriter, r *http.Request) {
	prefs, _ := s.store.GetNotationPrefs(r.Context(), personFromContext(r).ID)
	writeJSON(w, http.StatusOK, prefs)
}

func (s *Server) handleSetNotationPrefs(w http.ResponseWriter, r *http.Request) {
	var prefs store.NotationPrefs
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&prefs); err != nil {
		httpError(w, http.StatusBadRequest, "invalid preferences body")
		return
	}
	if err := s.store.SetNotationPrefs(r.Context(), personFromContext(r).ID, prefs); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// notationPrefsFor is the viewer's notation choices for a page, the
// defaults for a visitor who isn't signed in.
func (s *Server) notationPrefsFor(r *http.Request) store.NotationPrefs {
	if p := personFromContext(r); p != nil {
		prefs, _ := s.store.GetNotationPrefs(r.Context(), p.ID)
		return prefs
	}
	return store.NotationPrefs{}.WithDefaults()
}
